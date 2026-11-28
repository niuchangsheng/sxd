// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件提供统一的派生子会话工厂，集中管理子会话启动、事件驱动激活等待与失败资源回收。
package client

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// openChildSession 统一创建、启动并等待派生子会话（仙界跨服/圣域/仙盟城镇/全网聊天室）进入激活态。
//
// lifeCtx 决定子会话的存活生命周期（常驻辅助会话绑定主服 s.ctx，临时会话绑定调用方 ctx）；
// waitCtx 决定本次等待激活的截止时间（如 restoreCrossKeepalive 传入的 8 秒超时）；
// 若激活前关闭或超时，自动 Close 清理底层连接与协程，防止资源泄漏。
func (s *RoleSession) openChildSession(
	lifeCtx context.Context,
	waitCtx context.Context,
	label string,
	cfg SessionConfig,
	activateTimeout time.Duration,
) (*RoleSession, error) {
	if lifeCtx == nil {
		lifeCtx = context.Background()
	}
	if waitCtx == nil {
		waitCtx = lifeCtx
	}
	// 派生子会话（跨服/圣域/仙盟/聊天室）不走主服 0/6 半开探针。
	cfg.StaleProbeInterval = -1
	child := NewRoleSession(cfg)
	if err := child.Start(lifeCtx); err != nil {
		return nil, fmt.Errorf("client: 启动%s会话失败: %w", label, err)
	}

	effWaitCtx := waitCtx
	if activateTimeout > 0 {
		var cancel context.CancelFunc
		effWaitCtx, cancel = context.WithTimeout(waitCtx, activateTimeout)
		defer cancel()
	}

	if err := child.AwaitActive(effWaitCtx); err != nil {
		child.Close()
		if errors.Is(err, ErrSessionClosed) {
			return nil, fmt.Errorf("client: %s会话在激活前关闭", label)
		}
		return nil, fmt.Errorf("client: 等待%s会话激活超时: %w", label, err)
	}
	return child, nil
}
