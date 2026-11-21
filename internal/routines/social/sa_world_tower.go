// 本文件实现全网幻魔塔 (Mod_SaWorldTower_Base, Module 521) 日常挑战。
//
// get_open_status(0) 未开放时直接退出；开放后读取面板并按塔编号 fight(3)。
// 永不调用 buy_fight_count(6) 元宝购买次数。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxSaWorldTowerFights 单轮最多挑战次数。
const maxSaWorldTowerFights = 5

// saWorldTowerFailureThreshold 连续多少座幻魔塔战斗失败后结束本轮。
// 按用户要求「挑战副本需 N 次失败才退出」，不再第一次失败就放弃。
const saWorldTowerFailureThreshold = 3

// NewSaWorldTowerRoutine 构造全网幻魔塔挑战任务。
func NewSaWorldTowerRoutine() scheduler.ActivityRoutine {
	const callTimeout = 15 * time.Second
	return scheduler.NewBaseRoutine(
		"sa_world_tower",
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
					slog.Info("全网幻魔塔协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			openResp := call(protocol.BuildSaWorldTowerOpenPacket(), protocol.ActionIDSaWorldTowerOpen)
			if openResp == nil {
				slog.Info("全网幻魔塔开放状态无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "全网幻魔塔开放状态无回包")
			}
			if len(openResp.Payload) >= 4 {
				open := int32(openResp.Payload[0])<<24 | int32(openResp.Payload[1])<<16 |
					int32(openResp.Payload[2])<<8 | int32(openResp.Payload[3])
				// 521/0 返回 1=开放、0=未开放（实测 0 时 fight 返回 FUNCTION_NO_OPEN）。
				if open == 0 {
					slog.Info("全网幻魔塔未开放，稍后重试", "role_id", session.RoleID(), "status", open)
					return scheduler.NewCooldown(2*time.Hour, "全网幻魔塔未开放")
				}
			}

			towerIDs := []int32{1, 2, 3, 4, 5}
			if panelResp := call(protocol.BuildSaWorldTowerPanelPacket(), protocol.ActionIDSaWorldTowerPanel); panelResp != nil {
				slog.Info("全网幻魔塔面板", "role_id", session.RoleID(), "payload_len", len(panelResp.Payload))
				if ids, pErr := protocol.ParseSaWorldTowerTowerIDs(panelResp.Payload); pErr == nil && len(ids) > 0 {
					towerIDs = ids
				} else if pErr != nil {
					slog.Warn("全网幻魔塔面板解析失败", "role_id", session.RoleID(), "err", pErr)
				}
			}

			fights := 0
			failures := 0
			for _, towerID := range towerIDs {
				if ctx.Err() != nil || fights >= maxSaWorldTowerFights {
					return nil
				}
				fightResp := call(protocol.BuildSaWorldTowerFightPacket(towerID), protocol.ActionIDSaWorldTowerFight)
				if fightResp == nil {
					// 静默丢包时不能 return nil，剩余塔要等到下一个 cron 时段。
					slog.Info("全网幻魔塔挑战无回包，按连续丢包退避",
						"role_id", session.RoleID(), "tower_id", towerID)
					return scheduler.HandleSilentDrop(session, "sa_world_tower", 3,
						30*time.Minute, "全网幻魔塔挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "sa_world_tower")
				code, err := protocol.ParseActionFirstByte(fightResp.Payload, "全网幻魔塔挑战")
				if err != nil {
					slog.Warn("全网幻魔塔挑战回包解析失败", "role_id", session.RoleID(),
						"tower_id", towerID, "err", err)
					return scheduler.NewCooldown(10*time.Minute, "全网幻魔塔挑战回包解析失败")
				}
				switch code {
				case protocol.SaWorldTowerSuccess:
					fights++
					failures = 0
					slog.Info("全网幻魔塔挑战成功", "role_id", session.RoleID(), "tower_id", towerID)
				case protocol.SaWorldTowerCDTime:
					// 服务端未在回包中给出剩余秒数，30 分钟后重试。
					slog.Info("全网幻魔塔挑战冷却中，30 分钟后再试",
						"role_id", session.RoleID(), "tower_id", towerID)
					return scheduler.NewCooldown(30*time.Minute, "全网幻魔塔挑战冷却中")
				case protocol.SaWorldTowerNoTime:
					slog.Info("全网幻魔塔今日挑战次数已用尽", "role_id", session.RoleID(), "tower_id", towerID)
					return scheduler.ErrAttemptsExhausted
				case protocol.SaWorldTowerFuncNoOpen:
					slog.Info("全网幻魔塔未开放，2 小时后再试",
						"role_id", session.RoleID(), "tower_id", towerID)
					return scheduler.NewCooldown(2*time.Hour, "全网幻魔塔未开放")
				case protocol.SaWorldTowerErrorID:
					slog.Info("全网幻魔塔关卡 ID 无效，尝试下一座",
						"role_id", session.RoleID(), "tower_id", towerID)
					continue
				case protocol.SaWorldTowerFailure:
					failures++
					if failures >= saWorldTowerFailureThreshold {
						slog.Info("全网幻魔塔连续多座战斗失败，结束本轮",
							"role_id", session.RoleID(), "failures", failures)
						return nil
					}
					slog.Info("全网幻魔塔挑战失败，继续尝试下一座",
						"role_id", session.RoleID(), "tower_id", towerID, "failures", failures)
					continue
				case protocol.SaWorldTowerNoIngot:
					slog.Info("全网幻魔塔需要元宝，自动化不主动消耗",
						"role_id", session.RoleID(), "tower_id", towerID)
					return nil
				default:
					slog.Info("全网幻魔塔挑战未成功，结束本轮", "role_id", session.RoleID(),
						"tower_id", towerID, "result", code,
						"result_text", protocol.SaWorldTowerResultText(code))
					return nil
				}
			}
			slog.Info("全网幻魔塔本轮结束", "role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}
