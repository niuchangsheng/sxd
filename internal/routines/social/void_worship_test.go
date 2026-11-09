// Package social 提供仙界虚空膜拜自动化测试。
//
// @author Ateng
// @since 2026-11-08
package social

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestVoidWorshipRoutine_OpenPanelThenLikeUnlikedMember(t *testing.T) {
	var likeCalls, godRankCalls atomic.Int32
	var likedUID atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStVoidOpen:
			_ = protocol.WritePacket(conn, protocol.BuildStVoidPanelResultPacket(protocol.StVoidPanel{
				Result: protocol.StVoidResultSuccess,
				StName: "st-void",
				StUID:  100,
				Members: []protocol.StVoidMember{
					{StID: 1, StUID: 200, MatchRank: 1, Nickname: "已膜拜"},
					{StID: 2, StUID: 300, MatchRank: 2, Nickname: "待膜拜"},
				},
				LikedUIDs: []int32{200},
			}))
		case protocol.ActionIDStVoidLike:
			likeCalls.Add(1)
			if len(pkt.Payload) >= 4 {
				likedUID.Store(int32(binary.BigEndian.Uint32(pkt.Payload[:4])))
			}
			_ = protocol.WritePacket(conn, protocol.BuildStVoidLikeResultPacket(protocol.StVoidLikeResult{
				Result: protocol.StVoidResultSuccess,
				Awards: []protocol.StVoidAward{{ItemID: 2016, Num: 1}},
			}))
		case protocol.ActionIDGodRankWorship:
			godRankCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewVoidWorshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("void worship: %v", err)
	}
	if got := likeCalls.Load(); got != 1 {
		t.Fatalf("应对 1 位未膜拜玩家点赞, 实际 %d 次", got)
	}
	if got := likedUID.Load(); got != 300 {
		t.Fatalf("应跳过已膜拜玩家 200, 实际点赞 %d", got)
	}
	if got := godRankCalls.Load(); got != 0 {
		t.Fatalf("652 成功后不应回退 500, 实际 %d 次", got)
	}
}

func TestVoidWorshipRoutine_FallsBackToGodRankWhenPanelFails(t *testing.T) {
	var godRankCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStVoidOpen:
			_ = protocol.WritePacket(conn, protocol.BuildStVoidPanelResultPacket(protocol.StVoidPanel{
				Result: protocol.StVoidResultFailed,
			}))
		case protocol.ActionIDStVoidLike:
			t.Error("面板失败时不得调用 like")
		case protocol.ActionIDGodRankWorship:
			godRankCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipActionCodePacket(
				protocol.ActionIDGodRankWorship, protocol.GodRankResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewVoidWorshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("void worship: %v", err)
	}
	if got := godRankCalls.Load(); got != 1 {
		t.Fatalf("652 面板失败时应回退 500/1, 实际 %d 次", got)
	}
}

func TestVoidWorshipRoutine_BothPanelsSilentDropCooldown(t *testing.T) {
	sess := newSocialMockSession(t, func(net.Conn, *protocol.Packet) {
		// 652 与 500/1 均模拟服务端静默丢包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewVoidWorshipRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("两条膜拜链路无回包应返回 CooldownError, 实际: %v", err)
	}
}
