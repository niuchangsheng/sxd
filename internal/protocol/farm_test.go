// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestFarmCodec_BuildAndParse(t *testing.T) {
	// 1. 测试 FarmGetInfoRequest
	getInfoReq := FarmGetInfoRequest{PrevAct: 0x00010000}
	pktInfo, err := BuildFarmGetInfoPacket(getInfoReq)
	if err != nil {
		t.Fatalf("BuildFarmGetInfoPacket 失败: %v", err)
	}
	if pktInfo.ActionID != ActionIDFarmGetInfo {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFarmGetInfo, pktInfo.ActionID)
	}

	parsedInfo, err := ParseFarmGetInfoRequest(pktInfo.Payload)
	if err != nil {
		t.Fatalf("ParseFarmGetInfoRequest 失败: %v", err)
	}
	if parsedInfo.PrevAct != 0x00010000 {
		t.Errorf("PrevAct 不匹配: 期望 0x00010000, 实际 0x%08X", parsedInfo.PrevAct)
	}

	// 2. 测试 FarmPlantRequest 对齐官方请求
	// 官方 plant_new_herbs request=[Int,Int]: 0000000a000000a9 (LandID=10, SeedID=169)
	plantReq := FarmPlantRequest{
		LandID:       10,
		SeedOrRoleID: 169,
		PrevAct:      0x000D0016,
	}
	pktPlant, err := BuildFarmPlantPacket(plantReq)
	if err != nil {
		t.Fatalf("BuildFarmPlantPacket 失败: %v", err)
	}
	if pktPlant.ActionID != ActionIDFarmPlant {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFarmPlant, pktPlant.ActionID)
	}

	expectedHex := "0000000a000000a9"
	actualHex := hex.EncodeToString(pktPlant.Payload)
	if actualHex != expectedHex {
		t.Errorf("播种载荷 Hex 不匹配: 期望 %s, 实际 %s", expectedHex, actualHex)
	}

	parsedPlant, err := ParseFarmPlantRequest(pktPlant.Payload)
	if err != nil {
		t.Fatalf("ParseFarmPlantRequest 失败: %v", err)
	}
	if parsedPlant.LandID != 10 || parsedPlant.SeedOrRoleID != 169 {
		t.Errorf("播种解析结果不符: %+v", parsedPlant)
	}

	// 3. 测试 FarmHarvestRequest 对齐官方请求
	// 官方 harvest_new request=[Int]: 0000000a (LandID=10)
	harvestReq := FarmHarvestRequest{
		LandID:  10,
		PrevAct: 0x000D0018,
	}
	pktHarvest, err := BuildFarmHarvestPacket(harvestReq)
	if err != nil {
		t.Fatalf("BuildFarmHarvestPacket 失败: %v", err)
	}
	if pktHarvest.ActionID != ActionIDFarmHarvest {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFarmHarvest, pktHarvest.ActionID)
	}

	expectedHarvestHex := "0000000a"
	actualHarvestHex := hex.EncodeToString(pktHarvest.Payload)
	if actualHarvestHex != expectedHarvestHex {
		t.Errorf("收获载荷 Hex 不匹配: 期望 %s, 实际 %s", expectedHarvestHex, actualHarvestHex)
	}

	parsedHarvest, err := ParseFarmHarvestRequest(pktHarvest.Payload)
	if err != nil {
		t.Fatalf("ParseFarmHarvestRequest 失败: %v", err)
	}
	if parsedHarvest.LandID != 10 {
		t.Errorf("收获解析结果不符: %+v", parsedHarvest)
	}
}

func TestFarmCodec_ResultsRoundtrip(t *testing.T) {
	// 1. 验证 FarmGetInfoResult
	infoRes := FarmGetInfoResult{
		Fields: []FarmField{
			{LandID: 10, State: 1, Cooldown: 0, SeedOrRoleID: 0},      // 空闲
			{LandID: 11, State: 3, Cooldown: 0, SeedOrRoleID: 169},    // 成熟待采摘
			{LandID: 12, State: 2, Cooldown: 3600, SeedOrRoleID: 169}, // 种植中
		},
	}
	pktInfoRes, err := BuildFarmGetInfoResultPacket(infoRes)
	if err != nil {
		t.Fatalf("BuildFarmGetInfoResultPacket 失败: %v", err)
	}
	if pktInfoRes.ActionID != ActionIDFarmGetInfo {
		t.Fatalf("ActionID 不匹配: 0x%08X", pktInfoRes.ActionID)
	}

	parsedInfoRes, err := ParseFarmGetInfoResult(pktInfoRes.Payload)
	if err != nil {
		t.Fatalf("ParseFarmGetInfoResult 失败: %v", err)
	}
	if len(parsedInfoRes.Fields) != 3 {
		t.Fatalf("土地列表长度不匹配: 期望 3, 实际 %d", len(parsedInfoRes.Fields))
	}
	if parsedInfoRes.Fields[1].State != 3 || parsedInfoRes.Fields[1].LandID != 11 {
		t.Errorf("第 2 块土地状态不符: %+v", parsedInfoRes.Fields[1])
	}

	// 2. 验证 FarmHarvestResult
	harvestRes := FarmHarvestResult{
		Success:   true,
		GainExp:   125000,
		GainCoins: 580000,
	}
	pktHarvestRes, err := BuildFarmHarvestResultPacket(harvestRes)
	if err != nil {
		t.Fatalf("BuildFarmHarvestResultPacket 失败: %v", err)
	}
	parsedHarvestRes, err := ParseFarmHarvestResult(pktHarvestRes.Payload)
	if err != nil {
		t.Fatalf("ParseFarmHarvestResult 失败: %v", err)
	}
	if !parsedHarvestRes.Success || parsedHarvestRes.GainExp != 125000 || parsedHarvestRes.GainCoins != 580000 {
		t.Errorf("收获解析结果不符: %+v", parsedHarvestRes)
	}

	// 3. 验证 FarmPlantResult
	plantRes := FarmPlantResult{
		Success:  true,
		LandID:   10,
		Cooldown: 28800,
	}
	pktPlantRes, err := BuildFarmPlantResultPacket(plantRes)
	if err != nil {
		t.Fatalf("BuildFarmPlantResultPacket 失败: %v", err)
	}
	parsedPlantRes, err := ParseFarmPlantResult(pktPlantRes.Payload)
	if err != nil {
		t.Fatalf("ParseFarmPlantResult 失败: %v", err)
	}
	if !parsedPlantRes.Success || parsedPlantRes.LandID != 10 || parsedPlantRes.Cooldown != 28800 {
		t.Errorf("播种解析结果不符: %+v", parsedPlantRes)
	}
}

func TestFarmCodec_RealPcapGroundTruth(t *testing.T) {
	// 1. 真实抓包 Act 25 收获成功 (23 字节)
	// Hex: 0800010000000c000013e50000000100007080
	realAct25Hex := "0800010000000c000013e50000000100007080"
	payload25, err := hex.DecodeString(realAct25Hex)
	if err != nil {
		t.Fatalf("DecodeString 失败: %v", err)
	}
	res25, err := ParseFarmHarvestResult(payload25)
	if err != nil {
		t.Fatalf("ParseFarmHarvestResult 真实报文解析失败: %v", err)
	}
	if !res25.Success || res25.LandID != 12 || res25.GainExp != 5093 || res25.Cooldown != 28800 {
		t.Errorf("Act 25 真实报文解析数据不符: %+v", res25)
	}

	// 2. 真实抓包 Act 25 未成熟 (7 字节)
	// Hex: 21000000000000
	failAct25Hex := "21000000000000"
	failPayload25, _ := hex.DecodeString(failAct25Hex)
	failRes25, err := ParseFarmHarvestResult(failPayload25)
	if err != nil {
		t.Fatalf("ParseFarmHarvestResult 失败报文解析错误: %v", err)
	}
	if failRes25.Success {
		t.Errorf("期望 Success=false, 实际 true")
	}

	// 3. 真实抓包 Act 24 播种成功 (15 字节)
	// Hex: 080000000c000000b0000000010000
	realAct24Hex := "080000000c000000b0000000010000"
	payload24, _ := hex.DecodeString(realAct24Hex)
	res24, err := ParseFarmPlantResult(payload24)
	if err != nil {
		t.Fatalf("ParseFarmPlantResult 真实报文解析失败: %v", err)
	}
	if !res24.Success || res24.LandID != 12 || res24.SeedOrRoleID != 176 {
		t.Errorf("Act 24 真实报文解析数据不符: %+v", res24)
	}

	// 4. 真实抓包 Act 23 购买种子成功 (1 字节: 0x08)
	buyPayload := []byte{FarmStatusSuccess}
	buyRes, err := ParseFarmBuySeedResult(buyPayload)
	if err != nil {
		t.Fatalf("ParseFarmBuySeedResult 失败: %v", err)
	}
	if !buyRes.Success {
		t.Errorf("期望购买成功，实际失败")
	}

	// 5. 真实抓包 Act 21 药园土地状态 (82 字节: 4 块空闲土地)
	// Hex: 00040000000a0000000000000000000000010100000000000b000000000000000000000001010000000000090000000000000000000000010100000000000c00000000000000000000000101000000000064
	realAct21Hex := "00040000000a0000000000000000000000010100000000000b000000000000000000000001010000000000090000000000000000000000010100000000000c00000000000000000000000101000000000064"
	payload21, _ := hex.DecodeString(realAct21Hex)
	res21, err := ParseFarmPlantStateResult(payload21)
	if err != nil {
		t.Fatalf("ParseFarmPlantStateResult 真实报文解析失败: %v", err)
	}
	if len(res21.Fields) != 4 {
		t.Fatalf("Act 21 土地数量不符: 期望 4, 实际 %d", len(res21.Fields))
	}
	for _, f := range res21.Fields {
		if f.State != 1 || f.Cooldown != 0 {
			t.Errorf("空闲土地状态解析错误，期望 State=1 Cooldown=0, 实际 State=%d Cooldown=%d (landID=%d)", f.State, f.Cooldown, f.LandID)
		}
	}
}

// TestFarmRequests_MarkPrevAct 验证空 request 的药园查询请求标记 HasPrevAct，
// 由会话层在发包时用真实最近动作号覆盖末尾 4 字节，避免手工链断档被静默丢弃。
// 注意: 13/23、13/24、13/25 官方请求分别只有 [Int]、[Int,Int]、[Int]，
// 不能追加 PrevAct，否则会被服务端静默丢弃（见下方 TestFarmIntRequests_BarePayload）。
func TestFarmRequests_MarkPrevAct(t *testing.T) {
	builders := []struct {
		name  string
		build func() (*Packet, error)
	}{
		{"get_info", func() (*Packet, error) {
			return BuildFarmGetInfoPacket(FarmGetInfoRequest{PrevAct: ActionIDTownEnter})
		}},
		{"plant_state", func() (*Packet, error) {
			return BuildFarmPlantStatePacket(FarmPlantStateRequest{PrevAct: ActionIDFarmGetInfo})
		}},
		{"seeds", func() (*Packet, error) {
			return BuildFarmSeedsPacket(FarmSeedsRequest{PrevAct: ActionIDFarmGetPlantState})
		}},
	}
	for _, b := range builders {
		pkt, err := b.build()
		if err != nil {
			t.Fatalf("%s: %v", b.name, err)
		}
		if !pkt.HasPrevAct {
			t.Errorf("%s 未标记 HasPrevAct，PrevAct 链可能断档", b.name)
		}
		if len(pkt.Payload) < 4 {
			t.Errorf("%s 载荷不足 4 字节，无法写入 PrevAct", b.name)
		}
	}
}

// TestFarmIntRequests_BarePayload 验证带 Int 参数的药园动作严格按官方长度发送。
// 线上实测 13/23 裸 4 字节可成功扣铜钱并增加种子库存；多带 PrevAct 会被丢弃。
func TestFarmIntRequests_BarePayload(t *testing.T) {
	cases := []struct {
		name    string
		build   func() (*Packet, error)
		wantLen int
	}{
		{"buy_seed", func() (*Packet, error) {
			return BuildFarmBuySeedPacket(FarmBuySeedRequest{SeedID: 176})
		}, 4},
		{"plant", func() (*Packet, error) {
			return BuildFarmPlantPacket(FarmPlantRequest{LandID: 10, SeedOrRoleID: 176})
		}, 8},
		{"harvest", func() (*Packet, error) {
			return BuildFarmHarvestPacket(FarmHarvestRequest{LandID: 10})
		}, 4},
	}
	for _, c := range cases {
		pkt, err := c.build()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if pkt.HasPrevAct {
			t.Errorf("%s 不应标记 HasPrevAct", c.name)
		}
		if len(pkt.Payload) != c.wantLen {
			t.Errorf("%s 载荷长度 = %d, want %d", c.name, len(pkt.Payload), c.wantLen)
		}
	}
}
