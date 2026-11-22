// Package social 提供壶中界、仙盟、圣盟、好友祝福与帮派社交自动化任务。
//
// 本文件实现帮派转盘 (Mod_Faction_Base) 与帮派资格探测自动化。
package social

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

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

// factionWheelStartTimeout 帮派转盘单次抽奖等待上限；测试可缩短以模拟静默丢弃。
var factionWheelStartTimeout = 8 * time.Second

// FactionLuckyWheelConfig 帮派转盘自动化配置。
type FactionLuckyWheelConfig struct {
	// SellLowPillScrolls 抽到 1-5 品丹药制作卷时自动卖出 (默认开启)。
	// 低阶制作卷长期占背包且无使用价值，卖出仅回收铜钱，不消耗元宝。
	SellLowPillScrolls bool
}

// DefaultFactionLuckyWheelConfig 返回默认帮派转盘配置 (开启低阶丹药卷轴自动卖出)。
func DefaultFactionLuckyWheelConfig() FactionLuckyWheelConfig {
	return FactionLuckyWheelConfig{SellLowPillScrolls: true}
}

// lowPillScrollItemIDs 1-5 品武力/绝技/法术丹制作卷的道具编号。
// 数据来源 data/Pieb.db item 表 (sxdname 以“一~五品...丹制作卷”命名)。
var lowPillScrollItemIDs = map[int32]struct{}{
	382: {}, 383: {}, 385: {}, 386: {}, 387: {}, // 武力丹 1-5 品
	395: {}, 397: {}, 399: {}, 401: {}, 403: {}, // 绝技丹 1-5 品
	411: {}, 413: {}, 415: {}, 417: {}, 420: {}, // 法术丹 1-5 品
}

// NewFactionLuckyWheelRoutine 构造帮派转盘自动抽奖任务。
//
// 协议对齐 2026 客户端 Mod_LuckyWheel_Base:
//   - Action 1 open_lucky_wheel 查询转盘状态与剩余青龙令
//   - Action 2 start_lucky_wheel 转动一次转盘
//
// 配置开启 SellLowPillScrolls 时，抽到 1-5 品丹药制作卷会在本轮结束后
// 查询背包并主动卖出。
func NewFactionLuckyWheelRoutine(cfgs ...FactionLuckyWheelConfig) scheduler.ActivityRoutine {
	cfg := DefaultFactionLuckyWheelConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
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
			lowPillScrolls := make(map[int32]struct{})
			for remain > 0 && spins < maxLuckyWheelSpins {
				if err := ctx.Err(); err != nil {
					slog.Info("帮派转盘已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if spins > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}

				startPkt := protocol.BuildLuckyWheelStartPacket(prevAct)
				startCtx, startCancel := context.WithTimeout(ctx, factionWheelStartTimeout)
				startResp, err := session.Call(startCtx, startPkt, protocol.ActionIDLuckyWheelStart)
				startCancel()
				if err != nil || startResp == nil {
					slog.Info("帮派转盘抽奖未收到回包", "role_id", session.RoleID(), "err", err)
					// 已抽到的低阶卷轴仍要卖出；结束抽奖后走出售流程。
					break
				}

				result, err := protocol.ParseLuckyWheelRunResult(startResp.Payload)
				if err != nil {
					slog.Warn("解析帮派转盘抽奖回包失败", "role_id", session.RoleID(), "err", err)
					break
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
				if cfg.SellLowPillScrolls {
					if _, ok := lowPillScrollItemIDs[result.ItemID]; ok {
						lowPillScrolls[result.ItemID] = struct{}{}
						slog.Info("帮派转盘抽到 1-5 品丹药制作卷，稍后自动卖出",
							"role_id", session.RoleID(), "item_id", result.ItemID)
					}
				}

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
			if cfg.SellLowPillScrolls {
				// 大循环兜底：转盘消耗青龙令后，在 s0 卖出背包内所有 1-5 品丹药制作卷。
				sellLowPillScrolls(ctx, session)
			}
			return nil
		},
	)
}

// sellLowPillScrolls 查询主背包并卖出本轮转盘抽到的 1-5 品丹药制作卷。
//
// 出售仅回收铜钱，不消耗元宝；网络/解析失败只记录日志，不影响转盘结果。
func sellLowPillScrolls(ctx context.Context, session *client.RoleSession) {
	// 只在 ncs/qq 的 s0 服务器生效；s1 不动背包。
	if !strings.HasPrefix(session.RoleID(), "s0-") {
		return
	}
	packCtx, packCancel := context.WithTimeout(ctx, 8*time.Second)
	packResp, packErr := session.Call(packCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	packCancel()
	if packErr != nil || packResp == nil {
		slog.Info("帮派转盘低阶丹药卷轴出售：查询背包无回包，稍后重试",
			"role_id", session.RoleID(), "err", packErr)
		return
	}
	pack, pErr := protocol.ParsePackItemListResponse(packResp.Payload)
	if pErr != nil {
		slog.Warn("帮派转盘低阶丹药卷轴出售：解析背包失败",
			"role_id", session.RoleID(), "err", pErr)
		return
	}

	sold := 0
	for _, it := range pack.PackItems {
		if _, ok := lowPillScrollItemIDs[it.ItemID]; !ok {
			continue
		}
		if it.GridID <= 0 || it.GridID > 32767 {
			continue
		}
		sellPkt, bErr := protocol.BuildSellItemPacket(int16(it.GridID))
		if bErr != nil {
			continue
		}
		sellCtx, sellCancel := context.WithTimeout(ctx, 8*time.Second)
		sellResp, sellErr := session.Call(sellCtx, sellPkt, protocol.ActionIDItemSell)
		sellCancel()
		if sellErr != nil || sellResp == nil {
			slog.Info("帮派转盘低阶丹药卷轴出售无回包，中止本轮出售",
				"role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID, "err", sellErr)
			break
		}
		ok, code, rErr := protocol.ParseSellItemResponse(sellResp.Payload)
		if rErr != nil {
			slog.Warn("帮派转盘低阶丹药卷轴出售回包解析失败",
				"role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID, "err", rErr)
			continue
		}
		if !ok {
			slog.Warn("帮派转盘低阶丹药卷轴出售被服务端拒绝",
				"role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID, "result_code", code)
			continue
		}
		sold++
		slog.Info("帮派转盘低阶丹药卷轴已卖出",
			"role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID)
	}
	slog.Info("帮派转盘低阶丹药卷轴出售完成",
		"role_id", session.RoleID(), "sold", sold, "item_kinds", len(lowPillScrollItemIDs))
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
