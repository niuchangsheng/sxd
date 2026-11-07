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

// spiritPoolGateway 模拟淬炼池网关: 记录 refine 请求并回放指定结果码与奖励。
type spiritPoolGateway struct {
	result       uint8
	items        []protocol.RefinePoolAwardItem
	lingCaiItems []protocol.RefinePoolAwardItem
	awards       int
	// functionClosed 为 true 时模拟功能未开启, 开放状态查询返回 FALSE。
	functionClosed bool

	openCalls  atomic.Int32
	checkCalls atomic.Int32

	mu             sync.Mutex
	refinePayloads [][]byte
}

func (g *spiritPoolGateway) capture(payload []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cp := append([]byte(nil), payload...)
	g.refinePayloads = append(g.refinePayloads, cp)
}

func (g *spiritPoolGateway) lastRefinePayload() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.refinePayloads) == 0 {
		return nil
	}
	return g.refinePayloads[len(g.refinePayloads)-1]
}

// buildSpiritPoolPanelPayload 构造 open_panel 回包, 2 个已完成槽位。
func buildSpiritPoolPanelPayload() []byte {
	w := protocol.NewWriter()
	w.WriteUint16(2)
	for _, seq := range []int8{1, 2} {
		w.WriteInt8(seq)
		w.WriteUint8(protocol.RefinePoolProduceDone)
		w.WriteInt32(3600)
		w.WriteInt32(0)
		w.WriteInt8(1)
	}
	w.WriteInt8(1)  // cur_seq
	w.WriteInt8(5)  // level
	w.WriteInt32(0) // exp
	w.WriteUint8(protocol.RefinePoolProduceUnlock)
	w.WriteInt32(500000) // coins_need
	w.WriteInt8(50)      // ingot_need
	w.WriteInt32(100)    // lian_qi_jing_hua
	w.WriteInt16(10)     // xuan_tie
	return w.Bytes()
}

// buildSpiritPoolRefinePayload 构造 refine 回包。
func buildSpiritPoolRefinePayload(result uint8, awardCount int, items, lingCaiItems []protocol.RefinePoolAwardItem) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(2)
	for _, seq := range []int8{1, 2} {
		w.WriteInt8(seq)
		w.WriteUint8(protocol.RefinePoolProduceDoing)
		w.WriteInt32(3600)
		w.WriteInt32(3600)
		w.WriteInt8(0)
	}
	w.WriteInt8(1) // cur_seq
	w.WriteUint16(uint16(awardCount))
	for i := 0; i < awardCount; i++ {
		w.WriteInt8(int8(i + 1))
		w.WriteUint8(protocol.RefinePoolResultSuccess)
		w.WriteInt8(1)
		w.WriteInt16(10)
		w.WriteUint16(uint16(len(items)))
		for _, item := range items {
			w.WriteInt32(item.ItemID)
			w.WriteInt32(item.Count)
		}
		w.WriteUint16(uint16(len(lingCaiItems)))
		for _, item := range lingCaiItems {
			w.WriteInt32(item.ItemID)
			w.WriteInt32(item.Count)
		}
	}
	w.WriteInt8(5)  // level
	w.WriteInt32(0) // exp
	w.WriteUint8(protocol.RefinePoolProduceUnlock)
	w.WriteInt32(110) // lian_qi_jing_hua
	w.WriteInt16(10)  // xuan_tie
	return w.Bytes()
}

// startSpiritPoolSession 启动一条接入模拟网关的会话, 返回清理函数。
func startSpiritPoolSession(t *testing.T, gw *spiritPoolGateway) (*client.RoleSession, func()) {
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
			case protocol.ActionIDPlayerGetFunctions:
				gw.checkCalls.Add(1)
				w := protocol.NewWriter()
				if gw.functionClosed {
					w.WriteUint16(0)
				} else {
					w.WriteUint16(1)
					w.WriteInt32(protocol.FunctionIDRefinePool)
					w.WriteUint8(0) // isPlayed
				}
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetFunctions, w.Bytes()))
			case protocol.ActionIDRefinePoolOpen:
				gw.openCalls.Add(1)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDRefinePoolOpen, buildSpiritPoolPanelPayload()))
			case protocol.ActionIDRefinePoolPanel:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDRefinePoolPanel, []byte{0, 0, 0, 0, 0}))
			case protocol.ActionIDRefineExecute:
				gw.capture(pkt.Payload)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDRefineExecute,
					buildSpiritPoolRefinePayload(gw.result, gw.awards, gw.items, gw.lingCaiItems)))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-spirit-pool",
		RoleName:   "淬炼测试",
		Username:   "spirit_pool_user",
		Hash:       "dummy_spirit_pool_hash",
		ServerAddr: "spirit-pool-test-pipe",
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

func executeSpiritPool(t *testing.T, session *client.RoleSession, cfg farming.SpiritPoolConfig) error {
	t.Helper()
	routine := farming.NewSpiritPoolRoutine(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return routine.Execute(ctx, session, nil)
}

func assertRefinePayload(t *testing.T, payload []byte, target uint8) {
	t.Helper()
	want := []byte{target, protocol.RefinePoolCostCoins, protocol.RefinePoolTypeOneKey}
	if len(payload) < len(want) {
		t.Fatalf("refine payload 过短: %x", payload)
	}
	for i := range want {
		if payload[i] != want[i] {
			t.Fatalf("refine payload[%d] = %d, want %d (payload=%x)", i, payload[i], want[i], payload)
		}
	}
}

func TestSpiritPoolRoutine_XuanTieSuccess(t *testing.T) {
	gw := &spiritPoolGateway{
		result: protocol.RefinePoolResultSuccess,
		awards: 2,
		items: []protocol.RefinePoolAwardItem{
			{ItemID: protocol.ItemRefineEssence, Count: 5},
			{ItemID: protocol.ItemXuanTie, Count: 1},
		},
	}
	session, cleanup := startSpiritPoolSession(t, gw)
	defer cleanup()

	if err := executeSpiritPool(t, session, farming.DefaultSpiritPoolConfig()); err != nil {
		t.Fatalf("玄铁淬炼应成功, 实际: %v", err)
	}
	assertRefinePayload(t, gw.lastRefinePayload(), protocol.RefinePoolTargetXuanTie)
}

func TestSpiritPoolRoutine_LingCaiSuccess(t *testing.T) {
	gw := &spiritPoolGateway{
		result: protocol.RefinePoolResultSuccess,
		awards: 2,
		lingCaiItems: []protocol.RefinePoolAwardItem{
			{ItemID: 14, Count: 2},
		},
	}
	session, cleanup := startSpiritPoolSession(t, gw)
	defer cleanup()

	cfg := farming.SpiritPoolConfig{Target: farming.SpiritPoolRefineLingCai}
	if err := executeSpiritPool(t, session, cfg); err != nil {
		t.Fatalf("灵材提炼应成功, 实际: %v", err)
	}
	assertRefinePayload(t, gw.lastRefinePayload(), protocol.RefinePoolTargetLingCai)
}

func TestSpiritPoolRoutine_LackOfCoins(t *testing.T) {
	gw := &spiritPoolGateway{result: protocol.RefinePoolResultLackOfCoins}
	session, cleanup := startSpiritPoolSession(t, gw)
	defer cleanup()

	err := executeSpiritPool(t, session, farming.DefaultSpiritPoolConfig())
	if !errors.Is(err, scheduler.ErrConditionNotMet) {
		t.Fatalf("铜钱不足应返回 ErrConditionNotMet, 实际: %v", err)
	}
}

func TestSpiritPoolRoutine_SuccessWithoutReward(t *testing.T) {
	gw := &spiritPoolGateway{result: protocol.RefinePoolResultSuccess, awards: 0}
	session, cleanup := startSpiritPoolSession(t, gw)
	defer cleanup()

	if err := executeSpiritPool(t, session, farming.DefaultSpiritPoolConfig()); err == nil {
		t.Fatal("成功码无产出应返回错误")
	}
}

func TestSpiritPoolRoutine_FunctionClosedSkipsOpen(t *testing.T) {
	gw := &spiritPoolGateway{functionClosed: true}
	session, cleanup := startSpiritPoolSession(t, gw)
	defer cleanup()

	if err := executeSpiritPool(t, session, farming.DefaultSpiritPoolConfig()); err != nil {
		t.Fatalf("功能未开启应正常跳过, 实际: %v", err)
	}
	if got := gw.checkCalls.Load(); got != 1 {
		t.Fatalf("应查询 1 次功能开放状态, 实际 %d", got)
	}
	if got := gw.openCalls.Load(); got != 0 {
		t.Fatalf("功能未开启时不应发送 open_panel, 实际 %d 次", got)
	}
	if got := len(gw.lastRefinePayload()); got != 0 {
		t.Fatalf("功能未开启时不应发送 refine 请求, payload_len=%d", got)
	}
}

func TestParseSpiritPoolRefineTarget(t *testing.T) {
	cases := []struct {
		raw    string
		target farming.SpiritPoolRefineTarget
		ok     bool
	}{
		{"", farming.SpiritPoolRefineXuanTie, true},
		{"xuantie", farming.SpiritPoolRefineXuanTie, true},
		{"玄铁", farming.SpiritPoolRefineXuanTie, true},
		{"lingcai", farming.SpiritPoolRefineLingCai, true},
		{" 灵材 ", farming.SpiritPoolRefineLingCai, true},
		{"unknown", 0, false},
	}
	for _, tc := range cases {
		got, ok := farming.ParseSpiritPoolRefineTarget(tc.raw)
		if ok != tc.ok || got != tc.target {
			t.Errorf("ParseSpiritPoolRefineTarget(%q) = (%v, %v), want (%v, %v)",
				tc.raw, got, ok, tc.target, tc.ok)
		}
	}
}
