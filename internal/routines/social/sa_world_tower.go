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
			for _, towerID := range towerIDs {
				if ctx.Err() != nil || fights >= maxSaWorldTowerFights {
					return nil
				}
				fightResp := call(protocol.BuildSaWorldTowerFightPacket(towerID), protocol.ActionIDSaWorldTowerFight)
				if fightResp == nil {
					return nil
				}
				code, err := protocol.ParseActionFirstByte(fightResp.Payload, "全网幻魔塔挑战")
				if err != nil {
					return nil
				}
				switch code {
				case protocol.SaWorldTowerSuccess:
					fights++
					slog.Info("全网幻魔塔挑战成功", "role_id", session.RoleID(), "tower_id", towerID)
				case protocol.SaWorldTowerNoTime, protocol.SaWorldTowerCDTime,
					protocol.SaWorldTowerFuncNoOpen, protocol.SaWorldTowerErrorID,
					protocol.SaWorldTowerFailure, protocol.SaWorldTowerNoIngot:
					slog.Info("全网幻魔塔挑战结束", "role_id", session.RoleID(),
						"tower_id", towerID, "result", code,
						"result_text", protocol.SaWorldTowerResultText(code))
					return nil
				default:
					slog.Info("全网幻魔塔挑战未成功", "role_id", session.RoleID(),
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
