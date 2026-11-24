// Package pvp 提供竞技场、天梯、劫镖、世界 Boss 等对抗玩法自动化。
//
// 本文件实现圣域擂台赛自动抢占席位 (Mod_SaArena_Base, Module 298)。
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

// maxSaArenaRobs 单轮调度的最大抢占次数。
const maxSaArenaRobs = 10

// errSaArenaNoResponse 标记圣域擂台赛请求被服务端静默丢弃（无回包）。
var errSaArenaNoResponse = errors.New("圣域擂台赛请求无回包")

// openSaintAreaSession 建立圣域独立长连接。抽成变量以便单测注入。
var openSaintAreaSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenSaintAreaSession(ctx)
}

// NewHolyArenaRoutine 构造圣域擂台赛自动抢占任务。
//
// 官方 com.datas.SaArenaData: get_info 返回 5 个擂台席位与剩余可抢占次数。
// 空席位 (sa_player_id=0) 可直接免费占据；有人的席位则发起抢占战斗。
// 自动化优先选择空席位，其次选择战力最低的对手，避免无意义消耗。
//
// 官方 SaArenaThroneView 将 get_info / rob_the_seat 全部路由到圣域连接
// (DataBase.SOCKETSAINTAREA)。线上实测：主服调用 get_info 返回全空席位，
// rob_the_seat 固定返回 ACTIVITY_CLOSE(3)；必须改用圣域会话才能看到真实席位。
//
// 圣域擂台赛不消耗元宝，只消耗每日免费抢占次数。
func NewHolyArenaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"holy_arena",
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
			slog.Info("正在执行对抗任务: 圣域擂台赛", "role_id", session.RoleID())

			// 圣域擂台赛隶属圣域玩法，先查功能开放列表；未开放时不发送业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea)
			if fErr != nil {
				slog.Info("圣域擂台赛功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("圣域擂台赛功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDSaintArea)
				return nil
			}

			// 圣域开启状态：关闭/维护时直接跳过，避免建立无意义的透传连接。
			sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
			statusResp, sErr := session.Call(sCtx, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus)
			sCancel()
			if sErr != nil || statusResp == nil {
				// 状态查询被静默丢弃时不能 return nil，否则要等到下一个 cron 时段。
				slog.Info("查询圣域状态无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", sErr)
				return scheduler.HandleSilentDropCtx(ctx, session, "holy_arena", 3,
					30*time.Minute, "圣域状态查询无回包")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "holy_arena")
			status, sErr := protocol.ParseSaintAreaStatusResult(statusResp.Payload)
			if sErr != nil {
				slog.Warn("解析圣域状态失败，跳过圣域擂台赛", "role_id", session.RoleID(), "err", sErr)
				return scheduler.NewCooldown(10*time.Minute, "圣域状态解析失败")
			}
			if status.Status != protocol.SaintAreaStatusOpen {
				// 圣域状态回包带关闭窗口 [start, end]。若当前落在某个窗口内，
				// 精确到窗口结束再唤醒；否则保留 1 小时探测兜底。
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

			saCaller := kit.NewCaller(ctx, sa, 10*time.Second)

			fetchInfo := func() (*protocol.SaArenaInfo, error) {
				resp, err := saCaller.Call(protocol.BuildSaArenaGetInfoPacket(), protocol.ActionIDSaArenaGetInfo)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, errSaArenaNoResponse
				}
				return protocol.ParseSaArenaGetInfoResult(resp.Payload)
			}

			info, err := fetchInfo()
			if err != nil {
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				if errors.Is(err, errSaArenaNoResponse) || errors.Is(err, context.DeadlineExceeded) {
					slog.Info("圣域擂台赛面板无回包，按连续丢包退避",
						"role_id", session.RoleID(), "err", err)
					return scheduler.HandleSilentDropCtx(ctx, session, "holy_arena", 3,
						30*time.Minute, "圣域擂台赛面板无回包")
				}
				slog.Warn("圣域擂台赛面板解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "圣域擂台赛面板解析失败")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "holy_arena")
			slog.Info("圣域擂台赛面板",
				"role_id", session.RoleID(),
				"seats", len(info.Seats),
				"available_count", info.AvailableCount,
			)
			if info.AvailableCount <= 0 {
				slog.Info("圣域擂台赛今日抢占次数已用尽", "role_id", session.RoleID())
				return nil
			}

			robs := 0
			for robs < int(info.AvailableCount) && robs < maxSaArenaRobs {
				robs++
				if err := ctx.Err(); err != nil {
					return nil
				}
				seat, ok := pickSaArenaSeat(info.Seats, session.PlayerID())
				if !ok {
					slog.Info("圣域擂台赛没有可抢占的席位", "role_id", session.RoleID())
					return nil
				}
				resp, rErr := saCaller.Call(protocol.BuildSaArenaRobSeatPacket(seat.Seq), protocol.ActionIDSaArenaRobSeat)
				if rErr != nil || resp == nil {
					if abort, ok := kit.IsAbortErr(rErr); ok {
						return abort
					}
					// 抢占被静默丢弃时剩余可用次数不能丢：按连续丢包退避后重试。
					slog.Info("圣域擂台赛抢占无回包，按连续丢包退避",
						"role_id", session.RoleID(), "seq", seat.Seq, "err", rErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "holy_arena", 3,
						10*time.Minute, "圣域擂台赛抢占无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "holy_arena")
				res, pErr := protocol.ParseSaArenaRobResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析圣域擂台赛抢占回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch res.Result {
				case protocol.SaArenaResultSuccess, protocol.SaArenaResultSuccessFree:
					slog.Info("圣域擂台赛抢占成功",
						"role_id", session.RoleID(),
						"seq", seat.Seq,
						"target", seat.Nickname,
						"award_score", res.AwardScore,
						"win_player_id", res.WinPlayerID,
					)
				case protocol.SaArenaResultOnSeat:
					slog.Info("圣域擂台赛已在席位上", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultActivityClose:
					slog.Info("圣域擂台赛活动已关闭", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultCDTime:
					// 回包未带冷却秒数，按 10 分钟低频复查，避免把剩余
					// 免费席位拖到下一次 Cron。
					slog.Info("圣域擂台赛抢占冷却中，10 分钟后再试", "role_id", session.RoleID())
					return scheduler.NewCooldown(10*time.Minute, "圣域擂台赛抢占冷却")
				case protocol.SaArenaResultCountUsedUp:
					slog.Info("圣域擂台赛抢占次数已用尽", "role_id", session.RoleID())
					return nil
				case protocol.SaArenaResultMyself:
					slog.Debug("跳过自己的席位", "role_id", session.RoleID(), "seq", seat.Seq)
				case protocol.SaArenaResultPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("圣域擂台赛抢占未成功", "role_id", session.RoleID(), "seq", seat.Seq, "result", res.Result)
					return nil
				}

				refreshed, fErr := fetchInfo()
				if fErr != nil {
					slog.Warn("刷新圣域擂台赛面板失败", "role_id", session.RoleID(), "err", fErr)
					return nil
				}
				info = refreshed
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(300 * time.Millisecond):
				}
			}
			slog.Info("圣域擂台赛巡检完成", "role_id", session.RoleID(), "robs", robs)
			return nil
		},
	)
}

// pickSaArenaSeat 选择一个可抢占的席位：优先空席位，其次战力最低的对手。
func pickSaArenaSeat(seats []protocol.SaArenaSeat, selfPlayerID int32) (protocol.SaArenaSeat, bool) {
	empty := make([]protocol.SaArenaSeat, 0)
	occupied := make([]protocol.SaArenaSeat, 0)
	for _, s := range seats {
		if s.SaPlayerID == selfPlayerID {
			continue
		}
		if s.SaPlayerID == 0 {
			empty = append(empty, s)
			continue
		}
		occupied = append(occupied, s)
	}
	if len(empty) > 0 {
		sort.SliceStable(empty, func(i, j int) bool { return empty[i].Seq < empty[j].Seq })
		return empty[0], true
	}
	if len(occupied) == 0 {
		return protocol.SaArenaSeat{}, false
	}
	sort.SliceStable(occupied, func(i, j int) bool { return occupied[i].WarPower < occupied[j].WarPower })
	return occupied[0], true
}
