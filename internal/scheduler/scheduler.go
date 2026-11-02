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

type scheduledItem struct {
	routine      ActivityRoutine
	nextRun      time.Time
	lastRun      time.Time
	coolingUntil time.Time
	runCount     int
	disabled     bool
}

// RoleScheduler 为特定角色会话提供专属且隔离的任务调度器。
type RoleScheduler struct {
	roleID  string
	session *client.RoleSession
	jitter  *Jitter

	items   []*scheduledItem
	itemsMu sync.RWMutex

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
		items:   make([]*scheduledItem, 0),
	}
}

// SetRoutineEnabled 动态启用或禁用特定任务。
func (s *RoleScheduler) SetRoutineEnabled(name string, enabled bool) bool {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()

	for _, it := range s.items {
		if it.routine.Name() == name {
			it.disabled = !enabled
			return true
		}
	}
	return false
}

// Register 注册一个日常活动任务到调度队列。
func (s *RoleScheduler) Register(routine ActivityRoutine) error {
	if routine == nil {
		return errors.New("scheduler: cannot register nil routine")
	}

	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()

	item := &scheduledItem{
		routine: routine,
		nextRun: time.Now(), // 注册后立即进入就绪候选
	}
	s.items = append(s.items, item)
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

		item := s.pickNextReadyItem()
		if item == nil {
			if !idleLogged {
				idleLogged = true
				slog.Info("本轮任务已全部执行完毕，进入待机等待下一次定点唤醒",
					"role_id", s.roleID,
				)
			}
			// 当前无就绪任务，轻度休眠后继续探查
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}
		idleLogged = false

		// 零假动作铁律：会话未处于 StateActive 激活态时严禁触发业务 Routine
		if s.session != nil && s.session.State() != client.StateActive {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}

		// 动作前强制注入拟人防封抖动延迟
		if err := s.jitter.Wait(s.ctx); err != nil {
			return
		}

		slog.Debug("开始执行日常任务",
			"role_id", s.roleID,
			"routine", item.routine.Name(),
			"priority", item.routine.Priority(),
		)

		// 执行任务并捕获业务语义异常
		execErr := item.routine.Execute(s.ctx, s.session, s.jitter)

		item.lastRun = time.Now()
		item.runCount++

		s.itemsMu.Lock()
		if execErr != nil {
			if errors.Is(execErr, ErrAttemptsExhausted) {
				// 次数耗尽：挂起至次日凌晨 00:00:10
				tomorrow := time.Now().AddDate(0, 0, 1)
				midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 10, 0, tomorrow.Location())
				item.coolingUntil = midnight
				slog.Info("日常任务次数耗尽，进入智能熔断挂起",
					"role_id", s.roleID,
					"routine", item.routine.Name(),
					"cooling_until", midnight,
				)
			} else if errors.Is(execErr, ErrStaminaDepleted) {
				// 体力不足：挂起 30 分钟
				item.coolingUntil = time.Now().Add(30 * time.Minute)
				slog.Info("体力耗尽，进入智能熔断挂起",
					"role_id", s.roleID,
					"routine", item.routine.Name(),
					"cooling_until", item.coolingUntil,
				)
			} else if errors.Is(execErr, ErrBagFull) {
				// 背包已满：挂起 10 分钟
				item.coolingUntil = time.Now().Add(10 * time.Minute)
				slog.Warn("背包已满，日常任务休眠待命",
					"role_id", s.roleID,
					"routine", item.routine.Name(),
					"cooling_until", item.coolingUntil,
				)
			} else {
				slog.Warn("日常任务执行异常",
					"role_id", s.roleID,
					"routine", item.routine.Name(),
					"error", execErr,
				)
			}
		}

		// 处理执行周期更新或单次任务移除
		isCron := false
		if det, ok := item.routine.(DetailedRoutine); ok && det.ScheduleType() == ScheduleCron {
			isCron = true
		}
		if item.routine.Interval() > 0 {
			item.nextRun = time.Now().Add(item.routine.Interval())
		} else if isCron {
			// 定点任务等待下一次 TriggerBatch 唤醒或次日重置
			item.nextRun = time.Now().Add(24 * time.Hour)
		} else {
			s.removeItemLocked(item)
		}
		s.itemsMu.Unlock()
	}
}

func (s *RoleScheduler) pickNextReadyItem() *scheduledItem {
	s.itemsMu.RLock()
	defer s.itemsMu.RUnlock()

	now := time.Now()
	var candidates []*scheduledItem
	for _, it := range s.items {
		if it.disabled {
			continue
		}
		// 校验就绪时间与熔断冷却期
		isReady := now.After(it.nextRun) || now.Equal(it.nextRun)
		isCooled := it.coolingUntil.IsZero() || now.After(it.coolingUntil) || now.Equal(it.coolingUntil)

		if isReady && isCooled {
			candidates = append(candidates, it)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// 按优先级从大到小排序 (高优先级优先执行)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].routine.Priority() > candidates[j].routine.Priority()
	})

	return candidates[0]
}

// TriggerBatch 唤醒指定调度类型的全部任务（例如定点 Cron 重置），重置其执行时钟与冷却状态。
func (s *RoleScheduler) TriggerBatch(schedType ScheduleType) int {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()

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

		if matched {
			it.nextRun = now
			it.coolingUntil = time.Time{} // 解除冷却
			count++
		}
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
