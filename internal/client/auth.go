// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// @author Ateng
// @since 2026-10-08
package client

import (
	"log/slog"

	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
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
	// 1. 注册角色属性与体力更新动态通知监听器 (Mod_Player_Base 0x00000003)
	s.RegisterHandler(protocol.ActionIDPlayerUpdateData, func(p *protocol.Packet) {
		r := protocol.NewReader(p.Payload)
		for r.Remaining() >= 5 {
			prop, err := r.ReadUint8()
			if err != nil {
				break
			}
			val, err := r.ReadInt32()
			if err != nil {
				break
			}
			s.UpdatePlayerState(func(ps *PlayerState) {
				switch prop {
				case protocol.PlayerPropPower:
					ps.Stamina = int(val)
				case protocol.PlayerPropExtraPower:
					ps.ExtraStamina = int(val)
				case protocol.PlayerPropMaxPower:
					ps.MaxStamina = int(val)
				case protocol.PlayerPropCoins:
					ps.Coins = int64(val)
				case protocol.PlayerPropIngot:
					ps.Ingots = int64(val)
				case protocol.PlayerPropLevel:
					ps.Level = int(val)
				case protocol.PlayerPropExperience:
					ps.Experience = int64(val)
					slog.Info("角色总经验同步更新", "role_id", s.RoleID(), "experience", val)
				case protocol.PlayerPropVIPLevel:
					ps.VIP = int(val)
				case protocol.PlayerPropPackEmptyNum:
					ps.BagCapacity = int(val)
				}
			})
		}
	})

	// 2. 注册扫荡回包全局监听更新
	s.RegisterHandler(protocol.ActionMissionSweep, func(p *protocol.Packet) {
		res, err := protocol.ParseSweepResult(p.Payload)
		if err == nil && res.Success {
			s.AddRewards(res.GainExp, res.GainCoins)
		}
	})

	// 3. 执行严密的五阶段登录认证握手与城镇场景/全量资产初始化流水线
	return DefaultGatewayAuthenticator(s)
}

