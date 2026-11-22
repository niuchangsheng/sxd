// Package social 提供壶中界、仙盟、圣盟、好友祝福与帮派社交自动化任务。
//
// 本文件实现帮派祭神上香 (Mod_Faction_Base, Module 10) 自动化。
package social

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// FactionBlessingConfig 帮派祭神上香配置。
type FactionBlessingConfig struct {
	// OfferingID 上香供品编号 (1 白檀香免费 / 2 苏合香30元宝 / 3 天木香90元宝)
	OfferingID int32
	// AllowCover 当前剩余祝福次数高于本次上香收益时，是否仍允许覆盖
	AllowCover bool
}

// DefaultFactionBlessingConfig 返回默认帮派祭神配置 (免费白檀香，不主动覆盖更高祝福)。
func DefaultFactionBlessingConfig() FactionBlessingConfig {
	return FactionBlessingConfig{
		OfferingID: protocol.FactionOfferingWhiteSandalwood,
		AllowCover: false,
	}
}

// maxFactionIncenseTimes 单次调度的最大上香次数，防止服务端异常未扣减次数时死循环。
const maxFactionIncenseTimes = 100

// NewFactionBlessingRoutine 构造帮派祭神自动上香任务。
//
// 协议对齐 2026 客户端 Mod_Faction_Base:
//   - Action 22 faction_god_info 查询神像等级、今日上香次数与记录
//   - Action 24 get_blessing_count 查询当前祝福次数 (用于避免覆盖更高祝福)
//   - Action 23 incense 上香 (请求携带供品编号)
func NewFactionBlessingRoutine(cfgs ...FactionBlessingConfig) scheduler.ActivityRoutine {
	cfg := DefaultFactionBlessingConfig()
	if len(cfgs) > 0 {
		if cfgs[0].OfferingID > 0 {
			cfg.OfferingID = cfgs[0].OfferingID
		}
		cfg.AllowCover = cfgs[0].AllowCover
	}

	offering, ok := protocol.GetFactionOffering(cfg.OfferingID)
	if !ok {
		slog.Warn("帮派祭神供品编号无效，回退到免费白檀香", "offering_id", cfg.OfferingID)
		offering, _ = protocol.GetFactionOffering(protocol.FactionOfferingWhiteSandalwood)
	}

	return scheduler.NewBaseRoutine(
		"faction_blessing",
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
			if session.IsFactionFeatureUnavailable() {
				slog.Info("帮派模块对当前角色不可用，跳过帮派祭神", "role_id", session.RoleID())
				return nil
			}
			slog.Info("正在执行社交任务: 帮派祭神上香",
				"role_id", session.RoleID(),
				"offering_id", offering.ID,
				"offering_name", offering.Name,
			)

			// 1. 查询神像信息与今日剩余上香次数 (Mod 10, Act 22)
			infoPkt := protocol.BuildFactionGodInfoPacket(protocol.ActionIDTownEnter)
			infoCtx, infoCancel := context.WithTimeout(ctx, 8*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDFactionGodInfo)
			infoCancel()
			if infoErr != nil || infoResp == nil {
				if errors.Is(infoErr, context.DeadlineExceeded) {
					// 面板 (Act 22) 携带帮派上香成员列表，未加入帮派的角色上会被服务端静默丢弃。
					// 用免费白檀香 (Act 23，合法供品) 探测一次上香结果码以区分“未加入帮派”和偶发丢包：
					// MEMBER_NOT_EXIST / FACTION_NOT_EXIST 即确认无帮派；SUCCESS/COUNT_USED 说明帮派上下文正常。
					probeCtx, probeCancel := context.WithTimeout(ctx, 8*time.Second)
					probeResp, probeErr := session.Call(probeCtx,
						protocol.BuildFactionIncensePacket(protocol.FactionOfferingWhiteSandalwood),
						protocol.ActionIDFactionIncense)
					probeCancel()
					if probeErr == nil && probeResp != nil {
						if code, pErr := protocol.ParseFactionIncenseResult(probeResp.Payload); pErr == nil {
							switch code {
							case protocol.FactionIncenseResultMemberNotExist, protocol.FactionIncenseResultFactionNotExist:
								session.MarkFactionFeatureUnavailable()
								slog.Info("帮派祭神上香返回“无此帮派成员/帮派”，确认角色未加入帮派，跳过本轮帮派任务",
									"role_id", session.RoleID(), "result_code", code)
							case protocol.FactionIncenseResultSuccess:
								slog.Info("帮派祭神面板无回包，但免费白檀香上香成功", "role_id", session.RoleID())
							case protocol.FactionIncenseResultCountUsed:
								slog.Info("今日帮派祭神上香次数已用尽", "role_id", session.RoleID())
							default:
								slog.Info("帮派祭神面板无回包，上香探测返回未知结果码",
									"role_id", session.RoleID(), "result_code", code)
							}
							return nil
						}
					}
					// 双丢包不能直接判定角色未入帮派（可能只是网关瞬断），
					// 否则会把帮派模块永久禁用。改为冷却后重新探测，只有上香
					// 返回 MEMBER_NOT_EXIST / FACTION_NOT_EXIST 才标记不可用。
					slog.Info("帮派祭神面板与上香探测均无回包，可能为瞬断，稍后重试",
						"role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "帮派祭神面板与探测均无回包")
				}
				slog.Warn("查询帮派祭神信息失败", "role_id", session.RoleID(), "err", infoErr)
				return nil
			}

			info, err := protocol.ParseFactionGodInfoResult(infoResp.Payload)
			if err != nil {
				slog.Warn("解析帮派祭神信息失败", "role_id", session.RoleID(), "err", err)
				return nil
			}

			remain := info.RemainingIncense()
			slog.Info("帮派祭神当前状态",
				"role_id", session.RoleID(),
				"god_level", info.FactionGodLevel,
				"god_exp", info.FactionGodExp,
				"require_exp", info.RequireExp,
				"incense_count", info.IncenseCount,
				"total_count", info.TotalCount,
				"remain_count", remain,
				"is_cover", info.IsCover,
			)
			if remain <= 0 {
				slog.Info("今日帮派祭神上香次数已用尽", "role_id", session.RoleID())
				return nil
			}
			if remain > maxFactionIncenseTimes {
				remain = maxFactionIncenseTimes
			}

			// 2. 付费供品元宝充足性检查，避免上香到一半因元宝不足中断
			if offering.IngotCost > 0 {
				required := int64(offering.IngotCost) * int64(remain)
				ingots := session.GetPlayerState().Ingots
				if ingots < required {
					slog.Warn("元宝不足以完成帮派祭神上香，跳过本次执行",
						"role_id", session.RoleID(),
						"offering_id", offering.ID,
						"required_ingots", required,
						"current_ingots", ingots,
					)
					return nil
				}
			}

			prevAct := protocol.ActionIDFactionGodInfo

			// 3. 服务端提示会覆盖剩余祝福次数时，先确认当前祝福是否高于本次收益
			if info.IsCover == 1 && !cfg.AllowCover {
				countPkt := protocol.BuildFactionGetBlessingCountPacket(prevAct)
				countCtx, countCancel := context.WithTimeout(ctx, 8*time.Second)
				countResp, countErr := session.Call(countCtx, countPkt, protocol.ActionIDFactionGetBlessingCount)
				countCancel()
				if countErr != nil || countResp == nil {
					slog.Warn("查询帮派祭神祝福次数失败，继续按当前供品上香",
						"role_id", session.RoleID(), "err", countErr)
				} else if blessing, err := protocol.ParseFactionBlessingCountResult(countResp.Payload); err != nil {
					slog.Warn("解析帮派祭神祝福次数失败，继续按当前供品上香",
						"role_id", session.RoleID(), "err", err)
				} else {
					slog.Info("帮派祭神当前祝福次数",
						"role_id", session.RoleID(),
						"blessing_count", blessing.Count,
						"coin_add", blessing.CoinAdd,
					)
					if blessing.Count > offering.BlessingCount {
						slog.Warn("剩余祝福次数高于本次上香收益，跳过以避免覆盖；可在配置中设置 faction_blessing.allow_cover=true 强制上香",
							"role_id", session.RoleID(),
							"current_blessing", blessing.Count,
							"offering_blessing", offering.BlessingCount,
						)
						return nil
					}
				}
			}

			// 4. 循环上香直至今日次数用尽 (Mod 10, Act 23)
			incenses := 0
			for remain > 0 && incenses < maxFactionIncenseTimes {
				if err := ctx.Err(); err != nil {
					slog.Info("帮派祭神上香已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}

				incensePkt := protocol.BuildFactionIncensePacket(offering.ID)
				incenseCtx, incenseCancel := context.WithTimeout(ctx, 8*time.Second)
				incenseResp, err := session.Call(incenseCtx, incensePkt, protocol.ActionIDFactionIncense)
				incenseCancel()
				if err != nil || incenseResp == nil {
					slog.Info("帮派祭神上香未收到回包", "role_id", session.RoleID(), "err", err)
					return nil
				}

				result, err := protocol.ParseFactionIncenseResult(incenseResp.Payload)
				if err != nil {
					slog.Warn("解析帮派祭神上香回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if result != protocol.FactionIncenseResultSuccess {
					slog.Warn("帮派祭神上香被服务端拒绝",
						"role_id", session.RoleID(),
						"result", result,
						"message", factionIncenseResultText(result),
					)
					break
				}

				incenses++
				remain--
				slog.Info("帮派祭神上香成功",
					"role_id", session.RoleID(),
					"incenses", incenses,
					"remain_count", remain,
					"god_exp_gain", offering.GodExp,
					"fame_gain", offering.Fame,
					"blessing_gain", offering.BlessingCount,
				)

				if remain > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("帮派祭神上香完成", "role_id", session.RoleID(), "incenses", incenses, "remain_count", remain)
			return nil
		},
	)
}

// factionIncenseResultText 返回帮派祭神上香结果码的中文说明。
func factionIncenseResultText(result uint8) string {
	switch result {
	case protocol.FactionIncenseResultSuccess:
		return "成功"
	case protocol.FactionIncenseResultMemberNotExist:
		return "无此会员"
	case protocol.FactionIncenseResultFactionNotExist:
		return "帮派不存在"
	case protocol.FactionIncenseResultOfferingNotExist:
		return "香不存在"
	case protocol.FactionIncenseResultCountUsed:
		return "次数用完"
	case protocol.FactionIncenseResultInsufficientIngot:
		return "元宝不足"
	case protocol.FactionIncenseResultInsufficientSkill:
		return "阅历不足"
	default:
		return "未知结果"
	}
}
