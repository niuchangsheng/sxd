// 本文件实现百炼乾坤一 (Mod_BaiLianQianKun_Base, Module 272) 与
// 百炼乾坤二 (Mod_BaiLianQianKunTwo_Base, Module 320) 日常自动化。
//
// 两个模块动作一致：get_info(0) 查询面板，one_key_challenge(2) 一键扫荡。
// 仅调用免费挑战入口，不涉及任何元宝购买。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewBaiLianRoutine 构造百炼乾坤一/二日常任务。
func NewBaiLianRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"bailian",
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
					slog.Info("百炼乾坤协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			for _, two := range []bool{false, true} {
				name := "百炼乾坤一"
				if two {
					name = "百炼乾坤二"
				}
				infoResp := call(protocol.BuildBaiLianInfoPacket(two), baiLianInfoAction(two))
				if infoResp == nil {
					continue
				}
				info, err := protocol.ParseBaiLianInfo(infoResp.Payload, two)
				if err != nil {
					slog.Warn(name+"面板解析失败", "role_id", session.RoleID(), "err", err)
					continue
				}
				slog.Info(name+"面板", "role_id", session.RoleID(),
					"level", info.Level, "fields", info.Fields, "aux1", info.Aux1)

				keyResp := call(protocol.BuildBaiLianOneKeyPacket(two), baiLianKeyAction(two))
				if keyResp == nil {
					continue
				}
				code, err := protocol.ParseActionFirstByte(keyResp.Payload, name)
				if err != nil {
					slog.Warn(name+"一键挑战回包解析失败", "role_id", session.RoleID(), "err", err)
					continue
				}
				switch {
				case !two && (code == protocol.BaiLianOneSuccess || code == protocol.BaiLianOneWin ||
					code == protocol.BaiLianOnePass || code == protocol.BaiLianOneFighting):
					slog.Info(name+"一键挑战已提交", "role_id", session.RoleID(), "result", code)
				case two && (code == protocol.BaiLianTwoSuccess || code == protocol.BaiLianTwoWin ||
					code == protocol.BaiLianTwoPass || code == protocol.BaiLianTwoFighting):
					slog.Info(name+"一键挑战已提交", "role_id", session.RoleID(), "result", code)
				case !two && code == protocol.BaiLianOneFailed:
					// 一键扫荡不可用时再尝试单次挑战，确认是否有可挑战关卡。
					logBaiLianFallbackChallenge(name, two, session, call)
				case two && (code == protocol.BaiLianTwoFailed || code == protocol.BaiLianTwoTimeCD):
					logBaiLianFallbackChallenge(name, two, session, call)
				case two && code == protocol.BaiLianTwoFuncUnopen:
					slog.Info(name+"功能未开放", "role_id", session.RoleID())
				default:
					slog.Info(name+"一键挑战未成功", "role_id", session.RoleID(), "result", code)
				}
			}
			return nil
		},
	)
}

// logBaiLianFallbackChallenge 在一键扫荡失败后尝试单次挑战一次。
func logBaiLianFallbackChallenge(name string, two bool, session *client.RoleSession, call func(*protocol.Packet, uint32) *protocol.Packet) {
	act := protocol.ActionIDBaiLianOneFight
	if two {
		act = protocol.ActionIDBaiLianTwoFight
	}
	resp := call(protocol.BuildBaiLianChallengePacket(two), act)
	if resp == nil {
		return
	}
	code, err := protocol.ParseActionFirstByte(resp.Payload, name+"单次挑战")
	if err != nil {
		return
	}
	slog.Info(name+"单次挑战结果", "role_id", session.RoleID(), "result", code,
		"one_key_unavailable", true)
}

func baiLianInfoAction(two bool) uint32 {
	if two {
		return protocol.ActionIDBaiLianTwoInfo
	}
	return protocol.ActionIDBaiLianOneInfo
}

func baiLianKeyAction(two bool) uint32 {
	if two {
		return protocol.ActionIDBaiLianTwoKey
	}
	return protocol.ActionIDBaiLianOneKey
}
