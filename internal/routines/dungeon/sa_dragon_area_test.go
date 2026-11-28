package dungeon

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

func encodeSaDragonAreaInfo(seq, quickSeq, isClear int32) []byte {
	w := protocol.NewWriter()
	w.WriteInt32(seq)
	w.WriteUint16(0) // awards
	w.WriteInt16(10) // complete_count
	w.WriteInt8(0)   // decrease_difficulty
	w.WriteString("")
	w.WriteInt32(0)
	w.WriteString("")
	w.WriteString("")
	w.WriteInt32(0)
	w.WriteString("")
	w.WriteInt32(0) // special_pattern
	w.WriteInt32(0) // monster_team_id
	w.WriteUint16(0) // dragonballs
	w.WriteUint16(0) // monsters
	w.WriteInt32(0)  // five_elem
	w.WriteInt32(quickSeq)
	w.WriteInt32(quickSeq)
	w.WriteInt32(isClear)
	return w.Bytes()
}

func startMockPipeSession(t *testing.T, handler func(pkt *protocol.Packet, server net.Conn)) *client.RoleSession {
	t.Helper()
	c, s := net.Pipe()
	t.Cleanup(func() {
		_ = c.Close()
		_ = s.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(s)
			if err != nil {
				return
			}
			handler(pkt, s)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:            "role-dragon-test",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: time.Hour,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c, nil
		},
		Authenticator: func(rs *client.RoleSession) error { return nil },
	})
	t.Cleanup(sess.Close)
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if err := sess.AwaitActive(context.Background()); err != nil {
		t.Fatalf("await active: %v", err)
	}
	return sess
}

func writeFunctionOpenList(server net.Conn, funcIDs ...int32) {
	w := protocol.NewWriter()
	w.WriteUint16(uint16(len(funcIDs)))
	for _, id := range funcIDs {
		w.WriteInt32(id)
		w.WriteUint8(1)
	}
	_ = protocol.WritePacket(server, protocol.NewPacket(protocol.ActionIDPlayerGetFunctions, w.Bytes()))
}

// TestSaDragonArea_QuickSweepAndFightProgression 验证成功路径：
// 先执行 362/4 免费一键扫荡，再逐层 362/2 挑战通关直至 is_dragon_clear=1。
func TestSaDragonArea_QuickSweepAndFightProgression(t *testing.T) {
	mainSess := startMockPipeSession(t, func(pkt *protocol.Packet, s net.Conn) {
		if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
			writeFunctionOpenList(s, protocol.FunctionIDSaintArea)
		}
	})

	var curSeq atomic.Int32
	curSeq.Store(1)
	var isClear atomic.Int32
	var quickCalls atomic.Int32
	var fightCalls atomic.Int32

	saSess := startMockPipeSession(t, func(pkt *protocol.Packet, s net.Conn) {
		switch pkt.ActionID {
		case protocol.ActionIDSaDragonAreaInfo:
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaInfo,
				encodeSaDragonAreaInfo(curSeq.Load(), 5, isClear.Load())))
		case protocol.ActionIDSaDragonAreaQuickFight:
			quickCalls.Add(1)
			curSeq.Store(6)
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(5)
			w.WriteUint16(1)
			w.WriteInt32(1001)
			w.WriteInt32(50)
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaQuickFight, w.Bytes()))
		case protocol.ActionIDSaDragonAreaFight:
			n := fightCalls.Add(1)
			if n == 1 {
				curSeq.Store(7)
			} else {
				isClear.Store(1)
			}
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaFight,
				[]byte{protocol.SaDragonAreaResultSuccess}))
		}
	})

	origOpen := openDragonAreaSaintSession
	openDragonAreaSaintSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return saSess, nil
	}
	t.Cleanup(func() { openDragonAreaSaintSession = origOpen })

	err := NewSaDragonAreaRoutine().Execute(context.Background(), mainSess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("全部通关后期望 ErrAttemptsExhausted, got %v", err)
	}
	if quickCalls.Load() != 1 {
		t.Fatalf("期望执行 1 次免费一键扫荡, got %d", quickCalls.Load())
	}
	if fightCalls.Load() != 2 {
		t.Fatalf("期望执行 2 次逐层挑战, got %d", fightCalls.Load())
	}
}

// TestSaDragonArea_FailureResultCodes 验证失败语义分流（圣域凭据缺失、功能未开放、连续战败冷却）。
func TestSaDragonArea_FailureResultCodes(t *testing.T) {
	mainSess := startMockPipeSession(t, func(pkt *protocol.Packet, s net.Conn) {
		if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
			writeFunctionOpenList(s, protocol.FunctionIDSaintArea)
		}
	})

	// 1. 圣域凭据为空（如 s1 未开放圣域）-> ErrConditionNotMet
	origOpen := openDragonAreaSaintSession
	t.Cleanup(func() { openDragonAreaSaintSession = origOpen })
	openDragonAreaSaintSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return nil, client.ErrSaintAreaCredentialMissing
	}
	if err := NewSaDragonAreaRoutine().Execute(context.Background(), mainSess, nil); !errors.Is(err, scheduler.ErrConditionNotMet) {
		t.Fatalf("圣域凭据为空期望 ErrConditionNotMet, got %v", err)
	}

	// 2. fight 返回 SAINT_AREA_CLOSE (3) -> ErrConditionNotMet
	saClose := startMockPipeSession(t, func(pkt *protocol.Packet, s net.Conn) {
		switch pkt.ActionID {
		case protocol.ActionIDSaDragonAreaInfo:
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaInfo,
				encodeSaDragonAreaInfo(6, 0, 0)))
		case protocol.ActionIDSaDragonAreaFight:
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaFight,
				[]byte{protocol.SaDragonAreaResultSaintAreaClose}))
		}
	})
	openDragonAreaSaintSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return saClose, nil
	}
	if err := NewSaDragonAreaRoutine().Execute(context.Background(), mainSess, nil); !errors.Is(err, scheduler.ErrConditionNotMet) {
		t.Fatalf("SAINT_AREA_CLOSE 期望 ErrConditionNotMet, got %v", err)
	}

	// 3. fight 连续 3 次战败（seq 未推进）-> CooldownError
	var lossFights atomic.Int32
	saLoss := startMockPipeSession(t, func(pkt *protocol.Packet, s net.Conn) {
		switch pkt.ActionID {
		case protocol.ActionIDSaDragonAreaInfo:
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaInfo,
				encodeSaDragonAreaInfo(10, 0, 0)))
		case protocol.ActionIDSaDragonAreaFight:
			lossFights.Add(1)
			_ = protocol.WritePacket(s, protocol.NewPacket(protocol.ActionIDSaDragonAreaFight,
				[]byte{protocol.SaDragonAreaResultSuccess}))
		}
	})
	openDragonAreaSaintSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return saLoss, nil
	}
	err := NewSaDragonAreaRoutine().Execute(context.Background(), mainSess, nil)
	var cdErr *scheduler.CooldownError
	if !errors.As(err, &cdErr) {
		t.Fatalf("连续战败期望 CooldownError, got %v", err)
	}
	if lossFights.Load() != saDragonAreaMaxLossRetries {
		t.Fatalf("期望重试 %d 次战败后退出, got %d", saDragonAreaMaxLossRetries, lossFights.Load())
	}
}
