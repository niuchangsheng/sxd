package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestPetImmortalDomainPacketBuilders(t *testing.T) {
	if p := protocol.BuildPetImmortalGetStatePacket(); p.ActionID != protocol.ActionIDPetImmortalGetState || len(p.Payload) != 0 {
		t.Fatalf("755/0 封包异常: %+v", p)
	}
	if p := protocol.BuildPetImmortalSectDomainListPacket(); p.ActionID != protocol.ActionIDPetImmortalSectDomainList || len(p.Payload) != 0 {
		t.Fatalf("755/1 封包异常: %+v", p)
	}
	p2 := protocol.BuildPetImmortalSectDomainTravelInfoPacket(12879)
	if p2.ActionID != protocol.ActionIDPetImmortalSectDomainTravelInfo || len(p2.Payload) != 4 {
		t.Fatalf("755/2 封包异常: %+v", p2)
	}
	p15 := protocol.BuildPetImmortalPetTravelPacket(12879, 5, 78)
	if p15.ActionID != protocol.ActionIDPetImmortalPetTravel || len(p15.Payload) != 12 {
		t.Fatalf("755/15 封包异常: %+v", p15)
	}
	r := protocol.NewReader(p15.Payload)
	domID, _ := r.ReadInt32()
	posID, _ := r.ReadInt32()
	petID, _ := r.ReadInt32()
	if domID != 12879 || posID != 5 || petID != 78 {
		t.Fatalf("755/15 载荷不符: dom=%d pos=%d pet=%d", domID, posID, petID)
	}
}

func TestParsePetImmortalSectDomainList(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.PetImmortalSuccess)
	w.WriteInt32(20)
	w.WriteUint16(1)
	w.WriteInt32(12879) // id
	w.WriteInt32(6)     // domain_id
	w.WriteInt32(0)     // protect_ts
	w.WriteInt32(20)    // sect_id
	w.WriteInt32(0)     // attack_num
	w.WriteInt32(15)    // defense_num
	w.WriteInt32(40)    // score_pro
	w.WriteInt32(30)    // luck_pro
	w.WriteInt32(20)    // extra_score_pro
	w.WriteInt32(0)     // extra_luck_pro
	w.WriteUint8(0)     // is_travel
	w.WriteInt32(4)     // now_travel_num
	w.WriteInt32(0)     // special_ts
	w.WriteInt32(0)     // is_sect_alliance
	w.WriteUint16(0)    // sects

	got, err := protocol.ParsePetImmortalSectDomainList(w.Bytes())
	if err != nil {
		t.Fatalf("解析 755/1 失败: %v", err)
	}
	if got.Result != protocol.PetImmortalSuccess || got.MySectID != 20 || len(got.Domains) != 1 {
		t.Fatalf("755/1 字段不符: %+v", got)
	}
	d := got.Domains[0]
	if d.ID != 12879 || d.SectID != 20 || d.ScorePro != 40 || d.ExtraScorePro != 20 || d.NowTravelNum != 4 || d.IsTravel {
		t.Fatalf("domain 字段不符: %+v", d)
	}

	// 非成功码直接返回 Result，不报错
	fail, err := protocol.ParsePetImmortalSectDomainList([]byte{protocol.PetImmortalNoSect})
	if err != nil || fail.Result != protocol.PetImmortalNoSect {
		t.Fatalf("非成功码解析异常: %+v, err=%v", fail, err)
	}
}

func TestParsePetImmortalSectDomainTravelInfo(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.PetImmortalSuccess)
	w.WriteInt32(244)
	w.WriteInt32(20)
	w.WriteUint16(2)
	// occupied slot 1
	w.WriteInt32(30).WriteInt32(1000).WriteInt32(422).WriteString("小亲亲").WriteString("s0")
	w.WriteInt32(1041).WriteInt32(20).WriteInt32(1).WriteInt64(143766643).WriteString("烟雨阁")
	// empty slot 2
	w.WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteString("").WriteString("")
	w.WriteInt32(0).WriteInt32(0).WriteInt32(2).WriteInt64(0).WriteString("")
	w.WriteUint8(1) // can_travel_other_sect
	w.WriteInt32(20)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(6)
	w.WriteInt32(0)

	got, err := protocol.ParsePetImmortalSectDomainTravelInfo(w.Bytes())
	if err != nil {
		t.Fatalf("解析 755/2 失败: %v", err)
	}
	if got.MyAppID != 244 || got.NowSectID != 20 || !got.CanTravelOtherSect || got.PosNum != 6 || len(got.Pets) != 2 {
		t.Fatalf("755/2 字段不符: %+v", got)
	}
	if got.Pets[1].PetID != 0 || got.Pets[1].PosID != 2 {
		t.Fatalf("空槽位解析不符: %+v", got.Pets[1])
	}
}

func TestParsePetImmortalMyPetList(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint16(2)
	w.WriteInt32(30).WriteInt32(675).WriteUint8(1).WriteInt32(0).WriteInt32(10).WriteUint32(1)
	w.WriteInt32(78).WriteInt32(0).WriteUint8(0).WriteInt32(0).WriteInt32(15).WriteUint32(1)
	w.WriteUint8(0)

	got, err := protocol.ParsePetImmortalMyPetList(w.Bytes())
	if err != nil {
		t.Fatalf("解析 755/14 失败: %v", err)
	}
	if len(got.Pets) != 2 || got.IsUnread {
		t.Fatalf("755/14 长度/标志不符: %+v", got)
	}
	if !got.Pets[0].IsTravel || !got.Pets[0].IsHave || got.Pets[0].TravelTS != 675 {
		t.Fatalf("pet[0] 不符: %+v", got.Pets[0])
	}
	if got.Pets[1].IsTravel || !got.Pets[1].IsHave || got.Pets[1].LuckLimit != 15 {
		t.Fatalf("pet[1] 不符: %+v", got.Pets[1])
	}
}
