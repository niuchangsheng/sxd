// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// @author Ateng
// @since 2026-10-08
package client

// SessionState 代表角色会话的当前生命周期阶段状态。
type SessionState int

const (
	// StateDisconnected 未建立连接
	StateDisconnected SessionState = iota
	// StateConnecting 正在建立底层 TCP Socket 连接
	StateConnecting
	// StateAuthenticating 已连通 Socket，正在进行协议级身份握手认证
	StateAuthenticating
	// StateActive 认证通过，长连接激活就绪，心跳保活运行中
	StateActive
	// StateReconnecting 网络异常中断，正在尝试平滑重连
	StateReconnecting
	// StateClosed 会话主动销毁关闭，资源已全部回收
	StateClosed
)

func (s SessionState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StateAuthenticating:
		return "Authenticating"
	case StateActive:
		return "Active"
	case StateReconnecting:
		return "Reconnecting"
	case StateClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// IsRecovering 表示会话状态机正在自行恢复连接（建连/认证/重连中）。
//
// 这类状态下连接暂不可用但正在自愈，调用方应等待状态迁移而不是把它当成
// 「未连接」业务失败；只有 Disconnected/Closed 才应立即报错。
func (s SessionState) IsRecovering() bool {
	switch s {
	case StateConnecting, StateAuthenticating, StateReconnecting:
		return true
	default:
		return false
	}
}
