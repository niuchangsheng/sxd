// 本文件实现仙盟上香与仙盟魔神挑战
// (Mod_StUnionActivity_Base 179 / Mod_StUnionTask_Base 337)。
//
// 抓包基准: /tmp/xianmeng.pcapng（2026-11-15，qq s0）。
// 两个玩法均在**仙界跨服会话**执行，需先建立跨服会话。
//
//	上香: 179/10 面板 → 179/11 [offer_id=1]（铜钱档；禁止元宝档）
//	魔神: 337/0 面板 → 337/1 挑战，直到券不足/次数用完
//
// 严禁调用 337/3 buy_ticket 与任何元宝档上香。
package social

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// stUnionIncenseOfferID 上香档位（抓包中 1 为铜钱档）。
const stUnionIncenseOfferID int32 = 1

// maxStUnionGodFights 单轮最多魔神挑战次数（每周 3 次）。
const maxStUnionGodFights = 3

// maxStUnionTicketBuys 单轮最多购买挑战券次数（用户确认首次用道源购买）。
const maxStUnionTicketBuys = 2

// stUnionTicketBuyDisabled 一旦检测到购买消耗元宝，本进程内不再自动购买。
var stUnionTicketBuyDisabled atomic.Bool

// stUnionTicketIneffectiveDay 记录「买了挑战券仍然提示次数已用完」的日期
// (yyyy-mm-dd)。当天不再用道源购买，避免每天浪费道源。
var stUnionTicketIneffectiveDay atomic.Value // string

// openStUnionSession 建立仙界跨服会话；单测可替换为复用 mock 会话。
var openStUnionSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenCrossServerSession(ctx)
}

// NewStUnionRitualRoutine 构造仙盟上香 + 魔神挑战任务。
func NewStUnionRitualRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	const fightTimeout = 30 * time.Second
	return scheduler.NewBaseRoutine(
		"st_union_ritual",
		"social",
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
			cross, err := openStUnionSession(ctx, session)
			if err != nil {
				slog.Info("仙盟跨服会话建立失败，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙盟跨服会话不可用")
			}
			if cross != session {
				defer cross.Close()
			}
			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				resp, err := cross.CallWithPrevActResync(ctx, timeout, pkt, act)
				if err != nil {
					slog.Info("仙盟协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 仙盟上香（仅铜钱档）。抓包顺序：179/0 许愿树 → 179/10 神像 → 179/11 上香。
			_ = call(protocol.BuildStUnionTreeInfoPacket(), protocol.ActionIDStUnionTreeInfo, callTimeout)
			_ = call(protocol.BuildStUnionGodInfoPacket(), protocol.ActionIDStUnionGodInfo, callTimeout)
			incenseResp := call(protocol.BuildStUnionIncensePacket(stUnionIncenseOfferID),
				protocol.ActionIDStUnionGodIncense, callTimeout)
			if incenseResp == nil {
				// 上香（179/11）在当天已完成后会被服务端静默丢弃，但魔神挑战（337/1）
				// 仍可正常进行：2026-11-15 15:48/16:13/16:24/16:36 实测在上香无回包后
				// 挑战均成功。不能因为上香失败就放弃当天剩余挑战次数。
				slog.Info("仙盟上香无回包，继续尝试魔神挑战", "role_id", session.RoleID())
			} else {
				scheduler.ResetSilentDrops(session, "st_union_ritual")
				if res, pErr := protocol.ParseStUnionIncenseResult(incenseResp.Payload); pErr != nil {
					slog.Warn("仙盟上香回包解析失败", "role_id", session.RoleID(), "err", pErr)
				} else if res.Result == protocol.StUnionIncenseSuccess {
					slog.Info("仙盟上香成功", "role_id", session.RoleID(),
						"offer_id", stUnionIncenseOfferID, "a", res.A, "b", res.B, "c", res.C)
				} else {
					slog.Info("仙盟上香未成功", "role_id", session.RoleID(),
						"result", res.Result, "result_text", protocol.StUnionIncenseResultText(res.Result))
				}
			}

			// 2. 仙盟魔神挑战（券不足时可用道源购买；不消耗元宝）。
			if infoResp := call(protocol.BuildStUnionTaskInfoPacket(),
				protocol.ActionIDStUnionTaskInfo, callTimeout); infoResp == nil {
				slog.Info("仙盟魔神挑战面板无回包，稍后再试", "role_id", session.RoleID())
				return scheduler.HandleSilentDrop(session, "st_union_ritual", 3,
					time.Hour, "仙盟魔神挑战面板无回包")
			} else if info, iErr := protocol.ParseStUnionTaskInfo(infoResp.Payload); iErr == nil {
				slog.Info("仙盟魔神挑战面板", "role_id", session.RoleID(),
					"a", info.A, "b", info.B, "c", info.C, "d", info.D)
			}
			fights := 0
			buys := 0
			boughtThisRun := false
			today := time.Now().Format("2006-01-02")
			buyIneffectiveToday := func() bool {
				v, _ := stUnionTicketIneffectiveDay.Load().(string)
				return v == today
			}
			for fights < maxStUnionGodFights && ctx.Err() == nil {
				fightResp := call(protocol.BuildStUnionTaskFightPacket(),
					protocol.ActionIDStUnionTaskFight, fightTimeout)
				if fightResp == nil {
					return scheduler.HandleSilentDrop(session, "st_union_ritual", 3,
						time.Hour, "仙盟魔神挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "st_union_ritual")
				code, fErr := protocol.ParseStUnionResult(fightResp.Payload)
				if fErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "仙盟魔神挑战回包解析失败")
				}
				if code == protocol.StUnionTaskNoTicket && buys < maxStUnionTicketBuys &&
					!stUnionTicketBuyDisabled.Load() && !buyIneffectiveToday() {
					// 用户确认：挑战券首次用道源购买。仍做元宝差值保护：
					// 若观察到元宝减少，本进程内永久停止自动购买。
					before := cross.GetPlayerState().Ingots
					buyResp := call(protocol.BuildStUnionTaskBuyTicketPacket(),
						protocol.ActionIDStUnionTaskBuyTicket, callTimeout)
					after := cross.GetPlayerState().Ingots
					if buyResp == nil {
						slog.Info("仙盟购买挑战券无回包", "role_id", session.RoleID())
						return nil
					}
					buyCode, bErr := protocol.ParseStUnionResult(buyResp.Payload)
					if bErr != nil {
						return nil
					}
					slog.Info("仙盟购买挑战券", "role_id", session.RoleID(),
						"result", buyCode, "result_text", protocol.StUnionTaskResultText(buyCode),
						"ingots_before", before, "ingots_after", after)
					if buyCode != protocol.StUnionTaskSuccess {
						return nil
					}
					if before > 0 && after > 0 && after < before {
						stUnionTicketBuyDisabled.Store(true)
						slog.Error("仙盟挑战券购买消耗了元宝，已停止自动购买",
							"role_id", session.RoleID(), "ingots_before", before, "ingots_after", after)
						return nil
					}
					buys++
					boughtThisRun = true
					continue
				}
				if code != protocol.StUnionTaskSuccess {
					slog.Info("仙盟魔神挑战结束", "role_id", session.RoleID(),
						"fights", fights, "result", code,
						"result_text", protocol.StUnionTaskResultText(code))
					if boughtThisRun && code == protocol.StUnionTaskTimesUsed {
						// 刚用道源买了挑战券仍然次数已用完：当天不再购买。
						stUnionTicketIneffectiveDay.Store(today)
						slog.Info("购买挑战券后仍次数已用完，今日不再自动购买",
							"role_id", session.RoleID())
					}
					return nil
				}
				fights++
				slog.Info("仙盟魔神挑战成功", "role_id", session.RoleID(), "fights", fights)
			}
			slog.Info("仙盟魔神挑战本轮完成", "role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}
