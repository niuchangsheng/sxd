// 本文件实现仙界虚空/大神榜膜拜 (Mod_StVoid_Base 652 + Mod_GodRankRank_Base 500)。
//
// 652 的正确 Socket 流程是:
//
//	open_panel (652/1) -> 返回 st_info_list 与 like_list
//	like       (652/2) -> 对 st_info_list 中尚未膜拜的 st_uid 点赞，领取奖励
//
// 注意: 652/4 get_world_pk_rank 与 652/3 get_war_power_rank 在官方客户端中
// 由 StVoidRankView 通过 HTTP (server-web/route.php?m=svs|svp) 获取排行榜，
// 不是游戏 Socket 动作；向主服长连接发送 652/4 必然无回包。
// 652 不可用或没有可膜拜对象时，回退到 500/1 大神榜膜拜。
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

			// 1. 优先走仙界虚空 652：open_panel 取 st_info_list → like 膜拜。
			if openResp := call(protocol.BuildStVoidOpenPacket(), protocol.ActionIDStVoidOpen); openResp != nil {
				panel, pErr := protocol.ParseStVoidPanelResult(openResp.Payload)
				if pErr != nil {
					slog.Warn("解析仙界虚空面板失败", "role_id", session.RoleID(), "err", pErr)
				} else if panel.Result != protocol.StVoidResultSuccess {
					slog.Info("仙界虚空面板未开放", "role_id", session.RoleID(),
						"result", panel.Result, "result_text", protocol.StVoidResultText(panel.Result))
				} else {
					slog.Info("仙界虚空面板", "role_id", session.RoleID(),
						"st_name", panel.StName, "members", len(panel.Members),
						"liked", len(panel.LikedUIDs))
					liked := make(map[int32]bool, len(panel.LikedUIDs))
					for _, uid := range panel.LikedUIDs {
						liked[uid] = true
					}
					attempts := 0
					for _, member := range panel.Members {
						if attempts >= 3 {
							break
						}
						if member.StUID == 0 || liked[member.StUID] {
							continue
						}
						attempts++
						resp := call(protocol.BuildStVoidLikePacket(member.StUID), protocol.ActionIDStVoidLike)
						if resp == nil {
							break
						}
						got, lErr := protocol.ParseStVoidLikeResult(resp.Payload)
						if lErr != nil {
							slog.Warn("解析仙界虚空膜拜回包失败", "role_id", session.RoleID(), "err", lErr)
							break
						}
						switch got.Result {
						case protocol.StVoidResultSuccess:
							slog.Info("仙界虚空膜拜成功", "role_id", session.RoleID(),
								"st_uid", member.StUID, "nickname", member.Nickname,
								"awards", got.Awards)
							return nil
						case protocol.StVoidResultIsLiked:
							slog.Info("仙界虚空该玩家已膜拜，尝试下一位",
								"role_id", session.RoleID(), "st_uid", member.StUID)
							continue
						case protocol.StVoidResultPackFull:
							return scheduler.ErrBagFull
						default:
							slog.Info("仙界虚空膜拜未成功",
								"role_id", session.RoleID(), "st_uid", member.StUID,
								"result", got.Result, "result_text", protocol.StVoidResultText(got.Result))
						}
					}
				}
			} else {
				slog.Info("仙界虚空主面板无回包，回退大神榜膜拜", "role_id", session.RoleID())
			}

			// 2. 兜底：大神榜 500/1 worship。
			cCtx, cancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship)
			cancel()
			if err != nil || resp == nil {
				slog.Info("大神榜膜拜无回包，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "大神榜膜拜无回包")
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
