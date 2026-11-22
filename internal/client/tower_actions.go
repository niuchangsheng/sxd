// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件承载六道轮回塔 (Mod_Tower_Base, Module 32) 与背包出售协议封装。
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// GetTowerInfo 查询六道轮回塔面板与当前挑战层进度 (Mod_Tower_Base.get_tower_info, ActionID 0x00200000)。
func (s *RoleSession) GetTowerInfo(ctx context.Context) (*protocol.TowerInfo, error) {
	// 1. 获取当前六道轮回塔面板与进度信息 (对齐前序动作号)
	pkt := protocol.BuildTowerGetInfoPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerGetInfo)
	if err != nil {
		return nil, fmt.Errorf("get tower info: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty tower info response")
	}

	return protocol.ParseTowerInfoResponse(resp.Payload)
}

// SellItem 出售背包指定格位物品 (Mod_Item_Base.player_sell_item, ActionID 0x00020049)。
func (s *RoleSession) SellItem(ctx context.Context, gridID int16) (bool, error) {
	pkt, err := protocol.BuildSellItemPacket(gridID)
	if err != nil {
		return false, fmt.Errorf("build sell item packet: %w", err)
	}
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDItemSell)
	if err != nil {
		return false, fmt.Errorf("sell item grid %d: %w", gridID, err)
	}
	if resp == nil {
		return false, errors.New("empty sell item response")
	}

	success, _, pErr := protocol.ParseSellItemResponse(resp.Payload)
	return success, pErr
}

// ChallengeTower 发起六道轮回塔单层战斗挑战 (Mod_Tower_Base.start_challenge, ActionID 0x00200002)。
func (s *RoleSession) ChallengeTower(ctx context.Context, warIdx uint8, autoSellL, autoSellZ bool) (*protocol.TowerChallengeResult, error) {
	pkt := protocol.BuildTowerChallengePacket(warIdx, autoSellL, autoSellZ, s.LastActionID())
	timeout := s.callTimeout(10*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerStartChallenge)
	if err != nil {
		return nil, fmt.Errorf("challenge tower: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty tower challenge response")
	}

	return protocol.ParseTowerChallengeResponse(resp.Payload)
}

// ResetTowerLayer 重置六道轮回塔层数 (Mod_Tower_Base.reset_layer, ActionID 0x00200003)。
func (s *RoleSession) ResetTowerLayer(ctx context.Context) (*protocol.TowerResetResult, error) {
	pkt := protocol.BuildTowerResetPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerResetLayer)
	if err != nil {
		return nil, fmt.Errorf("reset tower layer: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty tower reset response")
	}

	return protocol.ParseTowerResetResponse(resp.Payload)
}

// StartTowerAutoChallenge 发起六道轮回自动挑战/爬塔扫荡 (Mod_Tower_Base.start_auto_challenge, ActionID 0x00200004)。
func (s *RoleSession) StartTowerAutoChallenge(ctx context.Context, autoSellL, autoSellZ bool) error {
	pkt := protocol.BuildTowerStartAutoChallengePacket(autoSellL, autoSellZ, s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerStartAutoChallenge)
	if err != nil {
		return fmt.Errorf("start tower auto challenge: %w", err)
	}
	if resp != nil && len(resp.Payload) > 0 {
		code, _ := protocol.ParseTowerAutoChallengeResponse(resp.Payload)
		if code != protocol.TowerResultSuccess {
			return fmt.Errorf("start tower auto challenge failed with code %d", code)
		}
	}
	return nil
}

// QuicklyTowerAutoChallenge 立即加速秒完成六道轮回自动爬塔 (Mod_Tower_Base.quickly_auto_challenge, ActionID 0x00200007)。
func (s *RoleSession) QuicklyTowerAutoChallenge(ctx context.Context, autoSellL, autoSellZ bool) error {
	pkt := protocol.BuildTowerQuicklyAutoChallengePacket(autoSellL, autoSellZ, s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerQuicklyAutoChallenge)
	if err != nil {
		return fmt.Errorf("quickly tower auto challenge: %w", err)
	}
	if resp != nil && len(resp.Payload) > 0 {
		code, _ := protocol.ParseTowerAutoChallengeResponse(resp.Payload)
		if code != protocol.TowerResultSuccess {
			return fmt.Errorf("quickly tower auto challenge failed with code %d", code)
		}
	}
	return nil
}

// StopTowerAutoChallenge 停止六道轮回自动爬塔 (Mod_Tower_Base.stop_auto_challenge, ActionID 0x00200005)。
func (s *RoleSession) StopTowerAutoChallenge(ctx context.Context) error {
	pkt := protocol.BuildTowerStopAutoChallengePacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := s.Call(cCtx, pkt, protocol.ActionIDTowerStopAutoChallenge)
	return err
}
