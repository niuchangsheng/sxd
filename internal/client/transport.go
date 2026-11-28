// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件将单条 TCP 连接的传输原语抽象为独立的 connTransport 实例：
// 每代连接独占自己的 net.Conn、串行写锁、16KB 读缓冲、pendingCalls 等待表与连接级 ctx；
// 连接断开后整对象销毁，从结构上彻底隔离新旧连接的状态与回包。
package client

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"sxd-pie-ng/internal/protocol"
)

const (
	transportReadBufSize  = 16 * 1024
	transportWriteTimeout = 10 * time.Second
)

// connTransport 封装单代 TCP 连接的读写、挂起 RPC 匹配与生命周期控制。
type connTransport struct {
	roleID string

	conn    net.Conn
	writeMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc

	pendingMu sync.RWMutex
	pending   map[uint32][]chan *protocol.Packet
	drained   bool

	readErrCh chan error
	readDone  chan struct{}
}

func newConnTransport(parentCtx context.Context, conn net.Conn, roleID string) *connTransport {
	ctx, cancel := context.WithCancel(parentCtx)
	return &connTransport{
		roleID:    roleID,
		conn:      conn,
		ctx:       ctx,
		cancel:    cancel,
		pending:   make(map[uint32][]chan *protocol.Packet),
		readErrCh: make(chan error, 1),
		readDone:  make(chan struct{}),
	}
}

func (t *connTransport) startReadLoop(onPacket func(*protocol.Packet)) {
	t.writeMu.Lock()
	conn := t.conn
	t.writeMu.Unlock()

	go func() {
		defer func() {
			t.cancel()
			t.drainPending()
			close(t.readDone)
		}()

		if conn == nil {
			t.readErrCh <- io.EOF
			return
		}
		br := bufio.NewReaderSize(conn, transportReadBufSize)
		for {
			pkt, err := protocol.ReadPacket(br)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					t.readErrCh <- io.EOF
				} else {
					t.readErrCh <- err
				}
				return
			}

			hexDump := hex.EncodeToString(pkt.Payload)
			if len(hexDump) > 64 {
				hexDump = hexDump[:64] + "..."
			}
			slog.Debug("收到网关回包",
				"role_id", t.roleID,
				"action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", pkt.ActionID, pkt.ActionID>>16, pkt.ActionID&0xFFFF),
				"payload_len", len(pkt.Payload),
				"hex", hexDump,
			)
			onPacket(pkt)
		}
	}()
}

// registerPending 在本代连接上登记一个等待特定 ActionID 回包的通道。
// 若连接已排空关闭，返回 ok=false。
func (t *connTransport) registerPending(actionID uint32) (chan *protocol.Packet, func(), bool) {
	ch := make(chan *protocol.Packet, 1)
	t.pendingMu.Lock()
	if t.drained {
		t.pendingMu.Unlock()
		return nil, nil, false
	}
	t.pending[actionID] = append(t.pending[actionID], ch)
	t.pendingMu.Unlock()

	cleanup := func() {
		t.pendingMu.Lock()
		calls := t.pending[actionID]
		for i, c := range calls {
			if c == ch {
				t.pending[actionID] = append(calls[:i], calls[i+1:]...)
				break
			}
		}
		t.pendingMu.Unlock()
	}
	return ch, cleanup, true
}

// deliverPending 将收到的回包投递给本代连接上首个等待该 ActionID 的调用方。
// 返回 true 表示成功匹配到了挂起调用。
func (t *connTransport) deliverPending(pkt *protocol.Packet) bool {
	t.pendingMu.Lock()
	calls, ok := t.pending[pkt.ActionID]
	if !ok || len(calls) == 0 {
		t.pendingMu.Unlock()
		return false
	}
	ch := calls[0]
	t.pending[pkt.ActionID] = calls[1:]
	t.pendingMu.Unlock()

	select {
	case ch <- pkt:
	default:
	}
	return true
}

// hasPending 返回本代连接当前是否存在等待回包的在途业务请求。
func (t *connTransport) hasPending() bool {
	t.pendingMu.RLock()
	defer t.pendingMu.RUnlock()
	for _, calls := range t.pending {
		if len(calls) > 0 {
			return true
		}
	}
	return false
}

// drainPending 排空并关闭本代连接的全部挂起等待通道，幂等安全。
func (t *connTransport) drainPending() {
	t.pendingMu.Lock()
	if t.drained {
		t.pendingMu.Unlock()
		return
	}
	t.drained = true
	pending := t.pending
	t.pending = nil
	t.pendingMu.Unlock()

	for _, calls := range pending {
		for _, ch := range calls {
			close(ch)
		}
	}
}

// writeLocked 在独占写锁与写超时保护下向底层 TCP 连接写入数据。
// 若写入失败或超时，立即关闭连接并取消本代 ctx，防止 TCP 帧边界错乱。
func (t *connTransport) writeLocked(fn func(net.Conn) error) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if t.conn == nil {
		return ErrNotConnected
	}
	_ = t.conn.SetWriteDeadline(time.Now().Add(transportWriteTimeout))
	if err := fn(t.conn); err != nil {
		_ = t.conn.Close()
		t.conn = nil
		t.cancel()
		return fmt.Errorf("%w: %w", ErrConnectionLost, err)
	}
	return nil
}

// close 关闭底层连接、取消本代 ctx 并排空所有等待通道（幂等安全）。
func (t *connTransport) close() {
	t.writeMu.Lock()
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
	t.writeMu.Unlock()

	t.cancel()
	t.drainPending()
}

func (t *connTransport) waitReadDone() {
	<-t.readDone
}
