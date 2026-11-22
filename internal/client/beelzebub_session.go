// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件承载魔王试炼 (Mod_BeelzebubTrials_Base, Module 57) 的 RoleSession 协议封装与单人自动挑战流程。
package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// BeelzebubTrialsGetOpenTime 查询魔王试炼活动开启时间段 (Module 57, Action 0)。
//
// 活动未开启时服务端会对 create_team 静默丢包，因此建队前必须先以本接口
// 判定是否处于开启窗口，避免把“活动未开放”误判为资格/协议限制。
func (s *RoleSession) BeelzebubTrialsGetOpenTime(ctx context.Context) (*protocol.BeelzebubTrialsOpenTime, error) {
	pkt := protocol.BuildBeelzebubTrialsGetOpenTimePacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetOpenTime)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub trials open time: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty beelzebub trials open time response")
	}
	return protocol.ParseBeelzebubTrialsOpenTimeResponse(resp.Payload)
}

// BeelzebubTrialsCheckCanChallenge 检查今日是否可挑战魔王试炼 (Module 57, Action 20)。
func (s *RoleSession) BeelzebubTrialsCheckCanChallenge(ctx context.Context) (bool, error) {
	pkt := protocol.BuildBeelzebubTrialsCheckCanChallengePacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsCheckCanChallenge)
	if err != nil {
		return false, fmt.Errorf("check beelzebub trials can challenge: %w", err)
	}
	if resp == nil {
		return false, errors.New("empty beelzebub check response")
	}
	return protocol.ParseBeelzebubTrialsCheckCanChallengeResponse(resp.Payload)
}

// BeelzebubTrialsGetCanChallengeList 获取可挑战魔王编号列表 (Module 57, Action 22)。
func (s *RoleSession) BeelzebubTrialsGetCanChallengeList(ctx context.Context) ([]int32, error) {
	pkt := protocol.BuildBeelzebubTrialsGetCanChallengeListPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetCanChallengeList)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub trials challenge list: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty beelzebub challenge list response")
	}
	return protocol.ParseBeelzebubTrialsGetCanChallengeListResponse(resp.Payload)
}

// BeelzebubTrialsOpen 打开魔王试炼面板并获取基础道行与活动信息 (Module 57, Action 1)。
func (s *RoleSession) BeelzebubTrialsOpen(ctx context.Context) (*protocol.BeelzebubTrialsOpenData, error) {
	pkt := protocol.BuildBeelzebubTrialsOpenPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsOpen)
	if err != nil {
		return nil, fmt.Errorf("open beelzebub trials: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty beelzebub trials open response")
	}
	return protocol.ParseBeelzebubTrialsOpenResponse(resp.Payload)
}

// BeelzebubTrialsCreateTeam 创建指定魔王挑战队伍 (Module 57, Action 4)。
func (s *RoleSession) BeelzebubTrialsCreateTeam(ctx context.Context, bossID int32, teamType uint8) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsCreateTeamPacket(bossID, teamType, s.LastActionID())
	// 服务端拒绝建队时不会回任何错误码；4 秒足以覆盖正常 RTT，又避免单关
	// 无回包时长时间占用调度（逐关尝试下一个可挑战关卡）。
	timeout := s.callTimeout(4*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsCreateTeam)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// 会话已断开/重连中的超时属于网络中断，不能判定为服务端拒绝玩法；
			// 仅当会话仍处于 Active 时才按“静默丢弃”处理。
			if s.State() != StateActive {
				return 0, fmt.Errorf("create beelzebub trials team: session interrupted: %w", err)
			}
			// 等待期内没有任何回包：服务端静默丢弃该动作，交由上层熔断本轮试炼。
			return 0, fmt.Errorf("create beelzebub trials team: %w: %v", ErrBeelzebubTrialsNoResponse, err)
		}
		return 0, fmt.Errorf("create beelzebub trials team: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty create team response")
	}
	return protocol.ParseBeelzebubTrialsCreateTeamResponse(resp.Payload)
}

// BeelzebubTrialsJoinTeam 加入已有魔王试炼队伍 (Module 57, Action 6)。
func (s *RoleSession) BeelzebubTrialsJoinTeam(ctx context.Context, teamID int32) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsJoinTeamPacket(teamID, s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsJoinTeam)
	if err != nil {
		return 0, fmt.Errorf("join beelzebub trials team: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty join team response")
	}
	return protocol.ParseBeelzebubTrialsJoinTeamResponse(resp.Payload)
}

// BeelzebubTrialsGetPlayerTeamInfo 获取当前魔王试炼队伍成员状态 (Module 57, Action 3)。
func (s *RoleSession) BeelzebubTrialsGetPlayerTeamInfo(ctx context.Context) (int32, []protocol.BeelzebubTeamMember, error) {
	pkt := protocol.BuildBeelzebubTrialsGetPlayerTeamInfoPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo)
	if err != nil {
		return 0, nil, fmt.Errorf("get beelzebub trials player team info: %w", err)
	}
	if resp == nil {
		return 0, nil, errors.New("empty player team info response")
	}
	return protocol.ParseBeelzebubTrialsPlayerTeamInfoResponse(resp.Payload)
}

// BeelzebubTrialsGetHelperRoles 获取可调遣助战伙伴/镜像列表 (Module 57, Action 8)。
func (s *RoleSession) BeelzebubTrialsGetHelperRoles(ctx context.Context) ([]protocol.BeelzebubHelperRole, error) {
	pkt := protocol.BuildBeelzebubTrialsGetHelperRoleListPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetHelperRoleList)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub helper roles: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty helper roles response")
	}
	return protocol.ParseBeelzebubTrialsHelperRoleListResponse(resp.Payload)
}

// BeelzebubTrialsChooseHelper 选择助战伙伴填充队伍二号或三号空位 (Module 57, Action 9)。
func (s *RoleSession) BeelzebubTrialsChooseHelper(ctx context.Context, roleID int32, slot uint8) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsChooseHelperRolePacket(roleID, slot, s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsChooseHelperRole)
	if err != nil {
		return 0, fmt.Errorf("choose beelzebub helper role: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty choose helper response")
	}
	return protocol.ParseBeelzebubTrialsChooseHelperRoleResponse(resp.Payload)
}

// BeelzebubTrialsFight 队长向服务器发起战斗指令 (Module 57, Action 13)。
// 官方 AS3 Mod_BeelzebubTrials_Base 规范：Action 13 属于单向无回包发包指令，触发后由 Action 14 拉取完整战报。
func (s *RoleSession) BeelzebubTrialsFight(ctx context.Context) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsFightPacket(s.LastActionID())
	if err := s.Send(pkt); err != nil {
		return 0, fmt.Errorf("fight beelzebub trials: %w", err)
	}
	return protocol.BeelzebubTrialsSuccess, nil
}

// BeelzebubTrialsGetWarResult 拉取魔王试炼战报结果与奖励结算 (Module 57, Action 14)。
func (s *RoleSession) BeelzebubTrialsGetWarResult(ctx context.Context) (*protocol.BeelzebubTrialsWarResult, error) {
	pkt := protocol.BuildBeelzebubTrialsGetWarResultPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetWarResult)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub war result: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty war result response")
	}
	return protocol.ParseBeelzebubTrialsWarResultResponse(resp.Payload)
}

// BeelzebubTrialsLeaveWar 退出魔王试炼战斗结算场景 (Module 57, Action 16)。
// 根据官方 AS3 客户端协议实现，leave_war 为单向离场通知，服务端不回包。
func (s *RoleSession) BeelzebubTrialsLeaveWar(ctx context.Context, warID int32) error {
	pkt := protocol.BuildBeelzebubTrialsLeaveWarPacket(warID, s.LastActionID())
	return s.Send(pkt)
}

// BeelzebubTrialsExitTeam 退出或解散当前队伍 (Module 57, Action 7)。
func (s *RoleSession) BeelzebubTrialsExitTeam(ctx context.Context) error {
	pkt := protocol.BuildBeelzebubTrialsExitTeamPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsExitTeam)
	if err != nil {
		return fmt.Errorf("exit beelzebub team: %w", err)
	}
	if resp == nil {
		return errors.New("empty exit team response")
	}
	code, err := protocol.ParseBeelzebubTrialsExitTeamResponse(resp.Payload)
	if err != nil {
		return fmt.Errorf("parse exit beelzebub team response: %w", err)
	}
	if code != protocol.BeelzebubTrialsSuccess {
		return fmt.Errorf("exit beelzebub team failed with result=%d", code)
	}
	return nil
}

// BeelzebubTrialsOpenMoral 查询当前道行属性与等级奖励信息 (Module 57, Action 23)。
func (s *RoleSession) BeelzebubTrialsOpenMoral(ctx context.Context) (*protocol.BeelzebubMoralInfo, error) {
	pkt := protocol.BuildBeelzebubTrialsOpenMoralPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsOpenMoral)
	if err != nil {
		return nil, fmt.Errorf("open beelzebub moral: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty open moral response")
	}
	return protocol.ParseBeelzebubTrialsOpenMoralResponse(resp.Payload)
}

// BeelzebubTrialsGetMoralAward 领取指定道行等级奖励 (Module 57, Action 24)。
func (s *RoleSession) BeelzebubTrialsGetMoralAward(ctx context.Context, level int32) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsGetMoralAwardPacket(level, s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetMoralAward)
	if err != nil {
		return 0, fmt.Errorf("get beelzebub moral award: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty moral award response")
	}
	return protocol.ParseBeelzebubTrialsGetMoralAwardResponse(resp.Payload)
}

// BeelzebubTrialsAutoChallenge 全自动执行魔王试炼单挑通关闭环：
// 1. 检查今日试炼资格
// 2. 拉取可挑战魔王列表并选定最高难度 (或优先选定 preferBossID)
// 3. 自建队伍
// 4. 招募助战镜像补齐队伍
// 5. 发起战斗并拉取收益
// 6. 清理离开战场与队伍
// 7. 领取累积道行等级奖励
func (s *RoleSession) BeelzebubTrialsAutoChallenge(ctx context.Context, preferBossID int32) (*protocol.BeelzebubTrialsWarResult, error) {
	// 1. 检查今日挑战资格
	canChallenge, err := s.BeelzebubTrialsCheckCanChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查魔王试炼挑战状态失败: %w", err)
	}
	if !canChallenge {
		slog.Info("今日魔王试炼挑战次数已用尽，无需重复挑战", "role_id", s.RoleID())
		return nil, nil
	}

	// 2. 拉取可挑战魔王列表并按等级从低到高排序，挨个确认是否可以挑战
	canList, err := s.BeelzebubTrialsGetCanChallengeList(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取可挑战魔王列表失败: %w", err)
	}
	if len(canList) == 0 {
		slog.Warn("未检测到可挑战的魔王列表，退出魔王试炼", "role_id", s.RoleID())
		return nil, nil
	}

	type candidateBoss struct {
		id    int32
		level int32
		name  string
	}
	candidates := make([]candidateBoss, 0, len(canList))
	for _, bid := range canList {
		candidates = append(candidates, candidateBoss{
			id:    bid,
			level: protocol.BeelzebubTrialsBossLevel(bid),
			name:  protocol.BeelzebubTrialsBossDisplayName(bid),
		})
	}

	// 按照等级从低到高排序；等级相同时按 ID 升序排序
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].level != candidates[j].level {
			return candidates[i].level < candidates[j].level
		}
		return candidates[i].id < candidates[j].id
	})

	// 若显式指定了 preferBossID，优先将其排在候选首位尝试
	if preferBossID > 0 {
		var prioritized []candidateBoss
		for _, c := range candidates {
			if c.id == preferBossID {
				prioritized = append(prioritized, c)
				break
			}
		}
		for _, c := range candidates {
			if c.id != preferBossID {
				prioritized = append(prioritized, c)
			}
		}
		candidates = prioritized
	}

	// 3. 从等级低的开始，挨个确认是否可以挑战 (创建队伍成功即确认锁定目标)
	var targetBossID int32
	var bossName string
	teamCreated := false

	for _, cand := range candidates {
		slog.Debug("正在确认魔王挑战资格", "role_id", s.RoleID(), "boss_id", cand.id, "boss_name", cand.name, "level", cand.level)
		cRes, err := s.BeelzebubTrialsCreateTeam(ctx, cand.id, protocol.BeelzebubTrialsNotLimit)
		if err != nil {
			slog.Debug("创建试炼队伍网络异常，尝试下一个魔王", "role_id", s.RoleID(), "boss_id", cand.id, "err", err)
			continue
		}
		if cRes == protocol.BeelzebubTrialsSuccess {
			targetBossID = cand.id
			bossName = cand.name
			teamCreated = true
			slog.Info("魔王试炼选定挑战目标 (低等级优先锁定)", "role_id", s.RoleID(), "boss_id", targetBossID, "boss_name", bossName, "level", cand.level)
			break
		}
		slog.Debug("魔王当前无法挑战，尝试下一魔王", "role_id", s.RoleID(), "boss_id", cand.id, "boss_name", cand.name, "level", cand.level, "code", cRes)
	}

	if !teamCreated {
		slog.Warn("经从低到高逐级确认，未检测到任何可挑战的魔王，退出魔王试炼", "role_id", s.RoleID())
		return nil, nil
	}

	// 4. 招募助战镜像补齐队伍
	helpers, err := s.BeelzebubTrialsGetHelperRoles(ctx)
	if err != nil {
		slog.Warn("获取助战伙伴列表异常", "err", err)
	} else if len(helpers) > 0 {
		slot := uint8(1)
		for _, h := range helpers {
			if slot > 2 {
				break
			}
			if !h.InBody {
				if _, err := s.BeelzebubTrialsChooseHelper(ctx, h.RoleID, slot); err == nil {
					slog.Info("已成功招募助战镜像加入队伍", "role_id", s.RoleID(), "slot", slot, "helper_name", h.Name)
					slot++
				}
			}
		}
	}

	// 5. 发起战斗 (单向通知指令)
	fRes, err := s.BeelzebubTrialsFight(ctx)
	if err != nil {
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, fmt.Errorf("发起魔王试炼战斗指令失败: %w", err)
	}
	if fRes != protocol.BeelzebubTrialsSuccess {
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, fmt.Errorf("发起战斗服务端返回错误代码: %d", fRes)
	}

	// 等待服务端生成战报
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}

	// 6. 获取战斗结果与收益结算
	warRes, err := s.BeelzebubTrialsGetWarResult(ctx)
	if err != nil {
		slog.Warn("拉取战斗结算数据异常", "err", err)
	} else {
		slog.Info("魔王试炼战斗结算完成",
			"role_id", s.RoleID(),
			"boss_name", bossName,
			"result", warRes.Result,
			"award_fame", warRes.AwardFame,
			"award_coin", warRes.AwardCoin,
			"award_moral", warRes.AwardMoral,
		)
	}

	// 7. 清理离场
	_ = s.BeelzebubTrialsLeaveWar(ctx, 0)
	_ = s.BeelzebubTrialsExitTeam(ctx)

	// 8. 检查并领取道行升级奖励
	if moral, err := s.BeelzebubTrialsOpenMoral(ctx); err == nil && moral != nil {
		if moral.CanClaim && moral.Level > 0 {
			if aRes, err := s.BeelzebubTrialsGetMoralAward(ctx, moral.Level); err == nil && aRes == protocol.BeelzebubTrialsSuccess {
				slog.Info("已成功领取道行等级宝箱奖励", "role_id", s.RoleID(), "moral_lv", moral.Level)
			}
		}
	}

	return warRes, nil
}
