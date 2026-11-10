// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件实现功能开放状态查询封装 (Mod_Player_Base.check_function_open)。
package client

import (
	"context"
	"fmt"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// checkFunctionOpenTimeout 功能开放探测的默认超时。
//
// 服务端对场景不符/未开放模块的只读探测会静默丢弃，而 Routine 传入的是调度器
// 上下文 (无 deadline)。此处必须自带超时上限，否则单次丢包会永久挂起调度协程，
// 冻结该角色全部后续玩法 (2026-11-10 叶公好龙卡死事故的直接原因)。
const checkFunctionOpenTimeout = 8 * time.Second

// functionOpenCallTimeout 探测超时；沙箱/离线环境使用短超时以免拖慢单测。
func (s *RoleSession) functionOpenCallTimeout() time.Duration {
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		return 300 * time.Millisecond
	}
	return checkFunctionOpenTimeout
}

// CheckFunctionOpen 查询当前角色指定功能是否开放 (Module 0, Action 69)。
//
// functionID 取自客户端 FunctionTypeData.List 的功能编号。返回 (true, nil) 表示
// 服务端明确回复已开放，(false, nil) 表示明确回复未开放；协议异常或未知标志位
// 会返回错误，调用方不得把错误当作“未开放”处理。
func (s *RoleSession) CheckFunctionOpen(ctx context.Context, functionID int32) (bool, error) {
	pkt := protocol.BuildCheckFunctionOpenPacket(functionID, s.LastActionID())
	// 调用方传入更短 deadline 时以其为准，否则套用探测短超时。
	cCtx, cancel := context.WithTimeout(ctx, s.functionOpenCallTimeout())
	defer cancel()
	resp, err := s.Call(cCtx, pkt, protocol.ActionIDPlayerCheckFunctionOpen)
	if err != nil {
		return false, err
	}
	if resp == nil {
		return false, fmt.Errorf("check function open: empty response")
	}
	flag, err := protocol.ParseCheckFunctionOpenResponse(resp.Payload)
	if err != nil {
		return false, err
	}
	switch flag {
	case protocol.PlayerFunctionOpenTrue:
		return true, nil
	case protocol.PlayerFunctionOpenFalse:
		return false, nil
	default:
		return false, fmt.Errorf("check function open: unexpected flag %d", flag)
	}
}
