package social

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// TestJieYuanBoxRoutine_SendsBatchOpenRequest 验证官方抓包链路：
// 232/30 列表 -> 528/1 上下文握手 [0x0704, PrevAct=232/30]
// -> 232/34 开箱 [item, autoMerge, PrevAct=528/1] -> 232/30 库存核对。
//
// 2026-11-22 对照 /tmp/jieyuan2.pcapng：官方客户端 232/30 之后紧接 528/1，
// 中间没有任何 2/41。本服在两者之间插入 2/41 会导致 528/1 前 EOF，在列表
// 之前插入也会让 528/1 静默丢弃（probe 不带 2/41 时同一账号开箱成功），
// 因此开箱流程完全不发 2/41；背包空格只在 232/34 失败后才补查。
func TestJieYuanBoxRoutine_SendsBatchOpenRequest(t *testing.T) {
	var listCalls, ctxCalls, openCalls atomic.Int32
	var lastAction uint32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		prev := lastAction
		lastAction = pkt.ActionID
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			n := listCalls.Add(1)
			w := protocol.NewWriter()
			if n == 1 {
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			} else {
				w.WriteUint16(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDItemGetPackList:
			// 容量 60、0 件、0 空格位：保证开箱前背包检查可通过。
			w := protocol.NewWriter()
			w.WriteInt16(60) // 主背包总格位容量 (int16)
			w.WriteInt32(0)  // 已用空格数量
			w.WriteUint16(0) // 物品条数
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			ctxCalls.Add(1)
			if len(pkt.Payload) != 6 {
				t.Errorf("528/1 载荷长度 = %d, want 6 (0704+PrevAct)", len(pkt.Payload))
			}
			if got := hex.EncodeToString(pkt.Payload[:2]); got != "0704" {
				t.Errorf("528/1 设置项 = %s, want 0704", got)
			}
			if got := binary.BigEndian.Uint32(pkt.Payload[2:6]); got != prev {
				t.Errorf("528/1 PrevAct = 0x%08X, want 上一条发送动作 0x%08X（背包查询后不得固定 232/30）", got, prev)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanOpenContextResultOK}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
			if len(pkt.Payload) != 12 {
				t.Errorf("开箱载荷长度 = %d, want 12", len(pkt.Payload))
			} else {
				if q := int32(binary.BigEndian.Uint32(pkt.Payload[4:8])); q != protocol.JieYuanBoxAutoMergeQuality {
					t.Errorf("开箱第二参数 = %d, want 自动合成档位 %d", q, protocol.JieYuanBoxAutoMergeQuality)
				}
				if got := binary.BigEndian.Uint32(pkt.Payload[8:12]); got != prev {
					t.Errorf("开箱 PrevAct = 0x%08X, want 上一条发送动作 0x%08X", got, prev)
				}
			}
			// 官方抓包回包解压后首字节为成功码 5。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanBoxOpenResultSuccess, 0x00, 0x00}))
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	if ctxCalls.Load() != 1 {
		t.Fatalf("每次开箱前应发送 1 次 528/1 上下文握手, 实际 %d 次", ctxCalls.Load())
	}
	if openCalls.Load() != 1 {
		t.Fatalf("应合并为 1 次批量开箱, 实际 %d 次", openCalls.Load())
	}
}

// TestJieYuanBoxRoutine_StopsOnOpenFailureCode 验证开箱失败码不会被当作成功继续下一批。
func TestJieYuanBoxRoutine_StopsOnOpenFailureCode(t *testing.T) {
	var listCalls, openCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			n := listCalls.Add(1)
			w := protocol.NewWriter()
			if n == 1 {
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			} else {
				// 失败码下库存数量不应变化，若实现继续核对就能暴露异常。
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDItemGetPackList:
			w := protocol.NewWriter()
			w.WriteInt16(60) // 主背包总格位容量 (int16)
			w.WriteInt32(0)  // 已用空格数量
			w.WriteUint16(0) // 物品条数
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanOpenContextResultOK}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
			// 非成功结果码（如次数不足/条件不满足）。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x01}))
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	if openCalls.Load() != 1 {
		t.Fatalf("失败码后不得继续开箱, 开箱次数 = %d", openCalls.Load())
	}
	if listCalls.Load() != 1 {
		t.Fatalf("失败码后不应进入库存核对, 列表查询次数 = %d", listCalls.Load())
	}
}

// TestJieYuanBoxRoutine_StopsOnContextReject 验证 528/1 握手被拒绝时不得发送开箱请求。
func TestJieYuanBoxRoutine_StopsOnContextReject(t *testing.T) {
	var openCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(protocol.ItemJieYuanBoxCommon)
			w.WriteInt32(2)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDItemGetPackList:
			w := protocol.NewWriter()
			w.WriteInt16(60) // 主背包总格位容量 (int16)
			w.WriteInt32(0)  // 已用空格数量
			w.WriteUint16(0) // 物品条数
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x01}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil)
	if err == nil {
		t.Fatal("握手被拒绝时应返回冷却错误")
	}
	if openCalls.Load() != 0 {
		t.Fatalf("握手被拒绝后不得发送开箱请求, 实际 %d 次", openCalls.Load())
	}
}

// TestJieYuanBoxRoutine_StillListsWhenFunctionClosed 验证功能列表缺少 140 时
// 不直接跳过，而是继续查询宝箱列表（2026-11-18 用户反馈 s1 仍需开箱）；
// 列表为空时不会发起开箱握手。
func TestJieYuanBoxRoutine_StillListsWhenFunctionClosed(t *testing.T) {
	var listCalls, ctxCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			// 功能 140 未在列表中下发。
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(nil))
		case protocol.ActionIDJieYuanBoxList:
			listCalls.Add(1)
			w := protocol.NewWriter()
			w.WriteUint16(0) // 空背包
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			ctxCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if listCalls.Load() == 0 {
		t.Fatal("功能列表缺少 140 时仍应查询结缘宝箱列表")
	}
	if ctxCalls.Load() != 0 {
		t.Fatalf("空背包不应发起开箱握手, ctx=%d", ctxCalls.Load())
	}
}

// TestJieYuanBoxRoutine_ContextRetryUsesLastRxAnchor 验证 528/1 首次无回包后，
// 重试不能用「上一条发送动作」（此时已是 528/1 自身），而应使用最后收到的
// 232/30 列表回包作为尾链锚点。
func TestJieYuanBoxRoutine_ContextRetryUsesLastRxAnchor(t *testing.T) {
	var ctxCalls atomic.Int32
	var lastResponseAction uint32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			lastResponseAction = pkt.ActionID
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			n := ctxCalls.Load()
			w := protocol.NewWriter()
			if n == 0 {
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			} else {
				w.WriteUint16(0)
			}
			lastResponseAction = pkt.ActionID
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDItemGetPackList:
			w := protocol.NewWriter()
			w.WriteInt16(60)
			w.WriteInt32(0)
			w.WriteUint16(0)
			lastResponseAction = pkt.ActionID
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			n := ctxCalls.Add(1)
			if n == 1 {
				// 首次静默丢弃：不回包，触发重试。
				return
			}
			tail := binary.BigEndian.Uint32(pkt.Payload[2:6])
			if tail != lastResponseAction {
				t.Errorf("528/1 重试尾链 = 0x%08X, want 最后收到动作 0x%08X",
					tail, lastResponseAction)
			}
			lastResponseAction = pkt.ActionID
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.JieYuanOpenContextResultOK}))
		case protocol.ActionIDJieYuanBoxOpen:
			lastResponseAction = pkt.ActionID
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.JieYuanBoxOpenResultSuccess, 0x00, 0x00}))
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	if got := ctxCalls.Load(); got != 2 {
		t.Fatalf("首次 528/1 无回包后应重试一次, ctx calls = %d", got)
	}
}

// TestJieYuanBoxRoutine_ForcesFreshConnectionOnContextDrop 验证 528/1 在旧会话上
// 连续无回包时，例程会强制重建连接并以新连接首个业务请求重试一次。
func TestJieYuanBoxRoutine_ForcesFreshConnectionOnContextDrop(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	var ctxCalls, openCalls atomic.Int32

	handle := func(conn net.Conn, connIndex int) {
		for {
			pkt, err := protocol.ReadPacket(conn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDJieYuanBoxList:
				w := protocol.NewWriter()
				if openCalls.Load() == 0 {
					w.WriteUint16(1)
					w.WriteInt32(protocol.ItemJieYuanBoxCommon)
					w.WriteInt32(2)
				} else {
					w.WriteUint16(0)
				}
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
			case protocol.ActionIDJieYuanBoxOpenContext:
				n := ctxCalls.Add(1)
				if connIndex == 0 {
					// 旧会话上 528/1 始终静默丢弃。
					continue
				}
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
					[]byte{protocol.JieYuanOpenContextResultOK}))
				_ = n
			case protocol.ActionIDJieYuanBoxOpen:
				openCalls.Add(1)
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
					[]byte{protocol.JieYuanBoxOpenResultSuccess, 0x00, 0x00}))
			}
		}
	}

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-jieyuan-fresh",
		RoleName:      "结缘测试",
		ServerAddr:    "mock-pipe",
		Authenticator: func(*client.RoleSession) error { return nil },
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			mu.Lock()
			idx := dials
			dials++
			mu.Unlock()
			serverConn, clientConn := net.Pipe()
			t.Cleanup(func() {
				_ = serverConn.Close()
				_ = clientConn.Close()
			})
			go handle(serverConn, idx)
			return clientConn, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("session not active: %s", sess.State())
	}

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	mu.Lock()
	d := dials
	mu.Unlock()
	if d < 2 {
		t.Fatalf("528/1 丢弃后应强制重建连接, dials=%d", d)
	}
	if openCalls.Load() != 1 {
		t.Fatalf("新连接握手成功后应开箱 1 次, opened=%d", openCalls.Load())
	}
}
