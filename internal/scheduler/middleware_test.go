package scheduler_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func newMockPipeSession(t *testing.T, roleID string, handler func(conn net.Conn, pkt *protocol.Packet)) *client.RoleSession {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if handler != nil {
				handler(c2, pkt)
			}
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        roleID,
		ServerAddr:    "mock-pipe",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return c1, nil },
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("session not active: %s", sess.State())
	}
	return sess
}

func TestBaseRoutine_WithReturnToTownMiddleware(t *testing.T) {
	var enterCalls atomic.Int32
	sess := newMockPipeSession(t, "role-mw-town", func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDTownEnter {
			enterCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
	})

	r := scheduler.NewBaseRoutine(
		"battle_mw",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PhasePriority(scheduler.PhaseEarlyBattle, 10),
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			return nil
		},
		scheduler.WithReturnToTown(2*time.Second),
	)
	if !r.NeedsReturnToTown() {
		t.Fatal("expected NeedsReturnToTown() == true")
	}

	if err := r.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := enterCalls.Load(); got != 1 {
		t.Fatalf("expected 1 ReturnToTown enter_town call, got %d", got)
	}
}

func TestBaseRoutine_WithFunctionIDGuard(t *testing.T) {
	const testFuncID int32 = 157

	t.Run("closed function skips business logic", func(t *testing.T) {
		sess := newMockPipeSession(t, "role-mw-fn-closed", func(conn net.Conn, pkt *protocol.Packet) {
			if pkt.ActionID == protocol.ActionIDPlayerCheckFunctionOpen {
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenFalse}))
			}
		})
		var ran atomic.Bool
		r := scheduler.NewBaseRoutine(
			"guarded_closed",
			"farming",
			scheduler.ScheduleCron,
			scheduler.PriorityNormal,
			0,
			func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
				ran.Store(true)
				return nil
			},
			scheduler.WithFunctionID(testFuncID),
		)
		if r.FunctionID() != testFuncID {
			t.Fatalf("FunctionID() = %d, want %d", r.FunctionID(), testFuncID)
		}
		err := r.Execute(context.Background(), sess, nil)
		if !errors.Is(err, scheduler.ErrConditionNotMet) {
			t.Fatalf("err = %v, want ErrConditionNotMet", err)
		}
		if ran.Load() {
			t.Fatal("business fn should not execute when function is closed")
		}
	})

	t.Run("open function executes business logic", func(t *testing.T) {
		sess := newMockPipeSession(t, "role-mw-fn-open", func(conn net.Conn, pkt *protocol.Packet) {
			if pkt.ActionID == protocol.ActionIDPlayerCheckFunctionOpen {
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PlayerFunctionOpenTrue}))
			}
		})
		var ran atomic.Bool
		r := scheduler.NewBaseRoutine(
			"guarded_open",
			"farming",
			scheduler.ScheduleCron,
			scheduler.PriorityNormal,
			0,
			func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
				ran.Store(true)
				return nil
			},
			scheduler.WithFunctionID(testFuncID),
		)
		if err := r.Execute(context.Background(), sess, nil); err != nil {
			t.Fatalf("Execute err = %v, want nil", err)
		}
		if !ran.Load() {
			t.Fatal("business fn should execute when function is open")
		}
	})
}

func TestRoleScheduler_IsolatedDropTrackerAndEscalation(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: time.Millisecond,
		MaxDuration: 2 * time.Millisecond,
	})

	// 两个独立调度器使用相同 roleID 与 routine 名，验证 DropTracker 完全隔离不互相污染。
	schedA := scheduler.NewRoleScheduler("same-role", nil, fastJitter)
	schedB := scheduler.NewRoleScheduler("same-role", nil, fastJitter)

	var callsA atomic.Int32
	rA := scheduler.NewBaseRoutine(
		"drop_test",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			callsA.Add(1)
			return context.DeadlineExceeded
		},
		scheduler.WithSilentDropPolicy(2, time.Second),
	)
	_ = schedA.Register(rA)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := schedA.Start(ctx); err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer schedA.Stop()

	// 等待连续 2 次超时触发长熔断（第 1 次按 1s 短重试，第 2 次达到阈值=2 进入长挂起）。
	deadline := time.Now().Add(4 * time.Second)
	for callsA.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if callsA.Load() < 2 {
		t.Fatalf("expected >=2 runs to trigger escalation, got %d", callsA.Load())
	}
	// 等待调度器写回状态
	time.Sleep(50 * time.Millisecond)

	if got := schedA.DropTracker().Count("same-role", "drop_test"); got < 2 {
		t.Fatalf("schedA drop count = %d, want >=2", got)
	}
	if got := schedB.DropTracker().Count("same-role", "drop_test"); got != 0 {
		t.Fatalf("schedB drop count leaked: got %d, want 0", got)
	}

	_, nextA, _, found := schedA.GetRoutineState("drop_test")
	if !found {
		t.Fatal("routine state not found")
	}
	// 升级为长熔断后，nextRun 应至少大于 1s 短重试（接近 min(次日 00:05, 6h)）
	wantCap := scheduler.NextDailyReset(time.Now())
	if maxCap := time.Now().Add(6 * time.Hour); wantCap.After(maxCap) {
		wantCap = maxCap
	}
	if diff := nextA.Sub(wantCap); diff > 2*time.Second || diff < -2*time.Second {
		t.Fatalf("escalated nextRun = %v, want ~%v", nextA, wantCap)
	}
}

func TestRoleScheduler_EventDrivenWakeOnRegisterAndEnable(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: time.Millisecond,
		MaxDuration: 2 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-wake", nil, fastJitter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	// 先让调度器进入空队列完全休眠状态
	time.Sleep(30 * time.Millisecond)

	done := make(chan struct{})
	start := time.Now()
	_ = sched.Register(scheduler.NewFuncRoutine(
		"late_registered",
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			close(done)
			return nil
		},
	))

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("late Register took too long to wake scheduler: %v", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not wake on dynamic Register")
	}
}

func TestRoleScheduler_ConcurrentGetRoutineStateRaceFree(t *testing.T) {
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: time.Millisecond,
		MaxDuration: 2 * time.Millisecond,
	})
	sched := scheduler.NewRoleScheduler("role-race", nil, fastJitter)
	_ = sched.Register(scheduler.NewFuncRoutine(
		"fast_loop",
		scheduler.PriorityNormal,
		5*time.Millisecond,
		func(ctx context.Context, s *client.RoleSession, j *scheduler.Jitter) error {
			return nil
		},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	stopRead := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopRead:
				return
			default:
				_, _, _, _ = sched.GetRoutineState("fast_loop")
			}
		}
	}()

	time.Sleep(150 * time.Millisecond)
	close(stopRead)
}

func TestPhasePriority_Ordering(t *testing.T) {
	if !(scheduler.PhasePriority(scheduler.PhaseSystem, 0) >
		scheduler.PhasePriority(scheduler.PhaseTownSensitive, 30) &&
		scheduler.PhasePriority(scheduler.PhaseTownSensitive, 0) >
			scheduler.PhasePriority(scheduler.PhaseEarlyBattle, 50) &&
		scheduler.PhasePriority(scheduler.PhaseEarlyBattle, 0) >
			scheduler.PhasePriority(scheduler.PhaseCoreLoop, 45) &&
		scheduler.PhasePriority(scheduler.PhaseCoreLoop, 0) >
			scheduler.PhasePriority(scheduler.PhaseEarlyWindow, 50) &&
		scheduler.PhasePriority(scheduler.PhaseEarlyWindow, 0) >
			scheduler.PhasePriority(scheduler.PhaseHighPriority, 30)) {
		t.Fatal("phase priority hierarchy violated")
	}
}
