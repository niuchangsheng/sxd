// Package protocol_test 验证仙界练功房 (Mod 117) 封包构造与回包解析。
//
// @author Ateng
// @since 2026-11-04
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBuildStPracticeRoomPackets(t *testing.T) {
	cases := []struct {
		name       string
		packet     *protocol.Packet
		actionID   uint32
		payloadLen int
		withPrev   bool
	}{
		{"get_rooms", protocol.BuildStPracticeRoomGetRoomsPacket(), protocol.ActionIDStPracticeRoomGetRooms, 4, true},
		{"get_room_info", protocol.BuildStPracticeRoomGetRoomInfoPacket(4), protocol.ActionIDStPracticeRoomGetRoomInfo, 1, false},
		{"sit_down", protocol.BuildStPracticeRoomSitDownPacket(5, 3), protocol.ActionIDStPracticeRoomSitDown, 2, false},
		{"get_seat_info", protocol.BuildStPracticeRoomGetSeatInfoPacket(4, 8), protocol.ActionIDStPracticeRoomGetSeatInfo, 2, false},
		{"rob_the_seat", protocol.BuildStPracticeRoomRobSeatPacket(5, 0), protocol.ActionIDStPracticeRoomRobSeat, 2, false},
		{"get_practice_exp", protocol.BuildStPracticeRoomGetPracticeExpPacket(), protocol.ActionIDStPracticeRoomGetPracticeExp, 4, true},
		{"get_exp_flag", protocol.BuildStPracticeRoomGetExpFlagPacket(), protocol.ActionIDStPracticeRoomGetExpFlag, 4, true},
		{"enter_room", protocol.BuildStPracticeRoomEnterRoomPacket(), protocol.ActionIDStPracticeRoomEnterRoom, 4, true},
		{"exit_room", protocol.BuildStPracticeRoomExitRoomPacket(), protocol.ActionIDStPracticeRoomExitRoom, 4, true},
		{"leave_seat", protocol.BuildStPracticeRoomPlayerLeaveSeatPacket(), protocol.ActionIDStPracticeRoomPlayerLeaveSeat, 4, true},
		{"get_free_room_info", protocol.BuildStPracticeRoomGetFreeRoomInfoPacket(), protocol.ActionIDStPracticeRoomGetFreeRoomInfo, 4, true},
		{"get_free_room_info_by_page", protocol.BuildStPracticeRoomGetFreeRoomInfoByPagePacket(2), protocol.ActionIDStPracticeRoomGetFreeRoomInfoByPage, 2, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.packet.ActionID != tc.actionID {
				t.Errorf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", tc.actionID, tc.packet.ActionID)
			}
			if len(tc.packet.Payload) != tc.payloadLen {
				t.Errorf("载荷长度不匹配: 期望 %d, 实际 %d", tc.payloadLen, len(tc.packet.Payload))
			}
			if tc.packet.HasPrevAct != tc.withPrev {
				t.Errorf("PrevAct 标记不匹配: 期望 %v, 实际 %v", tc.withPrev, tc.packet.HasPrevAct)
			}
		})
	}

	qualityPkt := protocol.BuildStPracticeRoomGetRoomInfoPacket(4)
	if qualityPkt.Payload[0] != 4 {
		t.Errorf("房间品质字段错误: %v", qualityPkt.Payload[0])
	}
	sitPkt := protocol.BuildStPracticeRoomSitDownPacket(5, 3)
	if sitPkt.Payload[0] != 5 || sitPkt.Payload[1] != 3 {
		t.Errorf("入座字段错误: %v", sitPkt.Payload)
	}
}

// TestParseStPracticeRoomList_LiveCapture 使用 s1 实机回包验证房间列表解析。
func TestParseStPracticeRoomList_LiveCapture(t *testing.T) {
	payload, err := hex.DecodeString("000601000005000c0200000300040400086400010264")
	if err != nil {
		t.Fatalf("解码实机载荷失败: %v", err)
	}
	list, err := protocol.ParseStPracticeRoomList(payload)
	if err != nil {
		t.Fatalf("解析房间列表失败: %v", err)
	}
	if len(list.Rooms) != 6 {
		t.Fatalf("房间数量不匹配: %d", len(list.Rooms))
	}
	want := []protocol.StPracticeRoomEntry{
		{Quality: 1, Count: 0},
		{Quality: 5, Count: 12},
		{Quality: 2, Count: 0},
		{Quality: 3, Count: 4},
		{Quality: 4, Count: 8},
		{Quality: 100, Count: 1},
	}
	for i := range want {
		if list.Rooms[i] != want[i] {
			t.Errorf("第 %d 个房间不匹配: 期望 %+v, 实际 %+v", i, want[i], list.Rooms[i])
		}
	}
	if list.InPracticeFlag != protocol.StPracticeRoomYes {
		t.Errorf("期望正在练功, 实际 flag=%d", list.InPracticeFlag)
	}
	if list.Quality != 100 {
		t.Errorf("期望当前品质 100, 实际 %d", list.Quality)
	}
	if !list.InPractice() {
		t.Error("InPractice 应返回 true")
	}
}

func TestParseStPracticeRoomList_Truncated(t *testing.T) {
	payload, _ := hex.DecodeString("000601000005000c0200000300040400086400010264")
	if _, err := protocol.ParseStPracticeRoomList(payload[:len(payload)-1]); err == nil {
		t.Fatal("截断载荷应返回解析错误")
	}
	if _, err := protocol.ParseStPracticeRoomList(nil); err == nil {
		t.Fatal("空载荷应返回解析错误")
	}
}

func TestParseStPracticeRoomInfo(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint16(2)
	writeSeat := func(seq int8, playerID int32, name string, attackPower int64, level int32, enemy uint8) {
		w.WriteInt8(seq)
		w.WriteInt32(playerID)
		w.WriteInt32(101)
		w.WriteInt32(1872)
		w.WriteString(name)
		w.WriteString("s1")
		w.WriteString("s1_仙界一区")
		w.WriteInt64(attackPower)
		w.WriteInt32(level)
		w.WriteUint8(enemy)
		w.WriteInt32(3)
	}
	writeSeat(0, 7001, "飞羽", 123456789, 88, 1)
	writeSeat(1, 7002, "剑宗", 987654321, 95, 0)
	w.WriteInt32(41417)
	w.WriteInt64(89562)
	w.WriteInt16(10)
	w.WriteInt8(4)
	w.WriteUint8(protocol.StPracticeRoomYes)
	w.WriteInt8(20)

	info, err := protocol.ParseStPracticeRoomInfo(w.Bytes())
	if err != nil {
		t.Fatalf("解析房间信息失败: %v", err)
	}
	if len(info.Seats) != 2 {
		t.Fatalf("席位数量不匹配: %d", len(info.Seats))
	}
	first := info.Seats[0]
	if first.SeatSeq != 0 || first.PlayerID != 7001 || first.NickName != "飞羽" ||
		first.AttackPower != 123456789 || first.Level != 88 || first.EnemyFlag != 1 ||
		first.SupremeVIPPrefix != 3 {
		t.Errorf("首个席位字段不匹配: %+v", first)
	}
	if info.PracticeTime != 41417 || info.Exp != 89562 || info.RobTime != 10 ||
		info.RoomQuality != 4 || info.ExpPlusFlag != protocol.StPracticeRoomYes || info.ExpPlusPercent != 20 {
		t.Errorf("房间尾字段不匹配: %+v", info)
	}

	if _, err := protocol.ParseStPracticeRoomInfo(w.Bytes()[:len(w.Bytes())-1]); err == nil {
		t.Fatal("截断载荷应返回解析错误")
	}
}

// TestParseStPracticeRoomFreeInfo_LiveCapture 使用 s1 实机回包验证普通练功场解析。
func TestParseStPracticeRoomFreeInfo_LiveCapture(t *testing.T) {
	payload, err := hex.DecodeString("000100010000a1c90000000000015dda000a0001640300")
	if err != nil {
		t.Fatalf("解码实机载荷失败: %v", err)
	}
	info, err := protocol.ParseStPracticeRoomFreeInfo(payload)
	if err != nil {
		t.Fatalf("解析普通练功场失败: %v", err)
	}
	if info.TotalPage != 1 || info.TotalPlayer != 1 {
		t.Errorf("分页字段不匹配: %+v", info)
	}
	if info.PracticeTime != 41417 || info.Exp != 89562 || info.RobTime != 10 || info.PlayerPage != 1 {
		t.Errorf("练功字段不匹配: %+v", info)
	}
	if info.RoomQuality != 100 || info.ExpPlusFlag != protocol.StPracticeRoomNo || info.ExpPlusPercent != 0 {
		t.Errorf("房间品质/加成字段不匹配: %+v", info)
	}
}

func TestParseStPracticeRoomActionResult(t *testing.T) {
	result, err := protocol.ParseStPracticeRoomActionResult([]byte{protocol.StPracticeRoomSuccess})
	if err != nil {
		t.Fatalf("解析结果码失败: %v", err)
	}
	if result != protocol.StPracticeRoomSuccess {
		t.Errorf("结果码不匹配: %d", result)
	}
	if _, err := protocol.ParseStPracticeRoomActionResult(nil); err == nil {
		t.Fatal("空载荷应返回解析错误")
	}

	flag, err := protocol.ParseStPracticeRoomSeatStatus([]byte{protocol.StPracticeRoomNobody})
	if err != nil || flag != protocol.StPracticeRoomNobody {
		t.Errorf("席位状态解析失败: result=%d err=%v", flag, err)
	}
}

func TestParseStPracticeRoomExpResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StPracticeRoomSuccess)
	w.WriteInt64(89562)

	result, err := protocol.ParseStPracticeRoomExpResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析经验回包失败: %v", err)
	}
	if result.Result != protocol.StPracticeRoomSuccess || result.Exp != 89562 {
		t.Errorf("经验字段不匹配: %+v", result)
	}

	// 业务失败时服务端可能只下发结果码。
	noExp, err := protocol.ParseStPracticeRoomExpResult([]byte{protocol.StPracticeRoomNoExpToGet})
	if err != nil {
		t.Fatalf("解析单字节失败回包失败: %v", err)
	}
	if noExp.Result != protocol.StPracticeRoomNoExpToGet || noExp.Exp != 0 {
		t.Errorf("单字节失败回包解析错误: %+v", noExp)
	}

	if _, err := protocol.ParseStPracticeRoomExpResult(nil); err == nil {
		t.Fatal("空载荷应返回解析错误")
	}
}

func TestParseStPracticeRoomRobCD(t *testing.T) {
	// s1 实机 get_rob_cd_time 回包: cd=0, 清冷却元宝=10
	payload, _ := hex.DecodeString("0000000a")
	cd, err := protocol.ParseStPracticeRoomRobCD(payload)
	if err != nil {
		t.Fatalf("解析争夺冷却失败: %v", err)
	}
	if cd.CD != 0 || cd.Ingot != 10 {
		t.Errorf("争夺冷却字段不匹配: %+v", cd)
	}
	if _, err := protocol.ParseStPracticeRoomRobCD([]byte{0x00}); err == nil {
		t.Fatal("截断载荷应返回解析错误")
	}
}

func TestStPracticeRoomHelpers(t *testing.T) {
	if !protocol.StPracticeRoomIsStarQuality(5) || protocol.StPracticeRoomIsStarQuality(100) {
		t.Error("星级房间判定错误")
	}
	if protocol.StPracticeRoomSeatCount(5) != 12 || protocol.StPracticeRoomSeatCount(100) != 18 ||
		protocol.StPracticeRoomSeatCount(99) != 0 {
		t.Error("房间席位数量配置错误")
	}
	if protocol.StPracticeRoomQualityRank(5) != 5 || protocol.StPracticeRoomQualityRank(100) != 0 {
		t.Error("房间收益等级计算错误")
	}
}
