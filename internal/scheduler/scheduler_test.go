// Package scheduler_test 验证任务调度器优先级排序、生命周期、拟人防封抖动与多角色并发隔离。
//
// @author Ateng
// @since 2026-10-08
package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

func TestScheduler_PriorityOrder(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 5 * time.Millisecond,
		MaxDuration: 10 * time.Millisecond,
	})

	sched := scheduler.NewRoleScheduler("role-priority", nil, fastJitter)

	executionOrder := make([]string, 0)
	var mu sync.Mutex

	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, name)
	}

	done := make(chan struct{})

	// 注册低优先级任务
	_ = sched.Register(scheduler.NewFuncRoutine(
		"low_routine",
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			record("low")
			close(done)
			return nil
		},
	))

	// 注册高优先级任务
	_ = sched.Register(scheduler.NewFuncRoutine(
		"high_routine",
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			record("high")
			return nil
		},
	))

	// 注册普通优先级任务
	_ = sched.Register(scheduler.NewFuncRoutine(
		"normal_routine",
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			record("normal")
			return nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("sched.Start failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for routines to complete")
	}

	sched.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 executed routines, got %d", len(executionOrder))
	}
	if executionOrder[0] != "high" || executionOrder[1] != "normal" || executionOrder[2] != "low" {
		t.Errorf("unexpected execution order: %v, expected [high, normal, low]", executionOrder)
	}
}

func TestScheduler_RecurringRoutine(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})

	sched := scheduler.NewRoleScheduler("role-recur", nil, fastJitter)

	var runCount atomic.Int32
	done := make(chan struct{})

	// 周期性任务，每 30ms 执行一次
	_ = sched.Register(scheduler.NewFuncRoutine(
		"herb_garden",
		scheduler.PriorityNormal,
		30*time.Millisecond,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			if runCount.Add(1) >= 3 {
				select {
				case <-done:
				default:
					close(done)
				}
			}
			return nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("sched.Start failed: %v", err)
	}

	select {
	case <-done:
		// 成功周期运行至少 3 次
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for recurring runs, actual: %d", runCount.Load())
	}

	sched.Stop()
}

func TestDispatcher_MultiRoleConcurrentIsolation(t *testing.T) {
	disp := scheduler.NewDispatcher()

	fastJitter := scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	}

	schedA, err := disp.AddRole("role-A", nil, fastJitter)
	if err != nil {
		t.Fatalf("AddRole A failed: %v", err)
	}
	schedB, err := disp.AddRole("role-B", nil, fastJitter)
	if err != nil {
		t.Fatalf("AddRole B failed: %v", err)
	}

	var roleARuns atomic.Int32
	var roleBRuns atomic.Int32
	roleBDone := make(chan struct{})

	// Role A 的任务被模拟网络延迟阻塞较长时间 (150ms)
	_ = schedA.Register(scheduler.NewFuncRoutine(
		"slow_action_A",
		scheduler.PriorityNormal,
		50*time.Millisecond,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			roleARuns.Add(1)
			time.Sleep(150 * time.Millisecond)
			return nil
		},
	))

	// Role B 的任务非常轻快，不受 Role A 阻塞影响
	_ = schedB.Register(scheduler.NewFuncRoutine(
		"fast_action_B",
		scheduler.PriorityNormal,
		20*time.Millisecond,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			if roleBRuns.Add(1) >= 4 {
				select {
				case <-roleBDone:
				default:
					close(roleBDone)
				}
			}
			return nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disp.StartAll(ctx)

	// 等待 Role B 迅速完成 4 次执行
	select {
	case <-roleBDone:
		// 此时验证 Role A 并未拖垮 Role B
		bCount := roleBRuns.Load()
		aCount := roleARuns.Load()
		if bCount < 4 {
			t.Errorf("Role B should have executed >= 4 times, got %d", bCount)
		}
		if aCount >= bCount {
			t.Errorf("Role A (slow) should have executed fewer times than Role B (fast): A=%d, B=%d", aCount, bCount)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Role B to complete multiple runs")
	}

	disp.StopAll()
}

func TestDispatcher_GetAndRemoveRole(t *testing.T) {
	disp := scheduler.NewDispatcher()
	sched, err := disp.AddRole("role-c", nil, scheduler.JitterConfig{})
	if err != nil {
		t.Fatalf("AddRole failed: %v", err)
	}

	if disp.GetRole("role-c") != sched {
		t.Errorf("expected GetRole to return matching scheduler")
	}

	// 重复添加报错
	_, err = disp.AddRole("role-c", nil, scheduler.JitterConfig{})
	if err == nil {
		t.Errorf("expected error adding duplicate role")
	}

	disp.RemoveRole("role-c")
	if disp.GetRole("role-c") != nil {
		t.Errorf("expected nil after RemoveRole")
	}
}

func TestScheduler_ErrorBranches(t *testing.T) {
	sched := scheduler.NewRoleScheduler("role-err", nil, nil)
	// 1. 注册空任务
	if err := sched.Register(nil); err == nil {
		t.Errorf("expected error registering nil routine")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 2. 重复启动
	if err := sched.Start(ctx); err == nil {
		t.Errorf("expected error on duplicate Start")
	}

	sched.Stop()
}

func TestScheduler_SemanticCoolingOff(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 1 * time.Millisecond,
		MaxDuration: 2 * time.Millisecond,
	})

	sched := scheduler.NewRoleScheduler("role-cool", nil, fastJitter)

	var runCount atomic.Int32
	firstRunDone := make(chan struct{})

	// 任务首次运行返回 ErrAttemptsExhausted
	_ = sched.Register(scheduler.NewFuncRoutine(
		"exhausted_routine",
		scheduler.PriorityNormal,
		10*time.Millisecond,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			c := runCount.Add(1)
			if c == 1 {
				close(firstRunDone)
				return scheduler.ErrAttemptsExhausted
			}
			return nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	<-firstRunDone

	// 稍作等待，验证由于进入熔断冷却期，该任务不会继续高频执行
	time.Sleep(50 * time.Millisecond)
	if count := runCount.Load(); count != 1 {
		t.Fatalf("期望任务处于熔断挂起状态且仅执行 1 次，实际执行了 %d 次", count)
	}

	// 检查任务状态确实处于冷却期
	_, _, coolingUntil, found := sched.GetRoutineState("exhausted_routine")
	if !found {
		t.Fatal("未找到任务状态")
	}
	if coolingUntil.Before(time.Now()) {
		t.Fatalf("期望冷却截止时间在未来，实际为 %v", coolingUntil)
	}

	// 次数耗尽的熔断属于语义冷却：批量唤醒（如盘中定点重置）不得提前解除，
	// 必须等到次日重置点（holdUntil）之后才恢复；到期恢复见
	// TestScheduler_TriggerBatchAfterHoldExpires。
	if woken := sched.TriggerBatch(scheduler.ScheduleLoop); woken != 0 {
		t.Fatalf("次数耗尽冷却期间不应被批量唤醒, woken=%d", woken)
	}
	time.Sleep(50 * time.Millisecond)
	if count := runCount.Load(); count != 1 {
		t.Fatalf("冷却期间任务不应恢复执行，实际执行次数: %d", count)
	}

	sched.Stop()
}

func TestScheduler_SetRoutineEnabledAndDispatcherBatch(t *testing.T) {
	disp := scheduler.NewDispatcher()
	sched, err := disp.AddRole("role-batch", nil, scheduler.JitterConfig{
		MinDuration: 5 * time.Millisecond,
		MaxDuration: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("AddRole failed: %v", err)
	}

	runChan := make(chan struct{}, 10)
	var runCount atomic.Int32
	cronRoutine := scheduler.NewBaseRoutine(
		"cron_task",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			runCount.Add(1)
			runChan <- struct{}{}
			return nil
		},
	)
	_ = sched.Register(cronRoutine)

	// 测试禁用
	sched.SetRoutineEnabled("cron_task", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp.StartAll(ctx)

	time.Sleep(30 * time.Millisecond)
	if count := runCount.Load(); count != 0 {
		t.Fatalf("已禁用任务不应执行，实际执行了 %d 次", count)
	}

	// 重新启用
	sched.SetRoutineEnabled("cron_task", true)
	// 通过 Dispatcher 批量触发 Cron 任务
	triggered := disp.TriggerBatch(scheduler.ScheduleCron)
	if triggered == 0 {
		t.Fatal("Dispatcher.TriggerBatch 预期触发 >= 1 项任务")
	}

	select {
	case <-runChan:
		// 成功执行
	case <-time.After(100 * time.Millisecond):
		t.Fatal("启用并触发后任务超时未被执行")
	}

	disp.StopAll()
}

// TestScheduler_CooldownErrorSchedulesExactWake 验证 Routine 返回 CooldownError 时，
// 调度器按指定时间唤醒，而不是把 Cron 任务固定推迟 24 小时。
func TestScheduler_CooldownErrorSchedulesExactWake(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-cooldown", nil, fastJitter)

	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"cooldown_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			if calls.Add(1) == 1 {
				return scheduler.NewCooldown(80*time.Millisecond, "测试冷却")
			}
			return nil
		},
	)
	if err := sched.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("routine 未执行")
	}

	_, next, _, found := sched.GetRoutineState("cooldown_routine")
	if !found {
		t.Fatal("未找到 routine 状态")
	}
	if wait := time.Until(next); wait > 5*time.Second {
		t.Fatalf("CooldownError 未按指定时间唤醒, next=%v (wait=%v)", next, wait)
	}

	deadline = time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatalf("冷却结束后未再次执行, calls=%d", calls.Load())
	}
}

// TestScheduler_TriggerBatchRespectsCooldown 验证定点 Cron 批量唤醒不会把仍处于
// 「冷却结束再唤醒」窗口的任务提前拉起（例如 4 小时建队拒绝冷却）。
func TestScheduler_TriggerBatchRespectsCooldown(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-batch-hold", nil, fastJitter)

	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"hold_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			calls.Add(1)
			return scheduler.NewCooldown(time.Hour, "服务端拒绝")
		},
	)
	if err := sched.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("routine 未执行")
	}

	if triggered := sched.TriggerBatch(scheduler.ScheduleCron); triggered != 0 {
		t.Fatalf("冷却中的任务不应被批量唤醒, triggered=%d", triggered)
	}
	_, next, _, found := sched.GetRoutineState("hold_routine")
	if !found {
		t.Fatal("未找到 routine 状态")
	}
	if wait := time.Until(next); wait < 55*time.Minute {
		t.Fatalf("批量唤醒后 nextRun 被提前, wait=%v", wait)
	}
	if calls.Load() != 1 {
		t.Fatalf("routine 被执行了 %d 次, want 1", calls.Load())
	}
}

// TestScheduler_TriggerBatchAfterHoldExpires 验证冷却到期后批量唤醒恢复正常。
func TestScheduler_TriggerBatchAfterHoldExpires(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-batch-expire", nil, fastJitter)

	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"expire_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			if calls.Add(1) == 1 {
				// NewCooldown 下限为 1 秒，测试按 1s 冷却等待。
				return scheduler.NewCooldown(80*time.Millisecond, "短暂冷却")
			}
			return nil
		},
	)
	if err := sched.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("routine 未执行")
	}
	time.Sleep(1200 * time.Millisecond)
	if triggered := sched.TriggerBatch(scheduler.ScheduleCron); triggered != 1 {
		t.Fatalf("冷却到期后应被批量唤醒, triggered=%d", triggered)
	}
	deadline = time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatalf("冷却到期后未再次执行, calls=%d", calls.Load())
	}
}

// TestScheduler_BagFullSchedulesShortRetry 验证 Cron 任务返回 ErrBagFull 时，
// nextRun 跟随 10 分钟冷却，而不是被 24h Cron 推迟到次日。
func TestScheduler_BagFullSchedulesShortRetry(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-bagfull", nil, fastJitter)

	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"bagfull_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			calls.Add(1)
			return scheduler.ErrBagFull
		},
	)
	if err := sched.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("routine 未执行")
	}

	_, next, cooling, found := sched.GetRoutineState("bagfull_routine")
	if !found {
		t.Fatal("未找到 routine 状态")
	}
	if wait := time.Until(next); wait > 15*time.Minute {
		t.Fatalf("背包满后 nextRun 被推迟过久: %v", wait)
	}
	if cooling.IsZero() {
		t.Fatal("背包满应设置 coolingUntil")
	}
}

// TestScheduler_DeadlineExceededRetriesInThirtyMinutes 验证 Routine 返回
// context.DeadlineExceeded（服务端静默丢包）时不再按 Cron 推迟到下一个时段，
// 而是 30 分钟后短重试。
func TestScheduler_DeadlineExceededRetriesInThirtyMinutes(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-timeout", nil, fastJitter)

	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"timeout_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			calls.Add(1)
			return context.DeadlineExceeded
		},
	)
	if err := sched.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("routine 未执行")
	}

	_, next, _, found := sched.GetRoutineState("timeout_routine")
	if !found {
		t.Fatal("未找到 routine 状态")
	}
	if wait := time.Until(next); wait < 25*time.Minute || wait > 35*time.Minute {
		t.Fatalf("静默丢包后应在约 30 分钟后重试, 实际 wait=%v", wait)
	}
}

// TestScheduler_NotConnectedRetriesShortly 验证执行中途断线
// (client.ErrNotConnected) 的 Cron 任务按 90 秒短重试，而不是推迟到下一次
// Cron（2~4 小时）导致剩余次数长期漏做。
func TestScheduler_NotConnectedRetriesShortly(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 2 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-not-connected", nil, fastJitter)

	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"not_connected_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			calls.Add(1)
			return client.ErrNotConnected
		},
	)
	if err := sched.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("routine 未执行")
	}

	_, next, _, found := sched.GetRoutineState("not_connected_routine")
	if !found {
		t.Fatal("未找到 routine 状态")
	}
	if wait := time.Until(next); wait < 60*time.Second || wait > 2*time.Minute {
		t.Fatalf("断线后应在约 90 秒后重试, 实际 wait=%v", wait)
	}
}

// TestBaseRoutine_InactiveSessionReturnsErrNotConnected 验证会话未激活（断线重连窗口）
// 时不再把任务当作成功返回 nil，而是返回 ErrNotConnected，交由调度器 90 秒后重试。
func TestBaseRoutine_InactiveSessionReturnsErrNotConnected(t *testing.T) {
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-inactive",
		ServerAddr: "mock",
	})
	var calls atomic.Int32
	r := scheduler.NewBaseRoutine(
		"inactive_routine",
		"test",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(context.Context, *client.RoleSession, *scheduler.Jitter) error {
			calls.Add(1)
			return nil
		},
	)

	err := r.Execute(context.Background(), sess, nil)
	if !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("非激活会话应返回 ErrNotConnected, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("非激活会话不应执行业务逻辑, calls=%d", calls.Load())
	}
}
