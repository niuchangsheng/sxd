// Package dungeon_test 验证锁妖塔自动挑战闭环与失败码分流。
package dungeon_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/scheduler"
)

// buildDemonTowerPanel 构造 Module 705 panel_info 回包。
func buildDemonTowerPanel(nowFinishLayer, tickets int32, layers [][2]int32) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.DemonTowerResultSuccess)
	w.WriteInt32(0)              // is_first
	w.WriteInt32(31)             // season
	w.WriteInt32(100)            // left_time
	w.WriteInt32(1)              // now_step
	w.WriteInt32(nowFinishLayer) // now_finish_layer
	w.WriteInt32(20)             // now_total_round
	w.WriteInt32(0)              // next_layer_left_time
	w.WriteInt32(10)             // rank
	w.WriteUint16(0)             // step_list
	w.WriteUint16(uint16(len(layers)))
	for _, layer := range layers {
		w.WriteInt32(layer[0])
		w.WriteInt32(layer[1])
	}
	w.WriteInt32(0) // buy_ticket_num
	w.WriteInt32(tickets)
	w.WriteInt32(184) // race_step
	w.WriteInt32(0)   // ranks
	return w.Bytes()
}

type demonTowerMockState struct {
	mu         sync.Mutex
	panelSeq   int
	panels     [][]byte
	challenges []int32
}

func (s *demonTowerMockState) nextPanel() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.panels) == 0 {
		return nil
	}
	idx := s.panelSeq
	if idx >= len(s.panels) {
		idx = len(s.panels) - 1
	}
	s.panelSeq++
	return s.panels[idx]
}

func (s *demonTowerMockState) challengLayers() []int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int32, len(s.challenges))
	copy(out, s.challenges)
	return out
}

// TestDemonTowerRoutine_ChallengesUntilTicketsExhausted 覆盖成功路径:
// 面板按 layer_list 最高层 +1 连续挑战，次数耗尽后停止。
func TestDemonTowerRoutine_ChallengesUntilTicketsExhausted(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	state := &demonTowerMockState{
		panels: [][]byte{
			buildDemonTowerPanel(2, 2, [][2]int32{{1, 10}, {2, 11}}),
			buildDemonTowerPanel(3, 1, [][2]int32{{1, 10}, {2, 11}, {3, 12}}),
			buildDemonTowerPanel(4, 0, [][2]int32{{1, 10}, {2, 11}, {3, 12}, {4, 13}}),
		},
	}

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDDemonTowerBaseInfo:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{1}))
			return true
		case protocol.ActionIDDemonTowerPanel:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, state.nextPanel()))
			return true
		case protocol.ActionIDDemonTowerChallenge:
			layer, err := protocol.NewReader(pkt.Payload).ReadInt32()
			if err != nil {
				t.Errorf("challenge 载荷解析失败: %v", err)
				return true
			}
			state.mu.Lock()
			state.challenges = append(state.challenges, layer)
			state.mu.Unlock()
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DemonTowerResultSuccess}))
			return true
		}
		return false
	})

	err := dungeon.NewDemonTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("次数耗尽应返回 ErrAttemptsExhausted, got %v", err)
	}

	got := state.challengLayers()
	want := []int32{3, 4}
	if len(got) != len(want) {
		t.Fatalf("挑战层数不匹配: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("挑战层数不匹配: got=%v want=%v", got, want)
		}
	}
}

// TestDemonTowerRoutine_NotEnoughTicketStops 覆盖失败码 3:
// 服务端返回次数不足时应立即结束本轮，不再重复发包。
func TestDemonTowerRoutine_NotEnoughTicketStops(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	state := &demonTowerMockState{
		panels: [][]byte{
			buildDemonTowerPanel(1, 1, [][2]int32{{1, 10}}),
		},
	}

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDDemonTowerBaseInfo:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{1}))
			return true
		case protocol.ActionIDDemonTowerPanel:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, state.nextPanel()))
			return true
		case protocol.ActionIDDemonTowerChallenge:
			layer, _ := protocol.NewReader(pkt.Payload).ReadInt32()
			state.mu.Lock()
			state.challenges = append(state.challenges, layer)
			state.mu.Unlock()
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DemonTowerResultNotEnoughTicket}))
			return true
		}
		return false
	})

	err := dungeon.NewDemonTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("次数不足应返回 ErrAttemptsExhausted, got %v", err)
	}

	got := state.challengLayers()
	if len(got) != 1 {
		t.Fatalf("次数不足后不应继续挑战, got=%v", got)
	}
}

// TestDemonTowerRoutine_ItemPackFullStops 覆盖失败码 5:
// 背包已满时熔断本轮，避免继续消耗挑战次数。
func TestDemonTowerRoutine_ItemPackFullStops(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	state := &demonTowerMockState{
		panels: [][]byte{
			buildDemonTowerPanel(1, 3, [][2]int32{{1, 10}}),
		},
	}

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDDemonTowerBaseInfo:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{1}))
			return true
		case protocol.ActionIDDemonTowerPanel:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, state.nextPanel()))
			return true
		case protocol.ActionIDDemonTowerChallenge:
			state.mu.Lock()
			state.challenges = append(state.challenges, 2)
			state.mu.Unlock()
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DemonTowerResultItemPackFull}))
			return true
		}
		return false
	})

	err := dungeon.NewDemonTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("背包已满应返回 ErrBagFull, got %v", err)
	}

	if got := state.challengLayers(); len(got) != 1 {
		t.Fatalf("背包已满后不应继续挑战, got=%v", got)
	}
}

// TestDemonTowerRoutine_EmptyLayerListUsesRankSelfLayer 覆盖跨阶次场景:
// layer_list 为空且 now_finish_layer 归零时，用排行榜 self_layer 推导下一层。
func TestDemonTowerRoutine_EmptyLayerListUsesRankSelfLayer(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	state := &demonTowerMockState{
		panels: [][]byte{
			buildDemonTowerPanel(0, 1, nil),
			buildDemonTowerPanel(0, 0, nil),
		},
	}

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDDemonTowerBaseInfo:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{1}))
			return true
		case protocol.ActionIDDemonTowerPanel:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, state.nextPanel()))
			return true
		case protocol.ActionIDDemonTowerRank:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.DemonTowerResultSuccess)
			w.WriteInt32(40) // self_layer
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			return true
		case protocol.ActionIDDemonTowerChallenge:
			layer, _ := protocol.NewReader(pkt.Payload).ReadInt32()
			state.mu.Lock()
			state.challenges = append(state.challenges, layer)
			state.mu.Unlock()
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.DemonTowerResultSuccess}))
			return true
		}
		return false
	})

	err := dungeon.NewDemonTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("次数耗尽应返回 ErrAttemptsExhausted, got %v", err)
	}

	got := state.challengLayers()
	if len(got) != 1 || got[0] != 41 {
		t.Fatalf("应挑战第 41 层, got=%v", got)
	}
}

// TestDemonTowerRoutine_SeasonClosedSkips 验证赛季未开放时不发送面板与挑战请求。
func TestDemonTowerRoutine_SeasonClosedSkips(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	var panelCalls, challengeCalls int
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDDemonTowerBaseInfo:
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, []byte{0}))
			return true
		case protocol.ActionIDDemonTowerPanel:
			panelCalls++
			return true
		case protocol.ActionIDDemonTowerChallenge:
			challengeCalls++
			return true
		}
		return false
	})

	if err := dungeon.NewDemonTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("赛季未开放应安全跳过, got %v", err)
	}
	if panelCalls != 0 || challengeCalls != 0 {
		t.Fatalf("赛季未开放时不应发送业务请求: panel=%d challenge=%d", panelCalls, challengeCalls)
	}
}
