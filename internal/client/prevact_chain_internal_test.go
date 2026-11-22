package client

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestBusinessAnchor_RewindsToAckAfterDrop 验证带参请求超时（服务端静默丢弃）后，
// 下一次带参请求的 PrevAct 锚点回退到服务端最后确认的动作，而不是继续使用
// 已被丢弃的动作，从而避免链污染导致后续请求连环被丢。
func TestBusinessAnchor_RewindsToAckAfterDrop(t *testing.T) {
	const (
		actA uint32 = 0x00040000
		actB uint32 = 0x00040001 // 该请求会被静默丢弃
		actC uint32 = 0x00040003
		actD uint32 = 0x00040004
	)

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	tails := make(map[uint32]uint32)
	var tailMu chan struct{} = make(chan struct{}, 1)

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			if len(pkt.Payload) >= 4 {
				tail := binary.BigEndian.Uint32(pkt.Payload[len(pkt.Payload)-4:])
				tailMu <- struct{}{}
				tails[pkt.ActionID] = tail
				<-tailMu
			}
			// actB 静默丢弃：不回包。
			if pkt.ActionID != actB {
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, []byte{0x00}))
			}
		}
	}()

	s := NewRoleSession(SessionConfig{
		RoleID:     "role-chain-rewind",
		ServerAddr: "mock-pipe",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	chainPkt := func(act uint32) *protocol.Packet {
		return &protocol.Packet{ActionID: act, Payload: make([]byte, 4), HasPrevAct: true}
	}

	// A：正常回包，成为最后确认动作。
	if _, err := s.Call(ctx, chainPkt(actA), actA); err != nil {
		t.Fatalf("call A: %v", err)
	}
	if got := s.LastAckActionID(); got != actA {
		t.Fatalf("lastAck = 0x%08X, want A", got)
	}

	// B：服务端静默丢弃，触发链失步。
	bCtx, bCancel := context.WithTimeout(ctx, 120*time.Millisecond)
	_, _ = s.Call(bCtx, chainPkt(actB), actB)
	bCancel()
	if !s.chainDirty.Load() {
		t.Fatal("B 超时后应标记链失步")
	}

	// C：锚点应回退到 A（最后确认），而不是 B（被丢弃）。
	if _, err := s.Call(ctx, chainPkt(actC), actC); err != nil {
		t.Fatalf("call C: %v", err)
	}
	tailMu <- struct{}{}
	tailC := tails[actC]
	<-tailMu
	if tailC != actA {
		t.Fatalf("C 的 PrevAct = 0x%08X, want A(0x%08X)", tailC, actA)
	}

	// D：C 已确认后链恢复，锚点应为 C。
	if _, err := s.Call(ctx, chainPkt(actD), actD); err != nil {
		t.Fatalf("call D: %v", err)
	}
	tailMu <- struct{}{}
	tailD := tails[actD]
	<-tailMu
	if tailD != actC {
		t.Fatalf("D 的 PrevAct = 0x%08X, want C(0x%08X)", tailD, actC)
	}
}
