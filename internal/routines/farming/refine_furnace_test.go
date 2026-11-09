package farming_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/scheduler"
)

type refineFurnaceGateway struct {
	awardCode  uint8
	selectCode uint8
	startCode  uint8
	materials  []protocol.RefineFurnaceMaterial

	selectCalls atomic.Int32
	startCalls  atomic.Int32
	// lessCoinTimes >0 时前 N 次 start_refine 返回 LESS_COIN。
	lessCoinTimes int32
	// lessAirTimes >0 时接着前 N 次 start_refine 返回 LESS_AIR。
	lessAirTimes int32

	mu           sync.Mutex
	startPayload []byte
}

func (g *refineFurnaceGateway) lastStartPayload() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]byte(nil), g.startPayload...)
}

func buildRefineFurnacePanel(materials []protocol.RefineFurnaceMaterial) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.RefineFurnaceSuccess)
	w.WriteInt32(2146).WriteInt32(4).WriteInt32(7923)
	w.WriteUint16(uint16(len(materials)))
	for _, m := range materials {
		w.WriteInt32(m.ItemID).WriteInt32(m.Count)
	}
	w.WriteUint16(0)
	w.WriteUint8(1).WriteInt32(0).WriteInt32(0)
	return w.Bytes()
}

func startRefineFurnaceSession(t *testing.T, gw *refineFurnaceGateway) (*client.RoleSession, func()) {
	t.Helper()
	c1, c2 := net.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerLogin, []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}))
			case protocol.ActionIDPlayerInitStep1:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep1,
					[]byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}))
			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))
			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))
			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x00, 0x64,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			case protocol.ActionIDRefineFurnacePanel:
				_ = protocol.WritePacket(c2, protocol.NewPacket(pkt.ActionID, buildRefineFurnacePanel(gw.materials)))
			case protocol.ActionIDRefineFurnaceAward:
				_ = protocol.WritePacket(c2, protocol.NewPacket(pkt.ActionID, []byte{gw.awardCode}))
			case protocol.ActionIDRefineFurnaceType:
				gw.selectCalls.Add(1)
				_ = protocol.WritePacket(c2, protocol.NewPacket(pkt.ActionID, []byte{gw.selectCode}))
			case protocol.ActionIDRefineFurnaceStart:
				n := gw.startCalls.Add(1)
				gw.mu.Lock()
				gw.startPayload = append([]byte(nil), pkt.Payload...)
				gw.mu.Unlock()
				code := gw.startCode
				if gw.lessCoinTimes > 0 && n <= gw.lessCoinTimes {
					code = protocol.RefineFurnaceLessCoin
				} else if gw.lessAirTimes > 0 && n <= gw.lessCoinTimes+gw.lessAirTimes {
					code = protocol.RefineFurnaceLessAir
				}
				_ = protocol.WritePacket(c2, protocol.NewPacket(pkt.ActionID, []byte{code}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-refine-furnace",
		RoleName:   "炼宝炉测试",
		Username:   "refine_furnace_user",
		Hash:       "dummy_refine_furnace_hash",
		ServerAddr: "refine-furnace-test-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("session.Start 失败: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}
	cleanup := func() {
		session.Close()
		c1.Close()
		c2.Close()
		wg.Wait()
	}
	return session, cleanup
}

func TestRefinePotRoutine_RefinesMaxBatches(t *testing.T) {
	gw := &refineFurnaceGateway{
		awardCode:  protocol.RefineFurnaceSuccess,
		selectCode: protocol.RefineFurnaceSuccess,
		startCode:  protocol.RefineFurnaceSuccess,
		materials: []protocol.RefineFurnaceMaterial{
			{ItemID: 7923, Count: 3675},
			{ItemID: 7926, Count: 64},
		},
	}
	session, cleanup := startRefineFurnaceSession(t, gw)
	defer cleanup()

	if err := farming.NewRefinePotRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("refine furnace: %v", err)
	}
	if got := gw.startCalls.Load(); got != 1 {
		t.Fatalf("start_refine calls = %d, want 1", got)
	}
	payload := gw.lastStartPayload()
	if len(payload) != 4 {
		t.Fatalf("start payload len = %d, want 4", len(payload))
	}
	batches := int32(payload[0])<<24 | int32(payload[1])<<16 | int32(payload[2])<<8 | int32(payload[3])
	if batches != 73 {
		t.Fatalf("batches = %d, want 73 (3675/50)", batches)
	}
	if gw.selectCalls.Load() != 1 {
		t.Fatalf("select calls = %d, want 1 (7926<500 应跳过)", gw.selectCalls.Load())
	}
}

func TestRefinePotRoutine_LessCoinHalvesBatches(t *testing.T) {
	gw := &refineFurnaceGateway{
		awardCode:     protocol.RefineFurnaceSuccess,
		selectCode:    protocol.RefineFurnaceSuccess,
		startCode:     protocol.RefineFurnaceSuccess,
		lessCoinTimes: 2,
		materials:     []protocol.RefineFurnaceMaterial{{ItemID: 7923, Count: 1000}},
	}
	session, cleanup := startRefineFurnaceSession(t, gw)
	defer cleanup()

	if err := farming.NewRefinePotRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("refine furnace: %v", err)
	}
	if got := gw.startCalls.Load(); got != 3 {
		t.Fatalf("LESS_COIN 两次后第三次应成功, start calls = %d, want 3", got)
	}
	payload := gw.lastStartPayload()
	batches := int32(payload[0])<<24 | int32(payload[1])<<16 | int32(payload[2])<<8 | int32(payload[3])
	if batches != 5 {
		t.Fatalf("最终批数 = %d, want 5 (20→10→5)", batches)
	}
}

func TestRefinePotRoutine_LessAirHalvesBatches(t *testing.T) {
	gw := &refineFurnaceGateway{
		awardCode:    protocol.RefineFurnaceSuccess,
		selectCode:   protocol.RefineFurnaceSuccess,
		startCode:    protocol.RefineFurnaceSuccess,
		lessAirTimes: 2,
		materials:    []protocol.RefineFurnaceMaterial{{ItemID: 7925, Count: 1000}},
	}
	session, cleanup := startRefineFurnaceSession(t, gw)
	defer cleanup()

	if err := farming.NewRefinePotRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("refine furnace: %v", err)
	}
	if got := gw.startCalls.Load(); got != 3 {
		t.Fatalf("LESS_AIR 两次后第三次应成功, start calls = %d, want 3", got)
	}
	payload := gw.lastStartPayload()
	batches := int32(payload[0])<<24 | int32(payload[1])<<16 | int32(payload[2])<<8 | int32(payload[3])
	if batches != 5 {
		t.Fatalf("最终批数 = %d, want 5 (20→10→5)", batches)
	}
}

func TestRefinePotRoutine_SelectFailedNoStart(t *testing.T) {
	gw := &refineFurnaceGateway{
		awardCode:  protocol.RefineFurnaceSuccess,
		selectCode: protocol.RefineFurnaceFailed,
		startCode:  protocol.RefineFurnaceSuccess,
		materials:  []protocol.RefineFurnaceMaterial{{ItemID: 7923, Count: 1000}},
	}
	session, cleanup := startRefineFurnaceSession(t, gw)
	defer cleanup()

	if err := farming.NewRefinePotRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("refine furnace: %v", err)
	}
	if got := gw.startCalls.Load(); got != 0 {
		t.Fatalf("选择失败时不应开炉, start calls = %d", got)
	}
}

func TestRefinePotRoutine_PackFull(t *testing.T) {
	gw := &refineFurnaceGateway{
		awardCode: protocol.RefineFurnacePackFull,
		materials: []protocol.RefineFurnaceMaterial{{ItemID: 7923, Count: 1000}},
	}
	session, cleanup := startRefineFurnaceSession(t, gw)
	defer cleanup()

	err := farming.NewRefinePotRoutine().Execute(context.Background(), session, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
}
