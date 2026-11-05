// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// @author Ateng
// @since 2026-10-08
package client

import (
	"sxd-pie-ng/internal/platform"
)

// ApplyTicket 将双轨平台凭据一键装载至角色会话配置中。
func ApplyTicket(cfg *SessionConfig, ticket *platform.Ticket) {
	if cfg == nil || ticket == nil {
		return
	}
	if ticket.Platform != "" {
		cfg.Platform = ticket.Platform
	}
	if ticket.ServerID != "" {
		cfg.ServerID = ticket.ServerID
	}
	if ticket.GatewayURL != "" {
		cfg.ServerAddr = ticket.GatewayURL
	}
	if ticket.RoleName != "" {
		cfg.Username = ticket.RoleName
	}
	cfg.Code = ticket.MainServer.Code
	cfg.Time = ticket.MainServer.Time
	cfg.Hash = ticket.MainServer.Hash
	cfg.Time1 = ticket.CrossServer.Time1
	cfg.Hash1 = ticket.CrossServer.Hash1
	cfg.Extra = ticket.Extra
}

// DefaultAuthenticator 为角色会话提供标准登录认证握手、全局通知监听与四步场景同步调度。
func DefaultAuthenticator(s *RoleSession) error {
	// 1. 角色属性/体力更新监听器 (Mod_Player_Base 0x00000003) 已由 NewRoleSession 统一注册,
	//    此处重复注册会导致同一条属性变更被应用两次, 故不再注册。

	// 2. 扫荡结算奖励由具体 Routine 在解析业务回包后自行结算。
	//    注意: 0x00230002 属于英雄副本 start_practice (Module 35)，
	//    不能再作为主线扫荡的全局奖励监听挂载，否则会把英雄副本回包误判为扫荡结算。

	// 3. 执行严密的五阶段登录认证握手与城镇场景/全量资产初始化流水线
	return DefaultGatewayAuthenticator(s)
}
