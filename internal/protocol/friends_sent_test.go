// Package protocol_test 提供好友来信协议 (Module 684) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFriendsSentPanelResult(t *testing.T) {
	letters, err := protocol.ParseFriendsSentPanelResult(protocol.BuildFriendsSentPanelResultPacket([]protocol.FriendsSentLetter{
		{ID: 1, UnlockTime: 100, UnlockMissionID: 2},
		{ID: 2, IsTimeLock: 1},
	}).Payload)
	if err != nil || len(letters) != 2 || letters[0].ID != 1 || letters[1].IsTimeLock != 1 {
		t.Fatalf("好友来信列表不一致: %+v err=%v", letters, err)
	}
}

func TestFriendsSentOpenResult(t *testing.T) {
	res, err := protocol.ParseFriendsSentOpenResult(protocol.BuildFriendsSentOpenResultPacket(protocol.FriendsSentOpenResult{
		IsRead: 0, IsGetAward: 0, ContentType: 1, ID: 3,
		Awards: [][2]int32{{5715, 2}, {1747, 1000}},
	}).Payload)
	if err != nil || res.ID != 3 || res.IsRead != 0 || len(res.Awards) != 2 || res.Awards[0][0] != 5715 {
		t.Fatalf("好友来信内容不一致: %+v err=%v", res, err)
	}
}

func TestFriendsSentActionResult(t *testing.T) {
	result, err := protocol.ParseFriendsSentActionResult(
		protocol.BuildFriendsSentActionResultPacket(protocol.ActionIDFriendsSentAward, protocol.FriendsSentResultSuccess).Payload)
	if err != nil || result != protocol.FriendsSentResultSuccess {
		t.Fatalf("期望成功码 0, got %d err=%v", result, err)
	}
	// 失败码: 未阅读必须能被识别
	result, err = protocol.ParseFriendsSentActionResult(
		protocol.BuildFriendsSentActionResultPacket(protocol.ActionIDFriendsSentAward, protocol.FriendsSentResultUnRead).Payload)
	if err != nil || result != protocol.FriendsSentResultUnRead {
		t.Fatalf("期望未阅读码 %d, got %d err=%v", protocol.FriendsSentResultUnRead, result, err)
	}
	if _, err := protocol.ParseFriendsSentActionResult(nil); err == nil {
		t.Error("空好友来信回包应返回解析错误")
	}
}
