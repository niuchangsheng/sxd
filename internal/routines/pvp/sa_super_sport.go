// Package pvp 提供竞技场、天梯、劫镖、世界 Boss 等对抗玩法自动化。
//
// 本文件实现圣域竞技场挑战 (Mod_SaSuperSport_Base, Module 384)。
package pvp

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// maxSaSuperSportChallenges 每日挑战次数上限 (面板 LeftTimes 为权威值)。
const maxSaSuperSportChallenges = 10

// saSuperSportNoResponse 标记圣域竞技场请求被服务端静默丢弃（无回包）。
var saSuperSportNoResponse = errors.New("圣域竞技场请求无回包")

// NewSaSuperSportRoutine 构造圣域竞技场挑战任务。
//
// 官方流程 (抓包 /tmp/shengyujingjichang.pcapng, 圣域连接 81.70.56.121:8220):
//
//	384/0 race_info → 384/3 challage [Byte slot][Int battle_id] → 再次 384/0
//
// race_info 返回今日剩余挑战次数与 5 名对手；挑战成功会消耗 1 次并返回战报。
// 自动化选择战力最低的对手，避免无意义失败；次数用尽/圣魂不足/上限时按
// 结果码结束，不消耗元宝、不购买次数。
func NewSaSuperSportRoutine() scheduler.ActivityRoutine {
	const (
		infoTimeout  = 12 * time.Second
		fightTimeout = 30 * time.Second
	)
	return scheduler.NewBaseRoutine(
		"sa_super_sport",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行对抗任务: 圣域竞技场挑战", "role_id", session.RoleID())

			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea)
			if fErr != nil {
				slog.Info("圣域竞技场功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("圣域竞技场功能未开放，跳过", "role_id", session.RoleID(),
					"function_id", protocol.FunctionIDSaintArea)
				return nil
			}

			// 圣域开启窗口：关闭/维护时直接按窗口结束时间唤醒。
			sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
			statusResp, sErr := session.Call(sCtx, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus)
			sCancel()
			if sErr != nil || statusResp == nil {
				slog.Info("查询圣域状态无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", sErr)
				return scheduler.HandleSilentDropCtx(ctx, session, "sa_super_sport", 3,
					30*time.Minute, "圣域状态查询无回包")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "sa_super_sport")
			status, sErr := protocol.ParseSaintAreaStatusResult(statusResp.Payload)
			if sErr != nil {
				slog.Warn("解析圣域状态失败，跳过圣域竞技场", "role_id", session.RoleID(), "err", sErr)
				return scheduler.NewCooldown(10*time.Minute, "圣域状态解析失败")
			}
			if status.Status != protocol.SaintAreaStatusOpen {
				until := scheduler.SaintAreaReopenTime(status, time.Now())
				slog.Info("圣域当前未开启，按关闭窗口唤醒",
					"role_id", session.RoleID(), "status", status.Status,
					"windows", len(status.CloseTimes), "wake_at", until)
				return scheduler.NewCooldownUntil(until, "圣域未开启")
			}

			// 竞技场协议必须走圣域独立长连接。
			sa, oErr := openSaintAreaSession(ctx, session)
			if oErr != nil || sa == nil {
				slog.Info("建立圣域会话失败，稍后重试", "role_id", session.RoleID(), "err", oErr)
				return scheduler.NewCooldown(10*time.Minute, "建立圣域会话失败")
			}
			if sa != session {
				defer sa.Close()
			}

			handleNoResponse := func(where string, err error) error {
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				slog.Info("圣域竞技场协议无回包，按连续丢包退避",
					"role_id", session.RoleID(), "where", where, "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, "sa_super_sport", 3,
					30*time.Minute, "圣域竞技场"+where+"无回包")
			}

			fetchInfo := func() (*protocol.SaSuperSportRaceInfo, error) {
				resp, err := kit.CallResync(ctx, sa, protocol.BuildSaSuperSportRaceInfoPacket(),
					protocol.ActionIDSaSuperSportRaceInfo, infoTimeout)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, saSuperSportNoResponse
				}
				return protocol.ParseSaSuperSportRaceInfo(resp.Payload)
			}

			info, err := fetchInfo()
			if err != nil {
				if errors.Is(err, saSuperSportNoResponse) || errors.Is(err, context.DeadlineExceeded) {
					return handleNoResponse("面板", err)
				}
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				slog.Warn("圣域竞技场面板解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "圣域竞技场面板解析失败")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "sa_super_sport")
			slog.Info("圣域竞技场面板", "role_id", session.RoleID(),
				"race_state", info.RaceState, "left_times", info.LeftTimes,
				"score", info.Score, "rank", info.Rank, "extra", info.Extra,
				"opponents", len(info.Opponents))
			if info.LeftTimes <= 0 {
				slog.Info("圣域竞技场今日挑战次数已用尽", "role_id", session.RoleID())
				return nil
			}

			challenges := 0
			for challenges < maxSaSuperSportChallenges && info.LeftTimes > 0 && ctx.Err() == nil {
				opponent, ok := pickSaSuperSportOpponent(info.Opponents)
				if !ok {
					slog.Info("圣域竞技场暂无可挑战对手", "role_id", session.RoleID())
					return nil
				}
				resp, cErr := kit.CallResync(ctx, sa,
					protocol.BuildSaSuperSportChallengePacket(opponent.Slot, opponent.BattleID),
					protocol.ActionIDSaSuperSportChallenge, fightTimeout)
				if cErr != nil || resp == nil {
					return handleNoResponse("挑战", cErr)
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "sa_super_sport")
				code, pErr := protocol.ParseSaSuperSportChallengeResult(resp.Payload)
				if pErr != nil {
					slog.Warn("圣域竞技场挑战回包解析失败", "role_id", session.RoleID(), "err", pErr)
					return scheduler.NewCooldown(10*time.Minute, "圣域竞技场挑战回包解析失败")
				}
				switch code {
				case protocol.SaSuperSportResultSuccess:
					challenges++
					slog.Info("圣域竞技场挑战成功", "role_id", session.RoleID(),
						"nickname", opponent.Nickname, "slot", opponent.Slot,
						"war_power", opponent.WarPower, "challenges", challenges)
				case protocol.SaSuperSportResultNoChallengeTimes,
					protocol.SaSuperSportResultTimesLimit:
					slog.Info("圣域竞技场挑战次数已用尽", "role_id", session.RoleID(),
						"result", code, "result_text", protocol.SaSuperSportResultText(code),
						"challenges", challenges)
					return nil
				case protocol.SaSuperSportResultNoShengHun:
					slog.Info("圣域竞技场圣魂不足，稍后再试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "圣域竞技场圣魂不足")
				case protocol.SaSuperSportResultPackFull:
					return scheduler.ErrBagFull
				case protocol.SaSuperSportResultError:
					slog.Info("圣域竞技场挑战失败", "role_id", session.RoleID(),
						"nickname", opponent.Nickname, "slot", opponent.Slot,
						"war_power", opponent.WarPower)
					return scheduler.NewCooldown(10*time.Minute, "圣域竞技场挑战失败")
				default:
					slog.Info("圣域竞技场挑战未成功", "role_id", session.RoleID(),
						"result", code, "result_text", protocol.SaSuperSportResultText(code))
					return nil
				}

				// 客户端挑战后立即刷新面板；LeftTimes 为权威剩余次数。
				refreshed, fErr := fetchInfo()
				if fErr != nil {
					if errors.Is(fErr, saSuperSportNoResponse) || errors.Is(fErr, context.DeadlineExceeded) {
						return handleNoResponse("刷新面板", fErr)
					}
					slog.Warn("刷新圣域竞技场面板失败", "role_id", session.RoleID(), "err", fErr)
					return nil
				}
				info = refreshed
				slog.Info("圣域竞技场剩余次数", "role_id", session.RoleID(),
					"left_times", info.LeftTimes, "challenges", challenges)
				if !kit.Sleep(ctx, 500*time.Millisecond) {
					return ctx.Err()
				}
			}
			slog.Info("圣域竞技场本轮完成", "role_id", session.RoleID(),
				"challenges", challenges, "left_times", info.LeftTimes)
			return nil
		},
	)
}

// pickSaSuperSportOpponent 选择战力最低的对手 (同战力取靠前 slot)。
func pickSaSuperSportOpponent(opponents []protocol.SaSuperSportOpponent) (protocol.SaSuperSportOpponent, bool) {
	candidates := make([]protocol.SaSuperSportOpponent, 0, len(opponents))
	for _, o := range opponents {
		if o.BattleID == 0 || o.Slot == 0 {
			continue
		}
		candidates = append(candidates, o)
	}
	if len(candidates) == 0 {
		return protocol.SaSuperSportOpponent{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].WarPower != candidates[j].WarPower {
			return candidates[i].WarPower < candidates[j].WarPower
		}
		return candidates[i].Slot < candidates[j].Slot
	})
	return candidates[0], true
}
