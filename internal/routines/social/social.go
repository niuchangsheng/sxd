// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-10-08
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// JieYuanBoxConfig 结缘宝箱开启配置。
type JieYuanBoxConfig struct {
	// MaxBatchSize 每批次最多开启数量 (按指令每批最多10个，默认10)
	MaxBatchSize int
}

// DefaultJieYuanBoxConfig 返回默认结缘宝箱配置。
func DefaultJieYuanBoxConfig() JieYuanBoxConfig {
	return JieYuanBoxConfig{
		MaxBatchSize: 10,
	}
}

// NewJieYuanBoxRoutine 构造结缘宝箱批量开启任务 (Feature 2: 结缘宝箱，除了传说，其他都开)。
// 每批次最多开启 10 个，严格排除传说结缘宝箱 (3942)。
func NewJieYuanBoxRoutine(cfgs ...JieYuanBoxConfig) scheduler.ActivityRoutine {
	cfg := DefaultJieYuanBoxConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	maxBatch := cfg.MaxBatchSize
	if maxBatch <= 0 || maxBatch > 10 {
		maxBatch = 10
	}

	return scheduler.NewBaseRoutine(
		"jieyuan_box",
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
			slog.Info("正在执行结缘任务: 结缘宝箱自动开启 (每批最多10个，严格排除传说宝箱)",
				"role_id", session.RoleID(),
				"max_batch_size", maxBatch,
			)

			// 1. 查询背包持有的结缘宝箱列表 (Mod 232, Act 30)
			queryPkt := protocol.BuildJieYuanBoxListPacket(protocol.ActionIDTownEnter)
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(callCtx, queryPkt, protocol.ActionIDJieYuanBoxList)
			cancel()

			if err != nil || resp == nil {
				slog.Warn("查询结缘宝箱列表超时或失败", "err", err)
				return nil
			}

			boxes, err := protocol.ParseJieYuanBoxListResult(resp.Payload)
			if err != nil {
				slog.Warn("解析结缘宝箱列表失败", "err", err)
				return nil
			}

			// 2. 严格遵循规则: 除了传说 (3942)，其他都开
			openable := protocol.FilterOpenableJieYuanBoxes(boxes)
			if len(openable) == 0 {
				slog.Info("无待开启的非传说结缘宝箱", "total_kinds", len(boxes))
				return nil
			}

			var totalOpened int
			prevAct := protocol.ActionIDJieYuanBoxList

			for _, box := range openable {
				remaining := box.Count
				maxBatchesPerItem := 50 // 单个道具最大批次上限，防止无限循环
				batchCount := 0

				for remaining > 0 && batchCount < maxBatchesPerItem {
					batchCount++
					openCount := int32(maxBatch)
					if remaining < openCount {
						openCount = remaining
					}

					// 2.1 开箱前置握手 (Mod 528, Act 1)
					prepPkt := protocol.BuildJieYuanBoxOpenContextPacket(prevAct)
					pCtx, pCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
					_, _ = session.Call(pCtx, prepPkt, protocol.ActionIDJieYuanBoxOpenContext)
					pCancel()

					// 2.2 发送批量开启指令 (Mod 232, Act 34: 每批最多10个)
					openPkt := protocol.BuildJieYuanBoxOpenPacket(box.ItemID, openCount, protocol.ActionIDJieYuanBoxOpenContext)
					oCtx, oCancel := context.WithTimeout(ctx, 2*time.Second)
					openResp, oErr := session.Call(oCtx, openPkt, protocol.ActionIDJieYuanBoxOpen)
					oCancel()

					prevAct = protocol.ActionIDJieYuanBoxOpen
					if oErr == nil && openResp != nil {
						totalOpened += int(openCount)
						remaining -= openCount
					} else {
						break
					}

					time.Sleep(50 * time.Millisecond)
				}

				slog.Info("结缘宝箱批次处理完成",
					"item_id", box.ItemID,
					"opened", box.Count-remaining,
					"legendary_skipped", true,
				)
			}

			slog.Info("结缘宝箱批量开启流程完成", "role_id", session.RoleID(), "total_opened", totalOpened)
			return nil
		},
	)
}

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
			slog.Info("正在执行社交任务: 帮派祭神上香",
				"role_id", session.RoleID(),
				"offering_id", offering.ID,
				"offering_name", offering.Name,
			)

			// 1. 查询神像信息与今日剩余上香次数 (Mod 10, Act 22)
			infoPkt := protocol.BuildFactionGodInfoPacket(protocol.ActionIDTownEnter)
			infoCtx, infoCancel := context.WithTimeout(ctx, 3*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDFactionGodInfo)
			infoCancel()
			if infoErr != nil || infoResp == nil {
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
				countCtx, countCancel := context.WithTimeout(ctx, 3*time.Second)
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
				incenseCtx, incenseCancel := context.WithTimeout(ctx, 3*time.Second)
				incenseResp, err := session.Call(incenseCtx, incensePkt, protocol.ActionIDFactionIncense)
				incenseCancel()
				if err != nil || incenseResp == nil {
					slog.Warn("帮派祭神上香未收到回包", "role_id", session.RoleID(), "err", err)
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

// maxLuckyWheelSpins 单次调度的最大转盘次数，防止服务端异常未扣减青龙令时死循环。
const maxLuckyWheelSpins = 100

// NewFactionLuckyWheelRoutine 构造帮派转盘自动抽奖任务。
//
// 协议对齐 2026 客户端 Mod_LuckyWheel_Base:
//   - Action 1 open_lucky_wheel 查询转盘状态与剩余青龙令
//   - Action 2 start_lucky_wheel 转动一次转盘
func NewFactionLuckyWheelRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"faction_lucky_wheel",
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
			slog.Info("正在执行社交任务: 帮派转盘", "role_id", session.RoleID())

			// 1. 查询转盘状态与剩余青龙令 (Mod 69, Act 1)
			openPkt := protocol.BuildLuckyWheelOpenPacket(protocol.ActionIDTownEnter)
			openCtx, openCancel := context.WithTimeout(ctx, 3*time.Second)
			openResp, openErr := session.Call(openCtx, openPkt, protocol.ActionIDLuckyWheelOpen)
			openCancel()
			if openErr != nil || openResp == nil {
				slog.Warn("查询帮派转盘状态失败", "role_id", session.RoleID(), "err", openErr)
				return nil
			}

			openInfo, err := protocol.ParseLuckyWheelOpenResult(openResp.Payload)
			if err != nil {
				slog.Warn("解析帮派转盘状态失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("帮派转盘当前状态",
				"role_id", session.RoleID(),
				"state", openInfo.State,
				"faction_today_contribution", openInfo.FactionTodayContribution,
				"rosefinch_number", openInfo.RosefinchNumber,
			)

			if openInfo.State != protocol.LuckyWheelResultOK {
				slog.Info("帮派转盘当前不可转动",
					"role_id", session.RoleID(),
					"state", openInfo.State,
					"message", luckyWheelResultText(openInfo.State),
				)
				return nil
			}
			remain := openInfo.RosefinchNumber
			if remain <= 0 {
				slog.Info("青龙令不足，无法转动帮派转盘", "role_id", session.RoleID())
				return nil
			}

			// 2. 循环转动直至青龙令耗尽 (Mod 69, Act 2)
			prevAct := protocol.ActionIDLuckyWheelOpen
			spins := 0
			for remain > 0 && spins < maxLuckyWheelSpins {
				if err := ctx.Err(); err != nil {
					slog.Info("帮派转盘已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if spins > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}

				startPkt := protocol.BuildLuckyWheelStartPacket(prevAct)
				startCtx, startCancel := context.WithTimeout(ctx, 3*time.Second)
				startResp, err := session.Call(startCtx, startPkt, protocol.ActionIDLuckyWheelStart)
				startCancel()
				if err != nil || startResp == nil {
					slog.Warn("帮派转盘抽奖未收到回包", "role_id", session.RoleID(), "err", err)
					return nil
				}

				result, err := protocol.ParseLuckyWheelRunResult(startResp.Payload)
				if err != nil {
					slog.Warn("解析帮派转盘抽奖回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if result.RunState != protocol.LuckyWheelResultSuccess {
					slog.Warn("帮派转盘抽奖被服务端拒绝",
						"role_id", session.RoleID(),
						"result", result.RunState,
						"message", luckyWheelResultText(result.RunState),
					)
					break
				}

				spins++
				slog.Info("帮派转盘抽奖成功",
					"role_id", session.RoleID(),
					"spins", spins,
					"reward_type", result.Type,
					"item_id", result.ItemID,
					"remain_rosefinch", result.RosefinchNumber,
				)

				// 熔断保护: 服务端未扣减青龙令时立即退出，避免死循环。
				if result.RosefinchNumber >= remain {
					slog.Warn("帮派转盘青龙令未扣减，熔断退出",
						"role_id", session.RoleID(),
						"before_number", remain,
						"after_number", result.RosefinchNumber,
					)
					remain = 0
					break
				}
				remain = result.RosefinchNumber
				prevAct = protocol.ActionIDLuckyWheelStart
			}

			slog.Info("帮派转盘抽奖完成", "role_id", session.RoleID(), "spins", spins, "remain_rosefinch", remain)
			return nil
		},
	)
}

// luckyWheelResultText 返回帮派转盘结果码的中文说明。
func luckyWheelResultText(result uint8) string {
	switch result {
	case protocol.LuckyWheelResultOK:
		return "可以转动"
	case protocol.LuckyWheelResultNo:
		return "不可开始"
	case protocol.LuckyWheelResultSuccess:
		return "成功"
	case protocol.LuckyWheelResultNotEnoughNumber:
		return "青龙令不足"
	case protocol.LuckyWheelResultFailed:
		return "失败"
	default:
		return "未知结果"
	}
}

// NewGuildActivitiesRoutine 构造仙盟神兽与捐献任务。
func NewGuildActivitiesRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"guild_activities",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 仙盟捐献与神兽挑战")
			return nil
		},
	)
}

// NewSacredAllianceRoutine 构造圣盟祭祀与幻魔塔任务。
func NewSacredAllianceRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"sacred_alliance",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 圣盟祭祀与幻魔塔挑战")
			return nil
		},
	)
}

// NewHomesteadRoutine 构造住宅家具祝福与夫妻宝箱任务。
func NewHomesteadRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"homestead",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 住宅家具祝福与夫妻宝箱领取")
			return nil
		},
	)
}

// NewFriendshipRoutine 构造好友结义与鲜花赠送任务。
func NewFriendshipRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"friendship",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 好友结义与日常送花互动")
			return nil
		},
	)
}

// NewFameBlessingRoutine 构造仙界膜拜大神与声望祝福任务。
func NewFameBlessingRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"fame_blessing",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 膜拜大神尊像与声望领用")
			return nil
		},
	)
}
