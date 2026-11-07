package daily

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func buildPackPayload(items []protocol.ItemRecord) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, int16(48))
	_ = binary.Write(buf, binary.BigEndian, int32(0))
	_ = binary.Write(buf, binary.BigEndian, uint16(len(items)))
	for _, it := range items {
		_ = protocol.WriteItemRecord(buf, it)
	}
	_ = binary.Write(buf, binary.BigEndian, int16(10))
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	return buf.Bytes()
}

func TestXiuDanRoutine_UsesBagPills(t *testing.T) {
	var useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskXiuDan, CurTimes: 0, MaxTimes: 15, PerActiveAdd: 10},
			}))
		case protocol.ActionIDItemGetPackList:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDItemGetPackList,
				buildPackPayload([]protocol.ItemRecord{
					{PlayerItemID: 1, ItemID: 8308, GridID: 4},
					{PlayerItemID: 2, ItemID: 8310, GridID: 7},
					{PlayerItemID: 3, ItemID: 940, GridID: 9},
				})))
		case protocol.ActionIDItemUseGrid:
			useCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDItemUseGrid, []byte{protocol.ItemActionSuccess}))
		}
	})
	defer sess.Close()

	if err := NewXiuDanRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("xiudan: %v", err)
	}
	if got := useCalls.Load(); got != 2 {
		t.Fatalf("use calls = %d, want 2", got)
	}
}

func TestXiuDanRoutine_SkipsWhenTaskComplete(t *testing.T) {
	var packCalls, useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskXiuDan, CurTimes: 15, MaxTimes: 15, PerActiveAdd: 10},
			}))
		case protocol.ActionIDItemGetPackList:
			packCalls.Add(1)
		case protocol.ActionIDItemUseGrid:
			useCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewXiuDanRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("xiudan: %v", err)
	}
	if packCalls.Load() != 0 || useCalls.Load() != 0 {
		t.Fatalf("expected no calls, pack=%d use=%d", packCalls.Load(), useCalls.Load())
	}
}

func TestXiuDanRoutine_NoPillsSkips(t *testing.T) {
	var useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskXiuDan, CurTimes: 0, MaxTimes: 15, PerActiveAdd: 10},
			}))
		case protocol.ActionIDItemGetPackList:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDItemGetPackList,
				buildPackPayload([]protocol.ItemRecord{{PlayerItemID: 3, ItemID: 940, GridID: 9}})))
		case protocol.ActionIDItemUseGrid:
			useCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewXiuDanRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("xiudan: %v", err)
	}
	if useCalls.Load() != 0 {
		t.Fatalf("use calls = %d, want 0", useCalls.Load())
	}
}
