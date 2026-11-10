package protocol_test

import (
	"bytes"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 抓包样本 (2026-11-10, qq s0): 341/4 创建房间请求载荷 = 前一个动作 0x01550001。
func TestBuildTowerRoomCreatePacket(t *testing.T) {
	pkt := protocol.BuildTowerRoomCreatePacket(protocol.ActionIDTowerRoomOpen)
	if pkt.ActionID != protocol.ActionIDTowerRoomCreate {
		t.Fatalf("ActionID = 0x%08X, 期望 0x%08X", pkt.ActionID, protocol.ActionIDTowerRoomCreate)
	}
	if !pkt.HasPrevAct {
		t.Fatal("HasPrevAct 未置位，PrevAct 链会被心跳打断")
	}
	want := []byte{0x01, 0x55, 0x00, 0x01}
	if !bytes.Equal(pkt.Payload, want) {
		t.Fatalf("载荷 = % X, 期望 % X", pkt.Payload, want)
	}
}

// 抓包样本: 341/12 解除封印请求载荷 = 0x01550004 (创建房间动作)。
func TestBuildTowerRoomUnsealPacket(t *testing.T) {
	pkt := protocol.BuildTowerRoomUnsealPacket(protocol.ActionIDTowerRoomCreate)
	if pkt.ActionID != protocol.ActionIDTowerRoomUnseal {
		t.Fatalf("ActionID = 0x%08X, 期望 0x%08X", pkt.ActionID, protocol.ActionIDTowerRoomUnseal)
	}
	want := []byte{0x01, 0x55, 0x00, 0x04}
	if !bytes.Equal(pkt.Payload, want) {
		t.Fatalf("载荷 = % X, 期望 % X", pkt.Payload, want)
	}
}

// 抓包样本: 341/13 请求载荷 = 0x0155000C (解除封印动作)。
func TestBuildTowerRoomNextPacket(t *testing.T) {
	pkt := protocol.BuildTowerRoomNextPacket(protocol.ActionIDTowerRoomUnseal)
	want := []byte{0x01, 0x55, 0x00, 0x0C}
	if !bytes.Equal(pkt.Payload, want) {
		t.Fatalf("载荷 = % X, 期望 % X", pkt.Payload, want)
	}
}

// 抓包样本: 创建房间回包 04 00 00 07 d1 ... -> SUCCESS + 房间 2001。
func TestParseTowerRoomCreateResult(t *testing.T) {
	payload := []byte{
		0x04, 0x00, 0x00, 0x07, 0xd1, 0x01, 0x00, 0x01, 0x01,
		0x00, 0x03, 0x95, 0x2d, 0x00, 0x06, 0xe5, 0x90, 0x8c,
	}
	res, err := protocol.ParseTowerRoomCreateResult(payload)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Result != protocol.TowerRoomResultSuccess {
		t.Fatalf("Result = %d, 期望 %d", res.Result, protocol.TowerRoomResultSuccess)
	}
	if res.RoomID != 2001 {
		t.Fatalf("RoomID = %d, 期望 2001", res.RoomID)
	}
}

// 失败路径: 截断回包必须报错，不能静默返回空结构。
func TestParseTowerRoomCreateResult_Truncated(t *testing.T) {
	if _, err := protocol.ParseTowerRoomCreateResult([]byte{0x04, 0x00}); err == nil {
		t.Fatal("截断回包应返回错误")
	}
}
