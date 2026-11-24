// Package scheduler 提供拟人随机抖动任务调度引擎，协调各类日常活动任务的执行时机，防止风控异常。
//
// @author Ateng
// @since 2026-10-08
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
)

// sessionLostRetryInterval 例行任务执行中途会话断开后的短重试间隔。
//
// 断线并不代表玩法已完成：剩余次数需要尽快补做，而不是等下一次 Cron
// （2~4 小时）。调度器在会话非激活期间本来就不会拉起任务，因此该间隔
// 只会在会话恢复后生效。
//
// 2026-11-24 实机统计：服务端对主服会话约 225 秒强制断链，重连成功耗时
// 中位数 23 秒（主动重建）/ 76 秒（被动踢线），p90 约 111 秒。原先的 5 分钟
// 会让被打断的玩法白等 4 分钟以上；90 秒覆盖绝大多数重连窗口，且会话未
// 恢复时任务会在 BaseRoutine 前置检查处立即返回，不产生额外请求。
const sessionLostRetryInterval = 90 * time.Second

type scheduledItem struct {
	routine      ActivityRoutine
	nextRun      time.Time
	lastRun      time.Time
	coolingUntil time.Time
	// holdUntil 语义冷却/定点唤醒截止时间（CooldownError、次数耗尽、体力/背包熔断）。
	// TriggerBatch 定点批量唤醒不得在 holdUntil 之前把任务提前拉起，否则会丢弃
	// 「冷却结束再唤醒」「活动开启后再唤醒」的语义。
	holdUntil time.Time
	runCount  int
	disabled  bool
}

// RoleScheduler 为特定角色会话提供专属且隔离的任务调度器。
type RoleScheduler struct {
	roleID  string
	session *client.RoleSession
	jitter  *Jitter
	drops   *DropTracker

	items   []*scheduledItem
	itemsMu sync.RWMutex
	wakeCh  chan struct{}

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	stateMu sync.Mutex
}

// NewRoleScheduler 创建新的角色任务调度器。
func NewRoleScheduler(roleID string, session *client.RoleSession, jitter *Jitter) *RoleScheduler {
	if jitter == nil {
		jitter = NewJitter(JitterConfig{})
	}
	return &RoleScheduler{
		roleID:  roleID,
		session: session,
		jitter:  jitter,
		drops:   NewDropTracker(),
		items:   make([]*scheduledItem, 0),
		wakeCh:  make(chan struct{}, 1),
	}
}

// DropTracker 返回该角色调度器私有的连续静默丢包计数器。
func (s *RoleScheduler) DropTracker() *DropTracker {
	return s.drops
}

func (s *RoleScheduler) notifyWake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// SetRoutineEnabled 动态启用或禁用特定任务。
func (s *RoleScheduler) SetRoutineEnabled(name string, enabled bool) bool {
	s.itemsMu.Lock()
	found := false
	for _, it := range s.items {
		if it.routine.Name() == name {
			it.disabled = !enabled
			found = true
			break
		}
	}
	s.itemsMu.Unlock()

	if found && enabled {
		s.notifyWake()
	}
	return found
}

// Register 注册一个日常活动任务到调度队列。
func (s *RoleScheduler) Register(routine ActivityRoutine) error {
	if routine == nil {
		return errors.New("scheduler: cannot register nil routine")
	}

	s.itemsMu.Lock()
	item := &scheduledItem{
		routine: routine,
		nextRun: time.Now(), // 注册后立即进入就绪候选
	}
	s.items = append(s.items, item)
	s.itemsMu.Unlock()

	s.notifyWake()
	return nil
}

// Start 启动调度器后台协程。
func (s *RoleScheduler) Start(parentCtx context.Context) error {
	s.stateMu.Lock()
	if s.running {
		s.stateMu.Unlock()
		return errors.New("scheduler: already running")
	}
	s.running = true
	s.ctx, s.cancel = context.WithCancel(parentCtx)
	s.stateMu.Unlock()

	s.wg.Add(1)
	go s.runLoop()

	return nil
}

// Stop 停止调度器并等待正在执行的任务结束。
func (s *RoleScheduler) Stop() {
	s.stateMu.Lock()
	if !s.running {
		s.stateMu.Unlock()
		return
	}
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
	s.stateMu.Unlock()

	s.wg.Wait()
}

func (s *RoleScheduler) runLoop() {
	defer s.wg.Done()

	// idleLogged 保证每轮任务批次结束后只输出一次待机日志，避免静默轮询被误判为卡死。
	idleLogged := false

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		item, wait := s.pickNextReadyOrWait()
		if item == nil {
			if !idleLogged {
				idleLogged = true
				slog.Info("本轮任务已全部执行完毕，进入待机等待下一次定点唤醒",
					"role_id", s.roleID,
				)
			}
			// 事件驱动待机：按最近到期时间精准休眠，收到 Register/TriggerBatch/Enable 信号立即唤醒。
			if wait < 0 {
				select {
				case <-s.ctx.Done():
					return
				case <-s.wakeCh:
					continue
				}
			}
			timer := time.NewTimer(wait)
			select {
			case <-s.ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-s.wakeCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				continue
			case <-timer.C:
				continue
			}
		}
		idleLogged = false

		// 零假动作铁律：会话未处于 StateActive 激活态时严禁触发业务 Routine
		if s.session != nil && s.session.State() != client.StateActive {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-s.ctx.Done():
				timer.Stop()
				return
			case <-s.wakeCh:
				timer.Stop()
				continue
			case <-timer.C:
				continue
			}
		}

		// 动作前强制注入拟人防封抖动延迟
		if err := s.jitter.Wait(s.ctx); err != nil {
			return
		}

		routineName := item.routine.Name()
		slog.Debug("开始执行日常任务",
			"role_id", s.roleID,
			"routine", routineName,
			"priority", item.routine.Priority(),
		)

		// 注入当前调度器隔离的 DropTracker，避免不同调度器/测试间的静默丢包计数串扰。
		execCtx := WithDropTracker(s.ctx, s.drops)
		execErr := item.routine.Execute(execCtx, s.session, s.jitter)
		// 例程内部可能把「断线」当成普通查询失败而 return nil（例如
		// IsFunctionOpen 报 session is not active 后直接跳过），Cron 任务就会
		// 直接等到次日。这里统一兜底：返回 nil 但会话已不在激活态，按断线
		// 处理，90 秒后重连重试。
		if execErr == nil && s.session != nil && s.session.State() != client.StateActive {
			execErr = client.ErrNotConnected
		}

		now := time.Now()
		s.itemsMu.Lock()
		item.lastRun = now
		item.runCount++

		cooldownScheduled := false
		coolingScheduled := false
		retrySoon := false
		if execErr != nil {
			var cdErr *CooldownError
			if errors.As(execErr, &cdErr) {
				// 服务端冷却/活动开启时间：按指定时间唤醒，而不是等到下一次 24h Cron。
				item.nextRun = cdErr.Until
				item.coolingUntil = time.Time{}
				item.holdUntil = cdErr.Until
				cooldownScheduled = true
				slog.Info("任务进入定时等待，将在指定时间后唤醒",
					"role_id", s.roleID,
					"routine", routineName,
					"wake_at", cdErr.Until,
					"reason", cdErr.Reason,
				)
			} else if errors.Is(execErr, ErrAttemptsExhausted) {
				// 次数耗尽：挂起至次日凌晨 00:00:10
				tomorrow := now.AddDate(0, 0, 1)
				midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 10, 0, tomorrow.Location())
				item.coolingUntil = midnight
				item.holdUntil = midnight
				coolingScheduled = true
				slog.Info("日常任务次数耗尽，进入智能熔断挂起",
					"role_id", s.roleID,
					"routine", routineName,
					"cooling_until", midnight,
				)
			} else if errors.Is(execErr, ErrStaminaDepleted) {
				// 体力不足：挂起 30 分钟
				item.coolingUntil = now.Add(30 * time.Minute)
				item.holdUntil = item.coolingUntil
				coolingScheduled = true
				slog.Info("体力耗尽，进入智能熔断挂起",
					"role_id", s.roleID,
					"routine", routineName,
					"cooling_until", item.coolingUntil,
				)
			} else if errors.Is(execErr, client.ErrNotConnected) || errors.Is(execErr, client.ErrSessionClosed) {
				// 执行中途断线：本轮确实没做完（可能还有剩余次数），
				// 90 秒后即重试；调度器在重连期间本身也不会拉起任务。
				retrySoon = true
				slog.Info("会话重连中，任务本轮跳过并稍后重试",
					"role_id", s.roleID,
					"routine", routineName,
					"retry_in", sessionLostRetryInterval,
				)
			} else if errors.Is(execErr, context.DeadlineExceeded) {
				// 会话非激活导致的超时属于网络中断而非服务端静默丢包，不计入丢包熔断。
				if s.session != nil && s.session.State() != client.StateActive {
					retrySoon = true
					slog.Info("会话断开期间任务超时，转为短重试",
						"role_id", s.roleID,
						"routine", routineName,
						"retry_in", sessionLostRetryInterval,
					)
				} else {
					threshold, retry := defaultSilentDropThreshold, defaultSilentDropRetry
					if lr, ok := item.routine.(LifecycleRoutine); ok {
						if t, r := lr.SilentDropPolicy(); t > 0 || r > 0 {
							if t > 0 {
								threshold = t
							}
							if r > 0 {
								retry = r
							}
						}
					}
					n := s.drops.Note(s.roleID, routineName)
					until, escalated := s.drops.BackoffUntil(now, n, threshold, retry)
					item.coolingUntil = until
					item.holdUntil = until
					coolingScheduled = true
					if escalated {
						slog.Info("任务连续静默丢包达到阈值，进入长熔断挂起",
							"role_id", s.roleID,
							"routine", routineName,
							"consecutive_drops", n,
							"cooling_until", until,
							"error", execErr,
						)
					} else {
						slog.Info("任务请求超时/被服务端静默丢弃，稍后短重试",
							"role_id", s.roleID,
							"routine", routineName,
							"consecutive_drops", n,
							"cooling_until", until,
							"error", execErr,
						)
					}
				}
			} else if errors.Is(execErr, ErrConditionNotMet) {
				// 等级/资源/功能条件不满足属于正常业务分支，不应记为执行异常。
				slog.Info("任务前置条件不满足，本轮跳过",
					"role_id", s.roleID,
					"routine", routineName,
					"error", execErr,
				)
			} else if errors.Is(execErr, ErrBagFull) {
				// 背包已满：挂起 10 分钟
				item.coolingUntil = now.Add(10 * time.Minute)
				item.holdUntil = item.coolingUntil
				coolingScheduled = true
				slog.Info("背包已满，日常任务休眠待命",
					"role_id", s.roleID,
					"routine", routineName,
					"cooling_until", item.coolingUntil,
				)
			} else {
				slog.Warn("日常任务执行异常",
					"role_id", s.roleID,
					"routine", routineName,
					"error", execErr,
				)
			}
		}
		if execErr == nil {
			// 本次执行成功：清除语义冷却与调度器级连续丢包计数，恢复常规调度。
			item.holdUntil = time.Time{}
			s.drops.Reset(s.roleID, routineName)
		}

		// 处理执行周期更新或单次任务移除
		isCron := false
		if det, ok := item.routine.(DetailedRoutine); ok && det.ScheduleType() == ScheduleCron {
			isCron = true
		}
		if cooldownScheduled {
			// nextRun 已由 CooldownError.Until 指定，保留该唤醒时间。
		} else if coolingScheduled {
			// 体力/背包/次数/静默丢包熔断：冷却结束即重新就绪，而不是被 24h Cron 推迟一天。
			item.nextRun = item.coolingUntil
		} else if retrySoon {
			// 会话中途断线：本轮可能还有未消耗的次数，按短间隔重试。
			item.nextRun = now.Add(sessionLostRetryInterval)
		} else if item.routine.Interval() > 0 {
			item.nextRun = now.Add(item.routine.Interval())
		} else if isCron {
			// 定点任务等待下一次 TriggerBatch 唤醒或次日重置
			item.nextRun = now.Add(24 * time.Hour)
		} else {
			s.removeItemLocked(item)
		}
		s.itemsMu.Unlock()
	}
}

// pickNextReadyOrWait 选出最高优先级的就绪任务；若当前无就绪任务，返回距离最近
// 待唤醒任务的时间间隔（无启用的待执行任务时返回 -1）。
func (s *RoleScheduler) pickNextReadyOrWait() (*scheduledItem, time.Duration) {
	s.itemsMu.RLock()
	defer s.itemsMu.RUnlock()

	now := time.Now()
	var candidates []*scheduledItem
	var earliest time.Time

	for _, it := range s.items {
		if it.disabled {
			continue
		}
		readyAt := it.nextRun
		if !it.coolingUntil.IsZero() && it.coolingUntil.After(readyAt) {
			readyAt = it.coolingUntil
		}
		if !now.Before(readyAt) {
			candidates = append(candidates, it)
			continue
		}
		if earliest.IsZero() || readyAt.Before(earliest) {
			earliest = readyAt
		}
	}

	if len(candidates) > 0 {
		// 按优先级从大到小稳定排序 (高优先级优先执行，同优先级保持注册顺序)
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].routine.Priority() > candidates[j].routine.Priority()
		})
		return candidates[0], 0
	}

	if earliest.IsZero() {
		return nil, -1
	}
	wait := earliest.Sub(now)
	if wait <= 0 {
		wait = time.Millisecond
	}
	return nil, wait
}

// TriggerBatch 唤醒指定调度类型的全部任务（例如定点 Cron 重置），重置其执行时钟与冷却状态。
//
// 例外：仍处于语义冷却/定点唤醒窗口（holdUntil 未到期）的任务不会被提前拉起，
// 以保证「冷却结束再唤醒」「活动开启后再唤醒」「次数耗尽等次日重置」的语义；
// 到期后自动恢复正常批量调度。
func (s *RoleScheduler) TriggerBatch(schedType ScheduleType) int {
	s.itemsMu.Lock()

	count := 0
	now := time.Now()
	for _, it := range s.items {
		matched := false
		if det, ok := it.routine.(DetailedRoutine); ok {
			if det.ScheduleType() == schedType {
				matched = true
			}
		} else if schedType == ScheduleLoop {
			matched = true
		}

		if !matched {
			continue
		}
		if !it.holdUntil.IsZero() && now.Before(it.holdUntil) {
			// 语义冷却未结束：保留原 nextRun/coolingUntil，不提前唤醒。
			continue
		}
		it.holdUntil = time.Time{}
		it.nextRun = now
		it.coolingUntil = time.Time{} // 解除冷却
		count++
	}
	s.itemsMu.Unlock()

	if count > 0 {
		s.notifyWake()
	}
	return count
}

// GetRoutineState 查询指定任务的执行状态与冷却情况。
func (s *RoleScheduler) GetRoutineState(name string) (lastRun time.Time, nextRun time.Time, coolingUntil time.Time, found bool) {
	s.itemsMu.RLock()
	defer s.itemsMu.RUnlock()

	for _, it := range s.items {
		if it.routine.Name() == name {
			return it.lastRun, it.nextRun, it.coolingUntil, true
		}
	}
	return time.Time{}, time.Time{}, time.Time{}, false
}

func (s *RoleScheduler) removeItemLocked(target *scheduledItem) {
	for i, it := range s.items {
		if it == target {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return
		}
	}
}
