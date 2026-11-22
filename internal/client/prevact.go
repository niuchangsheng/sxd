// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件集中管理单条 TCP 连接上的 PrevAct 业务动作尾链状态机与收包诊断缓冲，
// 消除原先分散在 RoleSession 中的多套尾链锚点逻辑。
package client

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"sxd-pie-ng/internal/protocol"
)

// PrevActChain 管理单条网关连接内的业务动作尾链 (PrevAct) 状态与诊断环形缓冲。
//
// 不变量：
//   - 心跳动作 (heartbeatActionID，默认 0x0000004B) 不参与业务尾链，不推进
//     lastBusiness / lastAck，也不作为业务锚点回退值；
//   - 带业务尾链的请求超时（服务端静默丢弃）后通过 MarkDirty 标记链失步，
//     下一次业务请求自动回退到服务端最后确认的 lastAck，避免连环污染；
//   - 收到任意非心跳业务回包后通过 RecordAck 更新 lastAck 并清除失步标记；
//   - 建立新连接时调用 Reset 清空旧连接的全部锚点状态。
type PrevActChain struct {
	heartbeatActionID uint32

	lastSent     atomic.Uint32
	lastBusiness atomic.Uint32
	lastAck      atomic.Uint32
	lastRx       atomic.Uint32
	chainDirty   atomic.Bool

	recentMu sync.Mutex
	recentRx [32]uint32
	recentN  int
}

// NewPrevActChain 构造新的 PrevAct 尾链状态机。
func NewPrevActChain(heartbeatActionID uint32) *PrevActChain {
	if heartbeatActionID == 0 {
		heartbeatActionID = protocol.ActionIDPlayerHeartbeat
	}
	return &PrevActChain{
		heartbeatActionID: heartbeatActionID,
	}
}

// Reset 在建立新 TCP 连接时清空旧连接的尾链锚点与失步状态。
func (c *PrevActChain) Reset() {
	c.lastSent.Store(0)
	c.lastBusiness.Store(0)
	c.lastRx.Store(0)
	c.lastAck.Store(0)
	c.chainDirty.Store(false)
}

// RecordSent 记录已成功写入线路的封包动作号。心跳不推进业务锚点。
func (c *PrevActChain) RecordSent(actionID uint32) {
	c.lastSent.Store(actionID)
	if actionID != c.heartbeatActionID {
		c.lastBusiness.Store(actionID)
	}
}

// RecordRx 记录从网关读取到的任意封包动作号（含推送、回包、心跳回包），供诊断与重同步参考。
func (c *PrevActChain) RecordRx(actionID uint32) {
	c.lastRx.Store(actionID)
	c.recentMu.Lock()
	c.recentRx[c.recentN%len(c.recentRx)] = actionID
	c.recentN++
	c.recentMu.Unlock()
}

// RecordAck 记录已匹配到等待者 (pendingCalls) 的业务回包动作号并解除链失步。
func (c *PrevActChain) RecordAck(actionID uint32) {
	if actionID == c.heartbeatActionID {
		return
	}
	c.lastAck.Store(actionID)
	c.chainDirty.Store(false)
}

// MarkDirty 标记业务尾链已失步（通常发生在带参业务请求超时/被服务端静默丢弃后）。
func (c *PrevActChain) MarkDirty() {
	c.chainDirty.Store(true)
}

// IsDirty 返回当前业务尾链是否处于失步状态。
func (c *PrevActChain) IsDirty() bool {
	return c.chainDirty.Load()
}

// SetManualLastSent 手动设置最后发送动作号（兼容探针/测试；0 表示清空自动尾链锚点）。
func (c *PrevActChain) SetManualLastSent(actionID uint32) {
	c.lastSent.Store(actionID)
	if actionID == 0 {
		c.lastBusiness.Store(0)
		return
	}
	if actionID != c.heartbeatActionID {
		c.lastBusiness.Store(actionID)
	}
}

// LastSent 返回最后发送的动作号（含心跳）。
func (c *PrevActChain) LastSent() uint32 {
	return c.lastSent.Load()
}

// LastBusiness 返回最后发送的非心跳业务动作号。
func (c *PrevActChain) LastBusiness() uint32 {
	return c.lastBusiness.Load()
}

// LastAck 返回服务端最后确认的非心跳业务动作号。
func (c *PrevActChain) LastAck() uint32 {
	return c.lastAck.Load()
}

// LastRx 返回最后收到的网关包动作号（含推送与心跳回包）。
func (c *PrevActChain) LastRx() uint32 {
	return c.lastRx.Load()
}

// RecentRx 返回最近收到的最多 32 个包动作号（按时间先后，最新在末尾）。
func (c *PrevActChain) RecentRx() []uint32 {
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	n := c.recentN
	if n > len(c.recentRx) {
		n = len(c.recentRx)
	}
	out := make([]uint32, 0, n)
	start := c.recentN - n
	for i := start; i < c.recentN; i++ {
		out = append(out, c.recentRx[i%len(c.recentRx)])
	}
	return out
}

// BusinessAnchor 返回带业务尾链请求应写入的 PrevAct 锚点：
// 链失步且存在已确认业务动作时回退到 lastAck；否则取 lastBusiness（无则回退到非心跳 lastSent）。
func (c *PrevActChain) BusinessAnchor() uint32 {
	if c.chainDirty.Load() {
		if ack := c.lastAck.Load(); ack != 0 && ack != c.heartbeatActionID {
			return ack
		}
	}
	if anchor := c.lastBusiness.Load(); anchor != 0 {
		return anchor
	}
	if anchor := c.lastSent.Load(); anchor != c.heartbeatActionID {
		return anchor
	}
	return 0
}

// SnapshotResyncFallback 在首次发送前快照重同步兜底锚点，防止重连后尚无收包时退回旧连接锚点。
func (c *PrevActChain) SnapshotResyncFallback() uint32 {
	if rx := c.lastRx.Load(); rx != 0 && rx != c.heartbeatActionID {
		return rx
	}
	if biz := c.lastBusiness.Load(); biz != 0 {
		return biz
	}
	if sent := c.lastSent.Load(); sent != 0 && sent != c.heartbeatActionID {
		return sent
	}
	return 0
}

// ResyncAnchor 计算 PrevAct 重同步重试时的显式锚点。
// 优先使用重试时刻最新到达的非心跳网关包；若期间无有效收包则使用首次发送前快照的兜底锚点。
func (c *PrevActChain) ResyncAnchor(fallback uint32) uint32 {
	if rx := c.lastRx.Load(); rx != 0 && rx != c.heartbeatActionID {
		return rx
	}
	return fallback
}

// UsesBusinessChain 判断封包在发送时是否受业务尾链约束（超时时应标记链失步）。
func (c *PrevActChain) UsesBusinessChain(p *protocol.Packet) bool {
	if p == nil {
		return false
	}
	switch p.EffectivePrevActMode() {
	case protocol.PrevActExplicit:
		return false
	case protocol.PrevActAutoBusiness:
		return true
	default:
		return isLegacyImplicitChainPacket(p, c.heartbeatActionID)
	}
}

// StampPayload 在发送前按封包的 PrevActMode 生成带最终尾链锚点的封包副本（不修改调用方原包）。
func (c *PrevActChain) StampPayload(p *protocol.Packet) *protocol.Packet {
	if p == nil {
		return nil
	}
	mode := p.EffectivePrevActMode()
	// 显式尾链严禁覆盖（包括重同步重试包与显式指定锚点的旧模块包）。
	if mode == protocol.PrevActExplicit {
		return p
	}

	anchor := c.BusinessAnchor()
	if anchor == 0 {
		return p
	}

	// 1. 声明了 PrevActAutoBusiness（含兼容旧标记 HasPrevAct / PrevActFromLastSent）
	if mode == protocol.PrevActAutoBusiness {
		if len(p.Payload) == 0 {
			buf := make([]byte, 4)
			binary.BigEndian.PutUint32(buf, anchor)
			return &protocol.Packet{
				ActionID:    p.ActionID,
				Payload:     buf,
				PrevActMode: protocol.PrevActAutoBusiness,
				HasPrevAct:  true,
			}
		}
		if len(p.Payload) >= 4 {
			cloned := make([]byte, len(p.Payload))
			copy(cloned, p.Payload)
			binary.BigEndian.PutUint32(cloned[len(cloned)-4:], anchor)
			return &protocol.Packet{
				ActionID:            p.ActionID,
				Payload:             cloned,
				PrevActMode:         protocol.PrevActAutoBusiness,
				HasPrevAct:          p.HasPrevAct,
				PrevActFromLastSent: p.PrevActFromLastSent,
			}
		}
		return p
	}

	// 2. 兼容存量未显式声明 PrevActMode 的历史模块封包
	if p.ActionID == protocol.ActionIDItemGetPackList && len(p.Payload) == 0 {
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, anchor)
		return &protocol.Packet{
			ActionID:    p.ActionID,
			Payload:     buf,
			PrevActMode: protocol.PrevActAutoBusiness,
			HasPrevAct:  true,
		}
	}
	if isLegacyImplicitChainPacket(p, c.heartbeatActionID) && len(p.Payload) >= 4 {
		cloned := make([]byte, len(p.Payload))
		copy(cloned, p.Payload)
		binary.BigEndian.PutUint32(cloned[len(cloned)-4:], anchor)
		return &protocol.Packet{
			ActionID:    p.ActionID,
			Payload:     cloned,
			PrevActMode: protocol.PrevActAutoBusiness,
			HasPrevAct:  true,
		}
	}
	return p
}

// isLegacyImplicitChainPacket 兼容早期未在构造器中声明 PrevActMode 的 Mod 3/4/32 与背包查询封包。
func isLegacyImplicitChainPacket(p *protocol.Packet, heartbeatActionID uint32) bool {
	if p.ActionID == protocol.ActionIDItemGetPackList {
		return true
	}
	if p.ActionID == heartbeatActionID {
		return len(p.Payload) >= 4
	}
	mod := p.ActionID >> 16
	return mod == 3 || mod == 4 || mod == 32
}
