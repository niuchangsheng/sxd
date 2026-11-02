// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestFactionRollCakeCodec_BuildRequests(t *testing.T) {
	cases := []struct {
		name     string
		pkt      *Packet
		actionID uint32
	}{
		{"remain_times", BuildFactionRollCakeRemainTimesPacket(ActionIDTownEnter), ActionIDFactionRollCakeRemainTimes},
		{"info", BuildFactionRollCakeInfoPacket(ActionIDTownEnter), ActionIDFactionRollCakeInfo},
		{"roll_cake", BuildFactionRollCakePacket(ActionIDFactionRollCakeInfo), ActionIDFactionRollCake},
	}

	for _, tc := range cases {
		if tc.pkt.ActionID != tc.actionID {
			t.Errorf("%s ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", tc.name, tc.actionID, tc.pkt.ActionID)
		}
		if len(tc.pkt.Payload) != 4 {
			t.Errorf("%s 请求载荷应为 4 字节前序协议号, 实际 %d 字节", tc.name, len(tc.pkt.Payload))
		}
	}

	// 未指定前序协议时默认回落到进入城镇动作
	if hex.EncodeToString(BuildFactionRollCakePacket(0).Payload) != "00010000" {
		t.Errorf("默认 prev_act 应为 town_enter, 实际: %s", hex.EncodeToString(BuildFactionRollCakePacket(0).Payload))
	}
}

func TestFactionRollCakeCodec_ParseRemainTimes(t *testing.T) {
	remain, err := ParseFactionRollCakeRemainTimesResult([]byte{0x03})
	if err != nil {
		t.Fatalf("解析剩余次数失败: %v", err)
	}
	if remain != 3 {
		t.Errorf("期望剩余 3 次, 实际 %d", remain)
	}

	if _, err := ParseFactionRollCakeRemainTimesResult(nil); err == nil {
		t.Error("空载荷应返回解析错误")
	}
}

func TestFactionRollCakeCodec_ParseInfo(t *testing.T) {
	payload, err := hex.DecodeString(
		"000003d4" + // week_best_score = 980
			"00000078" + // today_best_score = 120
			"02" + // remain_times = 2
			"00001388" + // faction_score = 5000
			"0001" + // 成员条数 = 1
			"00000002" + // player_id = 2
			"0009e6a2a6e4b880e59cba" + // nickname = "梦一场"
			"000003d4", // score = 980
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	info, err := ParseFactionRollCakeInfoResult(payload)
	if err != nil {
		t.Fatalf("解析吉星高照战绩失败: %v", err)
	}
	if info.WeekBestScore != 980 || info.TodayBestScore != 120 {
		t.Errorf("成绩解析不匹配: week=%d today=%d", info.WeekBestScore, info.TodayBestScore)
	}
	if info.RemainTimes != 2 {
		t.Errorf("剩余次数不匹配: %d", info.RemainTimes)
	}
	if info.FactionScore != 5000 {
		t.Errorf("帮派成绩不匹配: %d", info.FactionScore)
	}
	if len(info.Members) != 1 {
		t.Fatalf("成员条数不匹配: %d", len(info.Members))
	}
	if info.Members[0].PlayerID != 2 || info.Members[0].NickName != "梦一场" || info.Members[0].Score != 980 {
		t.Errorf("成员解析不匹配: %+v", info.Members[0])
	}
}

func TestFactionRollCakeCodec_ParseRollCake(t *testing.T) {
	payload, err := hex.DecodeString(
		"20" + // msg = 32 (SUCCESS)
			"03" + // type = 3
			"00000042" + // score = 66
			"00000084" + // today_score = 132
			"0003" + // 骰子数量 = 3
			"060606" + // 骰子点数 6/6/6
			"01", // remain_times = 1
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	res, err := ParseFactionRollCakeResult(payload)
	if err != nil {
		t.Fatalf("解析摇骰结算失败: %v", err)
	}
	if res.Msg != RollCakeResultSuccess {
		t.Errorf("结果码不匹配: %d", res.Msg)
	}
	if res.Type != 3 || res.Score != 66 || res.TodayScore != 132 {
		t.Errorf("结算字段不匹配: %+v", res)
	}
	if len(res.Dice) != 3 || res.Dice[0] != 6 || res.Dice[1] != 6 || res.Dice[2] != 6 {
		t.Errorf("骰子点数不匹配: %v", res.Dice)
	}
	if res.RemainTimes != 1 {
		t.Errorf("剩余次数不匹配: %d", res.RemainTimes)
	}
}

func TestFactionRollCakeCodec_ParseRollCakeTruncated(t *testing.T) {
	// 声明 3 颗骰子但只给 1 颗，必须报错而不是越界
	payload, _ := hex.DecodeString("20030000004200000084000306")
	if _, err := ParseFactionRollCakeResult(payload); err == nil {
		t.Error("截断载荷应返回解析错误")
	}
}
