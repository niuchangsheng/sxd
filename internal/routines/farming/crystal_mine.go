// Package farming 提供药园、取经护送/劫镖、仙界矿山、粗铁淬炼池与仙葫芦等资源养成自动化。
//
// 本文件实现仙界矿山自动占矿与抢夺 (Mod_StMine_Base, Module 264)。
package farming

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// NewCrystalMineRoutine 构造仙界矿山任务 (Module 264, Mod_StMine_Base)。
//
// 重要：矿山位于仙界跨服节点，主服会话 264/0 恒返回 FAILED(13)，必须先用
// StcLogin/StTown 建立仙界跨服会话再执行 264 系列动作。
//
// 流程与官方客户端对齐:
//  1. 功能开放门禁 FunctionTypeData 157 (StMine)；
//  2. get_mine_mountain_info(264/0) 查询矿山状态；
//  3. enter_mine_mountain(264/1) 进入当前矿脉并解析玩家矿洞 + 矿洞列表；
//  4. 被夺矿洞在期限内 → recapture_mine_hole(264/5)；
//  5. 有未领取收益 → receive_award(264/7)；
//  6. 未占据矿洞 → 优先占领空矿洞，否则抢夺等级最低的可抢矿洞 (264/3)；
//  7. leave_mine_mountain(264/2) 退出面板（不放弃矿洞）。
//
// 不购买抢夺次数 (264/10)、不清除抢夺冷却 (264/11)、不延长保护时间 (264/12)，
// 全程不消耗元宝。结果码按 Mod_StMine_Base 常量分流。
func NewCrystalMineRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"crystal_mine",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 功能开放门禁：未开放时直接跳过，避免每轮空等超时。
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDStMine); fErr != nil {
				slog.Info("仙界矿山功能开放状态查询失败，本轮跳过",
					"role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("仙界矿山功能未开放，本轮跳过",
					"role_id", session.RoleID(), "function_id", protocol.FunctionIDStMine)
				return nil
			}

			// 仙界矿山位于仙界跨服节点：主服会话 264/0 恒返回 FAILED(13)，
			// 必须通过 StcLogin/StTown 进入仙界跨服城镇后执行 264 系列动作。
			cross, cErr := openCrystalMineSession(ctx, session)
			if cErr != nil {
				slog.Info("仙界矿山跨服会话建立失败，稍后重试",
					"role_id", session.RoleID(), "err", cErr)
				return scheduler.NewCooldown(30*time.Minute, "仙界矿山跨服会话不可用")
			}
			defer cross.Close()
			return runCrystalMine(ctx, cross)
		},
	)
}

// runCrystalMine 在仙界跨服会话上执行矿山查询、进入、领取与抢夺。
func runCrystalMine(ctx context.Context, session *client.RoleSession) error {
	const callTimeout = 8 * time.Second
	caller := kit.NewCaller(ctx, session, callTimeout)
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		resp, err := caller.Call(pkt, act)
		if err != nil {
			slog.Info("仙界矿山协议无回包", "role_id", session.RoleID(),
				"action", act, "err", err)
			return nil
		}
		return resp
	}

	infoResp := call(protocol.BuildStMineInfoPacket(), protocol.ActionIDStMineInfo)
	if infoResp == nil {
		if abort := caller.AbortErr(); abort != nil {
			return abort
		}
		return scheduler.NewCooldown(30*time.Minute, "仙界矿山信息无回包")
	}
	info, err := protocol.ParseStMineInfoResult(infoResp.Payload)
	if err != nil {
		slog.Warn("解析仙界矿山信息失败，稍后重试", "role_id", session.RoleID(), "err", err)
		return scheduler.NewCooldown(30*time.Minute, "仙界矿山信息解析失败")
	}
	slog.Info("仙界矿山状态",
		"role_id", session.RoleID(),
		"result", info.Result,
		"level", info.Level,
		"cur_mountain", info.CurMountain,
	)
	if info.Result == protocol.StMineResultResting {
		// 仍停留在矿脉/矿洞内时先退出休息状态，再重新查询。
		leaveResp := call(protocol.BuildStMineLeavePacket(), protocol.ActionIDStMineLeave)
		if leaveResp == nil {
			return scheduler.NewCooldown(10*time.Minute, "退出矿山休息状态无回包")
		}
		if leaveCode, pErr := protocol.ParseStMineActionResult(leaveResp.Payload); pErr != nil {
			return scheduler.NewCooldown(10*time.Minute, "退出矿山回包解析失败")
		} else if leaveCode != protocol.StMineResultSuccess {
			slog.Info("退出矿山休息状态未成功", "role_id", session.RoleID(), "result", leaveCode)
			return scheduler.NewCooldown(10*time.Minute, "退出矿山休息状态未成功")
		}
		infoResp = call(protocol.BuildStMineInfoPacket(), protocol.ActionIDStMineInfo)
		if infoResp == nil {
			return scheduler.NewCooldown(30*time.Minute, "退出休息后矿山信息无回包")
		}
		if info, err = protocol.ParseStMineInfoResult(infoResp.Payload); err != nil {
			return scheduler.NewCooldown(30*time.Minute, "退出休息后矿山信息解析失败")
		}
		slog.Info("退出休息状态后重新查询矿山",
			"role_id", session.RoleID(), "result", info.Result,
			"level", info.Level, "cur_mountain", info.CurMountain)
	}
	switch info.Result {
	case protocol.StMineResultSuccess:
		// 继续
	case protocol.StMineResultFunctionUnopen, protocol.StMineResultSuperTownClosed:
		slog.Info("仙界矿山未开放，稍后重试", "role_id", session.RoleID(), "result", info.Result)
		return scheduler.NewCooldown(2*time.Hour, "仙界矿山未开放")
	default:
		slog.Info("仙界矿山暂不可进入，稍后重试", "role_id", session.RoleID(), "result", info.Result)
		return scheduler.NewCooldown(time.Hour, "仙界矿山暂不可进入")
	}

	mountain := info.CurMountain
	if mountain == 0 {
		mountain = pickStMineMountain(info)
	}
	enterResp := call(protocol.BuildStMineEnterPacket(mountain), protocol.ActionIDStMineEnter)
	if enterResp == nil {
		return scheduler.NewCooldown(10*time.Minute, "进入矿山无回包")
	}
	holes, err := protocol.ParseStMineEnterResponse(enterResp.Payload)
	if err != nil {
		slog.Warn("解析进入矿山回包失败，稍后重试", "role_id", session.RoleID(), "err", err)
		return scheduler.NewCooldown(10*time.Minute, "进入矿山回包解析失败")
	}
	if holes.Result != protocol.StMineResultSuccess {
		slog.Info("进入仙界矿山未成功", "role_id", session.RoleID(),
			"result", holes.Result, "result_text", protocol.StMineResultText(holes.Result))
		if holes.Result == protocol.StMineResultMountainLevelUnlock {
			return scheduler.NewCooldown(2*time.Hour, "矿脉等级未解锁")
		}
		return scheduler.NewCooldown(time.Hour, "进入仙界矿山未成功")
	}
	defer func() {
		lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
		defer lCancel()
		leaveResp, lErr := session.Call(lCtx, protocol.BuildStMineLeavePacket(), protocol.ActionIDStMineLeave)
		if lErr != nil {
			slog.Debug("离开仙界矿山面板失败", "role_id", session.RoleID(), "err", lErr)
			return
		}
		if leaveResp == nil {
			slog.Debug("离开仙界矿山面板回包为空", "role_id", session.RoleID())
			return
		}
		if code, pErr := protocol.ParseStMineActionResult(leaveResp.Payload); pErr != nil {
			slog.Warn("解析离开仙界矿山回包失败", "role_id", session.RoleID(), "err", pErr)
		} else if code != protocol.StMineResultSuccess {
			slog.Warn("离开仙界矿山返回非成功结果码", "role_id", session.RoleID(),
				"result", code, "result_text", protocol.StMineResultText(code))
		}
	}()
	slog.Info("进入仙界矿山",
		"role_id", session.RoleID(),
		"mountain", mountain,
		"skill_level", holes.SkillLevel,
		"remain_rob_count", holes.RemainRobCount,
		"next_rob_timestamp", holes.NextRobTimestamp,
		"player_hole", holes.PlayerHole.HoleID,
		"player_status", holes.PlayerHole.Status,
		"receive_count", holes.PlayerHole.ReceiveCount,
		"candidates", len(holes.Holes),
	)

	now := int32(time.Now().Unix())

	// 1. 被夺矿洞：期限内抢回。
	if ph := holes.PlayerHole; ph.HoleID > 0 &&
		ph.Status == protocol.StMineHoleBeRobbed &&
		ph.RecaptureTimestamp > now {
		recapResp := call(protocol.BuildStMineRecapturePacket(), protocol.ActionIDStMineRecapture)
		if recapResp == nil {
			return scheduler.NewCooldown(10*time.Minute, "抢回矿洞无回包")
		}
		code, pErr := protocol.ParseStMineActionResult(recapResp.Payload)
		if pErr != nil {
			return scheduler.NewCooldown(10*time.Minute, "抢回矿洞回包解析失败")
		}
		slog.Info("仙界矿山抢回矿洞", "role_id", session.RoleID(),
			"hole_id", ph.HoleID, "result", code, "result_text", protocol.StMineResultText(code))
		if code == protocol.StMineResultSuccess {
			return nil
		}
	}

	// 2. 领取已积累收益。
	if holes.PlayerHole.ReceiveCount > 0 {
		rewardResp := call(protocol.BuildStMineReceiveAwardPacket(), protocol.ActionIDStMineReceiveAward)
		if rewardResp == nil {
			return scheduler.NewCooldown(10*time.Minute, "矿山收益领取无回包")
		}
		code, pErr := protocol.ParseStMineActionResult(rewardResp.Payload)
		if pErr != nil {
			return scheduler.NewCooldown(10*time.Minute, "矿山收益回包解析失败")
		}
		switch code {
		case protocol.StMineResultSuccess:
			slog.Info("仙界矿山收益领取成功", "role_id", session.RoleID(),
				"receive_count", holes.PlayerHole.ReceiveCount)
		case protocol.StMineResultHasNoAward:
			slog.Info("仙界矿山暂无可领取收益", "role_id", session.RoleID())
		default:
			slog.Info("仙界矿山收益未领取", "role_id", session.RoleID(),
				"result", code, "result_text", protocol.StMineResultText(code))
		}
	}

	// 3. 已有矿洞则保持占据，不主动放弃。
	if holes.PlayerHole.HoleID > 0 {
		slog.Info("仙界矿山已占据矿洞，保持现状", "role_id", session.RoleID(),
			"hole_id", holes.PlayerHole.HoleID, "status", holes.PlayerHole.Status)
		return nil
	}

	// 4. 未占据矿洞：检查次数与冷却。
	if holes.RemainRobCount == 0 {
		slog.Info("仙界矿山抢夺次数已用尽", "role_id", session.RoleID())
		return scheduler.NewCooldown(2*time.Hour, "仙界矿山抢夺次数已用尽")
	}
	if holes.NextRobTimestamp > now {
		wait := time.Duration(holes.NextRobTimestamp-now) * time.Second
		if wait < time.Minute {
			wait = time.Minute
		}
		if wait > 2*time.Hour {
			wait = 2 * time.Hour
		}
		slog.Info("仙界矿山抢夺冷却中", "role_id", session.RoleID(),
			"next_rob_timestamp", holes.NextRobTimestamp)
		return scheduler.NewCooldown(wait, "仙界矿山抢夺冷却")
	}

	target, ok := pickStMineHole(holes.Holes, session.PlayerID())
	if !ok {
		slog.Info("仙界矿山暂无可占领矿洞", "role_id", session.RoleID())
		return scheduler.NewCooldown(30*time.Minute, "仙界矿山暂无可占领矿洞")
	}
	robResp := call(protocol.BuildStMineRobHolePacket(target.HoleID, target.PlayerID),
		protocol.ActionIDStMineRobHole)
	if robResp == nil {
		return scheduler.NewCooldown(10*time.Minute, "抢夺矿洞无回包")
	}
	robCode, pErr := protocol.ParseStMineActionResult(robResp.Payload)
	if pErr != nil {
		return scheduler.NewCooldown(10*time.Minute, "抢夺矿洞回包解析失败")
	}
	slog.Info("仙界矿山抢夺矿洞", "role_id", session.RoleID(),
		"hole_id", target.HoleID, "owner_player_id", target.PlayerID,
		"owner_level", target.PlayerLevel, "result", robCode,
		"result_text", protocol.StMineResultText(robCode))
	switch robCode {
	case protocol.StMineResultSuccess:
		// 占领成功后立即领取一次收益，避免可领收益被抢。
		if r := call(protocol.BuildStMineReceiveAwardPacket(), protocol.ActionIDStMineReceiveAward); r != nil {
			if c, e := protocol.ParseStMineActionResult(r.Payload); e == nil {
				slog.Info("仙界矿山占领后领取收益", "role_id", session.RoleID(),
					"result", c, "result_text", protocol.StMineResultText(c))
			}
		}
		return nil
	case protocol.StMineResultAwardHasNotReceive:
		return scheduler.NewCooldown(10*time.Minute, "需先领取当前收益")
	case protocol.StMineResultRobCD:
		wait := 10 * time.Minute
		if holes.NextRobTimestamp > now {
			wait = time.Duration(holes.NextRobTimestamp-now) * time.Second
			if wait < time.Minute {
				wait = time.Minute
			}
		}
		return scheduler.NewCooldown(wait, "仙界矿山抢夺冷却")
	case protocol.StMineResultRobCountLimit:
		return scheduler.NewCooldown(2*time.Hour, "仙界矿山抢夺次数已用尽")
	default:
		slog.Info("仙界矿山抢夺未成功，稍后重试", "role_id", session.RoleID(),
			"result", robCode, "result_text", protocol.StMineResultText(robCode))
		return scheduler.NewCooldown(30*time.Minute, "仙界矿山抢夺未成功")
	}
}

// pickStMineMountain 选择要进入的矿脉：优先有空位的最高等级矿脉，
// 否则回退到与角色矿山等级一致的矿脉，再否则取最高等级矿脉。
func pickStMineMountain(info *protocol.StMineInfo) uint8 {
	var freeBest, levelBest, anyBest uint8
	for _, m := range info.Mountains {
		if m.Level == 0 {
			continue
		}
		if m.UsedCount < m.TotalCount && m.Level > freeBest {
			freeBest = m.Level
		}
		if m.Level == info.Level {
			levelBest = m.Level
		}
		if m.Level > anyBest {
			anyBest = m.Level
		}
	}
	if freeBest > 0 {
		return freeBest
	}
	if levelBest > 0 {
		return levelBest
	}
	if anyBest > 0 {
		return anyBest
	}
	return 1
}

// openCrystalMineSession 建立仙界跨服会话；单测可替换为复用 mock 会话。
var openCrystalMineSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenCrossServerSession(ctx)
}

// pickStMineHole 选择可占领矿洞：优先空矿洞，其次等级最低的可抢矿洞。
func pickStMineHole(holes []protocol.StMineHole, selfPlayerID int32) (protocol.StMineHole, bool) {
	var empty *protocol.StMineHole
	var weakest *protocol.StMineHole
	for i := range holes {
		h := &holes[i]
		if h.HoleID == 0 {
			continue
		}
		if selfPlayerID > 0 && h.PlayerID == selfPlayerID {
			continue
		}
		if h.Status == protocol.StMineHoleEmpty || h.PlayerID == 0 {
			if empty == nil || h.HoleID < empty.HoleID {
				empty = h
			}
			continue
		}
		if h.Status == protocol.StMineHoleRob || h.Status == protocol.StMineHoleCanBeRob {
			if weakest == nil || h.PlayerLevel < weakest.PlayerLevel {
				weakest = h
			}
		}
	}
	if empty != nil {
		return *empty, true
	}
	if weakest != nil {
		return *weakest, true
	}
	return protocol.StMineHole{}, false
}
