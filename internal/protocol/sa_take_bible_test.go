package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseSaTakeBibleRoadResult(t *testing.T) {
	w := protocol.NewWriter()
	// 1 支队伍 (比仙界多 2 个 Byte 标志位)。
	w.WriteUint16(1)
	w.WriteInt32(1001) // player_id
	w.WriteUint8(3)    // protection 朱八戒
	w.WriteInt32(100)
	w.WriteInt32(200)
	w.WriteInt16(42)
	w.WriteInt32(0)
	w.WriteInt8(1)  // sequence
	w.WriteInt8(0)  // is_deeds_so
	w.WriteUint8(0) // extra1
	w.WriteUint8(1) // extra2
	// 尾部。
	w.WriteInt32(0)   // protect_player_id
	w.WriteInt16(4)   // can_rob_times
	w.WriteInt8(2)    // can_protect_times
	w.WriteInt8(3)    // can_take_bible_times
	w.WriteInt8(1)    // quickly_times
	w.WriteUint8(9)   // type
	w.WriteInt32(500) // time
	w.WriteUint16(1)  // last rob count
	w.WriteInt8(1)
	w.WriteInt16(30)
	w.WriteString("劫匪")
	w.WriteString("仙界")
	w.WriteString("s0")
	w.WriteInt16(77)
	w.WriteInt8(2) // buff
	w.WriteInt8(0) // extra tail

	road, err := protocol.ParseSaTakeBibleRoadResult(w.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(road.Teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(road.Teams))
	}
	team := road.Teams[0]
	if team.PlayerID != 1001 || team.Protection != 3 || team.SequenceID != 1 {
		t.Fatalf("unexpected team: %+v", team)
	}
	if road.CanRobTimes != 4 || road.CanTakeBibleTimes != 3 || road.Buff != 2 {
		t.Fatalf("unexpected road tail: %+v", road)
	}
	if len(road.LastRobs) != 1 || road.LastRobs[0].LastRobNickname != "劫匪" {
		t.Fatalf("unexpected last robs: %+v", road.LastRobs)
	}
}
