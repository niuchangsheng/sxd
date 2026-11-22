// Package pvp 提供竞技场、仙界竞技场、神魔大战、取经劫镖、世界 Boss 与跨服天梯等对抗玩法。
//
// 本文件实现跨服天梯 / 世界 PK (Mod_WorldPk_Base, Module 349) 自动匹配与领奖。
package pvp

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// claimCrossServerLadderRewards 领取跨服天梯每日任务/特殊奖励并开启自动匹配。
//
// 仅在 join_pk 成功后调用；所有购买次数/清冷却接口保持禁用。
// openLadderRewardSession 建立跨服奖励会话（可在测试中替换）。
var openLadderRewardSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenCrossServerSession(ctx)
}

func claimCrossServerLadderRewards(ctx context.Context, session *client.RoleSession) error {
	const callTimeout = 3 * time.Second
	caller := kit.NewCaller(ctx, session, callTimeout)
	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32) *protocol.Packet {
		resp, err := caller.CallOn(s, pkt, act)
		if err != nil {
			slog.Info("跨服PK奖励协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
			return nil
		}
		return resp
	}

	// 任务面板必须在主服读取；跨服/圣域会话会返回 FAILED。
	tasks := make([]protocol.WorldPkTask, 0)
	if resp := call(session, protocol.BuildWorldPkOpenTaskPanelPacket(), protocol.ActionIDWorldPkOpenTaskPanel); resp != nil {
		parsed, pErr := protocol.ParseWorldPkTasks(resp.Payload)
		if pErr != nil {
			slog.Warn("跨服PK任务面板解析失败", "role_id", session.RoleID(), "err", pErr)
		} else {
			tasks = parsed
		}
		// 面板载荷较小，打印原始字节与解析结果，便于核对任务 ID/进度/领取位
		// 是否正确（2026-11-17：15 条可领任务全部返回 FAILED(5)，需排除
		// 「解析错位导致用错 task_id」的可能）。
		head := resp.Payload
		if len(head) > 256 {
			head = head[:256]
		}
		slog.Info("跨服PK任务面板", "role_id", session.RoleID(),
			"payload_len", len(resp.Payload), "hex", hex.EncodeToString(head),
			"tasks", tasks)
	}
	// 回包条目为 [task_id, progress, claimed]；claimed=1 表示已领取。
	// 未领取的任务都尝试一次，未完成时服务端返回 NOT_COMPLETED(22) 并继续。
	claimable := make([]protocol.WorldPkTask, 0)
	for _, t := range tasks {
		if t.State == 0 {
			claimable = append(claimable, t)
		}
	}

	// 领取先走主服：任务面板只在主服可用，主服与面板同源；若主服返回
	// FAILED(5)，再懒加载仙界跨服会话重试一次同一任务并记录两者的结果码，
	// 用于确认「领奖应在哪个会话」。2026-11-17 起不再默认走跨服。
	reward := session
	crossTried := false
	var crossSess *client.RoleSession

	claimed := 0
	// taskResults 统计每个任务领取的结果码，用于定位「面板有可领但实际领不到」。
	taskResults := make(map[uint8]int, 4)
	for _, t := range claimable {
		if ctx.Err() != nil {
			break
		}
		awardPkt := protocol.BuildWorldPkTaskAwardPacket(t.ID)
		awardResp := call(reward, awardPkt, protocol.ActionIDWorldPkTaskAward)
		if awardResp == nil {
			break
		}
		code, cErr := protocol.ParseActionFirstByte(awardResp.Payload, "跨服PK任务奖励")
		if cErr != nil {
			break
		}
		if code == protocol.WorldPkRewardFailed && reward == session && !crossTried {
			crossTried = true
			cross, ccErr := openLadderRewardSession(ctx, session)
			if ccErr != nil || cross == nil {
				slog.Warn("跨服PK奖励会话建立失败", "role_id", session.RoleID(), "err", ccErr)
			} else {
				if cross != session {
					crossSess = cross
					defer crossSess.Close()
				}
				if retry := call(cross, awardPkt, protocol.ActionIDWorldPkTaskAward); retry != nil {
					if c2, e := protocol.ParseActionFirstByte(retry.Payload, "跨服PK任务奖励(跨服重试)"); e == nil {
						slog.Info("跨服PK任务奖励主服返回失败，跨服会话重试",
							"role_id", session.RoleID(), "task_id", t.ID,
							"main_result", code, "cross_result", c2)
						reward = cross
						code = c2
					}
				}
			}
		}
		taskResults[code]++
		switch code {
		case protocol.WorldPkRewardSuccess:
			claimed++
			slog.Info("跨服PK任务奖励领取成功", "role_id", session.RoleID(), "task_id", t.ID, "progress", t.Progress)
		case protocol.WorldPkRewardNotCompleted, protocol.WorldPkRewardFailed, protocol.WorldPkRewardTimesLimit:
			// 未完成/已领取，继续下一条。
		case protocol.WorldPkRewardPackFull:
			return scheduler.ErrBagFull
		default:
			slog.Info("跨服PK任务奖励未成功", "role_id", session.RoleID(), "task_id", t.ID, "result", code)
		}
	}

	if resp := call(reward, protocol.BuildWorldPkReceiveSpecialPacket(), protocol.ActionIDWorldPkReceiveSpecial); resp != nil {
		if code, err := protocol.ParseActionFirstByte(resp.Payload, "跨服PK特殊奖励"); err == nil {
			switch code {
			case protocol.WorldPkRewardSuccess:
				slog.Info("跨服PK特殊奖励领取成功", "role_id", session.RoleID())
			case protocol.WorldPkRewardPackFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("跨服PK特殊奖励未领取", "role_id", session.RoleID(), "result", code)
			}
		}
	}

	if resp := call(session, protocol.BuildWorldPkSetAutoMatchPacket(true), protocol.ActionIDWorldPkSetAutoMatch); resp != nil {
		if code, err := protocol.ParseActionFirstByte(resp.Payload, "跨服PK自动匹配"); err == nil {
			slog.Info("跨服PK自动匹配设置", "role_id", session.RoleID(), "result", code)
		}
	}

	if resp := call(reward, protocol.BuildWorldPkAwardPanelPacket(), protocol.ActionIDWorldPkAwardPanel); resp != nil {
		if code, aErr := protocol.ParseActionFirstByte(resp.Payload, "跨服PK奖励面板"); aErr == nil {
			slog.Info("跨服PK奖励面板", "role_id", session.RoleID(), "result", code)
		}
	}
	slog.Info("跨服PK奖励巡检完成", "role_id", session.RoleID(), "claimed", claimed,
		"claimable", len(claimable), "task_results", taskResults)
	return nil
}

// NewCrossServerLadderRoutine 构造跨服天梯争霸挑战任务。
// NewCrossServerLadderRoutine 构造跨服 PK 自动参赛任务 (Module 349)。
//
// 先查询活动状态，仅在 ON 时加入 PK；OFF/超级城镇关闭时跳过。
// 不购买挑战次数、不清除冷却。
// ladderWarStats 统计跨服PK战斗结算推送 (349/5) 的数量与首字节结果分布。
// 目前协议只确认首字节为结果码，具体含义待抓包对照，但对局胜负数量可直接
// 用「战报条数」衡量（每场匹配成功应收到一条）。
type ladderWarStats struct {
	mu       sync.Mutex
	total    int
	byResult map[uint8]int
}

var ladderWarStatsMap sync.Map // *client.RoleSession -> *ladderWarStats

// ladderWarStatsFor 返回会话对应的战报统计；首次创建时注册 349/5 处理器。
func ladderWarStatsFor(session *client.RoleSession) *ladderWarStats {
	if v, ok := ladderWarStatsMap.Load(session); ok {
		return v.(*ladderWarStats)
	}
	st := &ladderWarStats{byResult: map[uint8]int{}}
	actual, loaded := ladderWarStatsMap.LoadOrStore(session, st)
	if loaded {
		return actual.(*ladderWarStats)
	}
	session.RegisterHandler(protocol.ActionIDWorldPkWarReport, func(pkt *protocol.Packet) {
		if len(pkt.Payload) == 0 {
			return
		}
		st.mu.Lock()
		st.total++
		st.byResult[pkt.Payload[0]]++
		st.mu.Unlock()
	})
	return st
}

// snapshot 返回累计战报数与结果码分布副本。
func (s *ladderWarStats) snapshot() (int, map[uint8]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uint8]int, len(s.byResult))
	for k, v := range s.byResult {
		out[k] = v
	}
	return s.total, out
}

func NewCrossServerLadderRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"cross_server_ladder",
		"pvp",
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

			sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
			statusResp, err := session.Call(sCtx, protocol.BuildWorldPkStatusPacket(), protocol.ActionIDWorldPkStatus)
			sCancel()
			if err != nil {
				// 阶位赛 12:00/20:00 开放；窗口外服务端会把状态查询也静默丢弃
				// （2026-11-15 19:41 实测）。窗口外直接等下一次开放，窗口内
				// 才按 10 分钟重试，避免整晚每 30 分钟空等。
				now := time.Now()
				if !inWorldPkWindow(now) {
					wake := nextWorldPkOpenAt(now)
					slog.Info("跨服PK状态查询无回包，等待下次开放",
						"role_id", session.RoleID(), "wake_at", wake, "err", err)
					return scheduler.NewCooldown(time.Until(wake), "跨服PK状态查询无回包")
				}
				slog.Info("跨服PK状态查询无回包，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "跨服PK状态查询无回包")
			}
			status, err := protocol.ParseWorldPkStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析跨服PK状态失败: %w", err)
			}
			if status.Status != protocol.WorldPkStatusOn {
				wake := nextWorldPkOpenAt(time.Now())
				slog.Info("跨服PK当前未开放，等待下次开放",
					"role_id", session.RoleID(),
					"status", status.Status,
					"wake_at", wake,
				)
				return scheduler.NewCooldown(time.Until(wake), "跨服PK未开放")
			}

			// 领取奖励必须在 join_pk 之前，避免进入匹配队列后奖励请求被丢弃。
			if rErr := claimCrossServerLadderRewards(ctx, session); rErr != nil {
				return rErr
			}

			// 抓包 (/tmp/jiewei.pcapng)：官方客户端勾选自动匹配 + 跳过战斗后，
			// 约每 1 秒发送一次 join_pk；result=6(TIME_CD) 时回包带剩余秒数，
			// 冷却归零后服务端推送 349/5 战报。每次战斗约 10 秒。
			// 349/16、349/17 回包均为 UByte 结果码，3=SUCCESS。
			// 只有确认成功才记录「已开启」，否则日志会误导后续排查。
			if resp := call3(session, protocol.BuildWorldPkSetAutoMatchPacket(true),
				protocol.ActionIDWorldPkSetAutoMatch); resp != nil {
				if len(resp.Payload) > 0 && resp.Payload[0] == protocol.WorldPkResultSuccess {
					slog.Info("跨服PK自动匹配已开启", "role_id", session.RoleID())
				} else {
					slog.Info("跨服PK自动匹配设置未成功", "role_id", session.RoleID(),
						"result", worldPkResultByte(resp.Payload))
				}
			}
			if resp := call3(session, protocol.BuildWorldPkSetSkipWarPacket(true),
				protocol.ActionIDWorldPkSetSkipWar); resp != nil {
				if len(resp.Payload) > 0 && resp.Payload[0] == protocol.WorldPkResultSuccess {
					slog.Info("跨服PK跳过战斗已开启", "role_id", session.RoleID())
				} else {
					slog.Info("跨服PK跳过战斗设置未成功", "role_id", session.RoleID(),
						"result", worldPkResultByte(resp.Payload))
				}
			}

			// 30 次只是“跳过战斗”的次数上限；超过后仍可自动匹配，只是需要正常
			// 打完战斗。因此按时间预算持续 join，直到服务端返回次数上限/活动关闭。
			warStats := ladderWarStatsFor(session)
			warBefore, _ := warStats.snapshot()
			deadline := time.Now().Add(worldPkRunBudget)
			attempts := 0
			// 战报停滞计数：2026-11-20 20:00 实测 joins=93 但 war_reports 只有 30，
			// 说明 30 场（跳过战斗上限）打完后服务端仍返回 SUCCESS 却不产生战报，
			// 后续 60 多次 join 全是空转。连续 worldPkIdleJoinsLimit 次匹配未见新战报
			// 即判定今日战斗次数已用尽，直接等待下次开放。
			lastWarTotal := warBefore
			idleJoins := 0
			for {
				attempts++
				if ctx.Err() != nil || time.Now().After(deadline) {
					break
				}
				if worldPkTestMaxJoins > 0 && attempts > worldPkTestMaxJoins {
					break
				}
				i := attempts - 1
				jCtx, jCancel := context.WithTimeout(ctx, callTimeout)
				joinResp, jErr := session.Call(jCtx, protocol.BuildWorldPkJoinPacket(), protocol.ActionIDWorldPkJoin)
				jCancel()
				if jErr != nil || joinResp == nil {
					slog.Info("跨服PK加入无回包，结束本轮", "role_id", session.RoleID(), "attempt", i+1, "err", jErr)
					return scheduler.NewCooldown(10*time.Minute, "跨服PK加入无回包")
				}
				result, _, countdown, pErr := protocol.ParseWorldPkJoinDetail(joinResp.Payload)
				if pErr != nil {
					slog.Warn("解析跨服PK加入回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch result {
				case protocol.WorldPkResultSuccess:
					slog.Info("跨服PK匹配成功，等待战斗结算", "role_id", session.RoleID(),
						"attempt", i+1, "countdown", countdown)
					if !waitWorldPk(ctx, worldPkBattleWait) {
						return nil
					}
					if total, _ := warStats.snapshot(); total > lastWarTotal {
						lastWarTotal = total
						idleJoins = 0
					} else {
						idleJoins++
						if idleJoins >= worldPkIdleJoinsLimit {
							wake := nextWorldPkOpenAt(time.Now())
							slog.Info("跨服PK连续匹配无战报，判定今日战斗次数已用尽，等待下次开放",
								"role_id", session.RoleID(),
								"attempt", i+1,
								"war_reports", lastWarTotal,
								"idle_joins", idleJoins,
								"wake_at", wake,
							)
							return scheduler.NewCooldown(time.Until(wake), "跨服PK今日战斗次数已用尽")
						}
					}
				case protocol.WorldPkResultTimeCD:
					// 按服务端倒计时等待，避免 1.2s 一次的紧循环（实测会连续
					// 匹配上百次并占用角色调度协程）。
					wait := worldPkCDDuration(countdown)
					slog.Info("跨服PK冷却中，按倒计时等待", "role_id", session.RoleID(),
						"attempt", i+1, "countdown", countdown, "wait", wait)
					if !waitWorldPk(ctx, wait) {
						return nil
					}
				case protocol.WorldPkResultTimesLimit:
					slog.Info("跨服PK次数上限，结束本轮", "role_id", session.RoleID(), "attempt", i+1)
					return nil
				case protocol.WorldPkResultActivityNotOpen:
					slog.Info("跨服PK活动未开放，结束本轮", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("跨服PK加入未成功，结束本轮", "role_id", session.RoleID(),
						"attempt", i+1, "result", result)
					return nil
				}
			}
			warAfter, warDist := warStats.snapshot()
			slog.Info("跨服PK本轮匹配循环完成", "role_id", session.RoleID(),
				"joins", attempts-1, "budget", worldPkRunBudget,
				"war_reports", warAfter-warBefore, "war_result_dist", warDist)
			return nil
		},
	)
}

// 跨服PK匹配等待时间（单测可缩短）。
// worldPkOpenHours 阶位赛每日开放时刻：中午 12:00 与晚上 20:00（用户确认，
// 2026-11-15 12:07 实测已开放）。
var worldPkOpenHours = []int{12, 20}

// worldPkWindow 阶位赛单次开放窗口时长。窗口内状态查询正常，窗口外服务端会
// 把状态查询也静默丢弃（2026-11-15 19:41 实测）。
//
// 2026-11-20 实测：13:58/13:59 仍可匹配，14:00:00 的 join_pk 立即返回 FAILED(5)，
// 即午间窗口为 12:00–14:00（120 分钟），原 90 分钟会在 13:30–14:00 误判为窗外。
const worldPkWindow = 120 * time.Minute

// worldPkOpenAt 返回当天 hour 点的开放时刻。
func worldPkOpenAt(now time.Time, hour int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
}

// inWorldPkWindow 判断 now 是否处于任一开放窗口内。
func inWorldPkWindow(now time.Time) bool {
	for _, h := range worldPkOpenHours {
		open := worldPkOpenAt(now, h)
		if !now.Before(open) && now.Before(open.Add(worldPkWindow)) {
			return true
		}
	}
	return false
}

// nextWorldPkOpenAt 返回下一次阶位赛开放时刻：当天未过的 12:00/20:00，
// 都已过则次日 12:00。
func nextWorldPkOpenAt(now time.Time) time.Time {
	for _, h := range worldPkOpenHours {
		if open := worldPkOpenAt(now, h); now.Before(open) {
			return open
		}
	}
	return worldPkOpenAt(now.AddDate(0, 0, 1), worldPkOpenHours[0])
}

var (
	worldPkBattleWait = 10 * time.Second
	worldPkCDWait     = 1200 * time.Millisecond
	// worldPkRunBudget 单次例程的匹配时长预算（活动窗口内可持续匹配）。
	worldPkRunBudget = 20 * time.Minute
	// worldPkIdleJoinsLimit 连续多少次匹配未产生新战报即判定战斗次数已用尽。
	worldPkIdleJoinsLimit = 3
	// worldPkTestMaxJoins 仅供单测限制循环次数（>0 生效）。
	worldPkTestMaxJoins = 0
)

// call3 以固定超时发送请求并等待回包。
func call3(session *client.RoleSession, pkt *protocol.Packet, act uint32) *protocol.Packet {
	c, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := session.Call(c, pkt, act)
	if err != nil {
		return nil
	}
	return resp
}

// waitWorldPk 按上下文等待；被取消时返回 false。
// worldPkCDDuration 把服务端返回的冷却秒数转换为等待时长。
// countdown<=0 时退回默认短等待，并额外留 500ms 余量。
func worldPkCDDuration(countdown int32) time.Duration {
	if countdown <= 0 {
		return worldPkCDWait
	}
	wait := time.Duration(countdown)*time.Second + 500*time.Millisecond
	if wait > worldPkRunBudget {
		wait = worldPkRunBudget
	}
	return wait
}

func waitWorldPk(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// worldPkResultByte 提取 UByte 结果码用于日志（空回包返回 -1）。
func worldPkResultByte(payload []byte) int {
	if len(payload) == 0 {
		return -1
	}
	return int(payload[0])
}
