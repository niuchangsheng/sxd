// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件实现功能开放状态查询封装 (Mod_Player_Base.check_function_open)。
package client

import (
	"context"
	"fmt"

	"sxd-pie-ng/internal/protocol"
)

// CheckFunctionOpen 查询当前角色指定功能是否开放 (Module 0, Action 69)。
//
// functionID 取自客户端 FunctionTypeData.List 的功能编号。返回 (true, nil) 表示
// 服务端明确回复已开放，(false, nil) 表示明确回复未开放；协议异常或未知标志位
// 会返回错误，调用方不得把错误当作“未开放”处理。
func (s *RoleSession) CheckFunctionOpen(ctx context.Context, functionID int32) (bool, error) {
	pkt := protocol.BuildCheckFunctionOpenPacket(functionID, s.LastActionID())
	resp, err := s.Call(ctx, pkt, protocol.ActionIDPlayerCheckFunctionOpen)
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
