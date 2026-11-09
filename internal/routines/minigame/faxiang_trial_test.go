package minigame

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func faxiangTaskPayload() []byte {
	w := protocol.NewWriter().WriteUint8(protocol.FaxiangTrialSuccess)
	w.WriteUint16(1).WriteInt32(11).WriteInt32(1).WriteInt32(0)
	return w.Bytes()
}

func faxiangScorePayload() []byte {
	w := protocol.NewWriter().WriteUint8(protocol.FaxiangTrialSuccess)
	w.WriteInt32(100).WriteUint16(1).WriteInt32(3)
	return w.Bytes()
}

func TestFaxiangTrialRoutine_ClaimsAndChallenges(t *testing.T) {
	var taskAward, scoreAward, challenge atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangTrialPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		case protocol.ActionIDFaxiangTrialMonsterConfig:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		case protocol.ActionIDFaxiangTrialTaskPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, faxiangTaskPayload()))
		case protocol.ActionIDFaxiangTrialTaskAward:
			taskAward.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		case protocol.ActionIDFaxiangTrialScorePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, faxiangScorePayload()))
		case protocol.ActionIDFaxiangTrialScoreAward:
			scoreAward.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		case protocol.ActionIDFaxiangTrialStartChallenge:
			challenge.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		}
	})
	defer sess.Close()

	if err := NewFaxiangTrialRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("faxiang trial: %v", err)
	}
	if taskAward.Load() != 1 || scoreAward.Load() != 1 || challenge.Load() != 1 {
		t.Fatalf("calls task=%d score=%d challenge=%d, want 1/1/1",
			taskAward.Load(), scoreAward.Load(), challenge.Load())
	}
}

func TestFaxiangTrialRoutine_NotOpenSkips(t *testing.T) {
	var anyCall atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangTrialPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialFailed}))
		case protocol.ActionIDFaxiangTrialTaskPanel, protocol.ActionIDFaxiangTrialStartChallenge:
			anyCall.Add(1)
		}
	})
	defer sess.Close()

	if err := NewFaxiangTrialRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("faxiang trial: %v", err)
	}
	if anyCall.Load() != 0 {
		t.Fatalf("未开放时不应继续调用, calls = %d", anyCall.Load())
	}
}

func TestFaxiangTrialRoutine_PackFull(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangTrialPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		case protocol.ActionIDFaxiangTrialMonsterConfig:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialSuccess}))
		case protocol.ActionIDFaxiangTrialTaskPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, faxiangTaskPayload()))
		case protocol.ActionIDFaxiangTrialTaskAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangTrialPackFull}))
		}
	})
	defer sess.Close()

	err := NewFaxiangTrialRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
}
