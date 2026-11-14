// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-10-08
package social

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
			// 功能未开放时 232/30 仍会返回背包宝箱，但 232/34 开箱会被服务端
			// 静默丢弃（2026-11-14 s1 实测：功能 140 未开放，所有开箱载荷均无回包）。
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDLinkFate); fErr != nil {
				slog.Info("结缘宝箱功能开放状态查询失败，本轮跳过",
					"role_id", session.RoleID(), "err", fErr)
				return scheduler.NewCooldown(30*time.Minute, "结缘宝箱功能状态查询失败")
			} else if !open {
				slog.Info("结缘宝箱功能未开放，跳过",
					"role_id", session.RoleID(), "function_id", protocol.FunctionIDLinkFate)
				return nil
			}

			slog.Info("正在执行结缘任务: 结缘宝箱自动开启 (每批最多10个，严格排除传说宝箱)",
				"role_id", session.RoleID(),
				"max_batch_size", maxBatch,
			)

			// 1. 查询背包持有的结缘宝箱列表 (Mod 232, Act 30)
			queryPkt := protocol.BuildJieYuanBoxListPacket(protocol.ActionIDTownEnter)
			callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(callCtx, queryPkt, protocol.ActionIDJieYuanBoxList)
			cancel()

			if err != nil || resp == nil {
				slog.Warn("查询结缘宝箱列表超时或失败", "err", err)
				return nil
			}

			boxes, err := protocol.ParseJieYuanBoxListResult(resp.Payload)
			if err != nil {
				slog.Warn("解析结缘宝箱列表失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if len(boxes) == 0 {
				slog.Info("结缘宝箱功能未开放/背包无可开宝箱，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 严格遵循规则: 除了传说 (3942)，其他都开
			openable := protocol.FilterOpenableJieYuanBoxes(boxes)
			if len(openable) == 0 {
				slog.Info("无待开启的非传说结缘宝箱", "total_kinds", len(boxes))
				return nil
			}

			var totalOpened int

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

					// 每次 232/34 开箱前先发送 528/1 set_setting [0x07 0x04]（裸 2 字节），
					// 232/34 的 PrevAct 紧跟该设置动作 (0x02100001)。缺少设置时开箱被静默丢弃。
					ctxPkt := protocol.BuildJieYuanBoxOpenContextPacket()
					cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
					ctxResp, cErr := session.Call(cCtx, ctxPkt, protocol.ActionIDJieYuanBoxOpenContext)
					cCancel()
					if cErr != nil || ctxResp == nil {
						slog.Info("结缘宝箱开箱设置 528/1 无回包，稍后重试",
							"role_id", session.RoleID(), "err", cErr)
						return scheduler.NewCooldown(30*time.Minute, "结缘宝箱设置无回包")
					}
					ctxCode, ctxParseErr := protocol.ParseJieYuanOpenContextResult(ctxResp.Payload)
					if ctxParseErr != nil {
						slog.Warn("解析结缘宝箱上下文握手结果失败，终止本轮",
							"role_id", session.RoleID(), "err", ctxParseErr,
							"payload_hex", hex.EncodeToString(ctxResp.Payload))
						return nil
					}
					if ctxCode != protocol.JieYuanOpenContextResultOK {
						slog.Warn("结缘宝箱上下文握手被拒绝，终止本轮",
							"role_id", session.RoleID(), "result_code", ctxCode)
						return scheduler.NewCooldown(2*time.Hour, "结缘宝箱上下文握手被拒绝")
					}

					openPkt := protocol.BuildJieYuanBoxOpenPacket(box.ItemID, openCount, protocol.ActionIDJieYuanBoxOpenContext)
					oCtx, oCancel := context.WithTimeout(ctx, 8*time.Second)
					openResp, oErr := session.Call(oCtx, openPkt, protocol.ActionIDJieYuanBoxOpen)
					oCancel()

					if oErr != nil || openResp == nil {
						// 232/30 列表始终可读，而 232/34 开箱在活动未开放/不在
						// 可开箱时段会被服务端静默丢弃（与抓包中活动期开箱成功一致）。
						// 该状态无法与网络瞬断区分，按活动时段冷却 2 小时复查。
						slog.Info("结缘宝箱当前不可开启（活动未开放或不在可开箱时段），稍后重试",
							"role_id", session.RoleID(),
							"item_id", box.ItemID,
							"open_count", openCount,
							"err", oErr,
						)
						return scheduler.NewCooldown(2*time.Hour, "结缘宝箱不在可开启时段")
					}

					openCode, openParseErr := protocol.ParseJieYuanBoxOpenResult(openResp.Payload)
					if openParseErr != nil {
						slog.Warn("解析结缘宝箱开箱结果失败，终止本轮",
							"role_id", session.RoleID(), "err", openParseErr,
							"payload_len", len(openResp.Payload))
						return nil
					}
					if openCode != protocol.JieYuanBoxOpenResultSuccess {
						// 非成功码（如次数不足、道具不存在、条件不满足）不得盲目重试。
						slog.Warn("结缘宝箱开箱失败，终止本轮",
							"role_id", session.RoleID(),
							"item_id", box.ItemID,
							"open_count", openCount,
							"result_code", openCode)
						return nil
					}

					// 成功也要校验副作用：重新拉取列表，确认道具数量确实减少。
					// 服务端对无资格请求可能返回空回包，不能仅凭“收到回包”判定成功。
					verifyPkt := protocol.BuildJieYuanBoxListPacket(protocol.ActionIDJieYuanBoxOpen)
					vCtx, vCancel := context.WithTimeout(ctx, 8*time.Second)
					verifyResp, vErr := session.Call(vCtx, verifyPkt, protocol.ActionIDJieYuanBoxList)
					vCancel()
					if vErr != nil || verifyResp == nil {
						slog.Info("结缘宝箱开箱后核对库存无回包，稍后重试",
							"role_id", session.RoleID(),
							"item_id", box.ItemID,
							"err", vErr,
						)
						return scheduler.NewCooldown(30*time.Minute, "结缘宝箱库存核对无回包")
					}
					boxesAfter, parseErr := protocol.ParseJieYuanBoxListResult(verifyResp.Payload)
					if parseErr != nil {
						slog.Warn("解析结缘宝箱开箱后库存失败，终止本轮",
							"role_id", session.RoleID(), "err", parseErr)
						return nil
					}
					// 数量降到 0 时服务端可能直接从列表中移除该道具，此时按 0 处理。
					afterCount := int32(0)
					for _, b := range boxesAfter {
						if b.ItemID == box.ItemID {
							afterCount = b.Count
							break
						}
					}
					if afterCount == remaining {
						// 仅当数量完全不变时才判定开箱未生效；若数量增加，
						// 说明开出的奖励恰好是同一种宝箱道具，属于正常结算。
						slog.Warn("结缘宝箱开箱未生效（数量未变化），终止本轮",
							"role_id", session.RoleID(),
							"item_id", box.ItemID,
							"before", remaining,
							"after", afterCount,
							"reply_len", len(openResp.Payload),
						)
						return nil
					}

					openedNow := remaining - afterCount
					totalOpened += int(openedNow)
					remaining = afterCount

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

// maxLuckyWheelSpins 单次调度的最大转盘次数，防止服务端异常未扣减青龙令时死循环。
const maxLuckyWheelSpins = 100

// factionLuckyWheelOpenTimeout 转盘面板 (69/1) 的等待时长；单测可缩短。
var factionLuckyWheelOpenTimeout = 8 * time.Second

// factionProbeResult 帮派上下文探测结果。
type factionProbeResult int

const (
	factionProbeAvailable factionProbeResult = iota
	factionProbeUnavailable
	factionProbeUnknown
)

// probeFactionMembership 用免费白檀香 (10/23) 探测帮派上下文。
//
// 未加入帮派的角色上多数帮派只读请求会被静默丢弃，但 incense 始终回包：
// 22 MEMBER_NOT_EXIST / 18 FACTION_NOT_EXIST 表示无帮派，
// 32 SUCCESS / 34 COUNT_USED 表示帮派上下文正常。结果会写入会话标记。
func probeFactionMembership(ctx context.Context, session *client.RoleSession) (factionProbeResult, uint8, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := session.Call(probeCtx,
		protocol.BuildFactionIncensePacket(protocol.FactionOfferingWhiteSandalwood),
		protocol.ActionIDFactionIncense)
	if err != nil || resp == nil {
		return factionProbeUnknown, 0, err
	}
	code, pErr := protocol.ParseFactionIncenseResult(resp.Payload)
	if pErr != nil {
		return factionProbeUnknown, 0, pErr
	}
	switch code {
	case protocol.FactionIncenseResultMemberNotExist, protocol.FactionIncenseResultFactionNotExist:
		session.MarkFactionFeatureUnavailable()
		return factionProbeUnavailable, code, nil
	case protocol.FactionIncenseResultSuccess, protocol.FactionIncenseResultCountUsed:
		session.MarkFactionFeatureAvailable()
		return factionProbeAvailable, code, nil
	default:
		return factionProbeUnknown, code, nil
	}
}

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
			if session.IsFactionFeatureUnavailable() {
				slog.Info("帮派模块对当前角色不可用，跳过帮派转盘", "role_id", session.RoleID())
				return nil
			}
			// 未确认帮派上下文时先探测，避免 69/1 在无帮派角色上被静默丢弃。
			if !session.IsFactionFeatureAvailable() {
				result, code, pErr := probeFactionMembership(ctx, session)
				switch result {
				case factionProbeUnavailable:
					slog.Info("帮派转盘：免费白檀香返回无帮派，跳过", "role_id", session.RoleID(), "result_code", code)
					return nil
				case factionProbeUnknown:
					if pErr != nil {
						slog.Info("帮派转盘：帮派归属探测无回包，稍后重试", "role_id", session.RoleID(), "err", pErr)
					} else {
						slog.Info("帮派转盘：帮派归属探测返回未知结果码，稍后重试",
							"role_id", session.RoleID(), "result_code", code)
					}
					return scheduler.NewCooldown(30*time.Minute, "帮派转盘帮派归属探测失败")
				}
			}
			slog.Info("正在执行社交任务: 帮派转盘", "role_id", session.RoleID())

			// 1. 查询转盘状态与剩余青龙令 (Mod 69, Act 1)
			openPkt := protocol.BuildLuckyWheelOpenPacket(protocol.ActionIDTownEnter)
			openCtx, openCancel := context.WithTimeout(ctx, factionLuckyWheelOpenTimeout)
			openResp, openErr := session.Call(openCtx, openPkt, protocol.ActionIDLuckyWheelOpen)
			openCancel()
			if openErr != nil || openResp == nil {
				if errors.Is(openErr, context.DeadlineExceeded) {
					// 面板 (69/1) 在未加入帮派的角色上会被服务端静默丢弃。
					// 若并发执行的帮派祭神例程已判定无帮派，直接跳过，避免每轮空等。
					if session.IsFactionFeatureUnavailable() {
						slog.Info("帮派模块对当前角色不可用，跳过帮派转盘", "role_id", session.RoleID())
						return nil
					}
					// 用免费白檀香探测结果码，区分“未加入帮派”和偶发丢包。
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
								slog.Info("帮派转盘无回包，免费白檀香返回“无此帮派成员/帮派”，确认未加入帮派，跳过",
									"role_id", session.RoleID(), "result_code", code)
							case protocol.FactionIncenseResultSuccess, protocol.FactionIncenseResultCountUsed:
								slog.Info("帮派转盘面板无回包，但帮派上下文正常，稍后重试",
									"role_id", session.RoleID(), "result_code", code)
							default:
								slog.Info("帮派转盘面板无回包，上香探测返回未知结果码",
									"role_id", session.RoleID(), "result_code", code)
							}
							return nil
						}
					}
					// 两次请求均无回包不能判定未入帮派，避免把帮派模块永久禁用。
					slog.Info("帮派转盘面板与上香探测均无回包，可能为瞬断，稍后重试",
						"role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "帮派转盘面板与探测均无回包")
				}
				slog.Warn("查询帮派转盘状态失败，稍后重试", "role_id", session.RoleID(), "err", openErr)
				return scheduler.NewCooldown(30*time.Minute, "帮派转盘状态查询失败")
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
				startCtx, startCancel := context.WithTimeout(ctx, 8*time.Second)
				startResp, err := session.Call(startCtx, startPkt, protocol.ActionIDLuckyWheelStart)
				startCancel()
				if err != nil || startResp == nil {
					slog.Info("帮派转盘抽奖未收到回包", "role_id", session.RoleID(), "err", err)
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
// NewGuildActivitiesRoutine 构造仙盟日常任务 (Mod 571/591/575)。
//
// 流程：主服读取仙盟分组与城镇凭据 → 建立仙盟城镇会话 (Mod 575 登录+enter_town)
// → 读取仙盟主页 → 领取仙盟福利 → 每日膜拜。
// 未加入仙盟/城镇登录失败时安全跳过；不消耗元宝。
func NewGuildActivitiesRoutine() scheduler.ActivityRoutine {
	const townCtxTimeout = 30 * time.Second
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"guild_activities",
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

			// 先查功能开放列表；仙盟未开放时不再申请跨服城镇会话并等待超时。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDFaction)
			if fErr != nil {
				slog.Info("仙盟功能开放状态查询失败，稍后重试仙盟日常", "role_id", session.RoleID(), "err", fErr)
				return scheduler.NewCooldown(30*time.Minute, "仙盟功能开放状态无回包")
			}
			if !open {
				slog.Info("仙盟功能未开放，跳过仙盟日常", "role_id", session.RoleID(), "function_id", protocol.FunctionIDFaction)
				return nil
			}

			townCtx, townCancel := context.WithTimeout(ctx, townCtxTimeout)
			defer townCancel()
			town, err := session.OpenSectTownSession(townCtx)
			if err != nil {
				if errors.Is(err, client.ErrNotInSect) {
					slog.Info("账号未加入仙盟，跳过仙盟日常", "role_id", session.RoleID())
					return nil
				}
				if errors.Is(err, context.DeadlineExceeded) {
					slog.Info("仙盟面板无回包，稍后重试仙盟日常", "role_id", session.RoleID())
				} else {
					slog.Info("仙盟城镇会话不可用，稍后重试仙盟日常", "role_id", session.RoleID(), "err", err)
				}
				return scheduler.NewCooldown(30*time.Minute, "仙盟城镇会话无回包")
			}
			defer town.Close()

			cred, err := session.FetchSectTownCredentials(townCtx)
			if err != nil {
				slog.Warn("读取仙盟凭据失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙盟凭据无回包")
			}

			// 1. 仙盟主页
			mCtx, mCancel := context.WithTimeout(townCtx, callTimeout)
			mainResp, err := town.Call(mCtx, protocol.BuildSectMainInfoPacket(cred.SectID), 0x023C0001)
			mCancel()
			if err != nil {
				slog.Warn("读取仙盟主页失败", "role_id", session.RoleID(), "err", err)
			} else if mainInfo, pErr := protocol.ParseSectMainInfoResult(mainResp.Payload); pErr != nil {
				slog.Warn("解析仙盟主页失败", "role_id", session.RoleID(), "err", pErr)
			} else {
				slog.Info("仙盟主页",
					"role_id", session.RoleID(),
					"result", mainInfo.Result,
					"sect_id", mainInfo.SelfSectID,
					"sect_name", mainInfo.SectName,
					"sect_level", mainInfo.SectLevel,
					"alliance_id", mainInfo.AllianceID,
				)
			}

			// 2. 仙盟福利领取
			bCtx, bCancel := context.WithTimeout(townCtx, callTimeout)
			bonusResp, err := town.Call(bCtx, protocol.BuildSectBonusPanelPacket(cred.SectID), 0x024F0001)
			bCancel()
			if err != nil {
				slog.Warn("读取仙盟福利面板失败", "role_id", session.RoleID(), "err", err)
			} else if panel, pErr := protocol.ParseSectBonusPanelResult(bonusResp.Payload); pErr != nil {
				slog.Warn("解析仙盟福利面板失败", "role_id", session.RoleID(), "err", pErr)
			} else {
				slog.Info("仙盟福利面板",
					"role_id", session.RoleID(),
					"result", panel.Result,
					"gifts", panel.Gifts,
					"left_get_times", panel.LeftGetTimes,
					"sect_cont", panel.SectCont,
				)
				if panel.Result == 0 && panel.LeftGetTimes > 0 {
					// 官方客户端 get_gift 的参数是宗门 ID（不是 gift_id），每次领取一份。
					claimed := int32(0)
					for claimed < panel.LeftGetTimes {
						gCtx, gCancel := context.WithTimeout(townCtx, callTimeout)
						gResp, gErr := town.Call(gCtx, protocol.BuildSectBonusGetGiftPacket(cred.SectID), 0x024F0003)
						gCancel()
						if gErr != nil {
							slog.Info("领取仙盟福利无应答", "role_id", session.RoleID(), "err", gErr)
							break
						}
						result, rErr := protocol.ParseSectActionResult(gResp.Payload)
						if rErr != nil {
							slog.Warn("解析仙盟福利回包失败", "role_id", session.RoleID(), "err", rErr)
							break
						}
						switch result {
						case 0:
							claimed++
							slog.Info("仙盟福利领取成功", "role_id", session.RoleID(), "index", claimed)
						case 3: // LESS_TIMES
							slog.Info("仙盟福利领取次数已用尽", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						case 4: // ITEM_PACK_FULL
							return scheduler.ErrBagFull
						case 5: // NO_GIFT
							slog.Info("仙盟福利暂无可领礼包", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						case 1: // FUNC_NOT_OPEN
							slog.Info("仙盟福利功能未开放/未满足贡献条件", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						case 2: // FAILED
							slog.Info("仙盟福利领取失败", "role_id", session.RoleID(), "claimed", claimed)
							claimed = panel.LeftGetTimes
						default:
							slog.Info("仙盟福利未领取", "role_id", session.RoleID(), "result", result, "claimed", claimed)
							claimed = panel.LeftGetTimes
						}
					}
				}
			}

			// 3. 每日膜拜
			wCtx, wCancel := context.WithTimeout(townCtx, callTimeout)
			wResp, err := town.Call(wCtx, protocol.BuildSectWorshipPacket(cred.SectID), 0x023C000D)
			wCancel()
			if err != nil {
				slog.Info("仙盟膜拜无应答", "role_id", session.RoleID(), "err", err)
			} else if result, rErr := protocol.ParseSectActionResult(wResp.Payload); rErr != nil {
				slog.Warn("解析仙盟膜拜回包失败", "role_id", session.RoleID(), "err", rErr)
			} else if result == 0 {
				slog.Info("仙盟膜拜成功", "role_id", session.RoleID())
			} else if result == 16 { // IS_WORSHIP
				slog.Info("今日已膜拜", "role_id", session.RoleID())
			} else {
				slog.Info("仙盟膜拜未成功", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewSacredAllianceRoutine 构造圣盟祭祀与幻魔塔任务。
// NewSacredAllianceRoutine 构造圣盟日常任务 (Module 884)。
//
// 先查询圣盟当前赛季；无应答/赛季数据缺失时视为功能未开放并跳过。
func NewSacredAllianceRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"sacred_alliance",
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

			cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildSectAllianceSeasonPacket(), protocol.ActionIDSectAllianceSeason)
			cCancel()
			if err != nil {
				slog.Info("圣盟功能不可用/未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			season, endTime, pErr := protocol.ParseSectAllianceSeasonResult(resp.Payload)
			if pErr != nil {
				slog.Warn("解析圣盟赛季失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			if season <= 0 {
				slog.Info("圣盟当前无有效赛季，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("圣盟赛季开放；祭祀/奖励领取需仙盟城镇会话与 alliance_id，待下一批接入",
				"role_id", session.RoleID(),
				"season", season,
				"end_time", endTime,
			)
			return nil
		},
	)
}

// NewHomesteadRoutine 构造住宅家具祝福与夫妻宝箱任务。
// NewHomesteadRoutine 构造住宅日常任务 (Module 210)。
//
// 先查询自身住宅状态：未处于婚姻状态或功能未开放时跳过。
func NewHomesteadRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"homestead",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 官方小助手"家园奖励"直接调用 210/18，不发 210/0。
			// 但未结婚/无家园角色发送 210/18 会被服务端静默丢弃（qq 实测），
			// 因此先用只读的 210/0 判定婚姻状态：未结婚直接挂起至次日，
			// 已结婚才发送 210/18，避免每轮固定超时 8 秒。
			sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
			selfResp, sErr := session.Call(sCtx,
				protocol.BuildMarryHomeSelfInfoPacket(session.PlayerID()), protocol.ActionIDMarryHomeSelfInfo)
			sCancel()
			if sErr != nil || selfResp == nil {
				slog.Info("住宅状态查询无回包，稍后重试", "role_id", session.RoleID(), "err", sErr)
				return scheduler.NewCooldown(30*time.Minute, "住宅状态查询无回包")
			}
			selfResult, pErr := protocol.ParseMarryHomeSelfInfoResult(selfResp.Payload)
			if pErr != nil {
				return scheduler.NewCooldown(10*time.Minute, "住宅状态解析失败")
			}
			switch selfResult {
			case protocol.MarryHomeResultSuccess:
				// 正常，继续。
			case protocol.MarryHomeResultNonMarry:
				slog.Info("账号未处于婚姻状态，跳过住宅家具效果", "role_id", session.RoleID())
				return scheduler.ErrAttemptsExhausted
			case protocol.MarryHomeResultNonActivityTime:
				slog.Info("家园活动未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "家园活动未开放")
			case protocol.MarryHomeResultFunctionNoOpen:
				slog.Info("家园功能未开放，跳过", "role_id", session.RoleID())
				return scheduler.ErrAttemptsExhausted
			default:
				slog.Info("住宅状态不可用，稍后重试", "role_id", session.RoleID(), "result", selfResult)
				return scheduler.NewCooldown(30*time.Minute, "住宅状态不可用")
			}

			// 210/18 需要家园场景上下文：先按官方客户端调用 get_player_home_info。
			// 缺少该前置时 210/18 会被服务端静默丢弃（2026-11-14 s1 实测）。
			pCtx, pCancel := context.WithTimeout(ctx, callTimeout)
			homeResp, pErr := session.Call(pCtx,
				protocol.BuildMarryHomePlayerInfoPacket(session.PlayerID()),
				protocol.ActionIDMarryHomePlayerInfo)
			pCancel()
			if pErr != nil || homeResp == nil {
				slog.Info("住宅信息查询无回包，稍后重试", "role_id", session.RoleID(), "err", pErr)
				return scheduler.NewCooldown(2*time.Hour, "住宅信息无回包")
			}

			bCtx, bCancel := context.WithTimeout(ctx, callTimeout)
			effectResp, bErr := session.Call(bCtx, protocol.BuildMarryHomeBatchFurnitureEffectPacket(), protocol.ActionIDMarryHomeBatchFurnitureEffect)
			bCancel()
			if bErr != nil || effectResp == nil {
				slog.Info("住宅家具效果领取无回包，稍后重试", "role_id", session.RoleID(), "err", bErr)
				return scheduler.NewCooldown(2*time.Hour, "住宅家具效果无回包")
			}
			if len(effectResp.Payload) == 0 {
				slog.Info("住宅暂无可领取家具效果", "role_id", session.RoleID())
				return nil
			}
			effects, eErr := protocol.ParseMarryHomeBatchFurnitureEffectResult(effectResp.Payload)
			if eErr != nil {
				slog.Warn("解析住宅家具效果失败，稍后重试", "role_id", session.RoleID(), "err", eErr)
				return scheduler.NewCooldown(10*time.Minute, "住宅家具效果解析失败")
			}
			slog.Info("住宅家具效果领取完成",
				"role_id", session.RoleID(), "count", len(effects), "effects", effects)
			return nil
		},
	)
}

// NewFriendshipRoutine 构造好友来信领取任务 (Module 684, Mod_FriendsSent_Base)。
//
// 拉取好友来信列表，仅处理已解锁 (时间/任务条件均满足) 的来信：
// 未读则标记已读，未领奖则领取附件奖励。不消耗元宝。
func NewFriendshipRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"friendship",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			pCtx, pCancel := context.WithTimeout(ctx, callTimeout)
			panelResp, err := session.Call(pCtx, protocol.BuildFriendsSentPanelPacket(), protocol.ActionIDFriendsSentPanel)
			pCancel()
			if err != nil {
				slog.Info("好友来信功能不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "好友来信面板无回包")
			}
			letters, err := protocol.ParseFriendsSentPanelResult(panelResp.Payload)
			if err != nil {
				return fmt.Errorf("解析好友来信列表失败: %w", err)
			}
			unlocked := 0
			for _, l := range letters {
				if l.IsTimeLock != 0 || l.IsMissionLock != 0 {
					continue
				}
				unlocked++
				octx, ocancel := context.WithTimeout(ctx, callTimeout)
				openResp, oErr := session.Call(octx, protocol.BuildFriendsSentOpenPacket(l.ID), protocol.ActionIDFriendsSentOpen)
				ocancel()
				if oErr != nil {
					slog.Info("打开好友来信无应答，稍后重试", "role_id", session.RoleID(), "letter_id", l.ID, "err", oErr)
					return scheduler.NewCooldown(10*time.Minute, "打开好友来信无回包")
				}
				detail, pErr := protocol.ParseFriendsSentOpenResult(openResp.Payload)
				if pErr != nil {
					slog.Warn("解析好友来信内容失败", "role_id", session.RoleID(), "letter_id", l.ID, "err", pErr)
					continue
				}
				// 领取前总是补发一次 read：部分区服要求领奖请求前本会话内
				// 有 read 动作，即使账号维度的 is_read 已经是 1。
				if detail.IsGetAward == 0 {
					rctx, rcancel := context.WithTimeout(ctx, callTimeout)
					rResp, rErr := session.Call(rctx, protocol.BuildFriendsSentReadPacket(l.ID), protocol.ActionIDFriendsSentRead)
					rcancel()
					if rErr != nil || rResp == nil {
						slog.Info("标记好友来信已读无回包，稍后重试",
							"role_id", session.RoleID(), "letter_id", l.ID, "err", rErr)
						return scheduler.NewCooldown(10*time.Minute, "好友来信已读请求无回包")
					}
					if result, e := protocol.ParseFriendsSentActionResult(rResp.Payload); e == nil &&
						result != protocol.FriendsSentResultSuccess {
						slog.Info("标记好友来信已读未成功", "role_id", session.RoleID(),
							"letter_id", l.ID, "result", result)
					}
				}
				if detail.IsGetAward == 0 && len(detail.Awards) > 0 {
					actx, acancel := context.WithTimeout(ctx, callTimeout)
					aResp, aErr := session.Call(actx, protocol.BuildFriendsSentAwardPacket(l.ID), protocol.ActionIDFriendsSentAward)
					acancel()
					if aErr != nil {
						slog.Info("领取好友来信奖励无应答", "role_id", session.RoleID(), "letter_id", l.ID, "err", aErr)
						continue
					}
					result, e := protocol.ParseFriendsSentActionResult(aResp.Payload)
					if e != nil {
						slog.Warn("解析好友来信奖励回包失败", "role_id", session.RoleID(), "letter_id", l.ID, "err", e)
						continue
					}
					switch result {
					case protocol.FriendsSentResultSuccess:
						slog.Info("好友来信奖励领取成功", "role_id", session.RoleID(), "letter_id", l.ID, "awards", detail.Awards)
					case protocol.FriendsSentResultItemPackFull:
						return scheduler.ErrBagFull
					case protocol.FriendsSentResultUnRead:
						slog.Info("好友来信未阅读，跳过领奖", "role_id", session.RoleID(), "letter_id", l.ID)
					default:
						// 线上两套账号实测: 均已读、is_get_award=0 且列出附件，
						// get_award 仍固定返回 FAILED(1)，通常表示历史信件已领/不可领。
						slog.Info("好友来信奖励不可领取", "role_id", session.RoleID(),
							"letter_id", l.ID, "result", result)
					}
				}
			}
			slog.Info("好友来信处理完成", "role_id", session.RoleID(), "letters", len(letters), "unlocked", unlocked)
			return nil
		},
	)
}

// NewFameBlessingRoutine 构造仙界膜拜大神与声望祝福任务。
// NewFameBlessingRoutine 构造尊像膜拜任务 (Module 41, Mod_WorshipMars_Base)。
//
// 查询膜拜信息与剩余次数后，只使用免费供品 (白檀香, id=1) 上香；
// 需要元宝的苏合香/天木香一律不调用。
func NewFameBlessingRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"fame_blessing",
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

			// 0. 功能开放检查：使用 Mod 0 Act 6 的会话缓存。
			// 逐功能的 check_function_open 会被服务端静默丢弃，探测失败时继续走面板查询，
			// 由面板结果判定玩法可用性 (线上 s0 已实测面板可正常返回)。
			open, chkErr := session.IsFunctionOpen(ctx, protocol.FunctionIDWorshipMars)
			if chkErr != nil {
				slog.Info("查询膜拜功能开放状态失败，继续尝试面板查询",
					"role_id", session.RoleID(), "err", chkErr)
			} else if !open {
				slog.Info("膜拜功能未开启，跳过", "role_id", session.RoleID())
				return nil
			}

			// 1. 膜拜信息：无数据时跳过。
			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildWorshipMarsInfoPacket(), protocol.ActionIDWorshipMarsInfo)
			iCancel()
			if err != nil {
				slog.Info("查询膜拜信息无回包，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "膜拜信息无回包")
			}
			info, err := protocol.ParseWorshipMarsInfoResult(infoResp.Payload)
			if err != nil {
				slog.Warn("解析膜拜信息失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "膜拜信息解析失败")
			}
			slog.Info("膜拜信息",
				"role_id", session.RoleID(),
				"mars_level", info.MarsLevel,
				"incense_count", info.IncenseCount,
				"total_count", info.TotalCount,
				"logs", info.LogEntryCount,
			)

			// 2. 剩余免费次数
			rCtx, rCancel := context.WithTimeout(ctx, callTimeout)
			remainResp, err := session.Call(rCtx, protocol.BuildWorshipMarsRemainPacket(), protocol.ActionIDWorshipMarsRemain)
			rCancel()
			if err != nil {
				slog.Info("查询剩余膜拜次数失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "膜拜剩余次数无回包")
			}
			remain, err := protocol.ParseWorshipMarsRemainResult(remainResp.Payload)
			if err != nil {
				slog.Warn("解析剩余膜拜次数失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "膜拜剩余次数解析失败")
			}
			if remain <= 0 {
				slog.Info("膜拜免费次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 3. 使用免费供品 (白檀香) 上香，直到次数耗尽或失败。
			done := int32(0)
			dropped := false
			for done < remain {
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(cCtx, protocol.BuildWorshipMarsIncensePacket(protocol.WorshipMarsOfferingFree), protocol.ActionIDWorshipMarsIncense)
				cCancel()
				if cErr != nil {
					slog.Info("上香无应答", "role_id", session.RoleID(), "done", done, "err", cErr)
					dropped = true
					break
				}
				result, pErr := protocol.ParseWorshipMarsIncenseResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析上香回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result {
				case protocol.WorshipMarsResultSuccess:
					done++
					slog.Info("免费上香成功", "role_id", session.RoleID(), "index", done)
				case protocol.WorshipMarsResultCountUsed:
					slog.Info("膜拜次数已用尽", "role_id", session.RoleID(), "done", done)
					done = remain
				case protocol.WorshipMarsResultBlessingTimes, protocol.WorshipMarsResultExpAdd:
					slog.Info("膜拜祝福/经验加成已满", "role_id", session.RoleID(), "result", result)
					done = remain
				case protocol.WorshipMarsResultInsufficientIngot:
					slog.Warn("上香提示元宝不足 (免费供品不应出现)", "role_id", session.RoleID())
					done = remain
				default:
					slog.Warn("上香返回未知结果", "role_id", session.RoleID(), "result", result)
					done = remain
				}
				if done < remain {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(200 * time.Millisecond):
					}
				}
			}
			slog.Info("膜拜完成", "role_id", session.RoleID(), "free_incense_done", done)
			if done == 0 && dropped {
				return scheduler.NewCooldown(10*time.Minute, "膜拜上香无回包")
			}
			return nil
		},
	)
}
