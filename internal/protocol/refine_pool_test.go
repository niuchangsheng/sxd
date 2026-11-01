package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 回包样本来源于 haha.pcapng 实机抓包 (Module 282, Action 0 / Action 1)。
const (
	refinePoolOpenHex = "000a0a02000000000000000001090200000000000000000108020000000000000000010702000000000000000001060200000000000000000105020000000000000000010402000000000000000001030200000000000000000102020000000000000000010102000000000000000001090500000000040007a120320000a1360323"
	refineExecHex     = "0b000a0101000062700000627000080100014820000148200005010000e5b00000e5b00004010000c4e00000c4e0000201000083400000834000070100012750000127500003010000a4100000a410000a01000041a0000041a0000901000020d0000020d000060100010680000106800009000a080e01000a000100001297000000050000070e03001e0001000012970000000f0000060e0200140001000012970000000a0000050e03001e0001000012970000000f0000040e0200140001000012970000000a0000030b0600280001000012970000001400010000000e00000001020e0200140001000012970000000a0000010e01000a0001000012970000000500000a0e0200140001000012970000000a0000090e0200140001000012970000000a00000500000000040000a1a40323"
)

func TestRefinePoolBuilders(t *testing.T) {
	open := protocol.BuildRefinePoolOpenPacket(0x00EB000D)
	if open.ActionID != protocol.ActionIDRefinePoolOpen {
		t.Fatalf("open ActionID = 0x%08X, want 0x%08X", open.ActionID, protocol.ActionIDRefinePoolOpen)
	}
	if got, want := hex.EncodeToString(open.Payload), "00eb000d"; got != want {
		t.Fatalf("open payload = %s, want %s", got, want)
	}

	panel := protocol.BuildRefinePoolPanelPacket(protocol.ActionIDRefinePoolOpen)
	if got, want := hex.EncodeToString(panel.Payload), "00000004011a0000"; got != want {
		t.Fatalf("panel payload = %s, want %s", got, want)
	}

	// 一键淬炼: 铜钱消耗 + 10 个已完成槽位。
	exec := protocol.BuildRefineExecutePacket(
		protocol.RefinePoolQualityCoins,
		protocol.RefinePoolTypeOneKey,
		10,
		protocol.ActionIDRefinePoolPanel,
	)
	if exec.ActionID != protocol.ActionIDRefineExecute {
		t.Fatalf("exec ActionID = 0x%08X, want 0x%08X", exec.ActionID, protocol.ActionIDRefineExecute)
	}
	if got, want := hex.EncodeToString(exec.Payload), "05080a02160005"; got != want {
		t.Fatalf("exec payload = %s, want %s", got, want)
	}
}

func TestParseRefinePoolOpenResponse(t *testing.T) {
	raw, err := hex.DecodeString(refinePoolOpenHex)
	if err != nil {
		t.Fatal(err)
	}
	panel, err := protocol.ParseRefinePoolOpenResponse(raw)
	if err != nil {
		t.Fatalf("parse open response: %v", err)
	}
	if panel.Level != 5 || panel.CurSeq != 9 || panel.OneKeyLockFlag != protocol.RefinePoolProduceUnlock {
		t.Fatalf("unexpected header: %+v", panel)
	}
	if panel.CoinsNeed != 500000 || panel.IngotNeed != 50 {
		t.Fatalf("unexpected cost: coins=%d ingot=%d", panel.CoinsNeed, panel.IngotNeed)
	}
	if panel.LianQiJingHua != 41270 || panel.XuanTie != 803 {
		t.Fatalf("unexpected assets: lianqi=%d xuantie=%d", panel.LianQiJingHua, panel.XuanTie)
	}
	if len(panel.Irons) != 10 || panel.DoneCount() != 10 {
		t.Fatalf("irons=%d done=%d, want 10/10", len(panel.Irons), panel.DoneCount())
	}
}

func TestParseRefineExecuteResponse(t *testing.T) {
	raw, err := hex.DecodeString(refineExecHex)
	if err != nil {
		t.Fatal(err)
	}
	res, err := protocol.ParseRefineExecuteResponse(raw)
	if err != nil {
		t.Fatalf("parse refine response: %v", err)
	}
	if len(res.Awards) != 10 {
		t.Fatalf("awards = %d, want 10", len(res.Awards))
	}
	var essence int32
	for _, award := range res.Awards {
		for _, item := range award.Items {
			if item.ItemID == protocol.ItemRefineEssence {
				essence += item.Count
			}
		}
		for _, item := range award.ExtraItems {
			if item.ItemID == protocol.ItemRefineEssence {
				essence += item.Count
			}
		}
	}
	if essence != 110 {
		t.Fatalf("essence gained = %d, want 110", essence)
	}
	if got := res.LianQiJingHua - 41270; got != essence {
		t.Fatalf("lianqi delta = %d, want %d", got, essence)
	}
	if len(res.Awards[5].ExtraItems) != 1 || res.Awards[5].ExtraItems[0].ItemID != 14 {
		t.Fatalf("crit extra reward mismatch: %+v", res.Awards[5].ExtraItems)
	}
}