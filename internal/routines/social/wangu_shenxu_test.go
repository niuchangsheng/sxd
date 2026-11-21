package social

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// buildWanguShenxuPanelPayload 构造 840/0 主面板回包载荷。
func buildWanguShenxuPanelPayload(players []protocol.WanguShenxuPlayer, canGetCoin int64) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.WanguShenxuResultSuccess)
	w.WriteUint16(uint16(len(players)))
	for _, p := range players {
		w.WriteInt32(p.Type)
		w.WriteInt32(p.Season)
		w.WriteInt32(p.Step)
		w.WriteInt32(p.PlayerID)
		w.WriteString(p.ServerName)
		w.WriteString(p.Nickname)
		w.WriteInt32(p.RoleID)
		w.WriteInt32(p.SuitID)
		w.WriteInt64(p.WarPower)
		if p.IsWorship {
			w.WriteUint8(1)
		} else {
			w.WriteUint8(0)
		}
		w.WriteInt32(p.WorshipNum)
		w.WriteUint16(uint16(len(p.Seasons)))
		for _, s := range p.Seasons {
			w.WriteInt32(s.Season)
			w.WriteInt32(s.Step)
		}
		w.WriteInt64(p.WeedAwardCoin)
		w.WriteInt64(p.TotalAwardCoin)
	}
	w.WriteInt64(canGetCoin)
	return w.Bytes()
}

func TestWanguShenxuRoutine_WorshipsPendingStatues(t *testing.T) {
	var worshipCalls atomic.Int32
	var infoCalls atomic.Int32
	var gotTypes atomic.Value // []int32
	gotTypes.Store([]int32{})
	panel := buildWanguShenxuPanelPayload([]protocol.WanguShenxuPlayer{
		{Type: 4, Season: 1}, // 空位
		{Type: 3, Season: 15, PlayerID: 259239, ServerName: "s0", Nickname: "司空测试号"},
		{Type: 2, Season: 3, PlayerID: 259239, ServerName: "s0", Nickname: "司空测试号"},
		{Type: 5, Season: 5, PlayerID: 259239, ServerName: "s0", Nickname: "司空测试号", IsWorship: true},
	}, 0)

	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWanguShenxuMainInfo:
			infoCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuMainInfo, panel))
		case protocol.ActionIDWanguShenxuWorship:
			n := worshipCalls.Add(1)
			typ := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
				uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3]))
			gotTypes.Store(append(gotTypes.Load().([]int32), typ))
			resp := protocol.NewWriter()
			resp.WriteUint8(protocol.WanguShenxuResultSuccess)
			resp.WriteInt32(int32(5 * n))
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuWorship, resp.Bytes()))
		case protocol.ActionIDWanguShenxuGetAward:
			t.Errorf("can_get_coin=0 时不应请求 840/2")
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "wangu_shenxu")

	if err := NewWanguShenxuRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("膜拜成功不应报错: %v", err)
	}
	if worshipCalls.Load() != 2 {
		t.Fatalf("膜拜请求次数 = %d, want 2", worshipCalls.Load())
	}
	types := gotTypes.Load().([]int32)
	if infoCalls.Load() != 2 {
		t.Fatalf("主面板请求次数 = %d, want 2 (初始 + 每次成功后刷新)", infoCalls.Load())
	}
	if len(types) != 2 || types[0] != 3 || types[1] != 2 {
		t.Fatalf("膜拜神像类型 = %v, want [3 2]", types)
	}
}

func TestWanguShenxuRoutine_AlreadyWorshippedSkips(t *testing.T) {
	panel := buildWanguShenxuPanelPayload([]protocol.WanguShenxuPlayer{
		{Type: 3, Season: 15, PlayerID: 259239, IsWorship: true},
		{Type: 2, Season: 3, PlayerID: 259239, IsWorship: true},
		{Type: 1, Season: 7}, // 空位
	}, 0)

	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWanguShenxuMainInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuMainInfo, panel))
		case protocol.ActionIDWanguShenxuWorship:
			t.Error("已膜拜/空位不应发送 840/1")
		}
	})
	defer sess.Close()

	if err := NewWanguShenxuRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("今日已膜拜不应报错: %v", err)
	}
}

func TestWanguShenxuRoutine_ClaimsCoinAward(t *testing.T) {
	var awardCalls atomic.Int32
	panel := buildWanguShenxuPanelPayload([]protocol.WanguShenxuPlayer{
		{Type: 3, Season: 15, PlayerID: 259239, IsWorship: true},
	}, 123456)

	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWanguShenxuMainInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuMainInfo, panel))
		case protocol.ActionIDWanguShenxuGetAward:
			awardCalls.Add(1)
			resp := protocol.NewWriter()
			resp.WriteUint8(protocol.WanguShenxuResultSuccess)
			resp.WriteInt64(123456)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuGetAward, resp.Bytes()))
		}
	})
	defer sess.Close()

	if err := NewWanguShenxuRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("领取铜钱奖励不应报错: %v", err)
	}
	if awardCalls.Load() != 1 {
		t.Fatalf("领奖请求次数 = %d, want 1", awardCalls.Load())
	}
}

func TestWanguShenxuRoutine_NotOpenBacksOff(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDWanguShenxuMainInfo {
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDWanguShenxuMainInfo, []byte{protocol.WanguShenxuResultFuncNotOpen}))
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "wangu_shenxu")

	err := NewWanguShenxuRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("功能未开放应返回 CooldownError, 实际: %v", err)
	}
	if n := scheduler.SilentDropCount(sess, "wangu_shenxu"); n != 1 {
		t.Fatalf("沉默计数 = %d, want 1", n)
	}
}

func TestWanguShenxuRoutine_HasWorshippedIsSuccess(t *testing.T) {
	panel := buildWanguShenxuPanelPayload([]protocol.WanguShenxuPlayer{
		{Type: 3, Season: 15, PlayerID: 259239},
	}, 0)

	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWanguShenxuMainInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuMainInfo, panel))
		case protocol.ActionIDWanguShenxuWorship:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDWanguShenxuWorship, []byte{protocol.WanguShenxuResultHasWorshipped}))
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "wangu_shenxu")

	if err := NewWanguShenxuRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("已膜拜结果码不应报错: %v", err)
	}
	if n := scheduler.SilentDropCount(sess, "wangu_shenxu"); n != 0 {
		t.Fatalf("已膜拜不应累计丢包计数, got %d", n)
	}
}

func TestWanguShenxuRoutine_AllWorshipFailuresBackOff(t *testing.T) {
	panel := buildWanguShenxuPanelPayload([]protocol.WanguShenxuPlayer{
		{Type: 3, Season: 15, PlayerID: 259239},
		{Type: 2, Season: 3, PlayerID: 259239},
	}, 0)

	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWanguShenxuMainInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWanguShenxuMainInfo, panel))
		case protocol.ActionIDWanguShenxuWorship:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDWanguShenxuWorship, []byte{protocol.WanguShenxuResultFailed}))
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "wangu_shenxu")

	err := NewWanguShenxuRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("全部膜拜失败应返回 CooldownError, 实际: %v", err)
	}
	if n := scheduler.SilentDropCount(sess, "wangu_shenxu"); n != 1 {
		t.Fatalf("沉默计数 = %d, want 1", n)
	}
}
