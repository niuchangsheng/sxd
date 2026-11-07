// 本文件实现仙界虚空/大神榜膜拜 (Mod_GodRankRank_Base, Module 500)。
//
// 每日一次免费膜拜；HAS_WORSHIPED 视为已完成。当前仅接入 500/1 worship，
// 652 Mod_StVoid 的 like 需要玩家 ID，待探针确认后再接入。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewVoidWorshipRoutine 构造虚空膜拜任务。
func NewVoidWorshipRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"void_worship",
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
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("虚空协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 优先走仙界虚空 652：世界 PK 榜取玩家 ID → like 膜拜。
			if rankResp := call(protocol.BuildStVoidWorldPkRankPacket(), protocol.ActionIDStVoidWorldPkRank); rankResp != nil {
				if ids, err := protocol.ParseStVoidRankPlayerIDs(rankResp.Payload); err == nil && len(ids) > 0 {
					slog.Info("仙界虚空排行榜", "role_id", session.RoleID(), "players", len(ids))
					attempts := 0
					for _, id := range ids {
						if attempts >= 3 {
							break
						}
						attempts++
						resp := call(protocol.BuildStVoidLikePacket(id), protocol.ActionIDStVoidLike)
						if resp == nil {
							break
						}
						code, pErr := protocol.ParseStVoidResult(resp.Payload)
						if pErr != nil {
							break
						}
						switch code {
						case protocol.StVoidResultSuccess:
							slog.Info("仙界虚空膜拜成功", "role_id", session.RoleID(), "player_id", id)
							return nil
						case protocol.StVoidResultIsLiked:
							slog.Info("仙界虚空该玩家已膜拜，尝试下一位",
								"role_id", session.RoleID(), "player_id", id)
							continue
						case protocol.StVoidResultPackFull:
							return scheduler.ErrBagFull
						default:
							slog.Info("仙界虚空膜拜未成功",
								"role_id", session.RoleID(), "player_id", id, "result", code)
						}
						break
					}
				}
			}

			// 2. 兜底：大神榜 500/1 worship。
			cCtx, cancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship)
			cancel()
			if err != nil || resp == nil {
				slog.Info("大神榜膜拜无回包，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			code, pErr := protocol.ParseGodRankResult(resp.Payload)
			if pErr != nil {
				slog.Warn("解析大神榜膜拜回包失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			switch code {
			case protocol.GodRankResultSuccess:
				slog.Info("大神榜膜拜成功", "role_id", session.RoleID())
			case protocol.GodRankResultHasWorshiped:
				slog.Info("大神榜今日已膜拜", "role_id", session.RoleID())
			default:
				slog.Info("大神榜膜拜未成功", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.GodRankResultText(code))
			}
			return nil
		},
	)
}
