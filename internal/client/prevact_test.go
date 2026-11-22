package client_test

import (
	"encoding/binary"
	"sync"
	"testing"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func TestPrevActChain_HeartbeatExcludedAndDirtyRewind(t *testing.T) {
	const (
		hb   uint32 = protocol.ActionIDPlayerHeartbeat
		actA uint32 = 0x00040000
		actB uint32 = 0x03530005
		actC uint32 = 0x03530006
	)
	chain := client.NewPrevActChain(hb)

	// 1. 仅发送心跳不产生业务锚点
	chain.RecordSent(hb)
	if got := chain.LastSent(); got != hb {
		t.Fatalf("LastSent = 0x%08X, want heartbeat", got)
	}
	if got := chain.BusinessAnchor(); got != 0 {
		t.Fatalf("heartbeat should not become BusinessAnchor, got 0x%08X", got)
	}

	// 2. 业务请求发送并收到确认后，即使插入心跳也不漂移
	chain.RecordSent(actA)
	chain.RecordAck(actA)
	chain.RecordSent(hb)
	chain.RecordAck(hb)
	if got := chain.BusinessAnchor(); got != actA {
		t.Fatalf("BusinessAnchor after heartbeat = 0x%08X, want 0x%08X", got, actA)
	}

	// 3. actB 发送但被静默丢弃 -> MarkDirty 后回退到 actA
	chain.RecordSent(actB)
	chain.MarkDirty()
	if !chain.IsDirty() {
		t.Fatal("expected chain dirty")
	}
	if got := chain.BusinessAnchor(); got != actA {
		t.Fatalf("dirty BusinessAnchor = 0x%08X, want lastAck 0x%08X", got, actA)
	}

	// 4. actC 收到回包确认后恢复正常链
	chain.RecordSent(actC)
	chain.RecordAck(actC)
	if chain.IsDirty() {
		t.Fatal("expected dirty cleared after business ack")
	}
	if got := chain.BusinessAnchor(); got != actC {
		t.Fatalf("recovered BusinessAnchor = 0x%08X, want 0x%08X", got, actC)
	}

	// 5. Reset 清空全部连接态
	chain.Reset()
	if chain.LastSent() != 0 || chain.LastBusiness() != 0 || chain.LastAck() != 0 || chain.LastRx() != 0 || chain.IsDirty() {
		t.Fatalf("Reset did not clear all fields")
	}
}

func TestPrevActChain_StampPayloadModes(t *testing.T) {
	chain := client.NewPrevActChain(protocol.ActionIDPlayerHeartbeat)
	chain.RecordSent(0x00020029)

	// PrevActExplicit 绝不被覆盖，即使是 Mod 4 历史模块
	explicit := protocol.NewPacketWithExplicitPrevAct(0x00040001, []byte{0, 0, 0, 7}, 0xDEADBEEF)
	stampedExplicit := chain.StampPayload(explicit)
	if got := binary.BigEndian.Uint32(stampedExplicit.Payload[4:]); got != 0xDEADBEEF {
		t.Fatalf("explicit prevAct overwritten: got 0x%08X, want 0xDEADBEEF", got)
	}

	// PrevActAutoBusiness 自动写入业务锚点且不修改原包
	auto := protocol.NewPacketWithAutoPrevAct(0x03530005, []byte{0, 0, 0, 1})
	stampedAuto := chain.StampPayload(auto)
	if got := binary.BigEndian.Uint32(stampedAuto.Payload[4:]); got != 0x00020029 {
		t.Fatalf("auto prevAct = 0x%08X, want 0x00020029", got)
	}
	if got := binary.BigEndian.Uint32(auto.Payload[4:]); got != 0 {
		t.Fatalf("original packet payload was mutated: got 0x%08X", got)
	}

	// 空载荷背包查询兼容自动追加 4 字节锚点
	packPkt := protocol.BuildGetPackItemListPacket()
	stampedPack := chain.StampPayload(packPkt)
	if len(stampedPack.Payload) != 4 || binary.BigEndian.Uint32(stampedPack.Payload) != 0x00020029 {
		t.Fatalf("pack list payload = %X, want 00020029", stampedPack.Payload)
	}
}

func TestPrevActChain_RecentRxRingAndConcurrency(t *testing.T) {
	chain := client.NewPrevActChain(protocol.ActionIDPlayerHeartbeat)
	for i := uint32(1); i <= 40; i++ {
		chain.RecordRx(i)
	}
	recent := chain.RecentRx()
	if len(recent) != 32 {
		t.Fatalf("len(RecentRx) = %d, want 32", len(recent))
	}
	if recent[0] != 9 || recent[31] != 40 {
		t.Fatalf("RecentRx bounds = [%d..%d], want [9..40]", recent[0], recent[31])
	}

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(id uint32) {
			defer wg.Done()
			for i := uint32(0); i < 100; i++ {
				chain.RecordSent(0x01000000 | (id << 8) | i)
				chain.RecordRx(0x02000000 | (id << 8) | i)
				chain.RecordAck(0x01000000 | (id << 8) | i)
				_ = chain.BusinessAnchor()
				_ = chain.RecentRx()
			}
		}(uint32(g))
	}
	wg.Wait()
}
