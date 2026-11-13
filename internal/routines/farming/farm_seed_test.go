package farming

import (
	"context"
	"net"
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
