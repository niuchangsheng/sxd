// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
package social

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// wheelMockOptions 帮派转盘 mock 的可变行为。
type wheelMockOptions struct {
	// dropSecondSpin 第二次抽奖不回包，模拟服务端静默丢弃。
	dropSecondSpin bool
	// rejectSell 出售回包返回失败码 1。
	rejectSell bool
}

// wheelSellMock 搭建帮派转盘 mock：两次抽奖 (1 品丹药制作卷 382 + 非卷轴 1002)，
// 背包中同时存在 382/383/1002，用于验证卖出背包内所有 1-5 品丹药卷轴。
func wheelSellMock(t *testing.T, opt wheelMockOptions) (*client.RoleSession, func() []int16, func() int32) {
	t.Helper()
	serverConn, clientConn := net.Pipe()

	var mu sync.Mutex
	soldGrids := make([]int16, 0, 2)
	var packQueries int32
	remain := 2

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDFactionIncense:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDFactionIncense, []byte{protocol.FactionIncenseResultSuccess}))
			case protocol.ActionIDLuckyWheelOpen:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.LuckyWheelResultOK)
				w.WriteInt32(0)
				w.WriteInt32(int32(remain))
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDLuckyWheelOpen, w.Bytes()))
			case protocol.ActionIDLuckyWheelStart:
				if opt.dropSecondSpin && remain == 1 {
					continue // 第二次抽奖静默丢弃
				}
				remain--
				w := protocol.NewWriter()
				w.WriteUint8(protocol.LuckyWheelResultSuccess)
				w.WriteInt8(0)
				w.WriteInt32(int32(remain))
				if remain == 1 {
					w.WriteInt32(382) // 一品武力丹制作卷
				} else {
					w.WriteInt32(1002) // 非卷轴奖励
				}
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDLuckyWheelStart, w.Bytes()))
			case protocol.ActionIDItemGetPackList:
				mu.Lock()
				packQueries++
				mu.Unlock()
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.BigEndian, int16(30))
				_ = binary.Write(buf, binary.BigEndian, int32(27))
				_ = binary.Write(buf, binary.BigEndian, uint16(3))
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{PlayerItemID: 1, ItemID: 382, GridID: 7, Field5: 1})
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{PlayerItemID: 2, ItemID: 383, GridID: 8, Field5: 1})
				_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{PlayerItemID: 3, ItemID: 1002, GridID: 9, Field5: 1})
				_ = binary.Write(buf, binary.BigEndian, int16(10))
				_ = binary.Write(buf, binary.BigEndian, uint16(0))
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buf.Bytes()))
			case protocol.ActionIDItemSell:
				grid := int16(binary.BigEndian.Uint16(pkt.Payload[:2]))
				mu.Lock()
				soldGrids = append(soldGrids, grid)
				mu.Unlock()
				result := byte(0)
				if opt.rejectSell {
					result = 1
				}
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDItemSell, []byte{result}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "s0-role_wheel_sell",
		RoleName:   "转盘出售测试",
		ServerAddr: "wheel-sell-pipe",
		Dialer:     func(ctx context.Context, network, addr string) (net.Conn, error) { return clientConn, nil },
		Authenticator: func(*client.RoleSession) error {
			return nil
		},
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	t.Cleanup(func() {
		sess.Close()
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", sess.State())
	}
	return sess,
		func() []int16 {
			mu.Lock()
			defer mu.Unlock()
			return append([]int16(nil), soldGrids...)
		},
		func() int32 {
			mu.Lock()
			defer mu.Unlock()
			return packQueries
		}
}

// TestFactionLuckyWheelRoutine_SellsLowPillScrolls 验证转盘结束后
// 在 s0 卖出背包内所有 1-5 品丹药制作卷 (382/383)。
func TestFactionLuckyWheelRoutine_SellsLowPillScrolls(t *testing.T) {
	sess, sold, packQueries := wheelSellMock(t, wheelMockOptions{})

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := NewFactionLuckyWheelRoutine().Execute(execCtx, sess, nil); err != nil {
		t.Fatalf("帮派转盘执行失败: %v", err)
	}

	if got := packQueries(); got != 1 {
		t.Fatalf("应查询背包 1 次, 实际 %d 次", got)
	}
	grids := sold()
	if len(grids) != 2 || grids[0] != 7 || grids[1] != 8 {
		t.Fatalf("应卖出格位 7/8 (道具 382/383), 实际 %v", grids)
	}
}

// TestFactionLuckyWheelRoutine_SellSwitchOff 验证关闭开关后不查询背包也不卖出。
func TestFactionLuckyWheelRoutine_SellSwitchOff(t *testing.T) {
	sess, sold, packQueries := wheelSellMock(t, wheelMockOptions{})

	cfg := DefaultFactionLuckyWheelConfig()
	cfg.SellLowPillScrolls = false

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := NewFactionLuckyWheelRoutine(cfg).Execute(execCtx, sess, nil); err != nil {
		t.Fatalf("帮派转盘执行失败: %v", err)
	}

	if got := packQueries(); got != 0 {
		t.Fatalf("关闭开关后不应查询背包, 实际 %d 次", got)
	}
	if grids := sold(); len(grids) != 0 {
		t.Fatalf("关闭开关后不应卖出, 实际 %v", grids)
	}
}

// TestFactionLuckyWheelRoutine_SellAfterSpinDrop 验证抽到低阶卷轴后，
// 后续抽奖被静默丢弃仍会卖出已获得的卷轴。
func TestFactionLuckyWheelRoutine_SellAfterSpinDrop(t *testing.T) {
	oldTimeout := factionWheelStartTimeout
	factionWheelStartTimeout = 200 * time.Millisecond
	t.Cleanup(func() { factionWheelStartTimeout = oldTimeout })

	sess, sold, packQueries := wheelSellMock(t, wheelMockOptions{dropSecondSpin: true})

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := NewFactionLuckyWheelRoutine().Execute(execCtx, sess, nil); err != nil {
		t.Fatalf("帮派转盘执行失败: %v", err)
	}

	if got := packQueries(); got != 1 {
		t.Fatalf("抽奖中断后仍应查询背包 1 次, 实际 %d 次", got)
	}
	grids := sold()
	if len(grids) != 2 || grids[0] != 7 || grids[1] != 8 {
		t.Fatalf("抽奖中断后仍应卖出格位 7/8, 实际 %v", grids)
	}
}

// TestFactionLuckyWheelRoutine_SellRejected 验证出售被服务端拒绝时仅记录日志，
// 例程正常结束。
func TestFactionLuckyWheelRoutine_SellRejected(t *testing.T) {
	sess, sold, _ := wheelSellMock(t, wheelMockOptions{rejectSell: true})

	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := NewFactionLuckyWheelRoutine().Execute(execCtx, sess, nil); err != nil {
		t.Fatalf("出售被拒不应导致例程失败: %v", err)
	}
	if grids := sold(); len(grids) != 2 || grids[0] != 7 || grids[1] != 8 {
		t.Fatalf("应尝试卖出格位 7/8, 实际 %v", grids)
	}
}
