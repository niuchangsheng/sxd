// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件封装仙界练功房 (Mod_StPracticeRoom_Base, Module 117) 的跨服业务调用。
// 该模块仅在 Mod 94 登录并进入仙界城镇后的跨服会话上可用。
//
// @author Ateng
// @since 2026-11-04
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// stPracticeRoomCallTimeout 仙界练功房单次跨服调用超时预算。
//
// 跨服节点在高峰期可能比主服慢，但练功房回包体积很小；
// 5 秒已能覆盖 DNS/网络抖动，同时避免单个请求拖住整轮巡检。
const stPracticeRoomCallTimeout = 5 * time.Second

// callStPracticeRoom 发送练功房请求并等待同 ActionID 回包。
func (s *RoleSession) callStPracticeRoom(ctx context.Context, pkt *protocol.Packet, actionID uint32) (*protocol.Packet, error) {
	cCtx, cancel := context.WithTimeout(ctx, stPracticeRoomCallTimeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, actionID)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("client: 仙界练功房回包为空")
	}
	return resp, nil
}

// EnterStPracticeRoom 进入仙界练功房面板 (Mod 117, Act 8)。
func (s *RoleSession) EnterStPracticeRoom(ctx context.Context) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomEnterRoomPacket(), protocol.ActionIDStPracticeRoomEnterRoom)
	if err != nil {
		return 0, fmt.Errorf("client: 进入仙界练功房失败: %w", err)
	}
	result, err := protocol.ParseStPracticeRoomActionResult(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析进入仙界练功房回包失败: %w", err)
	}
	return result, nil
}

// ExitStPracticeRoom 退出仙界练功房面板 (Mod 117, Act 9)。
func (s *RoleSession) ExitStPracticeRoom(ctx context.Context) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomExitRoomPacket(), protocol.ActionIDStPracticeRoomExitRoom)
	if err != nil {
		return 0, fmt.Errorf("client: 退出仙界练功房失败: %w", err)
	}
	result, err := protocol.ParseStPracticeRoomActionResult(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析退出仙界练功房回包失败: %w", err)
	}
	return result, nil
}

// GetStPracticeRoomList 查询练功房房间列表与当前练功状态 (Mod 117, Act 0)。
func (s *RoleSession) GetStPracticeRoomList(ctx context.Context) (*protocol.StPracticeRoomList, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetRoomsPacket(), protocol.ActionIDStPracticeRoomGetRooms)
	if err != nil {
		return nil, fmt.Errorf("client: 查询仙界练功房列表失败: %w", err)
	}
	list, err := protocol.ParseStPracticeRoomList(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙界练功房列表失败: %w", err)
	}
	return list, nil
}

// GetStPracticeRoomInfo 查询指定品质星级房间的席位与经验状态 (Mod 117, Act 1)。
func (s *RoleSession) GetStPracticeRoomInfo(ctx context.Context, quality uint8) (*protocol.StPracticeRoomInfo, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetRoomInfoPacket(quality), protocol.ActionIDStPracticeRoomGetRoomInfo)
	if err != nil {
		return nil, fmt.Errorf("client: 查询仙界练功房 %d 星房间失败: %w", quality, err)
	}
	info, err := protocol.ParseStPracticeRoomInfo(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙界练功房 %d 星房间失败: %w", quality, err)
	}
	return info, nil
}

// GetStPracticeRoomFreeInfo 查询普通练功场分页与经验状态 (Mod 117, Act 16)。
func (s *RoleSession) GetStPracticeRoomFreeInfo(ctx context.Context) (*protocol.StPracticeRoomFreeInfo, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetFreeRoomInfoPacket(), protocol.ActionIDStPracticeRoomGetFreeRoomInfo)
	if err != nil {
		return nil, fmt.Errorf("client: 查询普通练功场失败: %w", err)
	}
	info, err := protocol.ParseStPracticeRoomFreeInfo(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析普通练功场失败: %w", err)
	}
	return info, nil
}

// GetStPracticeRoomExpFlag 查询是否存在未领取练功经验 (Mod 117, Act 7)。
func (s *RoleSession) GetStPracticeRoomExpFlag(ctx context.Context) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetExpFlagPacket(), protocol.ActionIDStPracticeRoomGetExpFlag)
	if err != nil {
		return 0, fmt.Errorf("client: 查询仙界练功房经验标记失败: %w", err)
	}
	flag, err := protocol.ParseStPracticeRoomActionResult(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析仙界练功房经验标记失败: %w", err)
	}
	return flag, nil
}

// ClaimStPracticeRoomExp 领取练功房累积经验 (Mod 117, Act 6)。
func (s *RoleSession) ClaimStPracticeRoomExp(ctx context.Context) (*protocol.StPracticeRoomExpResult, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetPracticeExpPacket(), protocol.ActionIDStPracticeRoomGetPracticeExp)
	if err != nil {
		return nil, fmt.Errorf("client: 领取仙界练功房经验失败: %w", err)
	}
	result, err := protocol.ParseStPracticeRoomExpResult(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙界练功房经验回包失败: %w", err)
	}
	return result, nil
}

// LeaveStPracticeRoomSeat 主动离开当前练功席位 (Mod 117, Act 13)。
func (s *RoleSession) LeaveStPracticeRoomSeat(ctx context.Context) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomPlayerLeaveSeatPacket(), protocol.ActionIDStPracticeRoomPlayerLeaveSeat)
	if err != nil {
		return 0, fmt.Errorf("client: 离开仙界练功房席位失败: %w", err)
	}
	result, err := protocol.ParseStPracticeRoomActionResult(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析离开仙界练功房席位回包失败: %w", err)
	}
	return result, nil
}

// GetStPracticeRoomSeatStatus 查询指定席位是否有人 (Mod 117, Act 3)。
func (s *RoleSession) GetStPracticeRoomSeatStatus(ctx context.Context, quality, seat uint8) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetSeatInfoPacket(quality, seat), protocol.ActionIDStPracticeRoomGetSeatInfo)
	if err != nil {
		return 0, fmt.Errorf("client: 查询仙界练功房 %d 星 %d 号席位失败: %w", quality, seat, err)
	}
	status, err := protocol.ParseStPracticeRoomSeatStatus(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析仙界练功房席位状态失败: %w", err)
	}
	return status, nil
}

// SitDownStPracticeRoom 坐上指定席位开始练功 (Mod 117, Act 2)。
func (s *RoleSession) SitDownStPracticeRoom(ctx context.Context, quality, seat uint8) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomSitDownPacket(quality, seat), protocol.ActionIDStPracticeRoomSitDown)
	if err != nil {
		return 0, fmt.Errorf("client: 坐上仙界练功房席位失败: %w", err)
	}
	result, err := protocol.ParseStPracticeRoomActionResult(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析坐上仙界练功房席位回包失败: %w", err)
	}
	return result, nil
}

// RobStPracticeRoomSeat 向已占用的席位发起争夺战 (Mod 117, Act 4)。
//
// 回包在结果码之后紧跟完整战斗回放；这里只消费首字节结果码，
// 战斗胜负与最终席位归属需重新查询房间列表确认。
func (s *RoleSession) RobStPracticeRoomSeat(ctx context.Context, quality, seat uint8) (uint8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomRobSeatPacket(quality, seat), protocol.ActionIDStPracticeRoomRobSeat)
	if err != nil {
		return 0, fmt.Errorf("client: 争夺仙界练功房席位失败: %w", err)
	}
	result, err := protocol.ParseStPracticeRoomActionResult(resp.Payload)
	if err != nil {
		return 0, fmt.Errorf("client: 解析争夺仙界练功房席位回包失败: %w", err)
	}
	return result, nil
}

// GetStPracticeRoomRobCD 查询争夺冷却与清冷却元宝价格 (Mod 117, Act 11)。
func (s *RoleSession) GetStPracticeRoomRobCD(ctx context.Context) (*protocol.StPracticeRoomRobCD, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetRobCDTimePacket(), protocol.ActionIDStPracticeRoomGetRobCDTime)
	if err != nil {
		return nil, fmt.Errorf("client: 查询仙界练功房争夺冷却失败: %w", err)
	}
	cd, err := protocol.ParseStPracticeRoomRobCD(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙界练功房争夺冷却失败: %w", err)
	}
	return cd, nil
}

// GetStPracticeRoomPlayerQuality 查询玩家当前所在房间品质 (Mod 117, Act 14)。
func (s *RoleSession) GetStPracticeRoomPlayerQuality(ctx context.Context) (int8, error) {
	resp, err := s.callStPracticeRoom(ctx, protocol.BuildStPracticeRoomGetPlayerRoomQualityPacket(), protocol.ActionIDStPracticeRoomGetPlayerRoomQuality)
	if err != nil {
		return 0, fmt.Errorf("client: 查询玩家当前练功房品质失败: %w", err)
	}
	r := protocol.NewReader(resp.Payload)
	quality, err := r.ReadInt8()
	if err != nil {
		return 0, fmt.Errorf("client: 解析玩家当前练功房品质失败: %w", err)
	}
	return quality, nil
}
