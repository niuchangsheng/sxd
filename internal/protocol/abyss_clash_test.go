package protocol

import (
	"encoding/hex"
	"testing"
)

// liveAbyssClashStep1PanelHex 为 2026-11-24 抓包 818/1 回包（699 字节）：
// 3 个怪物、剩余挑战次数 5、积分 3000。
const liveAbyssClashStep1PanelHex = "000000000000000bb800000005000001a96b05b480000300000020000032890000263900005acc8b4134c000007a3c704200000000003f0000001f0000328800003da000000f9bb0407df300007a3c7042000000000026000000210000328a00002af5000040eb831fa2ce00007a3c704200000000002500140001707400093335363739313839310000000500000000000000000000d0e30009e6a5a0e69ba6e5aea200000063000000000000110e000170490009333536373638363934000000050000000000000000000170960009e58d8ee8908de4ba910000000600000000000000000000bff30008e696b0e6989f303900000066000012820000000000010c030007e5b08fe790b32e0000000500000000000025af0000998d00054d657263790000006d00002a36000000000001509b0009e9a38ee4b98be5909f0000000500000000000019ea000008f60012e29d89e9a296e99baae5af92e586b0e29d89000000040000000000001a49000025ac0009e58aa8e58aa8e788860000020000001bca00000000000012110009e593aae58ebbe4ba860000006e000000000000000000014a53000fe59e83e4b8b6e9b8a1e6b8b8e6888f0000006e000000000000110e0000aac0000fe58d97e6b3a2e6b9bee6828de58caa0000000500000000000020140001516d000ce6adbbe7a59ee5b08fe4b880000000660000000000002b1700014f7f000ce4bd9be9ad94e998b4e998b30000006d000021bf00000000000165fe0012e799bde5ab96e8bf98e5b181e8af9de5a49a000000060000000100000000000152150009e59490e6a3aee7919e0000006400000000000004a800015e15000ce9a38ee4ba91e4b880e58880000000050000000000000000000147a30006e7a7a6e5b091000000050000075000000000000003260006e98181e4b880000001ff0000000100000000000000060000000100000bb8"

// liveAbyssClashFightTailHex 为 818/4 成功回包的末尾 12 字节
// ([Long total_damage][Int win_flag])。
const liveAbyssClashFightTailHex = "00005c06347e72d500000001"

func TestParseAbyssClashStep1Panel_LiveCapture(t *testing.T) {
	raw, err := hex.DecodeString(liveAbyssClashStep1PanelHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	p, err := ParseAbyssClashStep1Panel(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Result != AbyssClashResultSuccess || p.Step != 1 ||
		p.Score != 3000 || p.FightNum != 5 || p.CostPower != 425 {
		t.Fatalf("panel = %+v", p)
	}
	if len(p.Monsters) != 3 {
		t.Fatalf("monsters = %d, want 3", len(p.Monsters))
	}
	ids := []int32{p.Monsters[0].StepMonsterID, p.Monsters[1].StepMonsterID, p.Monsters[2].StepMonsterID}
	want := []int32{32, 31, 33}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("monster order = %v, want %v", ids, want)
		}
	}
	if p.Monsters[0].MonsterTeamID != 12937 || p.Monsters[0].WinNum != 9785 ||
		p.Monsters[0].MaxHealth != 134400000000000 {
		t.Fatalf("monster[0] = %+v", p.Monsters[0])
	}
	if p.ZoneID != 6 || p.STTeamID != 1 || p.TotalScore != 3000 {
		t.Fatalf("tail: zone=%d team=%d total=%d", p.ZoneID, p.STTeamID, p.TotalScore)
	}
	// 抓包挑战请求为 [Int step_monster_id][PrevAct]。
	pkt := BuildAbyssClashStartFightPacket(31)
	if pkt.ActionID != ActionIDAbyssClashStartFight || len(pkt.Payload) != 8 || !pkt.HasPrevAct {
		t.Fatalf("fight packet = %+v", pkt)
	}
	if pkt.Payload[0] != 0 || pkt.Payload[1] != 0 || pkt.Payload[2] != 0 || pkt.Payload[3] != 31 {
		t.Fatalf("fight payload = %X", pkt.Payload)
	}
}

func TestParseAbyssClashFuncOpen(t *testing.T) {
	open, err := ParseAbyssClashFuncOpen(BuildAbyssClashFuncOpenResultPacket(&AbyssClashFuncOpen{
		OpenFlag: 1, Step: 1, PlayerSuperTownServerID: 1,
	}).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if open.OpenFlag != 1 || open.Step != 1 || open.PlayerSuperTownServerID != 1 {
		t.Fatalf("open = %+v", open)
	}
}

func TestParseAbyssClashStep2Panel_RoundTrip(t *testing.T) {
	panel := &AbyssClashPanel{
		Result: AbyssClashResultSuccess, IsFirstOpen: 0, Score: 100, FightNum: 3,
		CostPower: 10, NextStepTS: 123, PlayerWinNum: 7, AwardTimes: 2,
		ZoneID: 9, STTeamID: 4, TotalScore: 900,
		Monsters: []AbyssClashMonster{
			{StepMonsterID: 41, MonsterTeamID: 501, IsBoss: 1, BossCanFightNum: 2, WinNum: 1, LastHealth: 100, MaxHealth: 200},
		},
	}
	got, err := ParseAbyssClashStep2Panel(BuildAbyssClashStep2PanelResultPacket(panel).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Step != 2 || got.FightNum != 3 || got.PlayerWinNum != 7 || got.AwardTimes != 2 ||
		got.ZoneID != 9 || got.TotalScore != 900 {
		t.Fatalf("panel = %+v", got)
	}
	if len(got.Monsters) != 1 || got.Monsters[0].IsBoss != 1 || got.Monsters[0].BossCanFightNum != 2 {
		t.Fatalf("monsters = %+v", got.Monsters)
	}
}

func TestParseAbyssClashFightResult(t *testing.T) {
	res, err := ParseAbyssClashFightResult(BuildAbyssClashFightResultPacket(
		AbyssClashResultSuccess, 101181720261333, 1).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != AbyssClashResultSuccess || res.TotalDamage != 101181720261333 || res.WinFlag != 1 {
		t.Fatalf("fight = %+v", res)
	}
	// 实机回包末尾 12 字节。
	tail, _ := hex.DecodeString(liveAbyssClashFightTailHex)
	live := append([]byte{AbyssClashResultSuccess}, make([]byte, 40)...)
	copy(live[len(live)-12:], tail)
	got, err := ParseAbyssClashFightResult(live)
	if err != nil {
		t.Fatalf("live parse: %v", err)
	}
	if got.TotalDamage != 101181720261333 || got.WinFlag != 1 {
		t.Fatalf("live fight = %+v", got)
	}
	for _, code := range []uint8{
		AbyssClashResultFuncNotOpen, AbyssClashResultWrongStep,
		AbyssClashResultFailed, AbyssClashResultNoFightNum,
		AbyssClashResultBossCanNotFight, AbyssClashResultNumLimit,
		AbyssClashResultPackageFull,
	} {
		r, err := ParseAbyssClashFightResult([]byte{code})
		if err != nil || r.Result != code {
			t.Fatalf("code %d: %+v err %v", code, r, err)
		}
	}
}
