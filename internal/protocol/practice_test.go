// Package protocol_test 验证副本挂机扫荡与加速秒完成协议封包编解码。
//
// @author Ateng
// @since 2026-10-08
package protocol_test

import (
	"bytes"
	"os"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestPractice_StartPracticeRoundtrip(t *testing.T) {
	req := protocol.StartPracticeRequest{
		MissionID: 105,
		Count:     10,
		AutoSale:  1,
	}

	pkt, err := protocol.BuildStartPracticePacket(req)
	if err != nil {
		t.Fatalf("BuildStartPracticePacket failed: %v", err)
	}

	if pkt.ActionID != protocol.ActionIDPracticeStart {
		t.Errorf("expected ActionID 0x%04X, got 0x%04X", protocol.ActionIDPracticeStart, pkt.ActionID)
	}

	parsed, err := protocol.ParseStartPracticeRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseStartPracticeRequest failed: %v", err)
	}

	if parsed.MissionID != req.MissionID || parsed.Count != req.Count || parsed.AutoSale != req.AutoSale {
		t.Errorf("expected %+v, got %+v", req, *parsed)
	}

	// 验证结果回包编解码
	res := protocol.StartPracticeResult{
		Result:    protocol.PracticeResultSuccess,
		MissionID: 105,
		Count:     10,
	}

	resPkt, err := protocol.BuildStartPracticeResultPacket(res)
	if err != nil {
		t.Fatalf("BuildStartPracticeResultPacket failed: %v", err)
	}

	parsedRes, err := protocol.ParseStartPracticeResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("ParseStartPracticeResult failed: %v", err)
	}

	if *parsedRes != res {
		t.Errorf("expected %+v, got %+v", res, *parsedRes)
	}
}

func TestPractice_QuicklyRoundtrip(t *testing.T) {
	req := protocol.QuicklyRequest{
		Count: 5,
	}

	pkt, err := protocol.BuildQuicklyPacket(req)
	if err != nil {
		t.Fatalf("BuildQuicklyPacket failed: %v", err)
	}

	if pkt.ActionID != protocol.ActionIDPracticeQuickly {
		t.Errorf("expected ActionID 0x%04X, got 0x%04X", protocol.ActionIDPracticeQuickly, pkt.ActionID)
	}

	parsed, err := protocol.ParseQuicklyRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseQuicklyRequest failed: %v", err)
	}

	if parsed.Count != 5 {
		t.Errorf("expected Count 5, got %d", parsed.Count)
	}

	res := protocol.QuicklyResult{
		Result: protocol.PracticeResultSuccess,
		Count:  5,
	}

	resPkt, err := protocol.BuildQuicklyResultPacket(res)
	if err != nil {
		t.Fatalf("BuildQuicklyResultPacket failed: %v", err)
	}

	parsedRes, err := protocol.ParseQuicklyResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("ParseQuicklyResult failed: %v", err)
	}

	if *parsedRes != res {
		t.Errorf("expected %+v, got %+v", res, *parsedRes)
	}
}

// TestGolden_Module25_LiveFilesReplay 遍历 test/testdata/golden/ 下提取的真实 Module 25 封包切片，
// 在纯离线状态下验证大端序编码、流式解压以及请求/响应结构体反序列化逐字节对齐。
// TestBuildPracticeGetStatusBarePacket 验证裸 4 字节状态查询载荷。
// 2026-11-14 实机: 带 PrevAct 的 25/0 全被静默丢弃，裸载荷稳定回包。
func TestBuildPracticeGetStatusBarePacket(t *testing.T) {
	pkt := protocol.BuildPracticeGetStatusBarePacket(854)
	if pkt.ActionID != protocol.ActionIDPracticeGetStatus {
		t.Fatalf("ActionID = 0x%08X, want 0x%08X", pkt.ActionID, protocol.ActionIDPracticeGetStatus)
	}
	want := []byte{0x00, 0x00, 0x03, 0x56}
	if !bytes.Equal(pkt.Payload, want) {
		t.Fatalf("payload = %x, want %x", pkt.Payload, want)
	}
	if pkt.HasPrevAct {
		t.Fatal("裸状态查询不能标记 HasPrevAct")
	}
}

func TestGolden_Module25_LiveFilesReplay(t *testing.T) {
	// 1. C2S Action 0: 查询扫荡状态请求包 (c2s_m25_a0.bin)
	t.Run("C2S_Action0_GetStatus", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/c2s_m25_a0.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/c2s_m25_a0.bin")
		}
		if err != nil {
			t.Fatalf("读取 c2s_m25_a0.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		mod, act := protocol.SplitActionID(pkt.ActionID)
		if mod != protocol.ModuleMissionPractice || act != 0 {
			t.Errorf("期望 Mod=25, Act=0, 实际 Mod=%d, Act=%d", mod, act)
		}

		// 验证构造函数组装与抓包 100% 逐字节完全对齐 (MissionID=854, PrevAct=0x00040019)
		builtPkt, err := protocol.BuildPracticeGetStatusPacket(854, 0x00040019)
		if err != nil {
			t.Fatalf("BuildPracticeGetStatusPacket 失败: %v", err)
		}
		builtBytes, err := builtPkt.Marshal()
		if err != nil {
			t.Fatalf("Marshal 失败: %v", err)
		}
		if !bytes.Equal(builtBytes, raw) {
			t.Fatalf("组装报文与抓包存在字节差异:\n期望: %x\n实际: %x", raw, builtBytes)
		}
	})

	// 2. S2C Action 0: 扫荡状态响应包 (s2c_m25_a0.bin)
	t.Run("S2C_Action0_StatusResult", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/s2c_m25_a0.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/s2c_m25_a0.bin")
		}
		if err != nil {
			t.Fatalf("读取 s2c_m25_a0.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		status, err := protocol.ParsePracticeStatusResult(pkt.Payload)
		if err != nil {
			t.Fatalf("ParsePracticeStatusResult 失败: %v", err)
		}

		if status.HeaderTag != "FP" {
			t.Errorf("期望 HeaderTag 'FP', 实际 %q", status.HeaderTag)
		}
		if len(status.Missions) != 3 {
			t.Fatalf("期望 Missions 长度 3, 实际 %d", len(status.Missions))
		}
		if status.Missions[0].MissionID != 1847 || status.Missions[1].MissionID != 1862 || status.Missions[2].MissionID != 1861 {
			t.Errorf("关卡 ID 列表解析不匹配: %+v", status.Missions)
		}
		if status.IsPracticing != 1 {
			t.Errorf("期望 IsPracticing=1, 实际 %d", status.IsPracticing)
		}
	})

	// 3. C2S Action 1: 发起扫荡请求包 (c2s_m25_a1.bin)
	t.Run("C2S_Action1_StartPractice", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/c2s_m25_a1.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/c2s_m25_a1.bin")
		}
		if err != nil {
			t.Fatalf("读取 c2s_m25_a1.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		req, err := protocol.ParseStartPracticeRequest(pkt.Payload)
		if err != nil {
			t.Fatalf("ParseStartPracticeRequest 失败: %v", err)
		}

		if req.MissionID != 854 {
			t.Errorf("期望 MissionID=854, 实际 %d", req.MissionID)
		}
		if req.Count != 30 {
			t.Errorf("期望 Count=30, 实际 %d", req.Count)
		}
		if req.AutoSale != 1 {
			t.Errorf("期望 AutoSale=1, 实际 %d", req.AutoSale)
		}
		if req.PrevAct != 0x00190000 {
			t.Errorf("期望 PrevAct=0x00190000, 实际 0x%08X", req.PrevAct)
		}

		// 验证构造函数组装并与抓包 100% 逐字节完全对齐
		builtPkt, err := protocol.BuildStartPracticePacket(*req)
		if err != nil {
			t.Fatalf("BuildStartPracticePacket 失败: %v", err)
		}
		builtBytes, err := builtPkt.Marshal()
		if err != nil {
			t.Fatalf("Marshal 失败: %v", err)
		}
		if !bytes.Equal(builtBytes, raw) {
			t.Fatalf("组装报文与抓包存在字节差异:\n期望: %x\n实际: %x", raw, builtBytes)
		}
	})

	// 4. S2C Action 1: 开始扫荡确认响应包 (s2c_m25_a1.bin)
	t.Run("S2C_Action1_StartPracticeResult", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/s2c_m25_a1.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/s2c_m25_a1.bin")
		}
		if err != nil {
			t.Fatalf("读取 s2c_m25_a1.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		if pkt.ActionID != protocol.ActionIDPracticeStart {
			t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDPracticeStart, pkt.ActionID)
		}
		if pkt.Payload[0] != protocol.PracticeResultSuccess {
			t.Errorf("期望返回码 SUCCESS(0), 实际 %d", pkt.Payload[0])
		}
	})

	// 5. C2S Action 3: 立即加速完成请求包 (c2s_m25_a3.bin)
	t.Run("C2S_Action3_QuicklyRequest", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/c2s_m25_a3.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/c2s_m25_a3.bin")
		}
		if err != nil {
			t.Fatalf("读取 c2s_m25_a3.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		req, err := protocol.ParseQuicklyRequest(pkt.Payload)
		if err != nil {
			t.Fatalf("ParseQuicklyRequest 失败: %v", err)
		}

		if req.Count != 0 {
			t.Errorf("期望 Count=0 (全部加速), 实际 %d", req.Count)
		}
		if req.PrevAct != 0x00190001 {
			t.Errorf("期望 PrevAct=0x00190001, 实际 0x%08X", req.PrevAct)
		}

		builtPkt, err := protocol.BuildQuicklyPacket(*req)
		if err != nil {
			t.Fatalf("BuildQuicklyPacket 失败: %v", err)
		}
		builtBytes, err := builtPkt.Marshal()
		if err != nil {
			t.Fatalf("Marshal 失败: %v", err)
		}
		if !bytes.Equal(builtBytes, raw) {
			t.Fatalf("组装报文与抓包存在字节差异:\n期望: %x\n实际: %x", raw, builtBytes)
		}
	})

	// 6. S2C Action 3: 加速完成响应包 (s2c_m25_a3.bin)
	t.Run("S2C_Action3_QuicklyResult", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/s2c_m25_a3.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/s2c_m25_a3.bin")
		}
		if err != nil {
			t.Fatalf("读取 s2c_m25_a3.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		if pkt.ActionID != protocol.ActionIDPracticeQuickly {
			t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDPracticeQuickly, pkt.ActionID)
		}
		if pkt.Payload[0] != protocol.PracticeResultSuccess {
			t.Errorf("期望返回码 SUCCESS(0), 实际 %d", pkt.Payload[0])
		}
	})

	// 7. S2C Action 4: 未压缩单轮战斗结算通知 (s2c_m25_a4_uncomp.bin)
	t.Run("S2C_Action4_UncompressedNotify", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/s2c_m25_a4_uncomp.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/s2c_m25_a4_uncomp.bin")
		}
		if err != nil {
			t.Fatalf("读取 s2c_m25_a4_uncomp.bin 失败: %v", err)
		}

		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 失败: %v", err)
		}

		if pkt.ActionID != protocol.ActionIDPracticeNotify {
			t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDPracticeNotify, pkt.ActionID)
		}

		notify, err := protocol.ParsePracticeNotifyResult(pkt.Payload)
		if err != nil {
			t.Fatalf("ParsePracticeNotifyResult 失败: %v", err)
		}
		if notify.SubType != 7 {
			t.Errorf("期望 SubType 7, 实际 %d", notify.SubType)
		}
	})

	// 8. S2C Action 4: zlib 压缩单轮战斗掉落与经验结算 (s2c_m25_a4_zlib.bin)
	t.Run("S2C_Action4_ZlibCompressedNotify", func(t *testing.T) {
		raw, err := os.ReadFile("../../test/testdata/golden/s2c_m25_a4_zlib.bin")
		if err != nil {
			raw, err = os.ReadFile("test/testdata/golden/s2c_m25_a4_zlib.bin")
		}
		if err != nil {
			t.Fatalf("读取 s2c_m25_a4_zlib.bin 失败: %v", err)
		}

		// 验证流式 Unmarshal 透明 zlib 解压机制
		pkt, err := protocol.Unmarshal(raw)
		if err != nil {
			t.Fatalf("Unmarshal 自动解压失败: %v", err)
		}

		if pkt.ActionID != protocol.ActionIDPracticeNotify {
			t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDPracticeNotify, pkt.ActionID)
		}

		notify, err := protocol.ParsePracticeNotifyResult(pkt.Payload)
		if err != nil {
			t.Fatalf("ParsePracticeNotifyResult 失败: %v", err)
		}

		if notify.SubType != 6 {
			t.Errorf("期望 SubType 6, 实际 %d", notify.SubType)
		}
		if notify.CoinsGain != 14881 {
			t.Errorf("期望获得铜钱 CoinsGain=14881, 实际 %d", notify.CoinsGain)
		}
		if len(notify.RoleExpItems) != 6 {
			t.Fatalf("期望 6 名伙伴参战获得经验, 实际 %d", len(notify.RoleExpItems))
		}
		if notify.RoleExpItems[0].ExpGain != 160020 {
			t.Errorf("期望伙伴 1 获得经验 160020, 实际 %d", notify.RoleExpItems[0].ExpGain)
		}
	})
}

// TestHeroPractice_OfficialProtocol 验证 Module 35 英雄副本扫荡的请求/响应布局。
// 基准: com.protocols.Mod_HeroMission_Base
//
//	start_practice request=[Short(town_id),Byte(auto_num),Byte(hero_type)]
//	start_practice response=[UByte(result),Int(practice_time)]
//	quickly request=[] response=[UByte(result)]
func TestHeroPractice_OfficialProtocol(t *testing.T) {
	pkt, err := protocol.BuildHeroPracticePacket(protocol.HeroPracticeRequest{
		TownID:   13,
		AutoNum:  0,
		HeroType: 0,
	})
	if err != nil {
		t.Fatalf("BuildHeroPracticePacket 失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionHeroMissionStartPractice {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionHeroMissionStartPractice, pkt.ActionID)
	}
	if len(pkt.Payload) != 4 {
		t.Fatalf("期望 4 字节裸业务载荷 (town+auto+hero_type), 实际 %d: %X", len(pkt.Payload), pkt.Payload)
	}
	if pkt.HasPrevAct {
		t.Errorf("start_practice 必须为裸载荷，2026-11-14 实机带 PrevAct 会被静默丢弃")
	}
	if got := int(pkt.Payload[0])<<8 | int(pkt.Payload[1]); got != 13 {
		t.Errorf("期望 town_id=13, 实际 %d", got)
	}
	if pkt.Payload[2] != 0 || pkt.Payload[3] != 0 {
		t.Errorf("期望 auto_num=0 hero_type=0, 实际 %d/%d", pkt.Payload[2], pkt.Payload[3])
	}

	// 成功回包: result=3, practice_time=45
	res, err := protocol.ParseHeroPracticeResult([]byte{3, 0, 0, 0, 45})
	if err != nil {
		t.Fatalf("ParseHeroPracticeResult 成功回包失败: %v", err)
	}
	if res.Result != protocol.HeroMissionResultSuccess || res.PracticeTime != 45 {
		t.Errorf("成功回包解析不一致: %+v", *res)
	}

	// 失败回包: result=7 (体力不足)
	failRes, err := protocol.ParseHeroPracticeResult([]byte{7, 0, 0, 0, 0})
	if err != nil {
		t.Fatalf("ParseHeroPracticeResult 失败回包解析出错: %v", err)
	}
	if failRes.Result != protocol.HeroMissionResultNotEnoughPower {
		t.Errorf("期望体力不足结果码 7, 实际 %d", failRes.Result)
	}

	// 截断回包必须报错, 不能静默按成功处理
	if _, err := protocol.ParseHeroPracticeResult([]byte{3, 0}); err == nil {
		t.Errorf("期望截断回包返回错误")
	}
}

// TestHeroQuickly_OfficialProtocol 验证英雄副本快速完成请求为裸 2 字节 count。
func TestHeroQuickly_OfficialProtocol(t *testing.T) {
	pkt := protocol.BuildHeroQuicklyPacket()
	if pkt.ActionID != protocol.ActionHeroMissionQuickly {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionHeroMissionQuickly, pkt.ActionID)
	}
	if len(pkt.Payload) != 2 {
		t.Errorf("期望 2 字节裸 count 载荷, 实际 %X", pkt.Payload)
	}
	if pkt.HasPrevAct {
		t.Errorf("quickly 必须为裸载荷，2026-11-14 实机带 PrevAct 会被静默丢弃")
	}

	res, err := protocol.ParseHeroQuicklyResponse([]byte{protocol.HeroMissionResultSuccess})
	if err != nil {
		t.Fatalf("ParseHeroQuicklyResponse 失败: %v", err)
	}
	if res.Result != protocol.HeroMissionResultSuccess {
		t.Errorf("期望成功结果码, 实际 %d", res.Result)
	}
	if _, err := protocol.ParseHeroQuicklyResponse(nil); err == nil {
		t.Errorf("空回包必须返回错误")
	}
}
