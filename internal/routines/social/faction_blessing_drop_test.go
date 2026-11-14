package social

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestFactionBlessingRoutine_BothSilentDropCooldownNotPermanent(t *testing.T) {
	sess := newSocialMockSession(t, func(net.Conn, *protocol.Packet) {
		// 神像面板与白檀香探测均模拟静默丢包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewFactionBlessingRoutine(DefaultFactionBlessingConfig()).Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("双丢包应返回 CooldownError 而非永久禁用, 实际: %v", err)
	}
	if sess.IsFactionFeatureUnavailable() {
		t.Fatal("瞬断不应标记帮派模块永久不可用")
	}
}

func TestFactionLuckyWheelRoutine_PanelSilentDropCooldownNotPermanent(t *testing.T) {
	sess := newSocialMockSession(t, func(net.Conn, *protocol.Packet) {
		// 帮派转盘面板模拟静默丢包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewFactionLuckyWheelRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("转盘面板无回包应返回 CooldownError, 实际: %v", err)
	}
	if sess.IsFactionFeatureUnavailable() {
		t.Fatal("瞬断不应标记帮派模块永久不可用")
	}
}

func TestFactionLuckyWheelRoutine_NoFactionProbeMarksUnavailable(t *testing.T) {
	oldTimeout := factionLuckyWheelOpenTimeout
	factionLuckyWheelOpenTimeout = 150 * time.Millisecond
	defer func() { factionLuckyWheelOpenTimeout = oldTimeout }()

	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		// 转盘面板静默丢弃；免费白檀香探测返回“无此帮派成员”。
		if pkt.ActionID == protocol.ActionIDFactionIncense {
			_ = protocol.WritePacket(conn,
				protocol.NewPacket(pkt.ActionID, []byte{protocol.FactionIncenseResultMemberNotExist}))
		}
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := NewFactionLuckyWheelRoutine().Execute(ctx, sess, nil); err != nil {
		t.Fatalf("确认无帮派应静默跳过, 实际: %v", err)
	}
	if !sess.IsFactionFeatureUnavailable() {
		t.Fatal("上香探测返回 MEMBER_NOT_EXIST 时应标记帮派模块不可用")
	}
}

func TestFactionLuckyWheelRoutine_ProbeSuccessThenSpins(t *testing.T) {
	var startCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFactionIncense:
			_ = protocol.WritePacket(conn,
				protocol.NewPacket(pkt.ActionID, []byte{protocol.FactionIncenseResultSuccess}))
		case protocol.ActionIDLuckyWheelOpen:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.LuckyWheelResultOK)
			w.WriteInt32(0)
			w.WriteInt32(1) // 剩余 1 枚青龙令
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDLuckyWheelStart:
			n := startCalls.Add(1)
			w := protocol.NewWriter()
			w.WriteUint8(protocol.LuckyWheelResultSuccess)
			w.WriteInt8(0)
			w.WriteInt32(1 - n) // 第一轮后归零
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})
	defer sess.Close()

	if err := NewFactionLuckyWheelRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("帮派转盘执行失败: %v", err)
	}
	if got := startCalls.Load(); got != 1 {
		t.Fatalf("青龙令 1 枚应转动 1 次, 实际 %d 次", got)
	}
	if !sess.IsFactionFeatureAvailable() {
		t.Fatal("上香探测成功应标记帮派上下文可用")
	}
}
