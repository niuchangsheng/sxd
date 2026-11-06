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

func TestFindBeastRoutine_DoAndClaim(t *testing.T) {
	var doCalls, getCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSpaceFindOpen:
			_ = protocol.WritePacket(conn, protocol.BuildSpaceFindOpenResultPacket(protocol.SpaceFindOpenInfo{
				Count: 1, CallGoldenIngot: 150,
			}))
		case protocol.ActionIDSpaceFindDo:
			doCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSpaceFindDoResultPacket(protocol.SpaceFindDoResult{
				Result: protocol.SpaceFindResultSuccess, ItemID: 1825, Golden: 5,
			}))
		case protocol.ActionIDSpaceFindGet:
			getCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSpaceFindGetResultPacket(protocol.SpaceFindGetResult{
				Result: protocol.SpaceFindResultSuccess,
				Goods:  []protocol.SpaceFindGoods{{ItemID: 1860, Num: 4}},
				Count:  0, CanBuyCount: 40,
			}))
		}
	})
	defer sess.Close()

	if err := NewFindBeastRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("find beast: %v", err)
	}
	if doCalls.Load() != 1 || getCalls.Load() != 1 {
		t.Fatalf("expected 1 do + 1 get, got do=%d get=%d", doCalls.Load(), getCalls.Load())
	}
}

func TestFindBeastRoutine_PackFullBranchesToCircuitBreaker(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSpaceFindOpen:
			_ = protocol.WritePacket(conn, protocol.BuildSpaceFindOpenResultPacket(protocol.SpaceFindOpenInfo{Count: 1}))
		case protocol.ActionIDSpaceFindDo:
			_ = protocol.WritePacket(conn, protocol.BuildSpaceFindDoResultPacket(protocol.SpaceFindDoResult{
				Result: protocol.SpaceFindResultPackFull,
			}))
		}
	})
	defer sess.Close()

	err := NewFindBeastRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("expected ErrBagFull, got %v", err)
	}
}

func TestDuobaoMountainRoutine_DigFreeCount(t *testing.T) {
	var digCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDMysticalTreasurePanel:
			_ = protocol.WritePacket(conn, protocol.BuildMysticalTreasurePanelResultPacket(protocol.MysticalTreasurePanel{
				Activity: 100, DigNum: 1,
			}))
		case protocol.ActionIDMysticalTreasureDig:
			digCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildMysticalTreasureDigResultPacket(protocol.MysticalTreasureDigResult{
				Result: protocol.MysticalTreasureResultSuccess, OpenGridID: 1, AwardID: 2,
			}))
		}
	})
	defer sess.Close()

	if err := NewDuobaoMountainRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("duobao: %v", err)
	}
	if digCalls.Load() != 1 {
		t.Fatalf("expected 1 dig, got %d", digCalls.Load())
	}
}

func TestDuobaoMountainRoutine_PackageFull(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDMysticalTreasurePanel:
			_ = protocol.WritePacket(conn, protocol.BuildMysticalTreasurePanelResultPacket(protocol.MysticalTreasurePanel{DigNum: 1}))
		case protocol.ActionIDMysticalTreasureDig:
			_ = protocol.WritePacket(conn, protocol.BuildMysticalTreasureDigResultPacket(protocol.MysticalTreasureDigResult{
				Result: protocol.MysticalTreasureResultPackageFull,
			}))
		}
	})
	defer sess.Close()

	err := NewDuobaoMountainRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("expected ErrBagFull, got %v", err)
	}
}

func TestEudemonsRoutine_ActivateMeridian(t *testing.T) {
	var activateCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEudemonsPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEudemonsPanelResultPacket(protocol.EudemonsPanel{
				Result:    protocol.EudemonsResultSuccess,
				SkinID:    1,
				Meridians: []protocol.EudemonsMeridian{{ID: 3, IsActive: 0}, {ID: 2, IsActive: 1}},
			}))
		case protocol.ActionIDEudemonsActivateMeridians:
			activateCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEudemonsActionCodePacket(
				protocol.ActionIDEudemonsActivateMeridians, protocol.EudemonsResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewEudemonsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("eudemons: %v", err)
	}
	if activateCalls.Load() != 1 {
		t.Fatalf("expected 1 activation, got %d", activateCalls.Load())
	}
}

func TestEudemonsRoutine_GemStepNotEnoughStops(t *testing.T) {
	var activateCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEudemonsPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEudemonsPanelResultPacket(protocol.EudemonsPanel{
				Result:    protocol.EudemonsResultSuccess,
				Meridians: []protocol.EudemonsMeridian{{ID: 3}, {ID: 2}},
			}))
		case protocol.ActionIDEudemonsActivateMeridians:
			activateCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEudemonsActionCodePacket(
				protocol.ActionIDEudemonsActivateMeridians, protocol.EudemonsResultGemStepNotEnough))
		}
	})
	defer sess.Close()

	if err := NewEudemonsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("eudemons: %v", err)
	}
	if activateCalls.Load() != 1 {
		t.Fatalf("expected stop after first failure, got %d activations", activateCalls.Load())
	}
}
