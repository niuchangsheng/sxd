package farming

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
)

func TestFarmPurchaseCandidates_PreferredThenOthers(t *testing.T) {
	seeds := []protocol.FarmSeedItem{
		{SeedID: 175},
		{SeedID: 169},
		{SeedID: 176},
		{SeedID: 171},
		{SeedID: 180},
		{SeedID: 169}, // 重复项只保留一次
	}
	got := farmPurchaseCandidates(seeds)
	want := []int32{176, 169, 171, 175, 180}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

func TestFarmPurchaseCandidates_CheapestCoinSeedFirst(t *testing.T) {
	seeds := []protocol.FarmSeedItem{
		{SeedID: 171, BuyNeedCoin: 1_000_000},
		{SeedID: 174, BuyNeedCoin: 500_000},
		{SeedID: 175, BuyNeedCoin: 200_000},
		{SeedID: 176, BuyNeedIngot: 30},
		{SeedID: 169, BuyNeedIngot: 20},
	}
	got := farmPurchaseCandidates(seeds)
	// 主力种子 176/169/171/174 优先（由调用方按元宝价跳过），
	// 其余未知种子按铜钱价格升序（175 在 174 之后）。
	want := []int32{176, 169, 171, 174, 175}
	rank := map[int32]int{}
	for i, id := range got {
		rank[id] = i
	}
	if rank[176] != 0 || rank[169] != 1 || rank[171] != 2 || rank[174] != 3 || rank[175] != 4 {
		t.Fatalf("candidates order = %v, want %v", got, want)
	}
}

// TestFarmCoinCandidateSummary 验证诊断日志只汇总铜钱计价且仍有次数的种子。
func TestFarmCoinCandidateSummary(t *testing.T) {
	seeds := []protocol.FarmSeedItem{
		{SeedID: 176, BuyNeedCoin: 1_500_000, RemainTimes: 3},
		{SeedID: 169, BuyNeedIngot: 30, RemainTimes: 3},
		{SeedID: 171, BuyNeedCoin: 0, RemainTimes: 3},
		{SeedID: 175, BuyNeedCoin: 200_000, RemainTimes: 0},
		{SeedID: 180, BuyNeedCoin: 300_000, RemainTimes: 1},
	}
	if got := farmCoinCandidateSummary(seeds); got != "176:1500000:3,180:300000:1" {
		t.Fatalf("summary = %q", got)
	}
}

func TestFarmCanAffordSeed_KeepsEqualReserve(t *testing.T) {
	cases := []struct {
		coins int64
		price int32
		want  bool
	}{
		{coins: 0, price: 1_000_000, want: true},          // 余额未知不拦截
		{coins: 1_900_000, price: 1_500_000, want: false}, // s0 实测：1.5M 占余额 79%
		{coins: 3_000_000, price: 1_500_000, want: true},
		{coins: 1_000_000, price: 500_000, want: true},
		{coins: 900_000, price: 500_000, want: false},
	}
	for _, c := range cases {
		if got := farmCanAffordSeed(c.coins, c.price); got != c.want {
			t.Errorf("farmCanAffordSeed(%d, %d) = %v, want %v", c.coins, c.price, got, c.want)
		}
	}
}

func TestFarmPlantCandidates_OnlyOwnedPreferredFirst(t *testing.T) {
	owned := map[int32]int32{
		176: 0,
		169: 2,
		171: 0,
		175: 1,
		180: 3,
	}
	got := farmPlantCandidates(owned)
	want := []int32{169, 175, 180}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

func TestFarmPlantCandidates_Empty(t *testing.T) {
	if got := farmPlantCandidates(map[int32]int32{176: 0, 169: 0}); len(got) != 0 {
		t.Fatalf("expected no candidates, got %v", got)
	}
}

// TestFarmSeedBuyBackoffPerRole 验证购买退避按角色隔离。
//
// 2026-11-14 实测: s1 的 buy_new_herbs 被拒绝时，旧实现用包级退避
// 同时阻塞了 s0 的正常补仓。
func TestFarmSeedBuyBackoffPerRole(t *testing.T) {
	until := time.Now().Add(time.Hour).Unix()
	blockFarmSeedBuy("role-s1", until)
	if got := farmSeedBuyBlockedUntil("role-s1"); got != until {
		t.Fatalf("s1 backoff = %d, want %d", got, until)
	}
	if got := farmSeedBuyBlockedUntil("role-s0"); got != 0 {
		t.Fatalf("s0 backoff = %d, want 0 (按角色隔离)", got)
	}
}

// TestTryLegacyCoinTreePlanting 验证摇钱树次数 >0 时会完成伙伴资格检查并播种。
//
// 实机受免费次数 (count=0, 回包 12) 限制无法立即观察到成功；本测用 mock
// 固定 count=1，验证 13/8 PASS(21) 后 13/5 返回 8(SUCCESS) 的完整链路。
func TestTryLegacyCoinTreePlanting(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDFarmCoinTreeInfo:
				w := protocol.NewWriter()
				w.WriteInt32(1) // count
				w.WriteInt32(0) // remain buy
				w.WriteInt32(2) // ingot
				w.WriteInt32(0) // recover cd
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			case protocol.ActionIDFarmRoleList:
				w := protocol.NewWriter()
				w.WriteUint16(1)
				w.WriteInt32(884402)
				w.WriteString("伙伴")
				w.WriteString("sign")
				w.WriteInt32(94)
				w.WriteInt64(1000)
				w.WriteInt64(2000)
				w.WriteInt32(1)
				w.WriteInt32(2)
				w.WriteInt32(3)
				w.WriteUint8(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			case protocol.ActionIDFarmCheckRoleState:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FarmCheckRoleStatePass}))
			case protocol.ActionIDFarmPlantHerbs:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FarmPlantSuccess}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-legacy-farm-test",
		RoleName:   "摇钱树测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	planted, retryAfter := tryLegacyCoinTreePlanting(context.Background(), sess,
		[]protocol.FarmField{{LandID: 162236, State: 1}}, time.Second)
	if planted != 1 {
		t.Fatalf("planted = %d, want 1", planted)
	}
	if retryAfter != 0 {
		t.Fatalf("retryAfter = %v, want 0", retryAfter)
	}
}

// TestFarmLegacyPlantBackoffPerRole 验证旧版播种退避按角色隔离。
func TestFarmLegacyPlantBackoffPerRole(t *testing.T) {
	until := time.Now().Add(time.Hour).Unix()
	blockFarmLegacyPlant("role-legacy-s1", until)
	if got := farmLegacyPlantBlockedUntil("role-legacy-s1"); got != until {
		t.Fatalf("s1 backoff = %d, want %d", got, until)
	}
	if got := farmLegacyPlantBlockedUntil("role-legacy-s0"); got != 0 {
		t.Fatalf("s0 backoff = %d, want 0", got)
	}
}

// TestHerbGardenRoutine_RetriesAnotherSeedAfterDrop 验证首个种子购买被服务端
// 静默丢弃后会换用下一种铜钱种子；只有连续两个种子都无回包才进入 6 小时退避。
func TestHerbGardenRoutine_RetriesAnotherSeedAfterDrop(t *testing.T) {
	oldTimeout := farmCallTimeout
	farmCallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { farmCallTimeout = oldTimeout })

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var buyCalls atomic.Int32
	var buyMu sync.Mutex
	bought := make([]int32, 0, 2)
	var serverWg sync.WaitGroup
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
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
					0x00, 0x00, 0x9a, 0x37,
					0x00, 0x00, 0x00, 0x08, 0x6f, 0x93, 0xac, 0xf8,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0a, 0xd1,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0f, 0x99,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			case protocol.ActionIDSaintAreaStatus:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.SaintAreaStatusClose)
				w.WriteUint16(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDSaintAreaStatus, w.Bytes()))
			case protocol.ActionIDFarmGetInfo:
				resp, _ := protocol.BuildFarmGetInfoResultPacket(protocol.FarmGetInfoResult{Fields: []protocol.FarmField{
					{LandID: 10, State: 1},
				}})
				_ = protocol.WritePacket(c2, resp)
			case protocol.ActionIDFarmGetPlantState:
				w := protocol.NewWriter()
				w.WriteUint16(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDFarmGetPlantState, w.Bytes()))
			case protocol.ActionIDFarmGetSeeds:
				w := protocol.NewWriter()
				w.WriteUint16(2)
				// 176: 首选种子，本测模拟服务端对该种子静默丢弃。
				w.WriteInt32(176)
				w.WriteInt32(0)
				w.WriteInt32(3)
				w.WriteInt32(100)
				w.WriteInt32(0)
				// 169: 备选种子，可正常购买。
				w.WriteInt32(169)
				w.WriteInt32(0)
				w.WriteInt32(3)
				w.WriteInt32(50)
				w.WriteInt32(0)
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDFarmGetSeeds, w.Bytes()))
			case protocol.ActionIDFarmBuySeed:
				buyCalls.Add(1)
				r := protocol.NewReader(pkt.Payload)
				seedID, _ := r.ReadInt32()
				buyMu.Lock()
				bought = append(bought, seedID)
				buyMu.Unlock()
				if seedID == 169 {
					_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDFarmBuySeed,
						[]byte{protocol.FarmStatusSuccess}))
				}
			case protocol.ActionIDFarmPlant:
				req, _ := protocol.ParseFarmPlantRequest(pkt.Payload)
				resp, _ := protocol.BuildFarmPlantResultPacket(protocol.FarmPlantResult{
					Success: true, LandID: req.LandID, Cooldown: 28800,
				})
				_ = protocol.WritePacket(c2, resp)
			case protocol.ActionIDFarmHarvest:
				resp, _ := protocol.BuildFarmHarvestResultPacket(protocol.FarmHarvestResult{Success: false})
				_ = protocol.WritePacket(c2, resp)
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-farmer-retry", RoleName: "换种重试", Username: "farmer_retry",
		Hash: "dummy", ServerAddr: "farm-retry-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) { return c1, nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("session.Start 失败: %v", err)
	}
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", sess.State())
	}

	execCtx, execCancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer execCancel()
	if err := NewHerbGardenRoutine().Execute(execCtx, sess, nil); err != nil &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("药园 Routine 执行失败: %v", err)
	}

	if got := buyCalls.Load(); got != 2 {
		t.Fatalf("应尝试 2 种种子购买, 实际 %d 次", got)
	}
	buyMu.Lock()
	got := append([]int32(nil), bought...)
	buyMu.Unlock()
	if len(got) != 2 || got[0] != 176 || got[1] != 169 {
		t.Fatalf("购买顺序 = %v, 期望 [176 169]", got)
	}
	// 成功购买后不应进入退避。
	if until := farmSeedBuyBlockedUntil("role-farmer-retry"); until != 0 {
		t.Fatalf("成功补仓后不应退避, blocked_until=%d", until)
	}
}
