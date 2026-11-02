// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestFactionBlessing_BuildRequests(t *testing.T) {
	godInfo := BuildFactionGodInfoPacket(ActionIDTownEnter)
	if godInfo.ActionID != ActionIDFactionGodInfo {
		t.Errorf("神像信息 ActionID 不匹配: 0x%08X", godInfo.ActionID)
	}
	if hex.EncodeToString(godInfo.Payload) != "00010000" {
		t.Errorf("神像信息请求应仅携带 prev_act, 实际: %s", hex.EncodeToString(godInfo.Payload))
	}

	blessingCount := BuildFactionGetBlessingCountPacket(ActionIDFactionGodInfo)
	if blessingCount.ActionID != ActionIDFactionGetBlessingCount {
		t.Errorf("祝福次数 ActionID 不匹配: 0x%08X", blessingCount.ActionID)
	}

	incense := BuildFactionIncensePacket(FactionOfferingSuhe)
	if incense.ActionID != ActionIDFactionIncense {
		t.Errorf("上香 ActionID 不匹配: 0x%08X", incense.ActionID)
	}
	// 线上服务端要求 incense 载荷严格为 4 字节供品编号，不能追加 prev_act
	if got, want := hex.EncodeToString(incense.Payload), "00000002"; got != want {
		t.Errorf("上香请求载荷不匹配: got=%s want=%s", got, want)
	}
}

func TestFactionBlessing_ParseGodInfo(t *testing.T) {
	payload, err := hex.DecodeString(
		"00000005" + // faction_god_level = 5
			"000004b0" + // faction_god_exp = 1200
			"00000bb8" + // require_exp = 3000
			"00000001" + // incense_count = 1
			"00000003" + // total_count = 3
			"00" + // is_cover = 0
			"0001" + // 记录条数 = 1
			"00000002" + // player_id = 2
			"0009e6a2a6e4b880e59cba" + // player_name = "梦一场"
			"00000003" + // incense_id = 3
			"000003e8" + // exp = 1000
			"12345678", // time
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	info, err := ParseFactionGodInfoResult(payload)
	if err != nil {
		t.Fatalf("解析神像信息失败: %v", err)
	}
	if info.FactionGodLevel != 5 || info.FactionGodExp != 1200 || info.RequireExp != 3000 {
		t.Errorf("神像等级/灵气解析不匹配: %+v", info)
	}
	if info.IncenseCount != 1 || info.TotalCount != 3 {
		t.Errorf("上香次数解析不匹配: %+v", info)
	}
	if info.IsCover != 0 {
		t.Errorf("is_cover 解析不匹配: %d", info.IsCover)
	}
	if remain := info.RemainingIncense(); remain != 2 {
		t.Errorf("剩余上香次数应为 2, 实际 %d", remain)
	}
	if len(info.Records) != 1 {
		t.Fatalf("上香记录条数不匹配: %d", len(info.Records))
	}
	record := info.Records[0]
	if record.PlayerID != 2 || record.PlayerName != "梦一场" || record.IncenseID != 3 || record.Exp != 1000 || record.Time != 0x12345678 {
		t.Errorf("上香记录解析不匹配: %+v", record)
	}
}

func TestFactionBlessing_RemainingIncenseClamp(t *testing.T) {
	info := &FactionGodInfo{IncenseCount: 5, TotalCount: 3}
	if remain := info.RemainingIncense(); remain != 0 {
		t.Errorf("超出总次数时剩余次数应钳制为 0, 实际 %d", remain)
	}

	var nilInfo *FactionGodInfo
	if remain := nilInfo.RemainingIncense(); remain != 0 {
		t.Errorf("空神像信息剩余次数应为 0, 实际 %d", remain)
	}
}

func TestFactionBlessing_ParseBlessingCount(t *testing.T) {
	payload, _ := hex.DecodeString("000000070000012c")
	count, err := ParseFactionBlessingCountResult(payload)
	if err != nil {
		t.Fatalf("解析祝福次数失败: %v", err)
	}
	if count.Count != 7 || count.CoinAdd != 300 {
		t.Errorf("祝福次数解析不匹配: %+v", count)
	}
}

func TestFactionBlessing_ParseIncenseResult(t *testing.T) {
	result, err := ParseFactionIncenseResult([]byte{FactionIncenseResultSuccess})
	if err != nil {
		t.Fatalf("解析上香结果失败: %v", err)
	}
	if result != FactionIncenseResultSuccess {
		t.Errorf("上香结果不匹配: %d", result)
	}

	if _, err := ParseFactionIncenseResult(nil); err == nil {
		t.Error("空载荷应返回解析错误")
	}
}

func TestFactionBlessing_ParseGodInfoTruncated(t *testing.T) {
	// 声明 1 条记录但未提供记录内容，必须报错而不是越界
	payload, _ := hex.DecodeString("00000005000004b000000bb80000000100000003000001")
	if _, err := ParseFactionGodInfoResult(payload); err == nil {
		t.Error("截断载荷应返回解析错误")
	}
}

func TestFactionOffering_Lookup(t *testing.T) {
	tests := []struct {
		id       int32
		name     string
		ingots   int32
		blessing int32
	}{
		{FactionOfferingWhiteSandalwood, "白檀香", 0, 10},
		{FactionOfferingSuhe, "苏合香", 30, 30},
		{FactionOfferingTianmu, "天木香", 90, 100},
	}
	for _, tc := range tests {
		offering, ok := GetFactionOffering(tc.id)
		if !ok {
			t.Errorf("供品 %d 未找到", tc.id)
			continue
		}
		if offering.Name != tc.name || offering.IngotCost != tc.ingots || offering.BlessingCount != tc.blessing {
			t.Errorf("供品 %d 数据不匹配: %+v", tc.id, offering)
		}
	}
	if _, ok := GetFactionOffering(99); ok {
		t.Error("不存在的供品应返回 false")
	}
}
