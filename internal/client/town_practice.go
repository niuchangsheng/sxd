// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件承载主城切换 (EnterTown/ReturnToTown) 与城镇打坐练功 (Mod_Player_Base / Mod_Practice_Base) 封装。
package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// 官方客户端在离开副本/试炼/战场地图后调用 townMap.gotoTownAtId(ctrlPlayer.townMapId)
// 重新进入城镇；服务端据此清除角色的战场上下文。缺少该步骤时，角色会残留在
// 特殊地图状态，后续部分模块的写操作会被静默丢弃。
//
// 返回服务端结果码 (0=成功)。
//
// 官方 Mod_Town_Base.enter_town 请求仅有 [Int townID]（HasPrevAct=false）。
// 2026-11-15 探针确认裸 4 字节稳定回包；带任意 PrevAct 的 8 字节形态在部分
// 会话状态下会被丢弃。get_sections 等 Mod 3/4 请求的 PrevAct 由会话层按
// 「最后收到的包」统一填充，不再依赖 enter_town 的锚点。
func (s *RoleSession) EnterTown(ctx context.Context, townID int32) (uint8, error) {
	return s.enterTownWithPacket(ctx, townID, protocol.BuildEnterTownPacket(townID))
}

// EnterTownWithAnchor 使用显式 PrevAct 锚点复位城镇。
//
// 2026-11-15 实测：裸 enter_town 会导致随后的 4/0 被丢弃；以「失败 4/0」
// (0x00040000) 或 3/9 为锚点的 8 字节 enter_town 可恢复 Mod 4 上下文。
func (s *RoleSession) EnterTownWithAnchor(ctx context.Context, townID int32, anchor uint32) (uint8, error) {
	return s.enterTownWithPacket(ctx, townID, protocol.BuildEnterTownWithPrevActPacket(townID, anchor))
}

// ReturnToTown 在战斗/活动结束后回到当前城镇，恢复后续玩法所需的城镇上下文。
//
// 2026-11-22 实测：五行天宫 453/3、灵域猎妖 669/3 等动作在上一玩法留下战斗
// 上下文时会被服务端静默丢弃；回到城镇后同类请求恢复正常。仅发送普通
// enter_town，不重放登录握手。返回 true 表示服务端确认进入城镇。
func (s *RoleSession) ReturnToTown(ctx context.Context, timeout time.Duration) bool {
	if s == nil || s.State() != StateActive {
		return false
	}
	var candidates []int32
	addCandidate := func(id int32) {
		if id <= 0 {
			return
		}
		for _, existing := range candidates {
			if existing == id {
				return
			}
		}
		candidates = append(candidates, id)
	}
	addCandidate(s.knownGoodTownID.Load())
	addCandidate(int32(s.GetPlayerState().TownMapID))
	addCandidate(3)
	addCandidate(2)
	addCandidate(1)

	for _, townID := range candidates {
		anchor := s.LastAckActionID()
		if anchor == 0 {
			anchor = s.LastBusinessActionID()
		}
		c, cancel := context.WithTimeout(ctx, timeout)
		code, err := s.EnterTownWithAnchor(c, townID, anchor)
		cancel()
		if err != nil {
			slog.Debug("返回城镇失败", "role_id", s.RoleID(), "town_id", townID, "err", err)
			// 超时/断连代表 1/0 通道本身未应答（或静默丢弃），继续轮询其他 townID 只会成倍放大超时；
			// 仅当服务端明确返回非 0 失败码 (如 153 -> code=1 FAILED) 时才回退尝试下一个候选主城。
			return false
		}
		if code == 0 {
			s.knownGoodTownID.Store(townID)
			return true
		}
	}
	return false
}

func (s *RoleSession) enterTownWithPacket(ctx context.Context, townID int32, pkt *protocol.Packet) (uint8, error) {
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTownEnter)
	if err != nil {
		return 0, fmt.Errorf("enter town %d: %w", townID, err)
	}
	if resp == nil || len(resp.Payload) == 0 {
		return 0, errors.New("empty enter town response")
	}
	return resp.Payload[0], nil
}

// StartTownPractice 发起城镇打坐练功请求 (Mod_Player_Base.start_practice, ActionID 0x00000024)。
func (s *RoleSession) StartTownPractice(ctx context.Context) error {
	pkt := protocol.BuildPlayerStartPracticePacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := s.Call(cCtx, pkt, protocol.ActionIDPlayerStartPractice)
	if err != nil {
		return fmt.Errorf("start town practice: %w", err)
	}

	s.playerMu.Lock()
	s.playerState.IsPracticing = true
	s.playerMu.Unlock()
	slog.Info("角色已成功进入普通城镇打坐练功", "role_id", s.RoleID())
	return nil
}

// GetTownPracticeData 获取城镇打坐练功实时数据 (Mod_Player_Base.get_practice_data, ActionID 0x00000025)。
func (s *RoleSession) GetTownPracticeData(ctx context.Context) (*protocol.TownPracticeData, error) {
	pkt := protocol.BuildPlayerGetPracticeDataPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDPlayerGetPracticeData)
	if err != nil {
		return nil, fmt.Errorf("get town practice data: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty town practice data response")
	}

	data, err := protocol.ParseTownPracticeDataResponse(resp.Payload)
	if err != nil {
		return nil, err
	}

	s.playerMu.Lock()
	s.playerState.PracticeExp = data.PracticeExp
	if data.PracticeTime > 0 || data.PracticeExp > 0 {
		s.playerState.IsPracticing = true
	}
	s.playerMu.Unlock()

	return data, nil
}

// CancelTownPractice 取消/中断当前城镇打坐练功 (Mod_Player_Base.cancel_practice, ActionID 0x00000027)。
func (s *RoleSession) CancelTownPractice(ctx context.Context) error {
	pkt := protocol.BuildPlayerCancelPracticePacket(s.LastActionID())
	if err := s.Send(pkt); err != nil {
		return fmt.Errorf("cancel town practice: %w", err)
	}

	s.playerMu.Lock()
	s.playerState.IsPracticing = false
	s.playerMu.Unlock()
	return nil
}

// SignFinishedTownPractice 结算并领取城镇打坐练功累积经验 (Mod_Player_Base.sign_finished_practice, ActionID 0x00000026)。
func (s *RoleSession) SignFinishedTownPractice(ctx context.Context) error {
	pkt := protocol.BuildPlayerSignFinishedPracticePacket(s.LastActionID())
	if err := s.Send(pkt); err != nil {
		return fmt.Errorf("sign finished town practice: %w", err)
	}

	s.playerMu.Lock()
	s.playerState.IsPracticing = false
	s.playerState.PracticeExp = 0
	s.playerMu.Unlock()
	slog.Info("已触发领取城镇打坐练功累积经验", "role_id", s.RoleID())
	return nil
}

// GetNewPracticeData 获取现代新版离线打坐练功进度与累积经验 (Mod_NewPractice_Base.get_practice_info, ActionID 0x02480001)。
func (s *RoleSession) GetNewPracticeData(ctx context.Context) (*protocol.NewPracticeInfo, error) {
	pkt := protocol.BuildNewPracticeGetInfoPacket()
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDNewPracticeGetInfo)
	if err != nil {
		return nil, err
	}
	info, err := protocol.ParseNewPracticeGetInfoResponse(resp.Payload)
	if err != nil {
		return nil, err
	}
	s.playerMu.Lock()
	s.playerState.PracticeExp = info.PracticeExp
	s.playerMu.Unlock()
	return info, nil
}

// ClaimNewPracticeExp 一键领取现代新版打坐练功累积的经验与灵气 (Mod_NewPractice_Base.collect_all, ActionID 0x02480003)。
func (s *RoleSession) ClaimNewPracticeExp(ctx context.Context) (*protocol.NewPracticeCollectResult, error) {
	pkt := protocol.BuildNewPracticeCollectAllPacket()
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDNewPracticeCollectAll)
	if err != nil {
		return nil, err
	}
	res, err := protocol.ParseNewPracticeCollectAllResponse(resp.Payload)
	if err != nil {
		return nil, err
	}
	// 只有服务端确认成功才清零本地累积经验，避免失败回包被当成已领取
	// 导致「经验没到账、本地却记为零」的资源无变化假象。
	if res.Result == protocol.NewPracticeSuccess {
		s.playerMu.Lock()
		s.playerState.PracticeExp = 0
		s.playerMu.Unlock()
	}
	return res, nil
}

// ClaimTownPracticeExp 上线直接结算并领取离线练功经验。
// 优先使用官方现代新版练功系统 (Mod_NewPractice_Base.collect_all)，无缝兼容老版双步结算流程。
func (s *RoleSession) ClaimTownPracticeExp(ctx context.Context) error {
	// 尝试现代打坐一键领取
	if res, err := s.ClaimNewPracticeExp(ctx); err == nil {
		if res == nil || res.Result == protocol.NewPracticeSuccess {
			return nil
		}
		// 服务端已明确返回业务失败码：不再回退老版协议，交由调用方记录。
		return fmt.Errorf("client: new practice collect failed: result=%d", res.Result)
	}

	// 回退机制：若服务端未开启新练功模块，执行老版结算流程
	s.playerMu.RLock()
	curExp := s.playerState.PracticeExp
	isPracticing := s.playerState.IsPracticing
	s.playerMu.RUnlock()

	slog.Debug("回退执行老版打坐练功结算流水线",
		"role_id", s.RoleID(),
		"pending_exp", curExp,
		"is_practicing", isPracticing,
	)

	_ = s.CancelTownPractice(ctx)
	return s.SignFinishedTownPractice(ctx)
}
