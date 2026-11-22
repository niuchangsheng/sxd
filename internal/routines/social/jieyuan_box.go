// Package social 提供壶中界、仙盟、圣盟、好友祝福与帮派社交自动化任务。
//
// 本文件实现结缘宝箱 (Mod_NewTown_Base, Module 232) 自动开启。
package social

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
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
// fetchJieYuanBoxCount 查询 232/30 列表中指定箱型的当前数量。
// 返回 (数量, 是否成功获取)；列表中不存在该箱型时返回 (0, true)。
// jieYuanOpenWithQuality 执行 528/1 上下文握手 + 232/34 开箱，返回开箱回包。
// 仅用于「换自动合成档位」的变体探测，正常开箱路径仍内联在例程中。
func jieYuanOpenWithQuality(ctx context.Context, session *client.RoleSession, itemID, quality int32) (*protocol.Packet, error) {
	ctxResp, err := callJieYuanContext(ctx, session)
	if err != nil || ctxResp == nil {
		return nil, err
	}
	if code, pErr := protocol.ParseJieYuanOpenContextResult(ctxResp.Payload); pErr != nil || code != protocol.JieYuanOpenContextResultOK {
		if pErr != nil {
			return nil, pErr
		}
		return nil, fmt.Errorf("528/1 握手被拒绝: result=%d", code)
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return session.Call(c, protocol.BuildJieYuanBoxOpenPacket(itemID, quality),
		protocol.ActionIDJieYuanBoxOpen)
}

// callJieYuanContext 发送 528/1 上下文握手。
//
// 528/1 尾部的 PrevAct 必须等于「上一条发送的请求」，由会话层在写入瞬间填充
// （见 protocol.BuildJieYuanBoxOpenContextPacket）。链路正确时服务端回 result=0；
// 若首次仍无回包（例如链路被并发心跳推进），重试一次即可用最新锚点重新握手。
// 不能用裸 2 字节兜底：服务端虽回 0，但上下文不成立，随后的 232/34 仍被丢弃。
func callJieYuanContext(ctx context.Context, session *client.RoleSession) (*protocol.Packet, error) {
	// 首次发送前记录最后一条业务回包锚点（排除心跳）。首次 528/1 超时期间
	// 心跳回包会推进 LastRxActionID，不能让重试尾链漂移到 0/75。
	anchor := session.LastAckActionID()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		pkt := protocol.BuildJieYuanBoxOpenContextPacket()
		if attempt > 0 {
			// 第一次 528/1 已把会话的「上一条发送动作」推进到 528/1 自身。
			// 若重试仍用 PrevActFromLastSent，会发出 528/1 -> 528/1 的自引用
			// 尾链，服务端必然丢弃。重试显式写回首次发送前的业务回包锚点
			// （通常是 232/30 列表回包）。
			pkt.PrevActFromLastSent = false
			if anchor != 0 && len(pkt.Payload) >= 6 {
				binary.BigEndian.PutUint32(pkt.Payload[2:6], anchor)
				pkt.PrevActMode = protocol.PrevActExplicit
			} else {
				pkt.PrevActMode = protocol.PrevActAutoBusiness
			}
		}
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		resp, err := session.Call(c, pkt, protocol.ActionIDJieYuanBoxOpenContext)
		cancel()
		if err == nil && resp != nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// fetchJieYuanPack 查询背包列表（用于开箱前确认空格）。
func fetchJieYuanPack(ctx context.Context, session *client.RoleSession) (*protocol.PackItemListResponse, error) {
	pkt := protocol.BuildGetPackItemListPacket()
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := session.Call(c, pkt, protocol.ActionIDItemGetPackList)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("背包列表返回空响应")
	}
	return protocol.ParsePackItemListResponse(resp.Payload)
}

func fetchJieYuanBoxCount(ctx context.Context, session *client.RoleSession, itemID int32) (int32, bool) {
	pkt := protocol.BuildJieYuanBoxListPacket(protocol.ActionIDJieYuanBoxOpen)
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(c, pkt, protocol.ActionIDJieYuanBoxList)
	cancel()
	if err != nil || resp == nil {
		return 0, false
	}
	boxes, pErr := protocol.ParseJieYuanBoxListResult(resp.Payload)
	if pErr != nil {
		return 0, false
	}
	for _, b := range boxes {
		if b.ItemID == itemID {
			return b.Count, true
		}
	}
	return 0, true
}

// jieyuanPriority 结缘开箱调度优先级：高于天帝宝库(200)，登录后第一批执行。
//
// 2026-11-22 实测：同一账号用 probe 新登录后立即执行
// 232/30 -> 528/1 -> 232/34 成功（3938 15->5）；而日常批次数分钟后才执行时，
// 528/1 载荷与抓包完全一致仍被服务端静默丢弃。因此 528/1 上下文对会话时长/
// 前序请求敏感，必须尽早执行；同时 232/30 之前不能插入 0/6 等功能探测请求。
var jieyuanPriority = scheduler.PhasePriority(scheduler.PhaseEarlyWindow, 50)

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
		jieyuanPriority,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			// 不在列表前做功能开放(0/6)探测：官方抓包中登录后第一个结缘请求就是
			// 232/30，任何前置业务请求都会改变 528/1 的上下文并在本服被静默丢弃
			// （2026-11-22 实测）。功能未开放时列表为空，下方直接返回。

			slog.Info("正在执行结缘任务: 结缘宝箱自动开启 (每批最多10个，严格排除传说宝箱)",
				"role_id", session.RoleID(),
				"max_batch_size", maxBatch,
			)

			// 注意：开箱前不要插入 2/41 背包查询。2026-11-22 实机对照
			// /tmp/jieyuan2.pcapng：官方链路是 232/30 -> 528/1 -> 232/34，
			// 中间没有任何其他请求；在列表之后插入 2/41 会让 528/1 直接 EOF，
			// 在列表之前插入则 528/1 被静默丢弃。背包空格只在开箱无回包时
			// 才补查（见下方失败分支），不干扰结缘上下文。

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

			// 2. 严格遵循规则: 除了传说 (3942)，其他都开；当天已被标记不可开启的箱型跳过。
			openable := protocol.FilterOpenableJieYuanBoxes(boxes)
			filtered := openable[:0]
			for _, b := range openable {
				if jiYuanBoxSkippedToday(session.RoleID(), b.ItemID) {
					slog.Info("结缘宝箱箱型暂不可开启，跳过", "role_id", session.RoleID(),
						"item_id", b.ItemID, "count", b.Count)
					continue
				}
				filtered = append(filtered, b)
			}
			openable = filtered
			if len(openable) == 0 {
				// 所有可开箱型都在 6 小时跳过期内：按最早到期时间唤醒，
				// 而不是等下一轮批量唤醒（否则到期后可能数小时无人重试）。
				var earliest time.Time
				for _, b := range boxes {
					if exp, ok := jiYuanBoxSkipExpiry(session.RoleID(), b.ItemID); ok && (earliest.IsZero() || exp.Before(earliest)) {
						earliest = exp
					}
				}
				if !earliest.IsZero() && time.Until(earliest) > 0 {
					slog.Info("无待开启的非传说结缘宝箱，等待跳过到期", "total_kinds", len(boxes),
						"wake_at", earliest)
					return scheduler.NewCooldownUntil(earliest, "结缘宝箱等待跳过到期")
				}
				slog.Info("无待开启的非传说结缘宝箱", "total_kinds", len(boxes))
				return nil
			}

			var totalOpened int

			// altQualityTried 记录已做过「0 档自动合成」变体探测的箱型，每进程每种一次。
			altQualityTried := make(map[int32]bool, len(openable))
			for _, box := range openable {
				remaining := box.Count
				maxBatchesPerItem := 200 // 单个道具最大批次上限（每次只开 1 个），防止无限循环
				batchCount := 0

				for remaining > 0 && batchCount < maxBatchesPerItem {
					batchCount++

					// 每次 232/34 开箱前先发送 528/1 set_setting [0x07 0x04]。
					// 其尾部 PrevAct 由会话层链到「上一条发送的请求」；开箱前若插入过
					// 背包查询 (2/41)，锚点会是 2/41 而不是列表 232/30。
					ctxResp, cErr := callJieYuanContext(ctx, session)
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

					// 232/34 的第二参数是自动合成档位（不是数量），见 protocol.BuildJieYuanBoxOpenPacket。
					// 尾部 PrevAct 由会话层填充为紧邻的 528/1。
					openPkt := protocol.BuildJieYuanBoxOpenPacket(box.ItemID,
						protocol.JieYuanBoxAutoMergeQuality)
					oCtx, oCancel := context.WithTimeout(ctx, 8*time.Second)
					openResp, oErr := session.Call(oCtx, openPkt, protocol.ActionIDJieYuanBoxOpen)
					oCancel()

					if oErr != nil || openResp == nil {
						// 服务端可能已经处理但没回包：先核对库存个数。
						afterCount, ok := fetchJieYuanBoxCount(ctx, session, box.ItemID)
						if ok && afterCount < remaining {
							openedNow := remaining - afterCount
							totalOpened += int(openedNow)
							slog.Info("结缘宝箱开箱无回包但库存已减少，按成功处理",
								"role_id", session.RoleID(),
								"item_id", box.ItemID,
								"before", remaining, "after", afterCount,
								"err", oErr)
							remaining = afterCount
							kit.Sleep(ctx, 50*time.Millisecond)
							continue
						}
						// 变体探测：普通结缘宝箱 (3937) 在 autoMergeQuality=4 下长期被
						// 静默丢弃，而同轮的 3938/3941 正常。用 0 档（不自动合成）对
						// 同一箱型重试一次，确认是否与合成档位相关；每种箱型每进程只试一次。
						if ok && afterCount >= remaining && !altQualityTried[box.ItemID] {
							altQualityTried[box.ItemID] = true
							slog.Info("结缘宝箱开箱无回包，改用 0 档自动合成重试一次",
								"role_id", session.RoleID(), "item_id", box.ItemID, "before", remaining)
							altResp, altErr := jieYuanOpenWithQuality(ctx, session, box.ItemID, 0)
							if altResp != nil {
								altAfter, altOK := fetchJieYuanBoxCount(ctx, session, box.ItemID)
								slog.Info("结缘宝箱 0 档重试结果",
									"role_id", session.RoleID(), "item_id", box.ItemID,
									"reply_hex", hex.EncodeToString(altResp.Payload),
									"after", altAfter, "verify_ok", altOK)
								if altOK && altAfter < remaining {
									totalOpened += int(remaining - altAfter)
									remaining = altAfter
									kit.Sleep(ctx, 50*time.Millisecond)
									continue
								}
							} else {
								slog.Info("结缘宝箱 0 档重试无回包",
									"role_id", session.RoleID(), "item_id", box.ItemID, "err", altErr)
							}
						}
						// 先排除背包已满：满仓时服务端同样静默丢弃开箱，不能把箱型误标
						// 6 小时跳过。2/41 放在 232/34 失败之后查询，不会影响 528/1
						// 的结缘上下文。
						if pack, pErr := fetchJieYuanPack(ctx, session); pErr == nil && pack != nil {
							freeSlots := int(pack.PackCapacity) - len(pack.PackItems)
							if freeSlots <= 0 {
								slog.Info("结缘宝箱开箱无回包且背包已满，等待背包清理后再开",
									"role_id", session.RoleID(),
									"capacity", pack.PackCapacity, "pack_items", len(pack.PackItems))
								return scheduler.ErrBagFull
							}
						}
						// 库存不变：该箱型可能本身需要元宝/其他条件（如高级/至尊箱），
						// 服务端静默丢弃。按错误记录并标记当天跳过，继续开启其余箱型。
						slog.Warn("结缘宝箱开箱无回包且库存数量未变化，6 小时内跳过该箱型",
							"role_id", session.RoleID(),
							"item_id", box.ItemID,
							"before", remaining, "after", afterCount,
							"verify_ok", ok,
							"recent_rx", fmt.Sprintf("%X", session.RecentRxActions()),
							"last_sent", fmt.Sprintf("0x%08X", session.LastActionID()),
							"err", oErr,
						)
						markJiYuanBoxSkipped(session.RoleID(), box.ItemID)
						break
					}

					openCode, openParseErr := protocol.ParseJieYuanBoxOpenResult(openResp.Payload)
					if openParseErr != nil {
						slog.Warn("解析结缘宝箱开箱结果失败，终止本轮",
							"role_id", session.RoleID(), "err", openParseErr,
							"payload_len", len(openResp.Payload))
						return nil
					}
					if openCode != protocol.JieYuanBoxOpenResultSuccess {
						// 非成功码（道具不足/背包不足/非活动时间/购买次数不足等）不得盲目重试；
						// 已开出的部分保留，本轮结束。
						slog.Info("结缘宝箱开箱结束",
							"role_id", session.RoleID(),
							"item_id", box.ItemID,
							"remaining", remaining,
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

					kit.Sleep(ctx, 50*time.Millisecond)
				}

				slog.Info("结缘宝箱批次处理完成",
					"item_id", box.ItemID,
					"opened", box.Count-remaining,
					"legendary_skipped", true,
				)
			}

			slog.Info("结缘宝箱批量开启流程完成", "role_id", session.RoleID(), "total_opened", totalOpened)
			// 本轮一个都没开成（全部箱型被重新标记跳过）时，按最早到期时间唤醒，
			// 避免 Cron 下一次唤醒被排到下一轮批量唤醒而漏掉到期时刻。
			if totalOpened == 0 {
				var earliest time.Time
				for _, b := range openable {
					if exp, ok := jiYuanBoxSkipExpiry(session.RoleID(), b.ItemID); ok && (earliest.IsZero() || exp.Before(earliest)) {
						earliest = exp
					}
				}
				if !earliest.IsZero() {
					slog.Info("结缘宝箱本轮未开启，等待跳过到期", "role_id", session.RoleID(),
						"wake_at", earliest)
					return scheduler.NewCooldownUntil(earliest, "结缘宝箱等待跳过到期")
				}
			}
			return nil
		},
	)
}
