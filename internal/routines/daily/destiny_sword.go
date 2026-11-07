// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现「飞剑」一键淬炼 (Mod_DestinySword_Base, Module 842) 自动化。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxDestinySwordTemperIntents 单轮最多处理的剑意数量。
const maxDestinySwordTemperIntents = 20

// destinySwordMaxIntentLevel 剑意最高等级 (官方 DestinySwordData.INTENT_MAX_LEVEL)。
const destinySwordMaxIntentLevel int32 = 8

// NewDestinySwordRoutine 构造飞剑日常任务。
//
// 官方 com.views.destiny_sword.DestinySwordOneClickTemperView 的免费强化闭环：
//  1. panel_info 读取角色飞剑、剑意碎片数量；
//  2. 对每把飞剑上已装备的剑意调用 one_click_temper_panel，读取所需材料；
//  3. 剑意碎片足够时调用 one_click_temper 一键淬炼。
//
// 一键淬炼只消耗剑意碎片/剑意材料，不消耗元宝；碎片不足时直接跳过。
func NewDestinySwordRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"destiny_sword",
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
			slog.Info("正在执行日常任务: 飞剑一键淬炼", "role_id", session.RoleID())

			// 先查功能开放列表；未开放时不再发送会被静默丢弃的飞剑面板请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDDestinySword)
			if fErr != nil {
				slog.Info("飞剑功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("飞剑功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDDestinySword)
				return nil
			}

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			resp, err := call(protocol.BuildDestinySwordPanelPacket(), protocol.ActionIDDestinySwordPanel)
			if err != nil || resp == nil {
				slog.Info("飞剑功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseDestinySwordPanelResult(resp.Payload)
			if err != nil {
				slog.Info("解析飞剑面板失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("飞剑面板",
				"role_id", session.RoleID(),
				"roles", len(panel.Roles),
				"item_num", panel.ItemNum,
				"jianyi_scrap", panel.JianyiScrap,
			)
			if panel.JianyiScrap <= 0 {
				slog.Info("飞剑剑意碎片不足，跳过淬炼", "role_id", session.RoleID())
				return nil
			}

			processed := 0
			for _, role := range panel.Roles {
				for _, sword := range role.EquipList {
					for _, intent := range sword.Intents {
						if processed >= maxDestinySwordTemperIntents {
							slog.Info("飞剑淬炼达到单轮上限", "role_id", session.RoleID())
							return nil
						}
						if intent.PlayerIntentID <= 0 || intent.IntentLevel >= destinySwordMaxIntentLevel {
							continue
						}
						if err := ctx.Err(); err != nil {
							return nil
						}
						targetLevel := intent.IntentLevel + 1
						pResp, pErr := call(
							protocol.BuildDestinySwordOneClickTemperPanelPacket(intent.PlayerIntentID, targetLevel),
							protocol.ActionIDDestinySwordOneClickTemperPanel,
						)
						if pErr != nil || pResp == nil {
							slog.Info("查询飞剑淬炼消耗无回包", "role_id", session.RoleID(), "err", pErr)
							return nil
						}
						cost, cErr := protocol.ParseDestinySwordTemperPanelResult(pResp.Payload)
						if cErr != nil {
							slog.Warn("解析飞剑淬炼消耗失败", "role_id", session.RoleID(), "err", cErr)
							return nil
						}
						switch cost.Result {
						case protocol.DestinySwordResultSuccess:
							// 继续
						case protocol.DestinySwordResultNotEnoughIntent, protocol.DestinySwordResultNoIntent:
							slog.Info("飞剑剑意材料不足，停止淬炼", "role_id", session.RoleID())
							return nil
						case protocol.DestinySwordResultMaxIntentLevel:
							continue
						case protocol.DestinySwordResultFuncNotOpen:
							slog.Info("飞剑功能未开放", "role_id", session.RoleID())
							return nil
						default:
							slog.Info("飞剑淬炼消耗查询未成功", "role_id", session.RoleID(), "result", cost.Result)
							return nil
						}
						if cost.NeedJianyiScrapNum > panel.JianyiScrap {
							slog.Info("飞剑剑意碎片不足以淬炼该剑意",
								"role_id", session.RoleID(),
								"need", cost.NeedJianyiScrapNum,
								"have", panel.JianyiScrap,
							)
							continue
						}
						tResp, tErr := call(
							protocol.BuildDestinySwordOneClickTemperPacket(intent.PlayerIntentID, targetLevel),
							protocol.ActionIDDestinySwordOneClickTemper,
						)
						if tErr != nil || tResp == nil {
							slog.Info("执行飞剑淬炼无回包", "role_id", session.RoleID(), "err", tErr)
							return nil
						}
						code, pErr := protocol.ParseDestinySwordActionCode(tResp.Payload)
						if pErr != nil {
							slog.Warn("解析飞剑淬炼回包失败", "role_id", session.RoleID(), "err", pErr)
							return nil
						}
						switch code {
						case protocol.DestinySwordResultSuccess:
							processed++
							panel.JianyiScrap -= cost.NeedJianyiScrapNum
							slog.Info("飞剑一键淬炼成功",
								"role_id", session.RoleID(),
								"sword_id", sword.SwordID,
								"intent_id", intent.IntentID,
								"target_level", targetLevel,
								"remaining_scrap", panel.JianyiScrap,
							)
						case protocol.DestinySwordResultNotEnoughItem,
							protocol.DestinySwordResultNotEnoughNum,
							protocol.DestinySwordResultNotEnoughIntent:
							slog.Info("飞剑淬炼材料不足，停止本轮", "role_id", session.RoleID(), "result", code)
							return nil
						case protocol.DestinySwordResultMaxIntentLevel:
							continue
						case protocol.DestinySwordResultFuncNotOpen:
							slog.Info("飞剑功能未开放", "role_id", session.RoleID())
							return nil
						default:
							slog.Info("飞剑一键淬炼未成功", "role_id", session.RoleID(), "result", code)
							return nil
						}
						select {
						case <-ctx.Done():
							return nil
						case <-time.After(200 * time.Millisecond):
						}
					}
				}
			}
			slog.Info("飞剑巡检完成", "role_id", session.RoleID(), "tempered", processed)
			return nil
		},
	)
}
