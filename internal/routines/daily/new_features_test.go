package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestDailyGiftsRoutine_ClaimsFreeGifts(t *testing.T) {
	var salaryCalls, stoneCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerCampSalary:
			salaryCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCampSalaryResultPacket(protocol.PlayerCampSalarySuccess, 1000))
		case protocol.ActionIDSealSoulDayStone:
			stoneCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSealSoulDayStoneResultPacket(protocol.SealSoulDayStoneSuccess, 2))
		case protocol.ActionIDPlayerClientGiftInfo:
			_ = protocol.WritePacket(conn, protocol.BuildClientGiftInfoResultPacket(protocol.ClientGiftInfo{Status: 0}))
		}
	})
	defer sess.Close()

	if err := NewDailyGiftsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("daily gifts: %v", err)
	}
	if salaryCalls.Load() != 1 || stoneCalls.Load() != 1 {
		t.Fatalf("expected salary + stone once, got %d/%d", salaryCalls.Load(), stoneCalls.Load())
	}
}

func TestDailyGiftsRoutine_CampSalaryPackFull(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDPlayerCampSalary {
			_ = protocol.WritePacket(conn, protocol.BuildCampSalaryResultPacket(protocol.PlayerCampSalaryPackFull, 0))
		}
	})
	defer sess.Close()

	err := NewDailyGiftsRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("expected ErrBagFull, got %v", err)
	}
}

func TestDestinySwordRoutine_TemperWithScrap(t *testing.T) {
	var temperCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDestinySwordPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordPanelResultPacket(protocol.DestinySwordPanel{
				Roles: []protocol.DestinySwordRole{{
					PlayerRoleID: 1, IsMainRole: 1, RoleID: 511, RoleName: "遁一", RoleLevel: 300,
					EquipList: []protocol.DestinySwordEquip{{
						SwordID: 1, Level: 1,
						Intents: []protocol.DestinySwordIntent{{PlayerIntentID: 123, IntentID: 1, IntentLevel: 1}},
					}},
				}},
				JianyiScrap: 10,
			}))
		case protocol.ActionIDDestinySwordOneClickTemperPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordTemperPanelResultPacket(protocol.DestinySwordTemperPanel{
				Result:             protocol.DestinySwordResultSuccess,
				NeedJianyiScrapNum: 2,
			}))
		case protocol.ActionIDDestinySwordOneClickTemper:
			temperCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordActionCodePacket(
				protocol.ActionIDDestinySwordOneClickTemper, protocol.DestinySwordResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewDestinySwordRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("destiny sword: %v", err)
	}
	if temperCalls.Load() != 1 {
		t.Fatalf("expected 1 temper, got %d", temperCalls.Load())
	}
}

func TestDestinySwordRoutine_NoScrapSkips(t *testing.T) {
	var temperPanelCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDestinySwordPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordPanelResultPacket(protocol.DestinySwordPanel{
				Roles: []protocol.DestinySwordRole{{
					PlayerRoleID: 1, RoleID: 511,
					EquipList: []protocol.DestinySwordEquip{{
						SwordID: 1, Intents: []protocol.DestinySwordIntent{{PlayerIntentID: 123, IntentLevel: 1}},
					}},
				}},
				JianyiScrap: 0,
			}))
		case protocol.ActionIDDestinySwordOneClickTemperPanel:
			temperPanelCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewDestinySwordRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("destiny sword: %v", err)
	}
	if temperPanelCalls.Load() != 0 {
		t.Fatalf("expected no temper attempts without scrap, got %d", temperPanelCalls.Load())
	}
}

func TestDestinySwordRoutine_TemperFailureStops(t *testing.T) {
	var temperCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDestinySwordPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordPanelResultPacket(protocol.DestinySwordPanel{
				Roles: []protocol.DestinySwordRole{{
					PlayerRoleID: 1, RoleID: 511,
					EquipList: []protocol.DestinySwordEquip{{
						SwordID: 1, Intents: []protocol.DestinySwordIntent{{PlayerIntentID: 123, IntentLevel: 1}},
					}},
				}},
				JianyiScrap: 10,
			}))
		case protocol.ActionIDDestinySwordOneClickTemperPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordTemperPanelResultPacket(protocol.DestinySwordTemperPanel{
				Result: protocol.DestinySwordResultSuccess, NeedJianyiScrapNum: 2,
			}))
		case protocol.ActionIDDestinySwordOneClickTemper:
			temperCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildDestinySwordActionCodePacket(
				protocol.ActionIDDestinySwordOneClickTemper, protocol.DestinySwordResultNotEnoughItem))
		}
	})
	defer sess.Close()

	if err := NewDestinySwordRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("destiny sword: %v", err)
	}
	if temperCalls.Load() != 1 {
		t.Fatalf("expected stop after first failure, got %d", temperCalls.Load())
	}
}

// TestDailyGiftsRoutine_MonthlyClientGiftNotReady 验证月度礼包类型 (首字节 2)
// 以 month_status 为准；month_status=0 时不得发起领取。
func TestDailyGiftsRoutine_MonthlyClientGiftNotReady(t *testing.T) {
	var receiveCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerCampSalary:
			_ = protocol.WritePacket(conn, protocol.BuildCampSalaryResultPacket(protocol.PlayerCampSalarySuccess, 1000))
		case protocol.ActionIDSealSoulDayStone:
			_ = protocol.WritePacket(conn, protocol.BuildSealSoulDayStoneResultPacket(protocol.SealSoulDayStoneSuccess, 2))
		case protocol.ActionIDPlayerClientGiftInfo:
			_ = protocol.WritePacket(conn, protocol.BuildClientGiftInfoResultPacket(
				protocol.ClientGiftInfo{Status: 2, MonthStatus: 0}))
		case protocol.ActionIDPlayerReceiveClientGift:
			receiveCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewDailyGiftsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("daily gifts: %v", err)
	}
	if got := receiveCalls.Load(); got != 0 {
		t.Fatalf("月度礼包未就绪时不应领取, 实际 %d 次", got)
	}
}
