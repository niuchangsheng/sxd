// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件实现角色已开放功能列表的查询与缓存 (Mod_Player_Base.get_player_function)。
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// playerFunctionsCacheTTL 已开放功能列表缓存有效期。
//
// 功能开放状态在角色升级/完成任务时才会变化，日常调度期间无需反复查询；
// 5 分钟足以覆盖一轮日常任务，同时保证跨过升级节点后能及时刷新。
const playerFunctionsCacheTTL = 5 * time.Minute

// RefreshPlayerFunctions 拉取当前角色已开放功能列表并刷新本地缓存。
func (s *RoleSession) RefreshPlayerFunctions(ctx context.Context) (map[int32]bool, error) {
	pkt := protocol.BuildGetPlayerFunctionsPacket(s.LastActionID())
	timeout := 8 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDPlayerGetFunctions)
	if err != nil {
		return nil, fmt.Errorf("get player functions: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty player functions response")
	}
	functions, err := protocol.ParsePlayerFunctionsResponse(resp.Payload)
	if err != nil {
		return nil, err
	}

	open := make(map[int32]bool, len(functions))
	for _, f := range functions {
		// 服务端只下发已开放条目；isPlayed 仅表示是否体验过，不影响开放判定。
		open[f.ID] = true
	}

	s.functionMu.Lock()
	s.functionOpen = open
	s.functionAt = time.Now()
	s.functionMu.Unlock()
	return open, nil
}

// IsFunctionOpen 判断指定功能当前是否对角色开放。
//
// 优先使用会话内缓存；缓存过期或尚未加载时调用 RefreshPlayerFunctions。
// 返回错误表示无法确定开放状态（网络/协议异常），调用方不应把错误当成“未开放”，
// 但也应立即跳过对应玩法，禁止继续向服务端发送可能被静默丢弃的业务请求。
func (s *RoleSession) IsFunctionOpen(ctx context.Context, functionID int32) (bool, error) {
	s.functionMu.RLock()
	cached := s.functionOpen
	fetchedAt := s.functionAt
	s.functionMu.RUnlock()

	if cached != nil && time.Since(fetchedAt) < playerFunctionsCacheTTL {
		return cached[functionID], nil
	}

	open, err := s.RefreshPlayerFunctions(ctx)
	if err != nil {
		return false, err
	}
	return open[functionID], nil
}

// RefreshPlayerFunctionsIfStale 仅在缓存为空/过期时刷新，供需要批量判定的调用方使用。
func (s *RoleSession) RefreshPlayerFunctionsIfStale(ctx context.Context) (map[int32]bool, error) {
	s.functionMu.RLock()
	cached := s.functionOpen
	fetchedAt := s.functionAt
	s.functionMu.RUnlock()

	if cached != nil && time.Since(fetchedAt) < playerFunctionsCacheTTL {
		return cached, nil
	}
	return s.RefreshPlayerFunctions(ctx)
}
