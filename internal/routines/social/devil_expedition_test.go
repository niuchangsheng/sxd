package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func devilPanel(weekStone, weekStone2 int32, souls []protocol.DevilSoul) protocol.DevilMatchPanel {
	return protocol.DevilMatchPanel{
		Result:        protocol.DevilResultSuccess,
		LevelSection:  6,
		Souls:         souls,
		WeekStone:     weekStone,
		WeekStoneMax:  20,
		WeekStone2:    weekStone2,
		WeekStone2Max: 20,
	}
}

// TestDevilExpeditionRoutine_SkipsWhenAllMaterialsCapped 验证三类材料达上限时
// 不再触发任何 start_expedition_single。
func TestDevilExpeditionRoutine_SkipsWhenAllMaterialsCapped(t *testing.T) {
	var startCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDDevilExpedition}))
		case protocol.ActionIDDevilActivityStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilActivityOpen}))
		case protocol.ActionIDDevilMatchPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDevilMatchPanelResultPacket(devilPanel(
				20, 20, []protocol.DevilSoul{
					{RoleID: 486, WeekSoul: 20, WeekMaxSoul: 20},
					{RoleID: 487, WeekSoul: 20, WeekMaxSoul: 20},
					{RoleID: 488, WeekSoul: 20, WeekMaxSoul: 20},
				})))
		case protocol.ActionIDDevilStartExpeditionSingle:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultSuccess}))
		}
	})
	defer sess.Close()

	if err := NewDevilExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("devil expedition: %v", err)
	}
	if startCalls.Load() != 0 {
		t.Fatalf("材料达上限时不得开始远征, start calls = %d", startCalls.Load())
	}
}

// TestDevilExpeditionRoutine_NotEnoughTumoling 验证屠魔令不足时跳过三类伙伴。
func TestDevilExpeditionRoutine_NotEnoughTumoling(t *testing.T) {
	var startCalls, enterCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDDevilExpedition}))
		case protocol.ActionIDDevilActivityStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilActivityOpen}))
		case protocol.ActionIDDevilMatchPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDevilMatchPanelResultPacket(devilPanel(
				0, 0, []protocol.DevilSoul{{RoleID: 486, WeekMaxSoul: 20}})))
		case protocol.ActionIDDevilStartExpeditionSingle:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultNotEnoughItem}))
		case protocol.ActionIDDevilEnterTown:
			enterCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultSuccess}))
		}
	})
	defer sess.Close()

	if err := NewDevilExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("devil expedition: %v", err)
	}
	if startCalls.Load() != 3 {
		t.Fatalf("三类伙伴各尝试一次, start calls = %d", startCalls.Load())
	}
	if enterCalls.Load() != 0 {
		t.Fatalf("屠魔令不足时不得进入关卡, enter calls = %d", enterCalls.Load())
	}
}

// TestDevilExpeditionRoutine_YinYangFightsUntilFailed 验证双生伙伴：
// 进入关卡后按 移动→战斗 循环，直到战斗返回失败，并退出远征。
func TestDevilExpeditionRoutine_YinYangFightsUntilFailed(t *testing.T) {
	var startCalls, enterCalls, fightCalls, quitCalls, moveCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDDevilExpedition}))
		case protocol.ActionIDDevilActivityStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilActivityOpen}))
		case protocol.ActionIDDevilMatchPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDevilMatchPanelResultPacket(devilPanel(
				0, 0, []protocol.DevilSoul{{RoleID: 486, WeekMaxSoul: 20}})))
		case protocol.ActionIDDevilStartExpeditionSingle:
			startCalls.Add(1)
			typeID := pkt.Payload[0]
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultSuccess}))
			if typeID == protocol.DevilMissionYinYang {
				// 模拟服务端推送 notify_enter_mission [Int 6][Int 144][Int 1]。
				w := protocol.NewWriter()
				w.WriteInt32(6).WriteInt32(144).WriteInt32(1)
				_ = protocol.WritePacket(conn, protocol.NewPacket(
					protocol.ActionIDDevilNotifyEnterMission, w.Bytes()))
			}
		case protocol.ActionIDDevilEnterTown:
			enterCalls.Add(1)
			w := protocol.NewWriter().WriteUint8(protocol.DevilResultSuccess).WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDDevilGetPlayers:
			w := protocol.NewWriter().WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDDevilGetMissionDataM6:
			_ = protocol.WritePacket(conn, protocol.BuildDevilMissionDataResultPacket(
				protocol.DevilMissionYinYang, protocol.DevilResultSuccess,
				[]int32{2, 1},
				[]protocol.DevilRefine{{Got: 0, CurrentBout: 0, NeedBout: 2, Week: 0, WeekMax: 20}}))
		case protocol.ActionIDDevilGetMissionDataM5, protocol.ActionIDDevilGetMissionDataM7:
			_ = protocol.WritePacket(conn, protocol.BuildDevilMissionDataResultPacket(
				protocol.DevilMissionMonsterGod, protocol.DevilResultSuccess, nil, nil))
		case protocol.ActionIDDevilMoveTo:
			moveCalls.Add(1) // 无回包
		case protocol.ActionIDDevilFightMonsterM6:
			n := fightCalls.Add(1)
			result := protocol.DevilResultSuccess
			if n >= 3 {
				result = protocol.DevilResultFailed
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result}))
		case protocol.ActionIDDevilQuit:
			quitCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultSuccess}))
		case protocol.ActionIDDevilRejectBuyBox, protocol.ActionIDDevilCloseMatchPanel,
			protocol.ActionIDDevilGetSkipWarInfo, protocol.ActionIDDevilGetDeployInfo,
			protocol.ActionIDDevilGetTeamStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultSuccess}))
		}
	})
	defer sess.Close()

	if err := NewDevilExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("devil expedition: %v", err)
	}
	if enterCalls.Load() != 1 {
		t.Fatalf("仅双生伙伴应进入关卡, enter calls = %d", enterCalls.Load())
	}
	// 队伍 1 连续成功 2 场后失败，切换队伍 2 再失败，共 4 场。
	if fightCalls.Load() != 4 {
		t.Fatalf("两个队伍均失败后应停止, fight calls = %d", fightCalls.Load())
	}
	if moveCalls.Load() != 4 {
		t.Fatalf("每次战斗前应移动, move calls = %d", moveCalls.Load())
	}
	if quitCalls.Load() == 0 {
		t.Fatal("结束该伙伴挑战后应退出远征")
	}
}

// TestDevilExpeditionRoutine_SkipsCappedSoul 验证魔灵全部达上限时跳过魔神伙伴。
func TestDevilExpeditionRoutine_SkipsCappedSoul(t *testing.T) {
	var startPayloads []byte
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDDevilExpedition}))
		case protocol.ActionIDDevilActivityStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilActivityOpen}))
		case protocol.ActionIDDevilMatchPanel:
			_ = protocol.WritePacket(conn, protocol.BuildDevilMatchPanelResultPacket(devilPanel(
				0, 0, []protocol.DevilSoul{{RoleID: 486, WeekSoul: 20, WeekMaxSoul: 20}})))
		case protocol.ActionIDDevilStartExpeditionSingle:
			startPayloads = append(startPayloads, pkt.Payload[0])
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DevilResultNotEnoughItem}))
		}
	})
	defer sess.Close()

	if err := NewDevilExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("devil expedition: %v", err)
	}
	for _, p := range startPayloads {
		if p == protocol.DevilMissionMonsterGod {
			t.Fatalf("魔灵达上限时不应触发魔神伙伴: %v", startPayloads)
		}
	}
	if len(startPayloads) != 2 {
		t.Fatalf("应仅尝试双生与隐仙, start payloads = %v", startPayloads)
	}
}

func TestDevilMaterialCapped(t *testing.T) {
	if capped, _ := devilMaterialCapped(&protocol.DevilMatchPanel{WeekStone: 20, WeekStoneMax: 20}, protocol.DevilMissionYinYang); !capped {
		t.Fatal("阴阳石 20/20 应判定达上限")
	}
	if capped, _ := devilMaterialCapped(&protocol.DevilMatchPanel{WeekStone: 10, WeekStoneMax: 20}, protocol.DevilMissionYinYang); capped {
		t.Fatal("阴阳石 10/20 不应判定达上限")
	}
	if capped, _ := devilMaterialCapped(&protocol.DevilMatchPanel{
		Souls: []protocol.DevilSoul{{WeekSoul: 20, WeekMaxSoul: 20}, {WeekSoul: 19, WeekMaxSoul: 20}},
	}, protocol.DevilMissionMonsterGod); capped {
		t.Fatal("仍有魔灵未达上限时不应跳过魔神")
	}
	if !devilRefinesCapped([]protocol.DevilRefine{{Week: 20, WeekMax: 20}}) {
		t.Fatal("炼化 20/20 应判定达上限")
	}
}
