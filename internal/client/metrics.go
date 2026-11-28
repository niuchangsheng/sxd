// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件提供会话连接健康度指标快照 (SessionMetrics)，用于控制台观测与断线诊断。
package client

import "time"

// SessionMetrics 描述单条角色会话的运行时连接健康指标快照。
type SessionMetrics struct {
	ConnGeneration        uint64 `json:"conn_generation"`
	ConnAgeSec            int64  `json:"conn_age_sec"`
	LastRxAgeSec          int64  `json:"last_rx_age_sec"`
	PassiveDisconnects    uint64 `json:"passive_disconnects"`
	IntentionalReconnects uint64 `json:"intentional_reconnects"`
	HandshakeFailures     uint64 `json:"handshake_failures"`
	StaleProbeTriggers    uint64 `json:"stale_probe_triggers"`
	CrossSessionActive    bool   `json:"cross_session_active"`
	LastDisconnectReason  string `json:"last_disconnect_reason,omitempty"`
}

// Metrics 返回当前会话的连接健康度指标快照（线程安全）。
func (s *RoleSession) Metrics() SessionMetrics {
	now := time.Now()
	var connAgeSec int64
	if since := s.ConnectionSince(); !since.IsZero() {
		connAgeSec = int64(now.Sub(since).Seconds())
		if connAgeSec < 0 {
			connAgeSec = 0
		}
	}
	var lastRxAgeSec int64
	if lastRx := s.LastRxTime(); !lastRx.IsZero() {
		lastRxAgeSec = int64(now.Sub(lastRx).Seconds())
		if lastRxAgeSec < 0 {
			lastRxAgeSec = 0
		}
	}

	s.stateMu.RLock()
	lastReason := s.lastDisconnectReason
	s.stateMu.RUnlock()

	return SessionMetrics{
		ConnGeneration:        s.connGeneration.Load(),
		ConnAgeSec:            connAgeSec,
		LastRxAgeSec:          lastRxAgeSec,
		PassiveDisconnects:    s.passiveDisconnects.Load(),
		IntentionalReconnects: s.intentionalReconnects.Load(),
		HandshakeFailures:     s.handshakeFailures.Load(),
		StaleProbeTriggers:    s.staleProbeTriggers.Load(),
		CrossSessionActive:    s.HasCrossSession(),
		LastDisconnectReason:  lastReason,
	}
}

func (s *RoleSession) recordDisconnectReason(err error) {
	if err == nil {
		return
	}
	s.stateMu.Lock()
	s.lastDisconnectReason = err.Error()
	s.stateMu.Unlock()
}
