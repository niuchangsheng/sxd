// Package protocol 提供宗门灵兽协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestSectMonsterListParseLive(t *testing.T) {
	// 2026-11-07 线上真实回包 (sect_id=7, 经仙盟城镇会话, 不带 PrevAct)。
	raw, _ := hex.DecodeString("00030000000300000064000000060000138800000001000000640000000a00001388" +
		"00000002000000640000000a00001388000000000000000000000000000000000003000000030000001500000004" +
		"000000020000001500000004000000010000001500000006")
	list, err := ParseSectMonsterListResult(raw)
	if err != nil {
		t.Fatalf("解析宗门灵兽列表失败: %v", err)
	}
	if len(list.Monsters) != 3 {
		t.Fatalf("灵兽数量期望 3, 实际 %d", len(list.Monsters))
	}
	if list.Monsters[0].Type != 3 || list.Monsters[0].Level != 100 || list.Monsters[0].ExpCap != 5000 {
		t.Fatalf("灵兽[0] 解析不符: %+v", list.Monsters[0])
	}
	if list.Monsters[1].Type != 1 || list.Monsters[2].Type != 2 {
		t.Fatalf("灵兽类型顺序不符: %+v", list.Monsters)
	}
	if list.FreeFeedNum != 0 || list.HighFeedNum != 0 || list.JobID != 0 || list.WeekAddMonsterExp != 0 {
		t.Fatalf("标量解析不符: %+v", list)
	}
	if len(list.CommonMonsters) != 3 || list.CommonMonsters[0].Type != 3 ||
		list.CommonMonsters[2].Type != 1 || list.CommonMonsters[2].Num != 6 {
		t.Fatalf("通用灵兽列表解析不符: %+v", list.CommonMonsters)
	}

	// 往返
	round := BuildSectMonsterListResultPacket(*list)
	again, err := ParseSectMonsterListResult(round.Payload)
	if err != nil || len(again.Monsters) != 3 || again.Monsters[0].Type != 3 {
		t.Fatalf("列表往返失败: %+v err=%v", again, err)
	}
}

func TestSectMonsterFeedParse(t *testing.T) {
	// 线上成功样本: result=0, 1 项奖励 2471×10。
	raw, _ := hex.DecodeString("000001000009a70000000a")
	res, err := ParseSectMonsterFeedResult(raw)
	if err != nil {
		t.Fatalf("解析喂养回包失败: %v", err)
	}
	if res.Result != SectMonsterResultSuccess || len(res.Awards) != 1 ||
		res.Awards[0].ItemID != 2471 || res.Awards[0].ItemNum != 10 {
		t.Fatalf("喂养回包解析不符: %+v", res)
	}

	// 失败码: 免费次数不足, 无奖励列表。
	fail, err := ParseSectMonsterFeedResult([]byte{SectMonsterResultFeedNumLimit})
	if err != nil || fail.Result != SectMonsterResultFeedNumLimit || len(fail.Awards) != 0 {
		t.Fatalf("失败码解析不符: %+v err=%v", fail, err)
	}

	if _, err := ParseSectMonsterFeedResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
}

func TestSectMonsterPackets(t *testing.T) {
	list := BuildSectMonsterListPacket(7)
	if list.ActionID != ActionIDSectMonsterList || list.HasPrevAct {
		t.Fatalf("列表封包不符: %+v", list)
	}
	if got := hex.EncodeToString(list.Payload); got != "00000007" {
		t.Fatalf("列表载荷不符: %s", got)
	}

	feed := BuildSectMonsterFeedPacket(1, 0)
	if feed.ActionID != ActionIDSectMonsterFeed || feed.HasPrevAct {
		t.Fatalf("喂养封包不符: %+v", feed)
	}
	if got := hex.EncodeToString(feed.Payload); got != "0000000100000000" {
		t.Fatalf("喂养载荷不符: %s", got)
	}
}