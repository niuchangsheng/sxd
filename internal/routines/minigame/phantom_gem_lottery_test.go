package minigame

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func buildTestPhantomGemPanel(remainNormal, remainExtra int32) protocol.PhantomGemLotteryPanel {
	mountains := make([]protocol.PhantomGemLotteryMountain, 0, 4)
	for _, id := range []int8{
		protocol.PhantomGemMountainLingYu,
		protocol.PhantomGemMountainCuiQing,
		protocol.PhantomGemMountainXuanHui,
		protocol.PhantomGemMountainCangMing,
	} {
		mountains = append(mountains, protocol.PhantomGemLotteryMountain{
			MountainID:             id,
			RemainNormalTimes:      remainNormal,
			RemainHighTimes:        102,
			RemainExtraNormalTimes: remainExtra,
		})
	}
	return protocol.PhantomGemLotteryPanel{
		Mountains:    mountains,
		RequiteValue: 4010,
	}
}

func TestPhantomGemLotteryRoutine_DefaultLingYuSuccess(t *testing.T) {
	var mu sync.Mutex
	remain := int32(1)
	var gotMountain int8
	var gotFindType int8
	var findCalls atomic.Int32

	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPhantomGemLotteryPanel:
			mu.Lock()
			r := remain
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryPanelResultPacket(buildTestPhantomGemPanel(r, 0)))
		case protocol.ActionIDPhantomGemLotteryFind:
			findCalls.Add(1)
			if len(pkt.Payload) < 2 {
				t.Errorf("find payload too short: %x", pkt.Payload)
				return
			}
			mu.Lock()
			gotMountain = int8(pkt.Payload[0])
			gotFindType = int8(pkt.Payload[1])
			remain--
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryFindResultPacket(protocol.PhantomGemLotteryFindResult{
				Result: protocol.PhantomGemLotteryResultSuccess,
				Awards: []protocol.PhantomGemLotteryAward{{ItemID: 10702, ItemNum: 1, IsHigh: 1}},
			}))
		}
	})
	defer sess.Close()

	err := NewPhantomGemLotteryRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("expected ErrAttemptsExhausted after consuming daily count, got %v", err)
	}
	if findCalls.Load() != 1 {
		t.Fatalf("findCalls = %d, want 1", findCalls.Load())
	}
	if gotMountain != protocol.PhantomGemMountainLingYu {
		t.Fatalf("mountain = %d, want LingYu(%d)", gotMountain, protocol.PhantomGemMountainLingYu)
	}
	if gotFindType != protocol.PhantomGemFindTypeNormal {
		t.Fatalf("findType = %d, want Normal(%d)", gotFindType, protocol.PhantomGemFindTypeNormal)
	}
}

func TestPhantomGemLotteryRoutine_AllFourMountainsSelectable(t *testing.T) {
	cases := []struct {
		name   string
		wantID int8
	}{
		{"沧溟", protocol.PhantomGemMountainCangMing},
		{"玄晖", protocol.PhantomGemMountainXuanHui},
		{"翠青", protocol.PhantomGemMountainCuiQing},
		{"灵玉", protocol.PhantomGemMountainLingYu},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			remain := int32(1)
			var sentMountain int8
			var sentType int8

			sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDPhantomGemLotteryPanel:
					mu.Lock()
					r := remain
					mu.Unlock()
					_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryPanelResultPacket(buildTestPhantomGemPanel(r, 0)))
				case protocol.ActionIDPhantomGemLotteryFind:
					mu.Lock()
					sentMountain = int8(pkt.Payload[0])
					sentType = int8(pkt.Payload[1])
					remain--
					mu.Unlock()
					_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryFindResultPacket(protocol.PhantomGemLotteryFindResult{
						Result: protocol.PhantomGemLotteryResultSuccess,
						Awards: []protocol.PhantomGemLotteryAward{{ItemID: 8903, ItemNum: 1, IsHigh: 1}},
					}))
				}
			})
			defer sess.Close()

			err := NewPhantomGemLotteryRoutine(PhantomGemLotteryConfig{Mountain: tc.name}).Execute(context.Background(), sess, nil)
			if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
				t.Fatalf("mountain=%s: expected ErrAttemptsExhausted, got %v", tc.name, err)
			}
			if sentMountain != tc.wantID {
				t.Fatalf("mountain=%s: sentMountain = %d, want %d", tc.name, sentMountain, tc.wantID)
			}
			if sentType != protocol.PhantomGemFindTypeNormal {
				t.Fatalf("mountain=%s: sentType = %d, want 0 (normal)", tc.name, sentType)
			}
		})
	}
}

func TestPhantomGemLotteryRoutine_ExtraNormalTimes(t *testing.T) {
	var mu sync.Mutex
	remainBase := int32(1)
	remainExtra := int32(1)
	var findCalls atomic.Int32

	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPhantomGemLotteryPanel:
			mu.Lock()
			b, e := remainBase, remainExtra
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryPanelResultPacket(buildTestPhantomGemPanel(b, e)))
		case protocol.ActionIDPhantomGemLotteryFind:
			findCalls.Add(1)
			mu.Lock()
			if remainBase > 0 {
				remainBase--
			} else if remainExtra > 0 {
				remainExtra--
			}
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryFindResultPacket(protocol.PhantomGemLotteryFindResult{
				Result: protocol.PhantomGemLotteryResultSuccess,
				Awards: []protocol.PhantomGemLotteryAward{{ItemID: 10702, ItemNum: 1, IsHigh: 1}},
			}))
		}
	})
	defer sess.Close()

	err := NewPhantomGemLotteryRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("expected ErrAttemptsExhausted, got %v", err)
	}
	if findCalls.Load() != 2 {
		t.Fatalf("findCalls = %d, want 2 (1 base + 1 extra)", findCalls.Load())
	}
}

func TestPhantomGemLotteryRoutine_FailureCodes(t *testing.T) {
	cases := []struct {
		name    string
		code    uint8
		wantErr error
	}{
		{"not_enough_daoyuan", protocol.PhantomGemLotteryResultNotEnoughDaoyuan, scheduler.ErrConditionNotMet},
		{"pack_full", protocol.PhantomGemLotteryResultItemPackFull, scheduler.ErrBagFull},
		{"not_enough_times", protocol.PhantomGemLotteryResultNotEnoughTimes, scheduler.ErrAttemptsExhausted},
		{"eudemons_not_opened", protocol.PhantomGemLotteryResultEudemonsFunNotOpened, scheduler.ErrConditionNotMet},
		{"not_enough_ingot", protocol.PhantomGemLotteryResultNotEnoughIngot, scheduler.ErrConditionNotMet},
		{"unknown_code", 99, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var findCalls atomic.Int32
			sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDPhantomGemLotteryPanel:
					_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryPanelResultPacket(buildTestPhantomGemPanel(1, 0)))
				case protocol.ActionIDPhantomGemLotteryFind:
					findCalls.Add(1)
					_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryFindResultPacket(protocol.PhantomGemLotteryFindResult{
						Result: tc.code,
					}))
				}
			})
			defer sess.Close()

			err := NewPhantomGemLotteryRoutine().Execute(context.Background(), sess, nil)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("code=%d: expected nil err, got %v", tc.code, err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("code=%d: expected %v, got %v", tc.code, tc.wantErr, err)
			}
			if findCalls.Load() != 1 {
				t.Fatalf("code=%d: findCalls = %d, want 1 (must not retry blindly)", tc.code, findCalls.Load())
			}
		})
	}
}

func TestPhantomGemLotteryRoutine_ZeroRemainShortCircuits(t *testing.T) {
	var findCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPhantomGemLotteryPanel:
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryPanelResultPacket(buildTestPhantomGemPanel(0, 0)))
		case protocol.ActionIDPhantomGemLotteryFind:
			findCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewPhantomGemLotteryRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("expected ErrAttemptsExhausted, got %v", err)
	}
	if findCalls.Load() != 0 {
		t.Fatalf("findCalls = %d, want 0 when remain=0", findCalls.Load())
	}
}

func TestPhantomGemLotteryRoutine_StaleRemainStopsLoop(t *testing.T) {
	var findCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPhantomGemLotteryPanel:
			// 服务端始终返回 remain=1，模拟成功码但未扣减次数的异常。
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryPanelResultPacket(buildTestPhantomGemPanel(1, 0)))
		case protocol.ActionIDPhantomGemLotteryFind:
			findCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildPhantomGemLotteryFindResultPacket(protocol.PhantomGemLotteryFindResult{
				Result: protocol.PhantomGemLotteryResultSuccess,
				Awards: []protocol.PhantomGemLotteryAward{{ItemID: 10702, ItemNum: 1, IsHigh: 1}},
			}))
		}
	})
	defer sess.Close()

	if err := NewPhantomGemLotteryRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("expected nil on stale remain stop, got %v", err)
	}
	if findCalls.Load() != 1 {
		t.Fatalf("findCalls = %d, want 1 (must break when remain does not decrease)", findCalls.Load())
	}
}
