package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestTakeBible_PacketBuilders(t *testing.T) {
	// 1. open_take_bible
	pOpen := protocol.BuildTakeBibleOpenPacket(0)
	if pOpen.ActionID != protocol.ActionIDTakeBibleOpen {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleOpen, pOpen.ActionID)
	}

	// 2. close_take_bible
	pClose := protocol.BuildTakeBibleClosePacket(0)
	if pClose.ActionID != protocol.ActionIDTakeBibleClose {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleClose, pClose.ActionID)
	}

	// 3. get_take_bible_info
	pInfo := protocol.BuildTakeBibleGetInfoPacket(protocol.ActionIDTakeBibleOpen)
	if pInfo.ActionID != protocol.ActionIDTakeBibleGetInfo {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleGetInfo, pInfo.ActionID)
	}

	// 4. refresh
	pRefresh := protocol.BuildTakeBibleRefreshPacket(protocol.ActionIDTakeBibleGetInfo)
	if pRefresh.ActionID != protocol.ActionIDTakeBibleRefresh {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleRefresh, pRefresh.ActionID)
	}

	// 5. apply_friend
	friendID := int32(88888)
	pApply := protocol.BuildTakeBibleApplyFriendPacket(friendID, protocol.ActionIDTakeBibleGetInfo)
	if pApply.ActionID != protocol.ActionIDTakeBibleApplyFriend {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleApplyFriend, pApply.ActionID)
	}
	r := protocol.NewReader(pApply.Payload)
	readFriendID, err := r.ReadInt32()
	if err != nil || readFriendID != friendID {
		t.Fatalf("解析 apply_friend 载荷失败, 期望好友ID %d, 实际 %d, err: %v", friendID, readFriendID, err)
	}

	// 6. start_take_bible
	pStart := protocol.BuildTakeBibleStartPacket(protocol.ActionIDTakeBibleGetInfo)
	if pStart.ActionID != protocol.ActionIDTakeBibleStart {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleStart, pStart.ActionID)
	}

	// 7. rob
	targetID := int32(99999)
	pRob := protocol.BuildTakeBibleRobPacket(targetID, protocol.ActionIDTakeBibleOpen)
	if pRob.ActionID != protocol.ActionIDTakeBibleRob {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleRob, pRob.ActionID)
	}

	// 8. quickly & get_rob_cd & buy_bless
	pQuickly := protocol.BuildTakeBibleQuicklyPacket(0)
	if pQuickly.ActionID != protocol.ActionIDTakeBibleQuickly {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleQuickly, pQuickly.ActionID)
	}
	pCD := protocol.BuildTakeBibleGetRobCDPacket(0)
	if pCD.ActionID != protocol.ActionIDTakeBibleGetRobCD {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleGetRobCD, pCD.ActionID)
	}
	pBless := protocol.BuildTakeBibleBuyBlessPacket(0)
	if pBless.ActionID != protocol.ActionIDTakeBibleBuyBless {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTakeBibleBuyBless, pBless.ActionID)
	}
}

func TestTakeBible_Parsers(t *testing.T) {
	// 1. ParseTakeBibleOpenResult
	wOpen := protocol.NewWriter()
	wOpen.WriteInt32(1)  // scene
	wOpen.WriteUint8(2)  // canTakeTimes = 2
	wOpen.WriteInt32(0)  // unused
	wOpen.WriteInt32(60) // cdSeconds = 60
	wOpen.WriteUint16(0) // unused
	wOpen.WriteUint8(3)  // protectTimes = 3
	wOpen.WriteUint8(4)  // robTimes = 4
	openRes, err := protocol.ParseTakeBibleOpenResult(wOpen.Bytes())
	if err != nil {
		t.Fatalf("ParseTakeBibleOpenResult 失败: %v", err)
	}
	if openRes.CanTakeTimes != 2 || openRes.ProtectTimes != 3 || openRes.RobTimes != 4 || openRes.CDSeconds != 60 {
		t.Errorf("ParseTakeBibleOpenResult 字段不符: %+v", openRes)
	}

	// 2. ParseTakeBibleGetInfoResult
	wInfo := protocol.NewWriter()
	wInfo.WriteUint8(protocol.TakeBibleSuccess) // 8
	wInfo.WriteInt8(protocol.TakeBibleSunWuKong) // 3 (孙悟空)
	wInfo.WriteInt32(12345)                     // protectPlayerID
	wInfo.WriteInt16(0)                         // speedUpCD
	// 在线好友列表 (1个好友)
	wInfo.WriteUint16(1)
	wInfo.WriteInt32(66666)
	wInfo.WriteString("至尊宝")
	wInfo.WriteInt8(2) // remainProtectTimes
	wInfo.WriteInt8(1) // online
	wInfo.WriteInt8(0) // not escorting
	// 尾部字段
	wInfo.WriteInt8(1) // totalTakeTimes
	wInfo.WriteInt8(3) // robTimes
	wInfo.WriteUint8(0) // isBusy = false
	infoRes, err := protocol.ParseTakeBibleGetInfoResult(wInfo.Bytes())
	if err != nil {
		t.Fatalf("ParseTakeBibleGetInfoResult 失败: %v", err)
	}
	if infoRes.CurrentAngel != protocol.TakeBibleSunWuKong {
		t.Errorf("期望使者 孙悟空(%d), 实际 %d", protocol.TakeBibleSunWuKong, infoRes.CurrentAngel)
	}
	if infoRes.ProtectPlayerID != 12345 {
		t.Errorf("期望保护好友ID 12345, 实际 %d", infoRes.ProtectPlayerID)
	}
	if len(infoRes.ProtectFriends) != 1 || infoRes.ProtectFriends[0].PlayerName != "至尊宝" {
		t.Errorf("好友列表解析错误: %+v", infoRes.ProtectFriends)
	}

	// 3. ParseTakeBibleRefreshResult
	wRefresh := protocol.NewWriter()
	wRefresh.WriteUint8(protocol.TakeBibleSuccess)
	wRefresh.WriteInt8(protocol.TakeBibleTangSeng) // 4 (唐僧)
	wRefresh.WriteInt16(50)                       // nextCost = 50
	refRes, err := protocol.ParseTakeBibleRefreshResult(wRefresh.Bytes())
	if err != nil {
		t.Fatalf("ParseTakeBibleRefreshResult 失败: %v", err)
	}
	if refRes.NewAngel != protocol.TakeBibleTangSeng || refRes.NextRefreshCost != 50 {
		t.Errorf("刷新使者结果不符: %+v", refRes)
	}

	// 4. ParseTakeBibleApplyFriendResult
	wApply := protocol.NewWriter()
	wApply.WriteUint8(protocol.TakeBibleSuccess)
	applyCode, err := protocol.ParseTakeBibleApplyFriendResult(wApply.Bytes())
	if err != nil || applyCode != protocol.TakeBibleSuccess {
		t.Errorf("ParseTakeBibleApplyFriendResult 失败: code=%d, err=%v", applyCode, err)
	}

	// 5. ParseTakeBibleStartResult
	wStart := protocol.NewWriter()
	wStart.WriteUint8(protocol.TakeBibleSuccess)
	startCode, err := protocol.ParseTakeBibleStartResult(wStart.Bytes())
	if err != nil || startCode != protocol.TakeBibleSuccess {
		t.Errorf("ParseTakeBibleStartResult 失败: code=%d, err=%v", startCode, err)
	}

	// 6. AngelName
	if protocol.AngelName(protocol.TakeBibleTangSeng) != "唐僧" {
		t.Errorf("期望 唐僧, 实际 %s", protocol.AngelName(protocol.TakeBibleTangSeng))
	}
	if protocol.AngelName(protocol.TakeBibleBaiLongMa) != "白龙马" {
		t.Errorf("期望 白龙马, 实际 %s", protocol.AngelName(protocol.TakeBibleBaiLongMa))
	}
}

func TestStTakeBible_PacketBuildersAndParsers(t *testing.T) {
	pInfo := protocol.BuildStTakeBibleGetInfoPacket(0)
	if pInfo.ActionID != protocol.ActionIDStTakeBibleGetInfo {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDStTakeBibleGetInfo, pInfo.ActionID)
	}

	pRef := protocol.BuildStTakeBibleRefreshPacket(0)
	if pRef.ActionID != protocol.ActionIDStTakeBibleRefresh {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDStTakeBibleRefresh, pRef.ActionID)
	}

	pBless := protocol.BuildStTakeBibleBuyBlessPacket(0)
	if pBless.ActionID != protocol.ActionIDStTakeBibleBuyBless {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDStTakeBibleBuyBless, pBless.ActionID)
	}

	pStart := protocol.BuildStTakeBibleStartPacket(0)
	if pStart.ActionID != protocol.ActionIDStTakeBibleStart {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDStTakeBibleStart, pStart.ActionID)
	}

	pQuick := protocol.BuildStTakeBibleQuicklyPacket(0)
	if pQuick.ActionID != protocol.ActionIDStTakeBibleQuickly {
		t.Fatalf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDStTakeBibleQuickly, pQuick.ActionID)
	}

	// Parser test
	wStart := protocol.NewWriter()
	wStart.WriteUint8(protocol.StTakeBibleSuccess)
	code, err := protocol.ParseStTakeBibleStartResult(wStart.Bytes())
	if err != nil || code != protocol.StTakeBibleSuccess {
		t.Fatalf("ParseStTakeBibleStartResult 失败: code=%d, err=%v", code, err)
	}
}
