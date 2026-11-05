// Package protocol_test 提供世界 Boss 协议 (Module 24) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestWorldBossListResult(t *testing.T) {
	entries := []protocol.WorldBossEntry{
		{BossID: 1, MapID: 11, Level: 280, State: protocol.WorldBossStateOpen, StartTime: 57000, EndTime: 60780, RobotCoin: 100, RobotFame: 20},
		{BossID: 2, MapID: 12, Level: 300, State: protocol.WorldBossStateOver},
	}
	pkt := protocol.BuildWorldBossListResultPacket(entries)
	parsed, err := protocol.ParseWorldBossListResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析世界Boss列表失败: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("期望 2 个 Boss, got %d", len(parsed))
	}
	first := parsed[0]
	if first.BossID != 1 || first.MapID != 11 || first.Level != 280 || first.State != protocol.WorldBossStateOpen ||
		first.StartTime != 57000 || first.EndTime != 60780 || first.RobotCoin != 100 || first.RobotFame != 20 {
		t.Errorf("Boss 条目不一致: %+v", first)
	}
	if parsed[1].State != protocol.WorldBossStateOver {
		t.Errorf("第二个 Boss 状态不一致: %+v", parsed[1])
	}
}

func TestWorldBossFightResult(t *testing.T) {
	// 成功路径
	result, err := protocol.ParseWorldBossFightResult(protocol.BuildWorldBossFightResultPacket(protocol.WorldBossResultSuccess).Payload)
	if err != nil || result != protocol.WorldBossResultSuccess {
		t.Fatalf("期望成功码 0, got result=%d err=%v", result, err)
	}
	// 失败码路径: 未到开战时间必须能被识别 (回包解析铁律)
	result, err = protocol.ParseWorldBossFightResult(protocol.BuildWorldBossFightResultPacket(protocol.WorldBossResultNotBegin).Payload)
	if err != nil || result != protocol.WorldBossResultNotBegin {
		t.Fatalf("期望未开战码 %d, got result=%d err=%v", protocol.WorldBossResultNotBegin, result, err)
	}
	// 空回包必须报错
	if _, err := protocol.ParseWorldBossFightResult(nil); err == nil {
		t.Error("空世界Boss战斗回包应返回解析错误")
	}
}

func TestWorldBossPackets(t *testing.T) {
	list := protocol.BuildWorldBossListPacket()
	if list.ActionID != protocol.ActionIDWorldBossList || len(list.Payload) != 1 || list.Payload[0] != 0 {
		t.Errorf("列表请求应为单字节 0, got action=0x%08X payload=%v", list.ActionID, list.Payload)
	}
	fight := protocol.BuildWorldBossFightPacket(3)
	if fight.ActionID != protocol.ActionIDWorldBossFight || len(fight.Payload) != 1 || fight.Payload[0] != 3 {
		t.Errorf("战斗请求应为单字节 boss_id=3, got action=0x%08X payload=%v", fight.ActionID, fight.Payload)
	}
}
