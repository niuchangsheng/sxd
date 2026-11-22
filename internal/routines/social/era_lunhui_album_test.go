package social

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

func TestEraLunHuiAlbumRoutine_ClaimsUnclaimed(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraLunHuiPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDEraLunHuiTaskInfo:
			tab := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
				uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3]))
			tasks := []protocol.EraLunHuiTask{}
			if tab == 1 {
				tasks = []protocol.EraLunHuiTask{
					{ID: 101, State: 10, Claimed: 0},
					{ID: 102, State: 10, Claimed: 1},
					{ID: 103, State: 10, Claimed: 0},
				}
			}
			_ = protocol.WritePacket(conn, protocol.BuildEraLunHuiTasksResultPacket(tasks, int32(tab)))
		case protocol.ActionIDEraLunHuiGetTask:
			n := claimCalls.Add(1)
			result := uint8(0)
			if n == 2 {
				result = 3 // 第二项条件不满足，应继续后续任务
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result}))
		case protocol.ActionIDEraLunHuiRedDotStatus:
			w := protocol.NewWriter().WriteUint8(0).WriteUint16(1)
			w.WriteInt32(1).WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})
	defer sess.Close()

	if err := NewEraLunHuiAlbumRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era lunhui album: %v", err)
	}
	if claimCalls.Load() != 2 {
		t.Fatalf("应只对两个未领取任务发起领取, calls = %d", claimCalls.Load())
	}
}

// TestEraLunHuiAlbumRoutine_TaskListOKClaimDroppedBacksOff 验证任务列表 882/1 正常回包、
// 但领奖 882/2 被静默丢弃时，连续丢包计数不会被列表成功清零：
// 连续 3 轮后挂起到每日重置。
func TestEraLunHuiAlbumRoutine_TaskListOKClaimDroppedBacksOff(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraLunHuiPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDEraLunHuiRedDotStatus:
			w := protocol.NewWriter().WriteUint8(0).WriteUint16(1)
			w.WriteInt32(1).WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraLunHuiTaskInfo:
			_ = protocol.WritePacket(conn, protocol.BuildEraLunHuiTasksResultPacket(
				[]protocol.EraLunHuiTask{{ID: 101, State: 10, Claimed: 0}}, 1))
		case protocol.ActionIDEraLunHuiGetTask:
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "era_lunhui_album")

	routine := NewEraLunHuiAlbumRoutine()
	ctx := kit.WithTimeoutOverride(context.Background(), 150*time.Millisecond)
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = routine.Execute(ctx, sess, nil)
		if lastErr == nil {
			t.Fatalf("第 %d 轮领奖无回包应返回冷却错误", i+1)
		}
	}
	if n := scheduler.SilentDropCount(sess, "era_lunhui_album"); n < 3 {
		t.Fatalf("任务列表成功不应清零领奖丢包计数, got %d", n)
	}
	var cd *scheduler.CooldownError
	if !errors.As(lastErr, &cd) {
		t.Fatalf("达到阈值后应返回 CooldownError, got %v", lastErr)
	}
	// HandleSilentDrop 到阈值后挂起至 min(次日 00:05, 6 小时)。
	exp := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); exp.After(cap) {
		exp = cap
	}
	if d := cd.Until.Sub(exp); d > time.Minute || d < -time.Minute {
		t.Fatalf("熔断唤醒时间 = %s, want %s", cd.Until, exp)
	}
}

func TestEraLunHuiAlbumRoutine_PanelFailedSkips(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraLunHuiPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{3}))
		case protocol.ActionIDEraLunHuiTaskInfo, protocol.ActionIDEraLunHuiGetTask:
			claimCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewEraLunHuiAlbumRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era lunhui album: %v", err)
	}
	if claimCalls.Load() != 0 {
		t.Fatalf("面板失败时不得领取, calls = %d", claimCalls.Load())
	}
}

func TestEraLunHuiAlbumRoutine_NoRedDotSkips(t *testing.T) {
	var taskInfoCalls, claimCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraLunHuiPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDEraLunHuiRedDotStatus:
			w := protocol.NewWriter().WriteUint8(0).WriteUint16(1)
			w.WriteInt32(1).WriteInt32(0) // 无可领取
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraLunHuiTaskInfo:
			taskInfoCalls.Add(1)
		case protocol.ActionIDEraLunHuiGetTask:
			claimCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewEraLunHuiAlbumRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era lunhui album: %v", err)
	}
	if taskInfoCalls.Load() != 0 || claimCalls.Load() != 0 {
		t.Fatalf("无可领取礼包时不得请求任务/领取, task_info=%d claim=%d",
			taskInfoCalls.Load(), claimCalls.Load())
	}
}
