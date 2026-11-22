// 本文件实现飞升灌注与飞升 (Mod_Levitate_Base, Module 357)。
//
// 抓包基准: /tmp/feisheng.pcapng（2026-11-15，qq s0）：
//
//	357/0 get_levitate_info [UByte type]
//	357/1 perfusion         [UByte type] → [UByte result, [Int,Int]]
//	357/2 levitate          [UByte type, Int is_use_shen_lian_zhu] → [UByte result, Int]
//
// 伙伴 type=1（官方 ROLE 页签），主角 type=0（MAINROLE，默认页签）。结果码 2=成功继续、3=已灌注完、4=功能未开放、5/7/8/9/10 等=停止。
// 不消耗元宝。
//
// 前置检查（用户要求）：
//   - 区分主角 (0) / 伙伴 (1)：优先伙伴页签，无面板时回退主角；
//   - 阶段经验未满时灌注经验 (357/1)；经验已满则不再灌注；
//   - 到达阶段 5（maxStep）且阶段经验已满、灵智已用完时，才是「阶段5已结束」，
//     此时检查飞升条件：材料与铜钱；
//   - 材料不足时允许勾选神炼石替代 (is_use_shen_lian_zhu=1)，铜钱不足则跳过；
//   - 面板 357/0 不可用时仍以 357/1 结果码为准（抓包证明官方客户端直接灌注）。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// maxLevitatePerfusions 单轮最多灌注次数。
const maxLevitatePerfusions = 20

// levitateExpFull 判断阶段经验是否已满（3425 的当前经验 >= 升阶所需）。
func levitateExpFull(info *protocol.LevitateInfo) bool {
	if info == nil {
		return false
	}
	for _, m := range info.Materials {
		if m.ItemID == protocol.LevitateMaterialExpItemID {
			return m.Need > 0 && m.Available >= m.Need
		}
	}
	return false
}

// levitatePerfusionSkipReason 返回不应继续灌注的原因；空串表示可以灌注。
//
// 灌注只消耗角色经验/经验丹，不消耗飞升材料与铜钱；材料与铜钱只在
// 357/2 飞升时结算（见 levitateAscendCheck）。
func levitatePerfusionSkipReason(info *protocol.LevitateInfo) string {
	if info == nil {
		return "无飞升信息"
	}
	if levitateExpFull(info) {
		return "阶段经验已满，等待升阶/飞升"
	}
	if info.PerfusionableExp <= 0 && len(info.ExpItems) == 0 {
		return "无可灌注经验且无灌注经验丹"
	}
	return ""
}

// levitateAscendCheck 返回飞升 (357/2) 的阻断原因与是否勾选神炼石替代。
// 空原因表示满足条件可以飞升。
func levitateAscendCheck(info *protocol.LevitateInfo) (string, bool) {
	if info == nil {
		return "无飞升信息", false
	}
	if info.Step < protocol.LevitateMaxStep {
		return fmt.Sprintf("阶段%d未到%d", info.Step, protocol.LevitateMaxStep), false
	}
	if !levitateExpFull(info) {
		return "阶段经验未满", false
	}
	if info.AvailableLingZhi > 0 {
		return fmt.Sprintf("仍有可用灵智 %d", info.AvailableLingZhi), false
	}
	var shortMaterials []string
	for _, m := range info.Materials {
		switch m.ItemID {
		case protocol.LevitateMaterialExpItemID:
			continue
		case protocol.LevitateMaterialCoinItemID:
			// 铜钱不能以神炼石替代。
			if m.Need > 0 && m.Available < m.Need {
				return fmt.Sprintf("铜钱不足(%d/%d)", m.Available, m.Need), false
			}
		default:
			if m.Need > 0 && m.Available < m.Need {
				shortMaterials = append(shortMaterials,
					fmt.Sprintf("item%d %d/%d", m.ItemID, m.Available, m.Need))
			}
		}
	}
	if len(shortMaterials) > 0 {
		if info.ShenLianShiNum <= 0 {
			return "材料不足且无神炼石: " + strings.Join(shortMaterials, ", "), false
		}
		return "", true
	}
	return "", false
}

// NewLevitatePerfusionRoutine 构造飞升灌注/飞升任务。
func NewLevitatePerfusionRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"levitate_perfusion",
		"daily",
		scheduler.ScheduleCron,
		// 与无量劫海同理：飞升面板/灌注在长会话后段更容易被服务端静默丢弃，
		// 登录后优先执行。
		scheduler.PhasePriority(scheduler.PhaseEarlyBattle, 40),
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.CallResync(pkt, act)
				if err != nil {
					slog.Info("飞升灌注协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err,
						"last_sent", fmt.Sprintf("0x%08X", session.LastActionID()),
						"recent_rx", fmt.Sprintf("%X", session.RecentRxActions()))
					return nil
				}
				return resp
			}
			fetchInfo := func(typ uint8) *protocol.LevitateInfo {
				resp := call(protocol.BuildLevitateInfoPacket(typ), protocol.ActionIDLevitateInfo)
				if resp == nil {
					return nil
				}
				info, err := protocol.ParseLevitateInfo(resp.Payload)
				if err != nil {
					slog.Warn("解析飞升信息失败", "role_id", session.RoleID(),
						"type", typ, "err", err)
					return nil
				}
				return info
			}
			logInfo := func(typ uint8, info *protocol.LevitateInfo) {
				slog.Info("飞升面板", "role_id", session.RoleID(), "type", typ,
					"step", info.Step, "max_step", protocol.LevitateMaxStep,
					"perfusionable_exp", info.PerfusionableExp,
					"avaliable_ling_zhi", info.AvailableLingZhi,
					"max_ling_zhi", info.MaxLingZhi,
					"materials", levitateMaterialDetail(info.Materials),
					"exp_items", levitateExpItemDetail(info.ExpItems),
					"shen_lian_shi", info.ShenLianShiNum)
			}

			// ascend 尝试 357/2 飞升；调用方必须先通过 levitateAscendCheck。
			ascend := func(typ uint8, useShenLianZhu bool) error {
				flag := int32(0)
				if useShenLianZhu {
					flag = 1
				}
				resp := call(protocol.BuildLevitatePacket(typ, flag), protocol.ActionIDLevitate)
				if resp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					return scheduler.HandleSilentDropCtx(ctx, session, "levitate_perfusion", 3,
						30*time.Minute, "飞升动作无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "levitate_perfusion")
				res, err := protocol.ParseLevitateActionResult(resp.Payload)
				if err != nil {
					return scheduler.NewCooldown(10*time.Minute, "飞升回包解析失败")
				}
				switch res.Result {
				case protocol.LevitateSuccess:
					slog.Info("飞升成功", "role_id", session.RoleID(), "type", typ,
						"use_shen_lian_zhu", useShenLianZhu, "value", res.Value)
					return nil
				case protocol.LevitateFuncNoOpen:
					slog.Info("飞升功能未开放，稍后重试", "role_id", session.RoleID(), "type", typ)
					return scheduler.NewCooldown(2*time.Hour, "飞升功能未开放")
				case protocol.LevitateNoEnoughMaterial, protocol.LevitateNoEnoughLingZhi:
					slog.Info("飞升材料或灵智不足，2 小时后再检查", "role_id", session.RoleID(),
						"type", typ, "use_shen_lian_zhu", useShenLianZhu,
						"result", res.Result, "result_text", protocol.LevitateResultText(res.Result))
					return scheduler.NewCooldown(2*time.Hour, "飞升材料/灵智不足")
				default:
					slog.Info("飞升未成功", "role_id", session.RoleID(), "type", typ,
						"result", res.Result, "result_text", protocol.LevitateResultText(res.Result))
					return nil
				}
			}
			// tryAscend 在 357/0 可用时按阶段/经验/灵智/材料检查后飞升。
			tryAscend := func(typ uint8, info *protocol.LevitateInfo) (bool, error) {
				if info == nil {
					return false, nil
				}
				reason, useShenLian := levitateAscendCheck(info)
				if reason != "" {
					slog.Info("飞升条件不满足", "role_id", session.RoleID(),
						"type", typ, "reason", reason)
					return false, nil
				}
				slog.Info("飞升条件满足，执行飞升", "role_id", session.RoleID(),
					"type", typ, "step", info.Step, "use_shen_lian_zhu", useShenLian)
				return true, ascend(typ, useShenLian)
			}

			// 优先伙伴页签（与历史自动化语义一致），面板不可用时回退主角。
			typ := uint8(protocol.LevitateTypeRole)
			resp := call(protocol.BuildLevitateInfoPacket(typ), protocol.ActionIDLevitateInfo)
			if resp == nil {
				slog.Info("飞升信息（伙伴页签）无回包，改试主角页签", "role_id", session.RoleID())
				typ = protocol.LevitateTypeMainRole
				resp = call(protocol.BuildLevitateInfoPacket(typ), protocol.ActionIDLevitateInfo)
			}

			if resp != nil {
				info, infoErr := protocol.ParseLevitateInfo(resp.Payload)
				if infoErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "飞升信息回包解析失败")
				}
				logInfo(typ, info)
				if reason := levitatePerfusionSkipReason(info); reason != "" {
					slog.Info("飞升灌注前置条件不满足，跳过", "role_id", session.RoleID(),
						"type", typ, "reason", reason)
					if _, err := tryAscend(typ, info); err != nil {
						return err
					}
					return nil
				}
			} else {
				// 面板不是业务前置：抓包证明官方客户端直接发 357/1 并按结果码
				// （2 成功 / 3 已灌注完 / 4 未开放…）决定后续，完全不依赖 357/0。
				// 面板被静默丢弃时仍尝试一次 357/1，以结果码为准。
				slog.Info("飞升信息面板无回包，直接尝试灌注一次（以结果码判断状态）",
					"role_id", session.RoleID())
			}
			// 注意：面板 357/0 回包成功不代表灌注动作 357/1 可用。早期实现在这里
			// ResetSilentDrops，导致「面板正常、灌注被静默丢弃」时连续丢包计数每轮
			// 都被清零，永远达不到熔断阈值。计数只在灌注请求真正得到回包后清零。

			perfused := 0
			for perfused < maxLevitatePerfusions && ctx.Err() == nil {
				perfResp := call(protocol.BuildLevitatePerfusionPacket(typ),
					protocol.ActionIDLevitatePerfusion)
				if perfResp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					return scheduler.HandleSilentDropCtx(ctx, session, "levitate_perfusion", 3,
						30*time.Minute, "飞升灌注无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "levitate_perfusion")
				res, err := protocol.ParseLevitatePerfusionResult(perfResp.Payload)
				if err != nil {
					return scheduler.NewCooldown(10*time.Minute, "飞升灌注回包解析失败")
				}
				switch res.Result {
				case protocol.LevitateSuccess:
					perfused++
					slog.Info("飞升灌注经验成功", "role_id", session.RoleID(), "type", typ,
						"count", perfused, "gains", res.Gains)
				case protocol.LevitateUsedUp, protocol.LevitateFull,
					protocol.LevitateMaxStepLimit, protocol.LevitateStepLimit2:
					slog.Info("飞升经验已灌注完/已满", "role_id", session.RoleID(), "type", typ,
						"count", perfused, "result", res.Result,
						"result_text", protocol.LevitateResultText(res.Result))
					// 经验刚灌注满时，同一轮内重新检查是否满足飞升条件。
					if info := fetchInfo(typ); info != nil {
						logInfo(typ, info)
						if _, err := tryAscend(typ, info); err != nil {
							return err
						}
					}
					return nil
				case protocol.LevitateFuncNoOpen:
					slog.Info("飞升功能未开放，稍后重试", "role_id", session.RoleID(), "type", typ)
					return scheduler.NewCooldown(2*time.Hour, "飞升功能未开放")
				case protocol.LevitateNoEnoughMaterial, protocol.LevitateNoEnoughLingZhi:
					// 材料/灵智不足：需要玩家补充资源，本日无法自行恢复。
					slog.Info("飞升材料或灵智不足，2 小时后再检查", "role_id", session.RoleID(),
						"type", typ, "count", perfused, "result", res.Result,
						"result_text", protocol.LevitateResultText(res.Result))
					return scheduler.NewCooldown(2*time.Hour, "飞升材料/灵智不足")
				default:
					slog.Info("飞升灌注结束", "role_id", session.RoleID(), "type", typ,
						"count", perfused, "result", res.Result,
						"result_text", protocol.LevitateResultText(res.Result))
					return nil
				}
			}
			slog.Info("飞升灌注本轮完成", "role_id", session.RoleID(), "type", typ,
				"count", perfused)
			return nil
		},
	)
}

func levitateMaterialDetail(materials []protocol.LevitateMaterial) string {
	var b strings.Builder
	for i, m := range materials {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d:%d/%d", m.ItemID, m.Available, m.Need)
	}
	return b.String()
}

func levitateExpItemDetail(items []protocol.LevitateExpItem) string {
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d:%d", it.ItemID, it.Num)
	}
	return b.String()
}
