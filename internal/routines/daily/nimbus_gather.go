// 本文件实现每日灵脉采集 (Mod_Nimbus_Base, Module 104)。
//
// 2026-11-27 官方抓包 (/tmp/lingmai.pcapng, s1) 实测：
//   - 104/0 nimbus_info 返回 [Int32 nimbus][Int16 remain][Int16 gain]，
//     抓包时 remain=30，即每日 30 次木葫芦；
//   - 104/1 gather_nimbus 请求 [Uint8 gourdType=0][Uint32 prevAct]，
//     回包 [Int32 result][8B 状态][Int16 remain][Int16 gain][4B 尾随]；
//     30 次 result=0，remain 从 29 递减到 0；
//   - 客户端在每次采集之间穿插 52/0 assistant info 刷新活动活跃度
//     （服务端另有 52/2 complete_number 推送），活跃度由日常任务例程统一领取。
//
// 「功能开启/终结」判定：
//   - 104/0 无回包 → 功能未开放或已终结（终结礼包由 online_gift 的
//     Mod 127 流程领取），按连续 3 次静默丢包挂起 6 小时；
//   - 面板 remain<=0 → 今日次数已用尽，等待次日重置；
//   - 采集返回非 0 结果码 → 按连续失败计数，3 次后挂起 6 小时，避免死循环。
package daily

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// nimbusCallTimeout 灵脉单请求超时。
	nimbusCallTimeout = 8 * time.Second
	// maxNimbusGatherPerRound 单轮最多采集次数（服务端每日 30 次，留出余量）。
	maxNimbusGatherPerRound = 30
	// nimbusGatherPace 采集间隔，避免高频请求触发服务端限流。
	nimbusGatherPace = 250 * time.Millisecond
	// nimbusFailureThreshold 连续失败阈值（无回包或业务失败均计数）。
	nimbusFailureThreshold = 3
	// nimbusRetryInterval 单次失败后的短重试间隔；连续 3 次后由
	// scheduler 的静默丢包熔断升级到 6 小时。
	nimbusRetryInterval = 10 * time.Minute
)

// NewNimbusGatherRoutine 构造每日灵脉采集任务。
func NewNimbusGatherRoutine() scheduler.ActivityRoutine {
	const routineID = "nimbus_gather"
	return scheduler.NewBaseRoutine(
		routineID,
		"daily",
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
			slog.Info("正在执行日常任务: 每日灵脉采集", "role_id", session.RoleID())

			caller := kit.NewCaller(ctx, session, nimbusCallTimeout)

			// 1. 面板：确认功能开启状态与今日剩余次数。
			infoResp, err := caller.Call(protocol.BuildNimbusInfoPacket(), protocol.ActionIDNimbusInfo)
			if err != nil || infoResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				if session.State() != client.StateActive {
					return client.ErrNotConnected
				}
				slog.Info("灵脉面板无回包（功能未开放或已终结）",
					"role_id", session.RoleID(), "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, routineID,
					nimbusFailureThreshold, nimbusRetryInterval, "灵脉面板无回包")
			}
			info, pErr := protocol.ParseNimbusInfo(infoResp.Payload)
			if pErr != nil {
				slog.Warn("解析灵脉面板失败", "role_id", session.RoleID(),
					"err", pErr, "payload_hex", hex.EncodeToString(infoResp.Payload))
				return scheduler.NewCooldown(30*time.Minute, "灵脉面板解析失败")
			}
			slog.Info("灵脉面板", "role_id", session.RoleID(),
				"nimbus", info.Nimbus, "remain", info.Remain, "gain", info.Gain)
			if info.Remain <= 0 {
				slog.Info("灵脉今日采集次数已用尽，等待次日重置",
					"role_id", session.RoleID(), "nimbus", info.Nimbus)
				return scheduler.NewCooldownUntil(scheduler.NextDailyReset(time.Now()),
					"灵脉今日采集次数已用尽")
			}

			// 2. 采集前复位城镇场景：灵脉采集属于场景写操作，副本/活动残留
			//    上下文会被服务端静默丢弃（2026-11-28 00:07 首次采集无回包）。
			if !session.ReturnToTown(ctx, 2*time.Second) {
				slog.Info("灵脉采集前返回城镇失败，继续尝试采集", "role_id", session.RoleID())
			}

			// 3. 逐次采集木葫芦，直到可用次数耗尽或达到本轮上限。
			gathered := 0
			for i := 0; i < int(info.Remain) && i < maxNimbusGatherPerRound; i++ {
				if ctx.Err() != nil {
					return nil
				}
				if i > 0 && !kit.Sleep(ctx, nimbusGatherPace) {
					return nil
				}
				// 尾链取「最近收到的包」：与剧情副本 4/2 一致，服务端按自己最后
				// 发出的包校验锚点；服务端推送（如 0/79）插入时，lastSent 锚点会失配。
				gatherPkt := protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDNimbusGather,
					[]byte{protocol.NimbusGourdWood}, session.LastRxActionID())
				if gathered == 0 {
					slog.Info("灵脉采集请求锚点", "role_id", session.RoleID(),
						"anchor", fmt.Sprintf("0x%08X", session.LastRxActionID()),
						"payload_hex", hex.EncodeToString(gatherPkt.Payload))
				}
				resp, err := caller.Call(gatherPkt, protocol.ActionIDNimbusGather)
				if err != nil || resp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					if session.State() != client.StateActive {
						return client.ErrNotConnected
					}
					slog.Info("灵脉采集无回包，中止本轮",
						"role_id", session.RoleID(), "gathered", gathered, "err", err)
					return scheduler.HandleSilentDropCtx(ctx, session, routineID,
						nimbusFailureThreshold, nimbusRetryInterval, "灵脉采集无回包")
				}
				res, rErr := protocol.ParseNimbusGatherResult(resp.Payload)
				if rErr != nil {
					slog.Warn("解析灵脉采集回包失败", "role_id", session.RoleID(),
						"gathered", gathered, "err", rErr,
						"payload_hex", hex.EncodeToString(resp.Payload))
					return scheduler.NewCooldown(30*time.Minute, "灵脉采集回包解析失败")
				}
				if res.Result != 0 {
					// 非 0：功能终结 / 次数不足 / 条件不满足等，按连续失败退避。
					slog.Info("灵脉采集未成功",
						"role_id", session.RoleID(),
						"gathered", gathered,
						"result", res.Result,
						"remain", res.Remain,
						"payload_hex", hex.EncodeToString(res.Raw))
					return scheduler.HandleSilentDropCtx(ctx, session, routineID,
						nimbusFailureThreshold, nimbusRetryInterval, "灵脉采集业务失败")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, routineID)
				gathered++
				slog.Info("灵脉采集成功", "role_id", session.RoleID(),
					"gathered", gathered, "gain", res.Gain, "remain", res.Remain)
				if res.Remain <= 0 {
					slog.Info("灵脉今日采集次数已用尽", "role_id", session.RoleID(),
						"gathered", gathered, "nimbus", info.Nimbus)
					return nil
				}
			}
			slog.Info("灵脉采集本轮完成", "role_id", session.RoleID(), "gathered", gathered)
			return nil
		},
	)
}
