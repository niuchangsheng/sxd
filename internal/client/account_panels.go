// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件封装账号养成进度探测所需的只读面板查询 (法阵/龙珠/血脉/藏经阁/
// 丹魂/圣器/兵法/混沌虚空/幻兽)，供天帝宝库动态选奖评估使用。
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sxd-pie-ng/internal/protocol"
)

func (s *RoleSession) panelCallTimeout() time.Duration {
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		return 300 * time.Millisecond
	}
	return 3 * time.Second
}

func (s *RoleSession) callPanel(ctx context.Context, pkt *protocol.Packet, action uint32) (*protocol.Packet, error) {
	cCtx, cancel := context.WithTimeout(ctx, s.panelCallTimeout())
	defer cancel()
	resp, err := s.Call(cCtx, pkt, action)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("empty panel response")
	}
	return resp, nil
}

// GetItemNum 查询指定道具的实际持有数量 (Module 2, Action 107)。
//
// 注意: get_player_pack_item_list 只返回主背包/仓库实体物品；灵珠、鸿蒙紫气、
// 星魂等系统货币必须通过本接口查询。
func (s *RoleSession) GetItemNum(ctx context.Context, itemID int32) (int32, error) {
	resp, err := s.callPanel(ctx, protocol.BuildGetItemNumPacket(itemID, s.LastActionID()), protocol.ActionIDItemGetTheItemNum)
	if err != nil {
		return 0, err
	}
	return protocol.ParseGetItemNumResponse(resp.Payload)
}

// GetMagicArrayPanel 查询法阵面板 (Module 486, Action 1)。
func (s *RoleSession) GetMagicArrayPanel(ctx context.Context) (*protocol.MagicArrayPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildMagicArrayPanelPacket(s.LastActionID()), 0x01E60001)
	if err != nil {
		return nil, fmt.Errorf("get magic array panel: %w", err)
	}
	return protocol.ParseMagicArrayPanelResponse(resp.Payload)
}

// GetDragonBallPanel 查询龙珠列表 (Module 111, Action 3)。
func (s *RoleSession) GetDragonBallPanel(ctx context.Context) (*protocol.DragonBallPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildDragonBallListPacket(s.LastActionID()), 0x006F0003)
	if err != nil {
		return nil, fmt.Errorf("get dragon ball panel: %w", err)
	}
	return protocol.ParseDragonBallListResponse(resp.Payload)
}

// GetBloodlinePanel 查询血脉面板 (Module 116, Action 0)。
func (s *RoleSession) GetBloodlinePanel(ctx context.Context) (*protocol.BloodlinePanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildBloodlineInfoPacket(s.LastActionID()), 0x00740000)
	if err != nil {
		return nil, fmt.Errorf("get bloodline panel: %w", err)
	}
	return protocol.ParseBloodlineInfoResponse(resp.Payload)
}

// GetLibraryPanel 查询藏经阁面板 (Module 85, Action 1)。
func (s *RoleSession) GetLibraryPanel(ctx context.Context) (*protocol.LibraryPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildLibraryInfoPacket(s.LastActionID()), 0x00550001)
	if err != nil {
		return nil, fmt.Errorf("get library panel: %w", err)
	}
	return protocol.ParseLibraryInfoResponse(resp.Payload)
}

// GetElixirSoulPanel 查询指定角色的丹魂面板 (Module 347, Action 0)。
func (s *RoleSession) GetElixirSoulPanel(ctx context.Context, playerRoleID int32) (*protocol.ElixirSoulPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildElixirSoulRoleInfoPacket(playerRoleID, s.LastActionID()), 0x015B0000)
	if err != nil {
		return nil, fmt.Errorf("get elixir soul panel: %w", err)
	}
	return protocol.ParseElixirSoulRoleInfoResponse(resp.Payload)
}

// GetSantWeaponPanel 查询圣器面板 (Module 280, Action 0)。
func (s *RoleSession) GetSantWeaponPanel(ctx context.Context) (*protocol.SantWeaponPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildSantWeaponInfoPacket(s.LastActionID()), 0x01180000)
	if err != nil {
		return nil, fmt.Errorf("get sant weapon panel: %w", err)
	}
	return protocol.ParseSantWeaponInfoResponse(resp.Payload)
}

// GetTacticsPanel 查询兵法面板 (Module 299, Action 0)。
func (s *RoleSession) GetTacticsPanel(ctx context.Context) (*protocol.TacticsPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildTacticsInfoPacket(s.LastActionID()), 0x012B0000)
	if err != nil {
		return nil, fmt.Errorf("get tactics panel: %w", err)
	}
	return protocol.ParseTacticsInfoResponse(resp.Payload)
}

// GetChaosPack 查询混沌虚空背包、灵叶与道具年代 (Module 124, Action 0)。
func (s *RoleSession) GetChaosPack(ctx context.Context) (*protocol.ChaosPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildChaosPackPacket(s.LastActionID()), 0x007C0000)
	if err != nil {
		return nil, fmt.Errorf("get chaos pack: %w", err)
	}
	return protocol.ParseChaosPackResponse(resp.Payload)
}

// GetChaosRoleMonsters 查询指定角色的混沌魔兽 (Module 124, Action 1)。
func (s *RoleSession) GetChaosRoleMonsters(ctx context.Context, playerRoleID int32) ([]protocol.ChaosMonster, error) {
	resp, err := s.callPanel(ctx, protocol.BuildChaosRoleMonsterPacket(playerRoleID, s.LastActionID()), 0x007C0001)
	if err != nil {
		return nil, fmt.Errorf("get chaos role monsters: %w", err)
	}
	return protocol.ParseChaosRoleMonsterResponse(resp.Payload)
}

// GetEudemonsPanel 查询幻兽主面板 (Module 693, Action 0)。
func (s *RoleSession) GetEudemonsPanel(ctx context.Context) (*protocol.EudemonsPanel, error) {
	resp, err := s.callPanel(ctx, protocol.BuildEudemonsPanelPacket(), protocol.ActionIDEudemonsPanel)
	if err != nil {
		return nil, fmt.Errorf("get eudemons panel: %w", err)
	}
	return protocol.ParseEudemonsPanelResult(resp.Payload)
}
