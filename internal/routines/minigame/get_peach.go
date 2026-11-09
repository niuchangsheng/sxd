// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现「摘仙桃」(Mod_GetPeach_Base, Module 40) 自动化：
// 先摘取已召唤的仙桃，再在 call_coin_times > 0 时召唤新的仙桃并逐一摘取。
// 召唤只在铜钱/免费次数可用时执行；call_coin_times == 0 需要消耗元宝，
// 自动化在任何情况下都不会调用。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// maxGetPeachHarvest 单轮最多摘取的仙桃数量，防止服务端异常时死循环。
	maxGetPeachHarvest = 10
	// maxGetPeachCalls 单轮最多召唤次数。正常每日次数远小于该值；
	// 若服务端下发异常大的计数，以此上限熔断。
	maxGetPeachCalls = 5
	// getPeachCallTimeout 单次协议请求超时。
	getPeachCallTimeout = 10 * time.Second
	// getPeachWarTimeout 摘取仙桃包含战报下发，超时放宽。
	getPeachWarTimeout = 20 * time.Second
	// getPeachWarCDQueryTimeout 战斗冷却查询超时。
	getPeachWarCDQueryTimeout = 6 * time.Second
	// maxGetPeachWarCDWait 单次战斗冷却的最大等待时长。
	// 线上实测摘取后 CD 仅 2 秒；超过该上限说明服务端处于异常长冷却，
	// 直接结束本轮，交由下一次定点唤醒继续。
	maxGetPeachWarCDWait = 15 * time.Second
	// getPeachPriority 摘仙桃调度优先级。
	//
	// 召唤仙桃对应每日活跃度任务 64，需要在长耗时日常任务之前尽早完成，
	// 并与叶公好龙一致避开后续场景切换可能造成的模块请求丢弃。
	getPeachPriority scheduler.RoutinePriority = 130
)

// NewGetPeachRoutine 构造每日摘仙桃任务。
//
// 官方客户端流程 (com.controllers.GetPeachController / com.views.GetPeachView)：
//  1. peach_info 读取 peachLv / peachNum / is_can_batch / is_can_call_peach；
//  2. player_info 读取 call_coin_times / call_need_coin 等；
//  3. peachNum > 0 时调用 get_peach 摘取；
//  4. call_coin_times > 0 时调用 call_peach 召唤，随后再次摘取；
//  5. 每次召唤获得每日活跃度任务 64「召唤仙桃」进度。
//
// 自动化永不调用 buy_monkeys (50 元宝) 与 batch_get_peach，也绝不在
// call_coin_times == 0 时召唤。
func NewGetPeachRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"get_peach",
		"minigame",
		scheduler.ScheduleCron,
		getPeachPriority,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			if session.State() != client.StateActive {
				return nil
			}
			slog.Info("正在执行益智任务: 摘仙桃", "role_id", session.RoleID())

			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDGetPeach)
			if fErr != nil {
				slog.Info("摘仙桃功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("摘仙桃功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDGetPeach)
				return nil
			}

			call := func(pkt *protocol.Packet, actionID uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, actionID)
				if err != nil {
					slog.Info("摘仙桃协议无回包，跳过本轮",
						"role_id", session.RoleID(), "action", actionID, "err", err)
					return nil
				}
				return resp
			}

			fetchPanel := func() *protocol.GetPeachPanel {
				resp := call(protocol.BuildGetPeachInfoPacket(session.LastActionID()),
					protocol.ActionIDGetPeachInfo, getPeachCallTimeout)
				if resp == nil {
					return nil
				}
				panel, err := protocol.ParseGetPeachPanelResult(resp.Payload)
				if err != nil {
					slog.Warn("解析摘仙桃面板失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				return panel
			}

			fetchPlayer := func() *protocol.GetPeachPlayerInfo {
				resp := call(protocol.BuildGetPeachPlayerInfoPacket(session.LastActionID()),
					protocol.ActionIDGetPeachPlayerInfo, getPeachCallTimeout)
				if resp == nil {
					return nil
				}
				info, err := protocol.ParseGetPeachPlayerInfoResult(resp.Payload)
				if err != nil {
					slog.Warn("解析摘仙桃玩家信息失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				return info
			}

			// harvestOnce 摘取一棵仙桃，返回 (是否继续, 是否成功)。
			harvestOnce := func() (bool, bool) {
				resp := call(protocol.BuildGetPeachHarvestPacket(session.LastActionID()),
					protocol.ActionIDGetPeachHarvest, getPeachWarTimeout)
				if resp == nil {
					return false, false
				}
				res, err := protocol.ParseGetPeachHarvestResult(resp.Payload)
				if err != nil {
					slog.Warn("解析摘取仙桃回包失败", "role_id", session.RoleID(), "err", err)
					return false, false
				}
				switch res.Result {
				case protocol.GetPeachResultSuccess:
					slog.Info("摘取仙桃成功", "role_id", session.RoleID(), "war_exp", res.WarExp)
					return true, true
				case protocol.GetPeachResultWarCD:
					slog.Info("摘取仙桃战斗冷却中，结束本轮", "role_id", session.RoleID())
					return false, false
				case protocol.GetPeachResultNotEnoughPeachCount:
					slog.Info("没有可摘取的仙桃，结束本轮", "role_id", session.RoleID())
					return false, false
				default:
					slog.Info("摘取仙桃未成功，结束本轮", "role_id", session.RoleID(),
						"result", res.Result, "result_text", protocol.GetPeachResultText(res.Result))
					return false, false
				}
			}

			// waitHarvestCD 对齐官方客户端 get_peach 前的战斗冷却检查。
			// 官方 GetPeachView.get_peach 会先调用
			// toolbar.checkWarCdTime(callBack, WarCdTimeType.Peach)，
			// 冷却未结束时不会发包；否则第二次摘取会被服务端判为失败。
			waitHarvestCD := func() bool {
				query := func() int32 {
					resp := call(protocol.BuildPlayerGetWarCdTimePacket(
						protocol.WarCdTypePeach, session.LastActionID()),
						protocol.ActionIDPlayerGetWarCdTime, getPeachWarCDQueryTimeout)
					if resp == nil {
						// 冷却诊断接口异常时不阻断摘取主流程。
						return -1
					}
					cd, err := protocol.ParsePlayerGetWarCdTimeResult(resp.Payload)
					if err != nil {
						slog.Warn("解析摘仙桃战斗冷却失败", "role_id", session.RoleID(), "err", err)
						return -1
					}
					return cd
				}

				cd := query()
				if cd <= 0 {
					return true
				}
				wait := time.Duration(cd)*time.Second + 300*time.Millisecond
				if wait > maxGetPeachWarCDWait {
					slog.Info("摘仙桃战斗冷却较长，结束本轮",
						"role_id", session.RoleID(), "cd_seconds", cd)
					return false
				}
				slog.Info("摘仙桃等待战斗冷却", "role_id", session.RoleID(), "cd_seconds", cd)
				select {
				case <-ctx.Done():
					return false
				case <-time.After(wait):
				}
				if remaining := query(); remaining > 0 {
					slog.Info("摘仙桃战斗冷却仍未结束，结束本轮",
						"role_id", session.RoleID(), "cd_seconds", remaining)
					return false
				}
				return true
			}

			// harvestAll 摘取面板上已召唤的全部仙桃。
			harvestAll := func(panel *protocol.GetPeachPanel) (int, bool) {
				if panel == nil {
					return 0, false
				}
				done := 0
				for done < int(panel.PeachNum) && done < maxGetPeachHarvest {
					if ctx.Err() != nil {
						return done, false
					}
					if !waitHarvestCD() {
						return done, false
					}
					cont, ok := harvestOnce()
					if !ok {
						return done, false
					}
					done++
					if !cont {
						break
					}
				}
				return done, true
			}

			panel := fetchPanel()
			if panel == nil {
				return nil
			}
			slog.Info("摘仙桃面板",
				"role_id", session.RoleID(),
				"peach_lv", panel.PeachLv,
				"peach_num", panel.PeachNum,
				"can_batch", panel.CanBatch,
				"can_call", panel.CanCall,
			)

			// 1. 先摘取已召唤的仙桃，避免占用召唤位。
			harvested, _ := harvestAll(panel)
			if harvested > 0 {
				slog.Info("摘仙桃已处理存量", "role_id", session.RoleID(), "harvested", harvested)
			}

			// 2. 重新读取面板与玩家信息，确认召唤资格。
			if panel = fetchPanel(); panel == nil {
				return nil
			}
			if !panel.CanCall {
				// 刚摘完存量仙桃时服务端仍处于 2 秒战斗冷却，can_call_peach 会暂时为 0。
				// 官方客户端先等 checkWarCdTime(Peach) 冷却结束再刷新面板，不能直接放弃。
				slog.Info("服务端暂不允许召唤仙桃，等待战斗冷却后重试", "role_id", session.RoleID())
				if waitHarvestCD() {
					panel = fetchPanel()
				}
			}
			if panel == nil {
				return nil
			}
			if !panel.CanCall {
				slog.Info("服务端当前不允许召唤仙桃，跳过", "role_id", session.RoleID())
				return nil
			}
			player := fetchPlayer()
			if player == nil {
				return nil
			}
			slog.Info("摘仙桃玩家信息",
				"role_id", session.RoleID(),
				"call_coin_times", player.CallCoinTimes,
				"call_ingot_times", player.CallIngotTimes,
				"call_need_coin", player.CallNeedCoin,
				"call_need_ingot", player.CallNeedIngot,
				"have_monkey_num", player.HaveMonkeyNum,
			)
			if player.CallCoinTimes == 0 {
				slog.Info("摘仙桃无免费/铜钱召唤次数，跳过 (不使用元宝)", "role_id", session.RoleID())
				return nil
			}

			// 3. 使用铜钱/免费次数召唤，并在每次召唤后摘取，便于继续下一次召唤。
			calls := 0
			for calls < int(player.CallCoinTimes) && calls < maxGetPeachCalls {
				if ctx.Err() != nil {
					return nil
				}
				prevTimes := player.CallCoinTimes
				resp := call(protocol.BuildGetPeachCallPacket(),
					protocol.ActionIDGetPeachCall, getPeachCallTimeout)
				if resp == nil {
					return nil
				}
				res, err := protocol.ParseGetPeachCallResult(resp.Payload)
				if err != nil {
					slog.Warn("解析召唤仙桃回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				switch res.Result {
				case protocol.GetPeachResultSuccess:
					calls++
					slog.Info("召唤仙桃成功", "role_id", session.RoleID(), "call_index", calls)
				case protocol.GetPeachResultFailure:
					slog.Info("召唤仙桃失败，结束本轮", "role_id", session.RoleID(),
						"result", res.Result, "result_text", protocol.GetPeachResultText(res.Result))
					return nil
				case protocol.GetPeachResultNoEnoughCoin:
					slog.Info("召唤仙桃铜钱不足，结束本轮", "role_id", session.RoleID())
					return nil
				case protocol.GetPeachResultNoEnoughIngot:
					// call_coin_times > 0 时不应出现；若出现说明服务端语义异常，立即停止且不重试。
					slog.Warn("召唤仙桃返回元宝不足，已停止以避免元宝消耗", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("召唤仙桃未成功，结束本轮", "role_id", session.RoleID(),
						"result", res.Result, "result_text", protocol.GetPeachResultText(res.Result))
					return nil
				}

				// 召唤成功后立刻摘取，否则下一次 call_peach 会因存在未摘仙桃而失败。
				if newPanel := fetchPanel(); newPanel != nil {
					harvestAll(newPanel)
				}

				// 重新读取召唤次数；若未递减则熔断，防止服务端异常导致重复扣费。
				next := fetchPlayer()
				if next == nil {
					break
				}
				player = next
				if player.CallCoinTimes >= prevTimes {
					slog.Info("摘仙桃召唤次数未递减，熔断本轮",
						"role_id", session.RoleID(), "before", prevTimes, "after", player.CallCoinTimes)
					break
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("摘仙桃巡检完成", "role_id", session.RoleID(), "calls", calls)
			return nil
		},
	)
}
