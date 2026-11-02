// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestLuckyWheel_BuildRequests(t *testing.T) {
	open := BuildLuckyWheelOpenPacket(ActionIDTownEnter)
	if open.ActionID != ActionIDLuckyWheelOpen {
		t.Errorf("打开转盘 ActionID 不匹配: 0x%08X", open.ActionID)
	}
	if got := hex.EncodeToString(open.Payload); got != "00010000" {
		t.Errorf("打开转盘请求应仅携带 prev_act, 实际: %s", got)
	}

	start := BuildLuckyWheelStartPacket(ActionIDLuckyWheelOpen)
	if start.ActionID != ActionIDLuckyWheelStart {
		t.Errorf("转动转盘 ActionID 不匹配: 0x%08X", start.ActionID)
	}
	if got := hex.EncodeToString(start.Payload); got != "00450001" {
		t.Errorf("转动转盘 prev_act 不匹配: %s", got)
	}
}

func TestLuckyWheel_ParseOpenResult(t *testing.T) {
	payload, _ := hex.DecodeString("000000138800000003")
	info, err := ParseLuckyWheelOpenResult(payload)
	if err != nil {
		t.Fatalf("解析转盘状态失败: %v", err)
	}
	if info.State != LuckyWheelResultOK {
		t.Errorf("转盘状态不匹配: %d", info.State)
	}
	if info.FactionTodayContribution != 5000 {
		t.Errorf("今日帮派贡献不匹配: %d", info.FactionTodayContribution)
	}
	if info.RosefinchNumber != 3 {
		t.Errorf("剩余青龙令不匹配: %d", info.RosefinchNumber)
	}
}

func TestLuckyWheel_ParseRunResult(t *testing.T) {
	payload, _ := hex.DecodeString("020200000002000061a8")
	result, err := ParseLuckyWheelRunResult(payload)
	if err != nil {
		t.Fatalf("解析转盘抽奖结果失败: %v", err)
	}
	if result.RunState != LuckyWheelResultSuccess {
		t.Errorf("抽奖结果不匹配: %d", result.RunState)
	}
	if result.Type != 2 {
		t.Errorf("奖励类型不匹配: %d", result.Type)
	}
	if result.RosefinchNumber != 2 {
		t.Errorf("剩余青龙令不匹配: %d", result.RosefinchNumber)
	}
	if result.ItemID != 25000 {
		t.Errorf("奖励编号不匹配: %d", result.ItemID)
	}
}

func TestLuckyWheel_ParseTruncated(t *testing.T) {
	if _, err := ParseLuckyWheelOpenResult([]byte{0x00, 0x00}); err == nil {
		t.Error("截断的打开转盘回包应返回解析错误")
	}
	// 声明成功但缺少 item_id，必须报错
	if _, err := ParseLuckyWheelRunResult([]byte{0x02, 0x02, 0x00, 0x00, 0x00, 0x02}); err == nil {
		t.Error("截断的抽奖回包应返回解析错误")
	}
}
