package protocol

import "testing"

func TestSaDragonAreaPackets(t *testing.T) {
	if pkt := BuildSaDragonAreaInfoPacket(); pkt.ActionID != ActionIDSaDragonAreaInfo || len(pkt.Payload) != 0 {
		t.Fatalf("unexpected info packet: %+v", pkt)
	}
	if pkt := BuildSaDragonAreaFightPacket(); pkt.ActionID != ActionIDSaDragonAreaFight || len(pkt.Payload) != 0 {
		t.Fatalf("unexpected fight packet: %+v", pkt)
	}
	if pkt := BuildSaDragonAreaQuickFightPacket(); pkt.ActionID != ActionIDSaDragonAreaQuickFight || len(pkt.Payload) != 0 {
		t.Fatalf("unexpected quick_fight packet: %+v", pkt)
	}
}

func TestParseSaDragonAreaInfo(t *testing.T) {
	w := NewWriter()
	w.WriteInt32(5) // seq
	w.WriteUint16(1)
	w.WriteInt32(1001)
	w.WriteInt32(20)
	w.WriteInt16(404)
	w.WriteInt8(50)
	w.WriteString("李淳风")
	w.WriteInt32(12112426)
	w.WriteString("2026112001")
	w.WriteString("血色亚少")
	w.WriteInt32(12121973)
	w.WriteString("2026112001")
	w.WriteInt32(20)
	w.WriteInt32(8197)
	w.WriteUint16(1) // dragonball
	w.WriteInt8(1)
	w.WriteInt32(42)
	w.WriteInt8(6)
	w.WriteUint16(1) // monster
	for i := 0; i < 7; i++ {
		w.WriteInt32(int32(i + 1))
	}
	w.WriteInt32(0)  // five_elem
	w.WriteInt32(23) // last_week_seq
	w.WriteInt32(23) // quick_seq
	w.WriteInt32(0)  // is_dragon_clear

	info, err := ParseSaDragonAreaInfo(w.Bytes())
	if err != nil {
		t.Fatalf("ParseSaDragonAreaInfo failed: %v", err)
	}
	if info.Seq != 5 || info.QuickSeq != 23 || info.LastWeekSeq != 23 || info.IsDragonClear != 0 {
		t.Fatalf("unexpected info: %+v", info)
	}
	if len(info.Awards) != 1 || info.Awards[0].ItemID != 1001 || info.Awards[0].Count != 20 {
		t.Fatalf("unexpected awards: %+v", info.Awards)
	}
	if info.FirstNickName != "李淳风" || info.BestNickName != "血色亚少" {
		t.Fatalf("unexpected nicknames: first=%q best=%q", info.FirstNickName, info.BestNickName)
	}

	if _, err := ParseSaDragonAreaInfo(nil); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestParseSaDragonAreaFightResult(t *testing.T) {
	for _, code := range []uint8{
		SaDragonAreaResultSuccess,
		SaDragonAreaResultFunctionNoOpen,
		SaDragonAreaResultCompleteAll,
		SaDragonAreaResultSaintAreaClose,
		SaDragonAreaResultCD,
		SaDragonAreaResultFailed,
	} {
		got, err := ParseSaDragonAreaFightResult([]byte{code, 0, 0})
		if err != nil || got != code {
			t.Fatalf("code=%d: got=%d err=%v", code, got, err)
		}
		if SaDragonAreaResultText(code) == "" {
			t.Fatalf("empty text for code=%d", code)
		}
	}
	if _, err := ParseSaDragonAreaFightResult(nil); err == nil {
		t.Fatal("expected error on empty fight payload")
	}
}

func TestParseSaDragonAreaQuickFightResult(t *testing.T) {
	w := NewWriter()
	w.WriteUint16(2)
	w.WriteInt32(1)
	w.WriteUint16(1)
	w.WriteInt32(1001)
	w.WriteInt32(10)
	w.WriteInt32(2)
	w.WriteUint16(2)
	w.WriteInt32(1001)
	w.WriteInt32(15)
	w.WriteInt32(1002)
	w.WriteInt32(3)

	entries, err := ParseSaDragonAreaQuickFightResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseSaDragonAreaQuickFightResult failed: %v", err)
	}
	if len(entries) != 2 || entries[0].Seq != 1 || entries[1].Seq != 2 || len(entries[1].Awards) != 2 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if _, err := ParseSaDragonAreaQuickFightResult([]byte{0x00}); err == nil {
		t.Fatal("expected error on truncated quick_fight payload")
	}
}
