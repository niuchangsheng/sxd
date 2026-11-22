// 本文件实现主角法相 (Mod_MainRoleFaXiang_Base, Module 518) 自动化。
//
// 抓包基准: /tmp/faxiang3.pcapng (2026-11-21)
//
//	518/11 造物佛手面板 → 518/17 领取炼化奖励 → 518/13 祈愿(随机 3 个奖励)
//	→ 518/15 选择奖励并开始炼化 → 518/12 守护魔兽面板 → 518/16 挑战
//
// 造物佛手：炼化奖励由客户端免费祈愿产生，但「选择并炼化」按游戏规则消耗道缘
// (MainRoleFaXiangType.extraPrayAwardList.cost_dao_yuan)；本例程按单位道缘熟练度
// 收益挑选返回答复中的奖励，且单次最多消耗 1000 道缘，超出则保留候选等待人工处理。
// 绝不调用 518/14 元宝重随，也不消耗元宝。
//
// 守护魔兽：挑战免费，服务端以 next_can_fight_time 控制冷却；冷却中回包
// HAS_CHALLENGE(22)，直接结束本轮，不购买次数。佛/魔两侧相互独立：佛侧失败
// 不会阻止守护魔兽挑战。
package minigame

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

const faxiangCallTimeout = 15 * time.Second

// faxiangMaxAutoDaoYuan 单次自动「选择并炼化」允许消耗的道缘上限。
// 超过上限时保留服务端返回的候选奖励，等待人工选择，避免无人值守时
// 一次消耗 2000–6000 道缘。
const faxiangMaxAutoDaoYuan int32 = 1000

// faxiangPrayAward 炼化奖励配置，来源 MainRoleFaXiangType.extraPrayAwardList。
type faxiangPrayAward struct {
	costDaoYuan int32
	addExp      int32
}

var faxiangPrayAwards = map[int32]faxiangPrayAward{
	1:  {costDaoYuan: 1000, addExp: 760},
	2:  {costDaoYuan: 700, addExp: 280},
	3:  {costDaoYuan: 600, addExp: 208},
	4:  {costDaoYuan: 400, addExp: 120},
	5:  {costDaoYuan: 400, addExp: 172},
	6:  {costDaoYuan: 500, addExp: 72},
	8:  {costDaoYuan: 400, addExp: 72},
	9:  {costDaoYuan: 600, addExp: 88},
	12: {costDaoYuan: 2000, addExp: 152},
	14: {costDaoYuan: 500, addExp: 88},
	15: {costDaoYuan: 100, addExp: 760},
	16: {costDaoYuan: 5000, addExp: 584},
	17: {costDaoYuan: 6000, addExp: 752},
	18: {costDaoYuan: 3000, addExp: 920},
}

// pickFaxiangPrayAward 在祈愿返回的奖励中挑选单位道缘熟练度最高者。
// 仅考虑已知且 cost_dao_yuan <= faxiangMaxAutoDaoYuan 的奖励；全部未知时
// 按原始顺序兜底，全部已知但均超上限时返回 0。
func pickFaxiangPrayAward(ids []int32) int32 {
	if len(ids) == 0 {
		return 0
	}
	best := ids[0]
	bestRate := float64(0)
	known := false
	affordable := false
	for _, id := range ids {
		award, ok := faxiangPrayAwards[id]
		if !ok {
			continue
		}
		known = true
		if award.costDaoYuan > faxiangMaxAutoDaoYuan {
			continue
		}
		affordable = true
		rate := float64(award.addExp) / float64(award.costDaoYuan)
		if rate > bestRate {
			bestRate = rate
			best = id
		}
	}
	if known && !affordable {
		return 0
	}
	return best
}

// faxiangRefineCooldown 按服务端下发的炼化完成时间安排下一次复查。
// 炼化通常需要 1–2 天（奖励表 cost_time 受熟练度等级缩短），无需每小时轮询。
func faxiangRefineCooldown(finishTime int32) error {
	if finishTime <= 0 {
		return scheduler.NewCooldown(time.Hour, "法相炼化进行中")
	}
	until := time.Unix(int64(finishTime), 0).Add(time.Minute)
	if until.Before(time.Now()) {
		until = time.Now().Add(time.Minute)
	}
	return scheduler.NewCooldownUntil(until, "法相炼化进行中")
}

// earliestFaxiangCooldown 合并佛/魔两侧的冷却，返回最早需要复查的时间点。
func earliestFaxiangCooldown(errs ...error) error {
	var best *scheduler.CooldownError
	for _, err := range errs {
		if err == nil {
			continue
		}
		var cd *scheduler.CooldownError
		if !errors.As(err, &cd) {
			return err
		}
		if best == nil || cd.Until.Before(best.Until) {
			best = cd
		}
	}
	if best != nil {
		return best
	}
	return nil
}

// faxiangMoCooldown 按守护魔兽 next_can_fight_time 安排下一次挑战。
func faxiangMoCooldown(nextFightTime int32) error {
	if nextFightTime <= 0 {
		return nil
	}
	until := time.Unix(int64(nextFightTime), 0).Add(time.Minute)
	if until.Before(time.Now()) {
		return nil
	}
	return scheduler.NewCooldownUntil(until, "法相守护魔兽冷却中")
}

// faxiangCall 统一封装带超时的 518 请求。
type faxiangCall func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet

// NewFaxiangRoutine 构造法相（造物佛手 / 守护魔兽）日常任务。
func NewFaxiangRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"faxiang_trial",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			caller := kit.NewCaller(ctx, session, faxiangCallTimeout)
			call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
				resp, err := caller.Call(pkt, act)
				if err != nil {
					slog.Info("法相协议无回包", "role_id", session.RoleID(), "name", name, "action", act, "err", err)
					return nil
				}
				return resp
			}

			// 官方客户端先打开法相主面板，再按开放状态请求 518/11、518/12。
			// 主面板无回包不阻塞后续子面板：部分会话状态下子面板仍可独立查询。
			if panelResp := call(protocol.BuildFaxiangGetPanelInfoPacket(),
				protocol.ActionIDFaxiangGetPanelInfo, "法相主面板"); panelResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
				slog.Info("法相主面板无回包，继续尝试子面板", "role_id", session.RoleID())
			} else {
				slog.Info("法相主面板已加载", "role_id", session.RoleID(), "payload_len", len(panelResp.Payload))
			}

			pendingRefine, foErr := runFaxiangFo(session, call)
			if abortErr := caller.AbortErr(); abortErr != nil {
				return abortErr
			}
			if errors.Is(foErr, scheduler.ErrBagFull) {
				return foErr
			}
			moErr := runFaxiangMo(ctx, session, call)
			if abortErr := caller.AbortErr(); abortErr != nil {
				return abortErr
			}
			if errors.Is(moErr, scheduler.ErrBagFull) {
				return moErr
			}
			if retry := earliestFaxiangCooldown(foErr, moErr); retry != nil {
				return retry
			}
			if pendingRefine {
				return faxiangRefineCooldown(0)
			}
			return nil
		},
	)
}

// runFaxiangFo 执行造物佛手：领奖 →（必要时）祈愿 → 选择炼化。
// 返回是否存在进行中的炼化，以及需要调度器延后重试的错误。
func runFaxiangFo(session *client.RoleSession, call faxiangCall) (bool, error) {
	foResp := call(protocol.BuildFaxiangExtraFoInfoPacket(), protocol.ActionIDFaxiangExtraFoInfo, "造物佛手面板")
	if foResp == nil {
		return false, scheduler.NewCooldown(30*time.Minute, "法相造物佛手面板无回包")
	}
	fo, err := protocol.ParseFaxiangExtraFoInfo(foResp.Payload)
	if err != nil {
		slog.Warn("法相造物佛手面板解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
		return false, scheduler.NewCooldown(30*time.Minute, "法相造物佛手面板解析失败")
	}
	slog.Info("法相造物佛手面板",
		"role_id", session.RoleID(), "level", fo.Level, "exp", fo.CurExp,
		"pending_awards", fo.AwardList, "cur_award_id", fo.CurPrayAwardID,
		"refine_finish_time", fo.RefineFinishTime)

	pendingRefine := fo.CurPrayAwardID > 0

	// 1. 有未领取的炼化奖励时先尝试领取；服务器会以 NOT_PRAY_AWARD(29)
	//    明确告知炼化尚未完成，不能凭时间戳本地猜测。
	if pendingRefine {
		claimResp := call(protocol.BuildFaxiangGainPrayAwardPacket(), protocol.ActionIDFaxiangGainPrayAward, "领取炼化奖励")
		if claimResp == nil {
			return pendingRefine, scheduler.NewCooldown(30*time.Minute, "法相领取炼化奖励无回包")
		}
		code, cErr := protocol.ParseFaxiangGainPrayAwardResult(claimResp.Payload)
		if cErr != nil {
			slog.Warn("法相领取炼化奖励回包解析失败", "role_id", session.RoleID(), "err", cErr)
			return pendingRefine, scheduler.NewCooldown(30*time.Minute, "法相领取炼化奖励回包解析失败")
		}
		switch code {
		case protocol.FaxiangSuccess:
			slog.Info("法相炼化奖励领取成功",
				"role_id", session.RoleID(), "award_id", fo.CurPrayAwardID)
			pendingRefine = false
		case protocol.FaxiangNotPrayAward, protocol.FaxiangRefining:
			slog.Info("法相炼化尚未完成",
				"role_id", session.RoleID(), "result", code, "result_text", protocol.FaxiangResultText(code))
			return true, faxiangRefineCooldown(fo.RefineFinishTime)
		case protocol.FaxiangPackageFull:
			return pendingRefine, scheduler.ErrBagFull
		case protocol.FaxiangFunctionNoOpen:
			slog.Info("法相功能未开放", "role_id", session.RoleID())
			return pendingRefine, scheduler.NewCooldown(6*time.Hour, "法相功能未开放")
		default:
			slog.Warn("法相领取炼化奖励失败，本轮结束",
				"role_id", session.RoleID(), "result", code, "result_text", protocol.FaxiangResultText(code))
			return pendingRefine, scheduler.NewCooldown(30*time.Minute, "法相领取炼化奖励失败")
		}
	}

	// 2. 没有进行中的炼化时：优先选择已祈愿的奖励，否则先祈愿再选择。
	if !pendingRefine {
		awards := fo.AwardList
		if len(awards) == 0 {
			prayResp := call(protocol.BuildFaxiangExtraPrayPacket(), protocol.ActionIDFaxiangExtraPray, "祈愿炼化奖励")
			if prayResp == nil {
				return false, scheduler.NewCooldown(30*time.Minute, "法相祈愿无回包")
			}
			opts, pErr := protocol.ParseFaxiangExtraPrayResult(prayResp.Payload)
			if pErr != nil {
				slog.Warn("法相祈愿回包解析失败", "role_id", session.RoleID(), "err", pErr)
				return false, scheduler.NewCooldown(30*time.Minute, "法相祈愿回包解析失败")
			}
			switch opts.Result {
			case protocol.FaxiangSuccess:
				awards = opts.AwardIDs
			case protocol.FaxiangCanNotPray, protocol.FaxiangRefining:
				slog.Info("法相当前不可祈愿，按炼化中处理",
					"role_id", session.RoleID(), "result", opts.Result)
				return true, faxiangRefineCooldown(fo.RefineFinishTime)
			case protocol.FaxiangPackageFull:
				return false, scheduler.ErrBagFull
			case protocol.FaxiangFunctionNoOpen:
				return false, scheduler.NewCooldown(6*time.Hour, "法相功能未开放")
			default:
				slog.Warn("法相祈愿失败",
					"role_id", session.RoleID(), "result", opts.Result,
					"result_text", protocol.FaxiangResultText(opts.Result))
				return false, scheduler.NewCooldown(30*time.Minute, "法相祈愿失败")
			}
		}

		if len(awards) == 0 {
			return false, scheduler.NewCooldown(30*time.Minute, "法相祈愿未返回奖励")
		}
		pick := pickFaxiangPrayAward(awards)
		if pick == 0 {
			slog.Info("法相祈愿奖励均超过自动炼化道缘上限，保留候选等待人工处理",
				"role_id", session.RoleID(), "award_list", awards, "max_dao_yuan", faxiangMaxAutoDaoYuan)
			return false, scheduler.NewCooldown(6*time.Hour, "法相炼化奖励超出自动消耗上限")
		}
		chooseResp := call(protocol.BuildFaxiangExtraChoosePrayAwardPacket(pick),
			protocol.ActionIDFaxiangExtraChoosePrayAward, "选择炼化奖励")
		if chooseResp == nil {
			return false, scheduler.NewCooldown(30*time.Minute, "法相选择炼化奖励无回包")
		}
		chosen, chErr := protocol.ParseFaxiangChoosePrayAwardResult(chooseResp.Payload)
		if chErr != nil {
			slog.Warn("法相选择炼化奖励回包解析失败", "role_id", session.RoleID(), "err", chErr)
			return false, scheduler.NewCooldown(30*time.Minute, "法相选择炼化奖励回包解析失败")
		}
		switch chosen.Result {
		case protocol.FaxiangSuccess:
			award := faxiangPrayAwards[pick]
			slog.Info("法相已选择奖励并开始炼化",
				"role_id", session.RoleID(), "award_id", pick,
				"cost_dao_yuan", award.costDaoYuan, "add_exp", award.addExp,
				"refine_finish_time", chosen.RefineFinishTime)
			return true, faxiangRefineCooldown(chosen.RefineFinishTime)
		case protocol.FaxiangNotEnoughDaoYuan:
			slog.Info("法相炼化道缘不足，暂停造物佛手",
				"role_id", session.RoleID(), "award_id", pick)
			return false, scheduler.NewCooldown(2*time.Hour, "法相炼化道缘不足")
		case protocol.FaxiangCanNotPray, protocol.FaxiangRefining:
			slog.Info("法相已有炼化进行中",
				"role_id", session.RoleID(), "result", chosen.Result)
			return true, faxiangRefineCooldown(fo.RefineFinishTime)
		case protocol.FaxiangPackageFull:
			return false, scheduler.ErrBagFull
		case protocol.FaxiangFunctionNoOpen:
			return false, scheduler.NewCooldown(6*time.Hour, "法相功能未开放")
		default:
			slog.Warn("法相选择炼化奖励失败",
				"role_id", session.RoleID(), "result", chosen.Result,
				"result_text", protocol.FaxiangResultText(chosen.Result))
			return false, scheduler.NewCooldown(30*time.Minute, "法相选择炼化奖励失败")
		}
	}

	return pendingRefine, nil
}

// runFaxiangMo 执行守护魔兽面板查询与免费挑战。挑战冷却/失败只记录日志，
// 不购买次数、不消耗元宝。
func runFaxiangMo(ctx context.Context, session *client.RoleSession, call faxiangCall) error {
	moResp := call(protocol.BuildFaxiangExtraMoInfoPacket(), protocol.ActionIDFaxiangExtraMoInfo, "守护魔兽面板")
	if moResp == nil {
		// 面板被静默丢弃时不能 return nil：ScheduleCron 会等到下一个 cron 时段。
		return scheduler.HandleSilentDropCtx(ctx, session, "faxiang_mo", 3,
			30*time.Minute, "法相守护魔兽面板无回包")
	}
	mo, mErr := protocol.ParseFaxiangExtraMoInfo(moResp.Payload)
	if mErr != nil {
		slog.Warn("法相守护魔兽面板解析失败", "role_id", session.RoleID(), "err", mErr)
		return scheduler.NewCooldown(10*time.Minute, "法相守护魔兽面板解析失败")
	}
	slog.Info("法相守护魔兽面板",
		"role_id", session.RoleID(), "level", mo.Level, "exp", mo.CurExp,
		"next_can_fight_time", mo.NextCanFightTime)
	// next_can_fight_time 已明确冷却时不再发挑战请求，直接按该时间挂起。
	if cooldown := faxiangMoCooldown(mo.NextCanFightTime); cooldown != nil {
		slog.Info("法相守护魔兽冷却中，跳过挑战请求",
			"role_id", session.RoleID(), "next_can_fight_time", mo.NextCanFightTime)
		return cooldown
	}
	challengeResp := call(protocol.BuildFaxiangExtraMoChallengePacket(),
		protocol.ActionIDFaxiangExtraMoChallenge, "守护魔兽挑战")
	if challengeResp == nil {
		return scheduler.HandleSilentDropCtx(ctx, session, "faxiang_mo", 3,
			30*time.Minute, "法相守护魔兽挑战无回包")
	}
	res, rErr := protocol.ParseFaxiangExtraMoChallengeResult(challengeResp.Payload)
	if rErr != nil {
		slog.Warn("法相守护魔兽挑战回包解析失败", "role_id", session.RoleID(), "err", rErr)
		return scheduler.NewCooldown(10*time.Minute, "法相守护魔兽挑战回包解析失败")
	}
	switch res.Result {
	case protocol.FaxiangSuccess:
		// 只有业务动作真正成功才清零连续丢包计数。
		scheduler.ResetSilentDropsCtx(ctx, session, "faxiang_mo")
		slog.Info("法相守护魔兽挑战完成",
			"role_id", session.RoleID(), "kill_num", res.KillNum)
	case protocol.FaxiangHasChallenge, protocol.FaxiangTimeLimit, protocol.FaxiangFailCD:
		slog.Info("法相守护魔兽挑战冷却中，重新查询下次可挑战时间",
			"role_id", session.RoleID(), "result", res.Result,
			"result_text", protocol.FaxiangResultText(res.Result))
		// 挑战回包不含冷却时间，重新查询面板读取 next_can_fight_time。
		if refreshed := call(protocol.BuildFaxiangExtraMoInfoPacket(),
			protocol.ActionIDFaxiangExtraMoInfo, "守护魔兽面板"); refreshed != nil {
			if mo2, err := protocol.ParseFaxiangExtraMoInfo(refreshed.Payload); err == nil {
				if cooldown := faxiangMoCooldown(mo2.NextCanFightTime); cooldown != nil {
					return cooldown
				}
			}
		}
		return scheduler.NewCooldown(30*time.Minute, "法相守护魔兽挑战冷却中")
	case protocol.FaxiangFunctionNoOpen:
		slog.Info("法相守护魔兽未开放，2 小时后再试", "role_id", session.RoleID())
		return scheduler.NewCooldown(2*time.Hour, "法相守护魔兽未开放")
	default:
		slog.Info("法相守护魔兽挑战未成功",
			"role_id", session.RoleID(), "result", res.Result,
			"result_text", protocol.FaxiangResultText(res.Result))
	}
	return nil
}
