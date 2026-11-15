// 本文件实现纪元秘藏·轮回图录任务奖励领取
// (Mod_EraLunHuiAlbum_Base, Module 882)。
//
// 抓包基准: /tmp/lunhuitulu.pcapng（2026-11-15，qq s0）：
//
//	882/0 面板 → 882/1 [tab 1..5] 任务列表
//	→ 对未领取任务逐个 882/2 [task_id]
//	→ 882/5 查询轮回周期状态
//
// 任务列表项为 [Int id, Int state, Int claimed]；claimed=1 表示已领取。
// 服务端对条件不满足的任务返回非 0 结果码，例程记录并继续下一项，不中断整批。
// 不消耗元宝。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// eraLunHuiTabs 轮回图录任务页签数量。
const eraLunHuiTabs = 5

// maxEraLunHuiClaims 单轮最多领取次数，防止异常循环。
const maxEraLunHuiClaims = 80

// NewEraLunHuiAlbumRoutine 构造轮回图录奖励领取任务。
func NewEraLunHuiAlbumRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"era_lunhui_album",
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
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				defer cCancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("轮回图录协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildEraLunHuiPanelPacket(), protocol.ActionIDEraLunHuiPanel)
			if panelResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "轮回图录面板无回包")
			}
			if code, err := protocol.ParseEraLunHuiResult(panelResp.Payload); err != nil {
				return scheduler.NewCooldown(30*time.Minute, "轮回图录面板解析失败")
			} else if code != 0 {
				slog.Info("轮回图录面板不可用", "role_id", session.RoleID(), "result", code)
				return nil
			}

			// 先探测页签红点/可领取状态；没有可领取礼包时不发任何领取请求。
			redResp := call(protocol.BuildEraLunHuiRedDotStatusPacket(),
				protocol.ActionIDEraLunHuiRedDotStatus)
			if redResp == nil {
				slog.Info("轮回图录可领取状态无回包，本轮跳过", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "轮回图录可领取状态无回包")
			}
			redDots, rErr := protocol.ParseEraLunHuiRedDotStatus(redResp.Payload)
			if rErr != nil {
				slog.Warn("解析轮回图录可领取状态失败，本轮跳过", "role_id", session.RoleID(), "err", rErr)
				return nil
			}
			anyClaimable := false
			for _, v := range redDots {
				if v {
					anyClaimable = true
					break
				}
			}
			if !anyClaimable {
				slog.Info("轮回图录暂无可领取礼包", "role_id", session.RoleID(), "red_dots", redDots)
				return nil
			}

			claimed := 0
			for tab := int32(1); tab <= eraLunHuiTabs; tab++ {
				if !redDots[tab] {
					continue
				}
				if claimed >= maxEraLunHuiClaims || ctx.Err() != nil {
					break
				}
				infoResp := call(protocol.BuildEraLunHuiTaskInfoPacket(tab),
					protocol.ActionIDEraLunHuiTaskInfo)
				if infoResp == nil {
					return scheduler.NewCooldown(10*time.Minute, "轮回图录任务列表无回包")
				}
				tasks, err := protocol.ParseEraLunHuiTasks(infoResp.Payload)
				if err != nil {
					slog.Warn("解析轮回图录任务列表失败", "role_id", session.RoleID(),
						"tab", tab, "err", err)
					continue
				}
				pending := 0
				for _, t := range tasks {
					if t.ID <= 0 || t.Claimed != 0 {
						continue
					}
					pending++
					if claimed >= maxEraLunHuiClaims {
						break
					}
					resp := call(protocol.BuildEraLunHuiGetTaskPacket(t.ID),
						protocol.ActionIDEraLunHuiGetTask)
					if resp == nil {
						return scheduler.NewCooldown(10*time.Minute, "轮回图录领取无回包")
					}
					code, cErr := protocol.ParseEraLunHuiResult(resp.Payload)
					if cErr != nil {
						continue
					}
					if code == 0 {
						claimed++
						slog.Info("轮回图录奖励领取成功", "role_id", session.RoleID(),
							"tab", tab, "task_id", t.ID, "state", t.State)
					} else {
						slog.Info("轮回图录奖励暂不可领", "role_id", session.RoleID(),
							"tab", tab, "task_id", t.ID, "state", t.State, "result", code)
					}
				}
				slog.Info("轮回图录页签检查完成", "role_id", session.RoleID(),
					"tab", tab, "tasks", len(tasks), "pending", pending)
			}

			slog.Info("轮回图录奖励巡检完成", "role_id", session.RoleID(), "claimed", claimed)
			return nil
		},
	)
}
