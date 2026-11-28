package daily

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func encodeMyPetList(pets []protocol.PetImmortalMyPet) []byte {
	w := protocol.NewWriter()
	w.WriteUint16(uint16(len(pets)))
	for _, p := range pets {
		var isT uint8
		if p.IsTravel {
			isT = 1
		}
		var isH uint32
		if p.IsHave {
			isH = 1
		}
		w.WriteInt32(p.PetID).WriteInt32(p.TravelTS).WriteUint8(isT).
			WriteInt32(p.LuckTimes).WriteInt32(p.LuckLimit).WriteUint32(isH)
	}
	w.WriteUint8(0)
	return w.Bytes()
}

func encodeSectDomainList(mySectID int32, domains []protocol.PetImmortalSectDomain) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.PetImmortalSuccess)
	w.WriteInt32(mySectID)
	w.WriteUint16(uint16(len(domains)))
	for _, d := range domains {
		var isT uint8
		if d.IsTravel {
			isT = 1
		}
		w.WriteInt32(d.ID).WriteInt32(d.DomainID).WriteInt32(d.ProtectTS).WriteInt32(d.SectID).
			WriteInt32(d.AttackNum).WriteInt32(d.DefenseNum).
			WriteInt32(d.ScorePro).WriteInt32(d.LuckPro).
			WriteInt32(d.ExtraScorePro).WriteInt32(d.ExtraLuckPro).
			WriteUint8(isT).WriteInt32(d.NowTravelNum).WriteInt32(d.SpecialTS).WriteInt32(d.IsSectAlliance)
	}
	w.WriteUint16(0)
	return w.Bytes()
}

func encodeDomainTravelInfo(myAppID, nowSectID, mySectID int32, canOther bool, pets []protocol.PetImmortalDomainPet) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.PetImmortalSuccess)
	w.WriteInt32(myAppID)
	w.WriteInt32(nowSectID)
	w.WriteUint16(uint16(len(pets)))
	for _, p := range pets {
		w.WriteInt32(p.PetID).WriteInt32(p.ProtectTS).WriteInt32(p.TravelTS).
			WriteString(p.Nickname).WriteString(p.ServerName).
			WriteInt32(p.AppID).WriteInt32(p.SectID).WriteInt32(p.PosID).
			WriteInt64(p.WarPower).WriteString(p.SectName)
	}
	if canOther {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	w.WriteInt32(mySectID).WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(6).WriteInt32(0)
	return w.Bytes()
}

func TestFairylandDispatchRoutine_ClosedSkips(t *testing.T) {
	var petCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetImmortalGetState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalClose}))
		case protocol.ActionIDPetImmortalMyPetList:
			petCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("未开放应返回 CooldownError, 实际: %v", err)
	}
	if petCalls.Load() != 0 {
		t.Fatalf("未开放时不应查询宠物列表, calls=%d", petCalls.Load())
	}
}

func TestFairylandDispatchRoutine_AlreadyTwoTravelingSkips(t *testing.T) {
	var domCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetImmortalGetState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalOpen}))
		case protocol.ActionIDPetImmortalMyPetList:
			payload := encodeMyPetList([]protocol.PetImmortalMyPet{
				{PetID: 30, TravelTS: 600, IsTravel: true, LuckLimit: 10, IsHave: true},
				{PetID: 78, TravelTS: 600, IsTravel: true, LuckLimit: 15, IsHave: true},
				{PetID: 25, TravelTS: 0, IsTravel: false, LuckLimit: 10, IsHave: true},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainList:
			domCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("已有2只游历中应返回 CooldownError, 实际: %v", err)
	}
	if domCalls.Load() != 0 {
		t.Fatalf("已有2只游历中不应查询仙域列表, calls=%d", domCalls.Load())
	}
}

func TestFairylandDispatchRoutine_AllPetsHitWeeklyLimitWaitsReset(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetImmortalGetState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalOpen}))
		case protocol.ActionIDPetImmortalMyPetList:
			payload := encodeMyPetList([]protocol.PetImmortalMyPet{
				{PetID: 30, TravelTS: maxPetTravelSeconds, IsTravel: false, LuckLimit: 10, IsHave: true},
				{PetID: 78, TravelTS: maxPetTravelSeconds, IsTravel: false, LuckLimit: 15, IsHave: true},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("全部宠物达周上限应返回 CooldownError, 实际: %v", err)
	}
	if cd.Until.IsZero() {
		t.Fatalf("全部宠物达周上限应按 Until 定时唤醒, got %+v", cd)
	}
}

func TestFairylandDispatchRoutine_DispatchesTwoPetsAcrossDomains(t *testing.T) {
	var mu sync.Mutex
	var dispatched [][3]int32

	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetImmortalGetState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalOpen}))
		case protocol.ActionIDPetImmortalMyPetList:
			payload := encodeMyPetList([]protocol.PetImmortalMyPet{
				{PetID: 30, TravelTS: 1000, IsTravel: false, LuckLimit: 10, IsHave: true},
				{PetID: 78, TravelTS: 0, IsTravel: false, LuckLimit: 15, IsHave: true},
				{PetID: 25, TravelTS: 0, IsTravel: false, LuckLimit: 10, IsHave: true},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainList:
			payload := encodeSectDomainList(20, []protocol.PetImmortalSectDomain{
				{ID: 12879, SectID: 20, ScorePro: 40, LuckPro: 30, ExtraScorePro: 20, NowTravelNum: 2},
				{ID: 12880, SectID: 20, ScorePro: 40, LuckPro: 30, ExtraLuckPro: 20, NowTravelNum: 1},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainTravelInfo:
			r := protocol.NewReader(pkt.Payload)
			domID, _ := r.ReadInt32()
			payload := encodeDomainTravelInfo(244, 20, 20, false, []protocol.PetImmortalDomainPet{
				{PosID: 1, PetID: 99, AppID: 999},
				{PosID: 2, PetID: 0},
			})
			_ = domID
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalPetTravel:
			r := protocol.NewReader(pkt.Payload)
			domID, _ := r.ReadInt32()
			posID, _ := r.ReadInt32()
			petID, _ := r.ReadInt32()
			mu.Lock()
			dispatched = append(dispatched, [3]int32{domID, posID, petID})
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalSuccess}))
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("派遣完成后应返回 2h 复查 CooldownError, 实际: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 2 {
		t.Fatalf("应派遣 2 只宠物, 实际 %d: %v", len(dispatched), dispatched)
	}
	// 幸运上限 15 的 pet 78 应优先派遣，且优先选择 luck 加成更高的 12880
	if dispatched[0] != [3]int32{12880, 2, 78} {
		t.Fatalf("首次派遣不符预期: got %v, want [12880 2 78]", dispatched[0])
	}
	if dispatched[1] != [3]int32{12879, 2, 25} {
		t.Fatalf("二次派遣不符预期: got %v, want [12879 2 25]", dispatched[1])
	}
}

func TestFairylandDispatchRoutine_FallbackToShareDomainWhenSectFull(t *testing.T) {
	var mu sync.Mutex
	var dispatched [][3]int32

	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetImmortalGetState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalOpen}))
		case protocol.ActionIDPetImmortalMyPetList:
			payload := encodeMyPetList([]protocol.PetImmortalMyPet{
				{PetID: 30, TravelTS: 0, IsTravel: true, LuckLimit: 10, IsHave: true},
				{PetID: 78, TravelTS: 0, IsTravel: false, LuckLimit: 15, IsHave: true},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainList:
			payload := encodeSectDomainList(20, []protocol.PetImmortalSectDomain{
				{ID: 12879, SectID: 20, ScorePro: 40, LuckPro: 30, NowTravelNum: 6},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainTravelInfo:
			// 全部满位
			payload := encodeDomainTravelInfo(244, 20, 20, false, []protocol.PetImmortalDomainPet{
				{PosID: 1, PetID: 11, AppID: 999},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalPetTravel:
			r := protocol.NewReader(pkt.Payload)
			domID, _ := r.ReadInt32()
			posID, _ := r.ReadInt32()
			petID, _ := r.ReadInt32()
			mu.Lock()
			dispatched = append(dispatched, [3]int32{domID, posID, petID})
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalSuccess}))
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("共享仙域派遣后应返回 CooldownError, 实际: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 1 || dispatched[0] != [3]int32{0, 0, 78} {
		t.Fatalf("应回退派遣至共享仙域 (0, 0, 78), 实际 %v", dispatched)
	}
}

func TestFairylandDispatchRoutine_HandlesFailureCodes(t *testing.T) {
	var attempts atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetImmortalGetState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalOpen}))
		case protocol.ActionIDPetImmortalMyPetList:
			payload := encodeMyPetList([]protocol.PetImmortalMyPet{
				{PetID: 78, TravelTS: 0, IsTravel: false, LuckLimit: 15, IsHave: true},
				{PetID: 25, TravelTS: 0, IsTravel: false, LuckLimit: 10, IsHave: true},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainList:
			payload := encodeSectDomainList(20, []protocol.PetImmortalSectDomain{
				{ID: 12879, SectID: 20, ScorePro: 40, LuckPro: 30},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalSectDomainTravelInfo:
			payload := encodeDomainTravelInfo(244, 20, 20, false, []protocol.PetImmortalDomainPet{
				{PosID: 1, PetID: 0},
				{PosID: 2, PetID: 0},
			})
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		case protocol.ActionIDPetImmortalPetTravel:
			n := attempts.Add(1)
			switch n {
			case 1:
				// 槽位被占 -> 应切同仙域下一槽位
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalPosNotEmpty}))
			case 2:
				// 宠物78达时长上限 -> 应跳过宠物78换宠物25
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalTravelTimeLimit}))
			default:
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.PetImmortalSuccess}))
			}
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("预期 CooldownError, 实际: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("应依次经历槽位被占/时长上限/成功共 3 次调用, 实际 %d", attempts.Load())
	}
}

func TestNextFairylandWeeklyReset(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{
			now:  time.Date(2026, 11, 15, 11, 0, 0, 0, loc),
			want: time.Date(2026, 11, 16, 5, 5, 0, 0, loc),
		},
		{
			now:  time.Date(2026, 11, 16, 0, 26, 0, 0, loc),
			want: time.Date(2026, 11, 16, 5, 5, 0, 0, loc),
		},
		{
			now:  time.Date(2026, 11, 16, 5, 5, 0, 0, loc),
			want: time.Date(2026, 11, 17, 5, 5, 0, 0, loc),
		},
	}
	for _, tc := range cases {
		if got := nextFairylandWeeklyReset(tc.now); !got.Equal(tc.want) {
			t.Errorf("now=%v got=%v want=%v", tc.now, got, tc.want)
		}
	}
}
