package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 2026-11-14 qq s0 实机抓包/探针回包。
const (
	devilPanelHex  = "02010000000000000000000000000000000000000000000000060000000000000000000000000000000000000000000000000001869f0000005a000001b50003000001e6000000050000000000000014000001e7000000000000000000000014000001e8000000000000000000000014000100001d8e000fe4bc97e4bb99e997afe9ad94e7958c0012e5a4aae8999ae5ae9ae58583e4bb99e5909b0002733000067665727963640000012c000000030000000601010000000301010000002c0000000a000000140000000c0000000c00000014"
	devilM6DataHex = "02000100000001010000012c0006e5908ce5b098030000000200000000690000000000000006000000010000000900000000000000000001010000000000000000000000000000000200000002000054d000000001000054cf0000000100000000000000000000000200000000000000140000000600000009000000010000"
)

func TestParseDevilMatchPanel(t *testing.T) {
	raw, err := hex.DecodeString(devilPanelHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	panel, err := protocol.ParseDevilMatchPanel(raw)
	if err != nil {
		t.Fatalf("ParseDevilMatchPanel: %v", err)
	}
	if panel.Result != protocol.DevilResultSuccess || panel.IsComplete != 1 || panel.LevelSection != 6 {
		t.Fatalf("面板头部不符: %+v", panel)
	}
	if len(panel.Souls) != 3 {
		t.Fatalf("魔灵条目数 = %d, want 3", len(panel.Souls))
	}
	if panel.Souls[0].RoleID != 486 || panel.Souls[0].TotalSoul != 5 ||
		panel.Souls[0].WeekSoul != 0 || panel.Souls[0].WeekMaxSoul != 20 {
		t.Fatalf("魔灵[0] 不符: %+v", panel.Souls[0])
	}
	if panel.WeekStone != 10 || panel.WeekStoneMax != 20 {
		t.Fatalf("阴阳石本周进度 = %d/%d, want 10/20", panel.WeekStone, panel.WeekStoneMax)
	}
	if panel.WeekStone2 != 12 || panel.WeekStone2Max != 20 {
		t.Fatalf("隐仙石本周进度 = %d/%d, want 12/20", panel.WeekStone2, panel.WeekStone2Max)
	}
}

func TestParseDevilMissionDataM6(t *testing.T) {
	raw, err := hex.DecodeString(devilM6DataHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	data, err := protocol.ParseDevilMissionData(protocol.DevilMissionYinYang, raw)
	if err != nil {
		t.Fatalf("ParseDevilMissionData: %v", err)
	}
	if data.Result != protocol.DevilResultSuccess {
		t.Fatalf("result = %d", data.Result)
	}
	if len(data.TeamIDs) != 2 || data.TeamIDs[0] != 2 || data.TeamIDs[1] != 1 {
		t.Fatalf("怪物队伍 = %v, want [2 1]", data.TeamIDs)
	}
	if len(data.Refines) != 1 {
		t.Fatalf("炼化条目 = %d, want 1", len(data.Refines))
	}
	r := data.Refines[0]
	if r.Got != 0 || r.CurrentBout != 0 || r.NeedBout != 2 || r.Week != 0 || r.WeekMax != 20 {
		t.Fatalf("炼化进度不符: %+v", r)
	}
}

func TestBuildDevilPackets(t *testing.T) {
	start := protocol.BuildDevilStartSinglePacket(protocol.DevilMissionYinYang)
	if start.ActionID != protocol.ActionIDDevilStartExpeditionSingle || !start.HasPrevAct || len(start.Payload) != 5 || start.Payload[0] != 68 {
		t.Fatalf("429/63 封包不符: %+v", start)
	}
	move := protocol.BuildDevilMoveToPacket(774, 481, 436, 413)
	if move.ActionID != protocol.ActionIDDevilMoveTo || len(move.Payload) != 12 {
		t.Fatalf("429/17 封包不符: %+v", move)
	}
	if got := hex.EncodeToString(move.Payload[:8]); got != "030601e101b4019d" {
		t.Fatalf("429/17 坐标编码不符: %s", got)
	}
	fight := protocol.BuildDevilFightMonsterPacket(protocol.DevilMissionYinYang, 1)
	if fight.ActionID != protocol.ActionIDDevilFightMonsterM6 || len(fight.Payload) != 8 {
		t.Fatalf("429/69 封包不符: %+v", fight)
	}
	if got := protocol.DevilFightAction(protocol.DevilMissionMonsterGod); got != protocol.ActionIDDevilFightMonsterM5 {
		t.Fatalf("魔神战斗动作 = 0x%08X", got)
	}
}
