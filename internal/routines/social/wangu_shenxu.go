// 本文件实现万古神墟膜拜 (Mod_EternalGamesPresent_Base, Module 840)。
//
// 2026-11-21 抓包 (/tmp/wangushenxu.pcapng) 与实机验证:
//   - 主面板 840/0 在主服会话回包，载荷为 [UByte result][大神像数组][Long can_get_coin]；
//   - 对每个未膜拜且存在对象的神像调用 840/1 [Int type]，成功回包
//     [UByte result][Int worship_ingot]。worship_ingot 是给**被膜拜榜一**的元宝奖励：
//     抓包客户端自身就是榜一，故能看到 0/3 PLAYER_INGOT 按该值增加；普通膜拜者
//     (2026-11-21 s0 同尘实测) 膜拜他人成功但自身元宝不变。膜拜对膜拜者免费；
//   - 官方客户端在每次膜拜成功后重新拉取 840/0；实机连续快速膜拜第 3 个神像会被
//     服务端静默丢弃，因此这里同样在每次成功后刷新面板并注入抖动间隔；
//   - 840/2 get_award 领取的是被膜拜者累计的铜钱奖励 (can_get_coin)，
//     普通角色通常为 0，因此仅在 can_get_coin > 0 时调用。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// NewWanguShenxuRoutine 构造万古神墟膜拜任务。
func NewWanguShenxuRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	const routineID = "wangu_shenxu"
	return scheduler.NewBaseRoutine(
		routineID,
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
			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet) *protocol.Packet {
				resp, err := caller.Call(pkt, pkt.ActionID)
				if err != nil {
					slog.Info("万古神墟协议无回包",
						"role_id", session.RoleID(), "action", pkt.ActionID, "err", err)
					return nil
				}
				return resp
			}
			isSessionAged := func() bool {
				since := session.ConnectionSince()
				return !since.IsZero() && time.Since(since) > client.HealthySessionAge
			}

			// 1. 先查询主面板：若今日已全部膜拜且无可领铜钱，不平白重建健康会话。
			panelResp := call(protocol.BuildWanguShenxuMainInfoPacket())
			if panelResp == nil && caller.AbortErr() == nil && isSessionAged() {
				if err := session.EnsureFreshConnection(ctx, client.HealthySessionAge); err != nil {
					return err
				}
				panelResp = call(protocol.BuildWanguShenxuMainInfoPacket())
			}
			if panelResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				return scheduler.HandleSilentDropCtx(ctx, session, routineID, 3, time.Hour, "万古神墟面板无回包")
			}
			panel, err := protocol.ParseWanguShenxuPanel(panelResp.Payload)
			if err != nil {
				slog.Warn("解析万古神墟主面板失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "万古神墟主面板解析失败")
			}
			if panel.Result != protocol.WanguShenxuResultSuccess {
				slog.Info("万古神墟当前不可用", "role_id", session.RoleID(),
					"result", panel.Result, "result_text", protocol.WanguShenxuResultText(panel.Result))
				return scheduler.HandleSilentDropCtx(ctx, session, routineID, 3, time.Hour, "万古神墟未开放")
			}
			targets := make([]protocol.WanguShenxuPlayer, 0, len(panel.Players))
			for _, p := range panel.Players {
				// player_id=0 为空神像位，无法膜拜。
				if p.PlayerID == 0 || p.IsWorship {
					continue
				}
				targets = append(targets, p)
			}
			slog.Info("万古神墟面板", "role_id", session.RoleID(),
				"statues", len(panel.Players), "worshippable", len(targets),
				"can_get_coin", panel.CanGetCoin)
			if len(targets) == 0 && panel.CanGetCoin <= 0 {
				return nil
			}

			// 存在可膜拜目标或可领铜钱时，若会话已超窗再按需重建并重拉面板。
			if isSessionAged() {
				if err := session.EnsureFreshConnection(ctx, client.HealthySessionAge); err != nil {
					return err
				}
				_ = call(protocol.BuildWanguShenxuMainInfoPacket())
			}

			// 2. 逐个膜拜未膜拜的大神像 (膜拜免费；worship_ingot 归被膜拜榜一)。
			// 膜拜是场景写操作：前面战斗玩法污染上下文时会静默丢弃，
			// 探针实测先 ReturnToTown 后同一载荷立即成功（result=0）。
			if len(targets) > 0 && !session.ReturnToTown(ctx, 5*time.Second) {
				slog.Info("万古神墟膜拜前返回城镇失败，继续尝试", "role_id", session.RoleID())
			}
			worshipped, failed := 0, 0
			for i, target := range targets {
				resp := call(protocol.BuildWanguShenxuWorshipPacket(target.Type))
				if resp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					return scheduler.HandleSilentDropCtx(ctx, session, routineID, 3, time.Hour, "万古神墟膜拜无回包")
				}
				got, pErr := protocol.ParseWanguShenxuWorshipResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析万古神墟膜拜回包失败",
						"role_id", session.RoleID(), "type", target.Type, "err", pErr)
					failed++
					continue
				}
				switch got.Result {
				case protocol.WanguShenxuResultSuccess:
					worshipped++
					scheduler.ResetSilentDropsCtx(ctx, session, routineID)
					slog.Info("万古神墟膜拜成功",
						"role_id", session.RoleID(), "type", target.Type,
						"nickname", target.Nickname, "server", target.ServerName,
						"target_ingot", got.Ingot)
					if i < len(targets)-1 {
						if refresh := call(protocol.BuildWanguShenxuMainInfoPacket()); refresh != nil {
							if refreshed, rErr := protocol.ParseWanguShenxuPanel(refresh.Payload); rErr != nil {
								slog.Warn("刷新万古神墟面板失败", "role_id", session.RoleID(), "err", rErr)
							} else if refreshed.Result == protocol.WanguShenxuResultSuccess {
								panel = refreshed
							}
						} else if abort := caller.AbortErr(); abort != nil {
							return abort
						}
						if jitter != nil {
							_ = jitter.Wait(ctx)
						}
					}
				case protocol.WanguShenxuResultHasWorshipped:
					slog.Info("万古神墟该神像今日已膜拜",
						"role_id", session.RoleID(), "type", target.Type)
				case protocol.WanguShenxuResultFuncNotOpen:
					slog.Info("万古神墟功能未开放，稍后重试",
						"role_id", session.RoleID(), "type", target.Type)
					return scheduler.HandleSilentDropCtx(ctx, session, routineID, 3, time.Hour, "万古神墟功能未开放")
				default:
					failed++
					slog.Info("万古神墟膜拜未成功",
						"role_id", session.RoleID(), "type", target.Type,
						"result", got.Result, "result_text", protocol.WanguShenxuResultText(got.Result))
				}
			}

			// 3. 仅有可领取铜钱奖励 (被膜拜累计) 时才请求 840/2，避免 NOT_AWARD 噪声。
			if panel.CanGetCoin > 0 {
				resp := call(protocol.BuildWanguShenxuGetAwardPacket())
				if resp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					return scheduler.HandleSilentDropCtx(ctx, session, routineID, 3, time.Hour, "万古神墟领奖无回包")
				}
				got, pErr := protocol.ParseWanguShenxuAwardResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析万古神墟领奖回包失败", "role_id", session.RoleID(), "err", pErr)
				} else {
					switch got.Result {
					case protocol.WanguShenxuResultSuccess:
						scheduler.ResetSilentDropsCtx(ctx, session, routineID)
						slog.Info("万古神墟领取铜钱奖励成功",
							"role_id", session.RoleID(), "coin", got.Coin)
					case protocol.WanguShenxuResultNotAward:
						slog.Info("万古神墟暂无可领取奖励", "role_id", session.RoleID())
					default:
						slog.Info("万古神墟领奖未成功", "role_id", session.RoleID(),
							"result", got.Result, "result_text", protocol.WanguShenxuResultText(got.Result))
					}
				}
			}

			// 有可膜拜对象但全部失败时才进入丢包退避，避免每轮重复请求。
			if len(targets) > 0 && worshipped == 0 && failed == len(targets) {
				return scheduler.HandleSilentDropCtx(ctx, session, routineID, 3, time.Hour, "万古神墟膜拜全部失败")
			}
			return nil
		},
	)
}
