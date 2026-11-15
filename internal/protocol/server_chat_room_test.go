package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestParseServerChatRoomRecentRooms_Live 使用 2026-11-15 线上回包验证房间列表解析。
func TestParseServerChatRoomRecentRooms_Live(t *testing.T) {
	payload, _ := hex.DecodeString("090003010208")
	result, rooms, err := protocol.ParseServerChatRoomRecentRoomsResult(payload)
	if err != nil {
		t.Fatalf("解析房间列表失败: %v", err)
	}
	if result != protocol.ServerChatRoomResultSuccess {
		t.Fatalf("result = %d, want %d", result, protocol.ServerChatRoomResultSuccess)
	}
	if len(rooms) != 3 || rooms[0] != 1 || rooms[1] != 2 || rooms[2] != 8 {
		t.Fatalf("rooms = %v, want [1 2 8]", rooms)
	}
}

// TestParseServerChatRoomStatus_Live 验证房间人数与状态解析。
func TestParseServerChatRoomStatus_Live(t *testing.T) {
	payload, _ := hex.DecodeString("09000301000000a70b02000000070b08000000050b")
	rooms, err := protocol.ParseServerChatRoomStatusResult(payload)
	if err != nil {
		t.Fatalf("解析房间状态失败: %v", err)
	}
	if len(rooms) != 3 {
		t.Fatalf("rooms = %d, want 3", len(rooms))
	}
	if rooms[0].RoomID != 1 || rooms[0].PlayerCount != 167 || rooms[0].Status != 11 {
		t.Fatalf("room[0] = %+v", rooms[0])
	}
	if rooms[2].RoomID != 8 || rooms[2].PlayerCount != 5 || rooms[2].Status != 11 {
		t.Fatalf("room[2] = %+v", rooms[2])
	}
}

// TestParseServerChatRoomLoginCode_Live 验证 336/11 登录信息字段序。
func TestParseServerChatRoomLoginCode_Live(t *testing.T) {
	payload, _ := hex.DecodeString("01001673304039783035302e7378647765622e78642e636f6d0002733000067665727963646af95a5a00203336363138616261396335393264393431626536386233363739366366376636001339783035302e7378647765622e78642e636f6d0000200d")
	info, err := protocol.ParseServerChatRoomLoginCodeResult(payload)
	if err != nil {
		t.Fatalf("解析登录码失败: %v", err)
	}
	if info.Flag != 1 {
		t.Errorf("flag = %d, want 1", info.Flag)
	}
	if info.Node != "s0@9x050.sxdweb.xd.com" {
		t.Errorf("node = %q", info.Node)
	}
	if info.ServerName != "s0" || info.StageName != "verycd" {
		t.Errorf("server/stage = %q/%q", info.ServerName, info.StageName)
	}
	if info.Timestamp != 1794726490 {
		t.Errorf("timestamp = %d", info.Timestamp)
	}
	if info.LoginCode != "36618aba9c592d941be68b36796cf7f6" {
		t.Errorf("login_code = %q", info.LoginCode)
	}
	if info.Host != "9x050.sxdweb.xd.com" || info.Port != 8205 {
		t.Errorf("host/port = %q:%d", info.Host, info.Port)
	}
}

// TestServerChatRoomLoginAndSpeakBuilders 验证登录/发言请求与结果解析。
func TestServerChatRoomLoginAndSpeakBuilders(t *testing.T) {
	info := &protocol.ChatRoomLoginInfo{
		Node:       "s0@host",
		ServerName: "s0",
		StageName:  "verycd",
		Timestamp:  1794726490,
		LoginCode:  "code123",
		Host:       "host",
		Port:       8205,
	}
	pkt, err := protocol.BuildServerChatRoomLoginPacket(info, 36047)
	if err != nil {
		t.Fatalf("构造登录包失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDServerChatRoomLogin {
		t.Fatalf("action = 0x%08X", pkt.ActionID)
	}
	result, playerID, err := protocol.ParseServerChatRoomLoginResult([]byte{0x09, 0x00, 0x00, 0x8c, 0xcf})
	if err != nil || result != 9 || playerID != 36047 {
		t.Fatalf("登录回包解析 = %d/%d/%v", result, playerID, err)
	}

	speak := protocol.BuildServerChatRoomSpeakPacket("", "1", "16")
	if speak.ActionID != protocol.ActionIDServerChatRoomSpeak {
		t.Fatalf("speak action = 0x%08X", speak.ActionID)
	}
	code, diff, err := protocol.ParseServerChatRoomSpeakResult([]byte{0, 0, 0, 0, 0x09})
	if err != nil || code != 9 || diff != 0 {
		t.Fatalf("发言回包解析 = %d/%d/%v", code, diff, err)
	}
}
