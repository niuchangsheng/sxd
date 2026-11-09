// 本文件实现奇缘宝藏 (Mod_FortuneBox_Base, Module 808)。
//
// is_open(0) 未开放时直接退出；开放后读取面板并逐次 open_box(3) 开启免费宝箱。
// 不调用 buy_daily_gift(2) 等可能消耗元宝的购买动作。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxFortuneBoxOpen 单轮最多开启次数。
const maxFortuneBoxOpen = 5

// NewFortuneBoxRoutine 构造奇缘宝藏日常任务。
func NewFortuneBoxRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"fortune_box",
		"minigame",
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
					slog.Info("奇缘宝藏协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			openResp := call(protocol.BuildFortuneBoxOpenPacket(), protocol.ActionIDFortuneBoxOpen)
			if openResp == nil {
				slog.Info("奇缘宝藏开放状态无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "奇缘宝藏开放状态无回包")
			}
			open := int32(0)
			if len(openResp.Payload) >= 4 {
				open = int32(openResp.Payload[0])<<24 | int32(openResp.Payload[1])<<16 |
					int32(openResp.Payload[2])<<8 | int32(openResp.Payload[3])
			}
			if open == 0 {
				slog.Info("奇缘宝藏未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "奇缘宝藏未开放")
			}

			panelResp := call(protocol.BuildFortuneBoxPanelPacket(), protocol.ActionIDFortuneBoxPanel)
			if panelResp != nil {
				slog.Info("奇缘宝藏面板", "role_id", session.RoleID(), "payload_len", len(panelResp.Payload))
			}

			for boxID := int32(1); boxID <= maxFortuneBoxOpen; boxID++ {
				if ctx.Err() != nil {
					return nil
				}
				boxResp := call(protocol.BuildFortuneBoxOpenBoxPacket(boxID), protocol.ActionIDFortuneBoxOpenBox)
				if boxResp == nil {
					return nil
				}
				code, err := protocol.ParseActionFirstByte(boxResp.Payload, "奇缘宝藏开启")
				if err != nil {
					return nil
				}
				switch code {
				case protocol.FortuneBoxSuccess:
					slog.Info("奇缘宝藏开启成功", "role_id", session.RoleID(), "box_id", boxID)
				case protocol.FortuneBoxFuncNoOpen, protocol.FortuneBoxNoScore,
					protocol.FortuneBoxMaxChooseNum, protocol.FortuneBoxCanNotChoose,
					protocol.FortuneBoxWrongID, protocol.FortuneBoxFailed:
					slog.Info("奇缘宝藏开启结束", "role_id", session.RoleID(),
						"box_id", boxID, "result", code,
						"result_text", protocol.FortuneBoxResultText(code))
					return nil
				case protocol.FortuneBoxPackageFull:
					return scheduler.ErrBagFull
				case protocol.FortuneBoxNoIngot, protocol.FortuneBoxNoChargeIngot:
					slog.Info("奇缘宝藏返回元宝不足，停止以免误消费", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("奇缘宝藏开启未成功", "role_id", session.RoleID(),
						"box_id", boxID, "result", code,
						"result_text", protocol.FortuneBoxResultText(code))
					return nil
				}
			}
			return nil
		},
	)
}
