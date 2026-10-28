package protocol

import (
	"bytes"
	"testing"
)

func TestTowerLayerNameAndLimits(t *testing.T) {
	cases := []struct {
		layer    uint8
		expected string
		minLevel int
	}{
		{TowerLayerHell, "地狱道", 45},
		{TowerLayerHungryGhost, "饿鬼道", 56},
		{TowerLayerBeast, "畜生道", 76},
		{TowerLayerAsura, "修罗道", 96},
		{TowerLayerHuman, "人间道", 106},
		{TowerLayerHeaven, "天道", 116},
		{TowerLayerXianHell, "仙·地狱道", 136},
		{TowerLayerXianGhost, "仙·饿鬼道", 146},
		{TowerLayerXianBeast, "仙·畜生道", 156},
		{TowerLayerXianAsura, "仙·修罗道", 166},
		{TowerLayerXianHuman, "仙·人间道", 176},
		{TowerLayerXianHeaven, "仙·天道", 186},
		{99, "未知道(99)", 45},
	}

	for _, c := range cases {
		name := TowerLayerName(c.layer)
		if name != c.expected {
			t.Errorf("layer %d: expected name %s, got %s", c.layer, c.expected, name)
		}
		lvl := TowerLayerLevelLimit(c.layer)
		if lvl != c.minLevel {
			t.Errorf("layer %d: expected level %d, got %d", c.layer, c.minLevel, lvl)
		}
	}
}

func TestBuildAndParseTowerInfo(t *testing.T) {
	pkt := BuildTowerGetInfoPacket(0)
	if pkt.ActionID != ActionIDTowerGetInfo {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDTowerGetInfo, pkt.ActionID)
	}

	// 模拟服务端回包:
	// SequenceMax=25, Layer=1, SequenceWar=2, BagNum=5, ResetNum=1, ResetIngot=0, HideID=0, HighStone=30
	w := NewWriter()
	w.WriteUint8(25)
	w.WriteUint8(1)
	w.WriteUint8(2)
	w.WriteUint8(5)
	w.WriteUint8(1)
	w.WriteUint16(0)
	w.WriteUint8(0)
	w.WriteInt32(30)

	info, err := ParseTowerInfoResponse(w.Bytes())
	if err != nil {
		t.Fatalf("ParseTowerInfoResponse failed: %v", err)
	}
	if info.SequenceMax != 25 || info.Layer != 1 || info.SequenceWar != 2 || info.BagNum != 5 || info.ResetNum != 1 || info.ResetIngot != 0 || info.HighStone != 30 {
		t.Errorf("unexpected parsed tower info: %+v", info)
	}

	// 长度不足异常
	if _, err := ParseTowerInfoResponse([]byte{0x01, 0x02}); err == nil {
		t.Error("expected error for short payload, got nil")
	}
}

func TestBuildAndParseTowerChallenge(t *testing.T) {
	pkt := BuildTowerChallengePacket(3, true, false, 0)
	if pkt.ActionID != ActionIDTowerStartChallenge {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDTowerStartChallenge, pkt.ActionID)
	}
	expectedPayload := []byte{0x03, 0x01, 0x00}
	if !bytes.Equal(pkt.Payload, expectedPayload) {
		t.Errorf("expected payload %v, got %v", expectedPayload, pkt.Payload)
	}

	// 成功挑战回包
	w := NewWriter()
	w.WriteUint8(TowerResultSuccess)
	w.WriteInt32(1001)   // WarSoulID
	w.WriteInt32(500000) // WarExp
	w.WriteInt32(500)    // WarFame
	w.WriteInt32(100000) // WarCoin

	res, err := ParseTowerChallengeResponse(w.Bytes())
	if err != nil {
		t.Fatalf("ParseTowerChallengeResponse failed: %v", err)
	}
	if res.Result != TowerResultSuccess || res.WarSoulID != 1001 || res.WarExp != 500000 || res.WarFame != 500 || res.WarCoin != 100000 || !res.IsPass {
		t.Errorf("unexpected challenge result: %+v", res)
	}

	// 失败挑战回包 (比如背包已满)
	failRes, err := ParseTowerChallengeResponse([]byte{TowerResultFullBag})
	if err != nil {
		t.Fatalf("ParseTowerChallengeResponse fail failed: %v", err)
	}
	if failRes.Result != TowerResultFullBag || failRes.IsPass {
		t.Errorf("expected failed challenge result, got %+v", failRes)
	}

	// 空回包
	if _, err := ParseTowerChallengeResponse([]byte{}); err == nil {
		t.Error("expected error for empty challenge response")
	}
}

func TestBuildAndParseTowerReset(t *testing.T) {
	pkt := BuildTowerResetPacket(0)
	if pkt.ActionID != ActionIDTowerResetLayer {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDTowerResetLayer, pkt.ActionID)
	}

	// 重置成功回包: Result=0, Layer=1, ResetIngot=50, ResetNum=0
	w := NewWriter()
	w.WriteUint8(TowerResultSuccess)
	w.WriteUint8(1)
	w.WriteUint16(50)
	w.WriteUint8(0)

	res, err := ParseTowerResetResponse(w.Bytes())
	if err != nil {
		t.Fatalf("ParseTowerResetResponse failed: %v", err)
	}
	if res.Result != TowerResultSuccess || res.Layer != 1 || res.ResetIngot != 50 || res.ResetNum != 0 {
		t.Errorf("unexpected reset result: %+v", res)
	}

	if _, err := ParseTowerResetResponse([]byte{0x00}); err == nil {
		t.Error("expected error for short reset payload")
	}
}

func TestBuildAndParseAutoChallenge(t *testing.T) {
	startPkt := BuildTowerStartAutoChallengePacket(true, true, 0)
	if startPkt.ActionID != ActionIDTowerStartAutoChallenge {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDTowerStartAutoChallenge, startPkt.ActionID)
	}
	if !bytes.Equal(startPkt.Payload, []byte{1, 1}) {
		t.Errorf("unexpected start auto payload: %v", startPkt.Payload)
	}

	stopPkt := BuildTowerStopAutoChallengePacket(0)
	if stopPkt.ActionID != ActionIDTowerStopAutoChallenge {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDTowerStopAutoChallenge, stopPkt.ActionID)
	}

	quickPkt := BuildTowerQuicklyAutoChallengePacket(true, false, 0)
	if quickPkt.ActionID != ActionIDTowerQuicklyAutoChallenge {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDTowerQuicklyAutoChallenge, quickPkt.ActionID)
	}

	code, err := ParseTowerAutoChallengeResponse([]byte{0})
	if err != nil || code != 0 {
		t.Errorf("unexpected auto response %v, err=%v", code, err)
	}

	// 战报解析
	rw := NewWriter()
	rw.WriteUint8(0)
	rw.WriteUint8(1)
	rw.WriteInt32(250000)
	rw.WriteInt32(200)
	rw.WriteUint8(10)
	rw.WriteInt32(5)

	rpt, err := ParseTowerAutoReport(rw.Bytes())
	if err != nil {
		t.Fatalf("ParseTowerAutoReport failed: %v", err)
	}
	if rpt.Result != 0 || rpt.Layer != 1 || rpt.Exp != 250000 || rpt.Fame != 200 || rpt.Hstone != 5 {
		t.Errorf("unexpected auto report: %+v", rpt)
	}
}

func TestBuildAndParseSealSoul(t *testing.T) {
	keyPkt := BuildSealSoulGetTowerKeyPacket(0)
	if keyPkt.ActionID != ActionIDSealSoulGetTowerKey {
		t.Errorf("expected 0x%08X, got 0x%08X", ActionIDSealSoulGetTowerKey, keyPkt.ActionID)
	}

	kw := NewWriter()
	kw.WriteInt32(99)
	kw.WriteUint8(1)
	k, s, err := ParseSealSoulGetTowerKeyResponse(kw.Bytes())
	if err != nil || k != 99 || s != 1 {
		t.Errorf("unexpected tower key response: k=%d, s=%d, err=%v", k, s, err)
	}

	sellPkt := BuildSealSoulSellPacket(1005, 0)
	if sellPkt.ActionID != ActionIDSealSoulSell {
		t.Errorf("expected 0x%08X, got 0x%08X", ActionIDSealSoulSell, sellPkt.ActionID)
	}

	scode, err := ParseSealSoulSellResponse([]byte{0})
	if err != nil || scode != 0 {
		t.Errorf("unexpected sell response: %d, err=%v", scode, err)
	}
}
