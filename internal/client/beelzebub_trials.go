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

// ErrBeelzebubTrialsNoResponse 表示魔王试炼请求被服务端静默丢弃：
// 等待期届满仍无任何回包（既没有结果码也没有截断回包）。
//
// 与普通的业务失败码不同，静默丢弃通常意味着服务端拒绝解析该请求
// （典型场景：角色等级/资格不满足、服务端版本不支持该动作、网关校验失败），
// 对同一角色换任意关卡或载荷形态重试都不会恢复，必须直接熔断本轮试炼。
var ErrBeelzebubTrialsNoResponse = errors.New("beelzebub trials request silently dropped by server")

const (
	// beelzebubFightSettleDelay 战斗指令发出后等待服务端生成战报的结算时间。
	beelzebubFightSettleDelay = 500 * time.Millisecond
	// beelzebubTeamSettleDelay 两关之间等待服务端同步队伍/战场状态的时间。
	beelzebubTeamSettleDelay = 200 * time.Millisecond
	// beelzebubSilentDropThreshold 建队请求被静默丢弃多少次后熔断本轮试炼。
	//
	// 2026-11-04/11 实机验证：s0 服 (等级 300 角色) 对 create_team 的任意
	// 载荷形态/任意 bossID 均静默丢弃，而同连接内 Action 1/3/6/7/8/20/21/22/23
	// 全部正常回包。该拒绝由服务端账号/版本状态决定，客户端无法通过换载荷或
	// 换关卡恢复；首次无回包即熔断，避免遍历全部关卡放大无效请求。
	// 详见 docs/protocol-spec.md 4.12 节。
	beelzebubSilentDropThreshold = 1
)

// beelzebubCandidate 魔王试炼可挑战关卡候选。
type beelzebubCandidate struct {
	id    int32
	level int32
	name  string
}

// beelzebubMaxChallenges 单次运行的挑战次数安全上限，防止服务端列表异常导致死循环。
const beelzebubMaxChallenges = 256

// BeelzebubTrialsAutoChallengeAll 全自动遍历可挑战魔王列表，逐关完成挑战闭环。
//
// 与原单关版本的差异：原逻辑在建队成功后即 break，导致可挑战列表中的其余关卡永远不会被挑战。
// 本方法会对每个关卡依次执行「建队 -> 招募助战 -> 发起战斗 -> 拉取战报 -> 离场 -> 退队」，
// 并在每关结束以及列表耗尽时重新拉取可挑战列表，合并挑战过程中新解锁的隐藏/神秘层关卡，
// 直到列表不再增长或挑战次数耗尽。单关失败不会中断后续关卡，返回已完成的战报列表。
func (s *RoleSession) BeelzebubTrialsAutoChallengeAll(ctx context.Context, preferBossID int32) ([]*protocol.BeelzebubTrialsWarResult, error) {
	// 清理上一轮/上一会话可能遗留的队伍与战场。
	// 服务端对“已在队伍中”的 create_team 会静默丢弃；先退队可避免被误判为玩法被拒。
	if teamID, _, tErr := s.BeelzebubTrialsGetPlayerTeamInfo(ctx); tErr != nil {
		// 连队伍信息都拿不到说明当前不在可建队的场景/状态；继续发 create_team 只会被静默丢弃。
		slog.Info("查询魔王试炼队伍状态无回包，本轮跳过", "role_id", s.RoleID(), "err", tErr)
		return nil, nil
	} else if teamID > 0 {
		slog.Info("发现遗留魔王试炼队伍，先退队再开始挑战", "role_id", s.RoleID(), "team_id", teamID)
		_ = s.BeelzebubTrialsExitTeam(ctx)
		_ = s.BeelzebubTrialsLeaveWar(ctx, 0)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(beelzebubTeamSettleDelay):
		}
	}

	canChallenge, err := s.BeelzebubTrialsCheckCanChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查魔王试炼挑战状态失败: %w", err)
	}
	if !canChallenge {
		slog.Info("今日魔王试炼挑战次数已用尽，无需重复挑战", "role_id", s.RoleID())
		s.claimBeelzebubMoralAward(ctx)
		return nil, nil
	}

	// 预检面板关卡状态：官方客户端以 [已挑战次数, 今日已挑战标记] 判定关卡是否完成
	// (今日已挑战 或 累计次数 >= maxTimes)，完成后的战斗不再产出奖励。
	// Action 20 / 22 只反映活动可用与等级解锁，无法用于判定剩余收益，故以此面板为准。
	panelStatus := make(map[int32]protocol.BeelzebubBossStatus)
	panelUsable := false
	if openData, oErr := s.BeelzebubTrialsOpen(ctx); oErr != nil {
		slog.Warn("打开魔王试炼面板失败，跳过关卡可用性预检", "role_id", s.RoleID(), "error", oErr)
	} else if openData != nil && len(openData.Bosses) > 0 {
		panelUsable = true
		available := 0
		for _, b := range openData.Bosses {
			panelStatus[b.BossID] = b
			if !b.Finished() {
				available++
			}
		}
		if available == 0 {
			slog.Info("魔王试炼今日已全部完成或累计次数已达上限，跳过本轮挑战",
				"role_id", s.RoleID(), "panel_boss_count", len(openData.Bosses))
			s.claimBeelzebubMoralAward(ctx)
			return nil, nil
		}
		slog.Debug("魔王试炼关卡可用性预检完成",
			"role_id", s.RoleID(), "available", available, "total", len(openData.Bosses))
	}

	canList, err := s.BeelzebubTrialsGetCanChallengeList(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取可挑战魔王列表失败: %w", err)
	}
	if len(canList) == 0 {
		slog.Warn("未检测到可挑战的魔王列表，退出魔王试炼", "role_id", s.RoleID())
		s.claimBeelzebubMoralAward(ctx)
		return nil, nil
	}

	// queue 保存已发现的全部关卡 ID；seen 记录已完成（含失败）的关卡。
	// 面板明确标记已完成的关卡直接排除；面板未出现的关卡（如清关后动态解锁的
	// 神秘层）保持可挑战，避免误伤新解锁内容。
	queue := append([]int32(nil), canList...)
	if panelUsable && len(panelStatus) > 0 {
		filtered := beelzebubFilterAvailableBosses(queue, panelStatus)
		if skipped := len(queue) - len(filtered); skipped > 0 {
			slog.Info("魔王试炼跳过今日已完成或次数已达上限的关卡",
				"role_id", s.RoleID(), "skipped", skipped, "remain_candidates", len(filtered))
		}
		queue = filtered
	}
	seen := make(map[int32]bool, len(queue))
	results := make([]*protocol.BeelzebubTrialsWarResult, 0, len(queue))
	slog.Info("魔王试炼开始逐关挑战", "role_id", s.RoleID(), "total", len(queue))

	// silentDrops 记录连续被服务端静默丢弃的建队请求次数；成功或业务失败码会清零。
	silentDrops := 0

	for attempts := 0; attempts < beelzebubMaxChallenges; {
		if ctx.Err() != nil {
			break
		}
		// 会话重连期间继续发包必然超时；直接结束本轮，等待下次调度。
		if s.State() != StateActive {
			slog.Warn("魔王试炼会话已断开，终止本轮", "role_id", s.RoleID(), "state", s.State().String())
			break
		}

		// 每关开始前确认剩余挑战次数，避免次数耗尽后仍盲目发起战斗。
		ok, cErr := s.BeelzebubTrialsCheckCanChallenge(ctx)
		if cErr != nil {
			// 服务端静默丢弃复检请求时无法确认剩余次数，按本轮结束处理，等待下次调度。
			slog.Info("复检魔王试炼挑战次数无回包，停止后续关卡", "role_id", s.RoleID(), "error", cErr)
			break
		}
		if !ok {
			slog.Info("今日魔王试炼挑战次数已耗尽，逐关挑战结束",
				"role_id", s.RoleID(), "completed", len(results), "discovered", len(queue))
			break
		}

		cand, found := nextBeelzebubCandidate(queue, seen, preferBossID)
		if !found {
			// 当前已知关卡已全部挑战完毕：刷新列表探测新解锁的隐藏/神秘层关卡。
			fresh, fErr := s.BeelzebubTrialsGetCanChallengeList(ctx)
			if fErr != nil {
				slog.Warn("刷新魔王试炼可挑战列表失败，逐关挑战结束", "role_id", s.RoleID(), "error", fErr)
				break
			}
			if panelUsable && len(panelStatus) > 0 {
				fresh = beelzebubFilterAvailableBosses(fresh, panelStatus)
			}
			added, newIDs := mergeBeelzebubCandidates(&queue, seen, fresh)
			if added == 0 {
				break
			}
			slog.Info("魔王试炼检测到新解锁关卡",
				"role_id", s.RoleID(), "count", added, "boss_ids", newIDs, "discovered", len(queue))
			continue
		}

		seen[cand.id] = true
		attempts++

		warRes, cErr := s.beelzebubChallengeOne(ctx, cand)
		if cErr != nil {
			if errors.Is(cErr, ErrBeelzebubTrialsNoResponse) {
				silentDrops++
				if silentDrops >= beelzebubSilentDropThreshold {
					slog.Info("魔王试炼建队请求被服务端静默丢弃，判定当前状态不满足，终止本轮",
						"role_id", s.RoleID(),
						"consecutive_drops", silentDrops,
						"boss_id", cand.id,
						"boss_name", cand.name,
						"error", cErr,
					)
					return results, fmt.Errorf("建队请求连续 %d 次无任何回包，疑似服务端资格/协议限制: %w",
						silentDrops, cErr)
				}
			} else {
				silentDrops = 0
			}
			slog.Warn("魔王试炼关卡挑战失败，继续尝试后续关卡",
				"role_id", s.RoleID(),
				"boss_id", cand.id,
				"boss_name", cand.name,
				"level", cand.level,
				"error", cErr,
			)
		} else if warRes != nil {
			silentDrops = 0
			results = append(results, warRes)
		}

		// 每关结束后立即刷新列表，尽早纳入新解锁的隐藏/神秘层关卡。
		if fresh, fErr := s.BeelzebubTrialsGetCanChallengeList(ctx); fErr == nil {
			if panelUsable && len(panelStatus) > 0 {
				fresh = beelzebubFilterAvailableBosses(fresh, panelStatus)
			}
			if added, newIDs := mergeBeelzebubCandidates(&queue, seen, fresh); added > 0 {
				slog.Info("魔王试炼检测到新解锁关卡",
					"role_id", s.RoleID(), "count", added, "boss_ids", newIDs, "discovered", len(queue))
			}
		}

		select {
		case <-ctx.Done():
		case <-time.After(beelzebubTeamSettleDelay):
		}
	}

	s.claimBeelzebubMoralAward(ctx)
	return results, nil
}

// nextBeelzebubCandidate 从已发现关卡中按等级顺序选出下一个尚未挑战的关卡。
func nextBeelzebubCandidate(ids []int32, seen map[int32]bool, preferBossID int32) (beelzebubCandidate, bool) {
	for _, cand := range orderBeelzebubCandidates(ids, preferBossID) {
		if !seen[cand.id] {
			return cand, true
		}
	}
	return beelzebubCandidate{}, false
}

// beelzebubFilterAvailableBosses 依据面板关卡状态过滤关卡 ID。
//
// 仅剔除面板中明确标记已完成（今日已挑战或累计次数达上限）的关卡；
// 面板未收录的关卡保持可挑战，以保留清关后动态解锁的神秘层关卡。
func beelzebubFilterAvailableBosses(ids []int32, status map[int32]protocol.BeelzebubBossStatus) []int32 {
	if len(status) == 0 {
		return ids
	}
	filtered := make([]int32, 0, len(ids))
	for _, id := range ids {
		if st, ok := status[id]; ok && st.Finished() {
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered
}

// mergeBeelzebubCandidates 将刷新回包中新出现的关卡并入 queue，返回新增数量与新关卡 ID。
func mergeBeelzebubCandidates(queue *[]int32, seen map[int32]bool, fresh []int32) (int, []int32) {
	existing := make(map[int32]bool, len(*queue))
	for _, id := range *queue {
		existing[id] = true
	}

	added := 0
	var newIDs []int32
	for _, id := range fresh {
		if id <= 0 || seen[id] || existing[id] {
			continue
		}
		*queue = append(*queue, id)
		existing[id] = true
		newIDs = append(newIDs, id)
		added++
	}
	return added, newIDs
}

// beelzebubChallengeOne 完成单个魔王关卡的挑战闭环。
func (s *RoleSession) beelzebubChallengeOne(ctx context.Context, cand beelzebubCandidate) (*protocol.BeelzebubTrialsWarResult, error) {
	cRes, err := s.BeelzebubTrialsCreateTeam(ctx, cand.id, protocol.BeelzebubTrialsNotLimit)
	if errors.Is(err, ErrBeelzebubTrialsNoResponse) {
		// 静默丢弃属于协议/资格层面的确定性拒绝：换关卡或重发同样的建队包都不会恢复，
		// 这里直接上抛，由 AutoChallengeAll 累计后熔断，避免多余退队与重试耗时。
		return nil, fmt.Errorf("创建试炼队伍失败: %w", err)
	}
	if err != nil || cRes != protocol.BeelzebubTrialsSuccess {
		// 上一关清理不彻底时服务端可能仍视角色处于队伍中，先退队再重试一次。
		_ = s.BeelzebubTrialsExitTeam(ctx)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(beelzebubTeamSettleDelay):
		}
		cRes, err = s.BeelzebubTrialsCreateTeam(ctx, cand.id, protocol.BeelzebubTrialsNotLimit)
		if err != nil {
			return nil, fmt.Errorf("创建试炼队伍失败: %w", err)
		}
		if cRes != protocol.BeelzebubTrialsSuccess {
			return nil, fmt.Errorf("创建试炼队伍被服务端拒绝 (code=%d)", cRes)
		}
	}
	slog.Info("魔王试炼已锁定关卡目标", "role_id", s.RoleID(), "boss_id", cand.id, "boss_name", cand.name, "level", cand.level)

	// 招募助战镜像补齐二号位与三号位。
	if helpers, hErr := s.BeelzebubTrialsGetHelperRoles(ctx); hErr != nil {
		slog.Warn("获取助战伙伴列表异常", "role_id", s.RoleID(), "boss_id", cand.id, "error", hErr)
	} else {
		slot := uint8(1)
		for _, h := range helpers {
			if slot > 2 {
				break
			}
			if h.InBody {
				continue
			}
			if _, cErr := s.BeelzebubTrialsChooseHelper(ctx, h.RoleID, slot); cErr == nil {
				slog.Info("已成功招募助战镜像加入队伍",
					"role_id", s.RoleID(), "boss_id", cand.id, "slot", slot, "helper_name", h.Name)
				slot++
			}
		}
	}

	// 发起战斗 (Action 13 为单向通知指令，战报由 Action 14 异步拉取)。
	if fRes, fErr := s.BeelzebubTrialsFight(ctx); fErr != nil {
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, fmt.Errorf("发起魔王试炼战斗指令失败: %w", fErr)
	} else if fRes != protocol.BeelzebubTrialsSuccess {
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, fmt.Errorf("发起战斗服务端返回错误代码: %d", fRes)
	}

	select {
	case <-ctx.Done():
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, ctx.Err()
	case <-time.After(beelzebubFightSettleDelay):
	}

	warRes, wErr := s.BeelzebubTrialsGetWarResult(ctx)

	// 无论结算是否成功，都清理战场与队伍，保证下一关能正常建队。
	_ = s.BeelzebubTrialsLeaveWar(ctx, 0)
	_ = s.BeelzebubTrialsExitTeam(ctx)

	if wErr != nil {
		return nil, fmt.Errorf("拉取战斗结算数据失败: %w", wErr)
	}

	result := uint8(0)
	if warRes != nil {
		result = warRes.Result
	}
	slog.Info("魔王试炼关卡挑战结算完成",
		"role_id", s.RoleID(),
		"boss_id", cand.id,
		"boss_name", cand.name,
		"level", cand.level,
		"result", result,
	)
	return warRes, nil
}

// orderBeelzebubCandidates 按等级从低到高排序候选关卡；显式指定 preferBossID 时优先尝试。
func orderBeelzebubCandidates(canList []int32, preferBossID int32) []beelzebubCandidate {
	candidates := make([]beelzebubCandidate, 0, len(canList))
	for _, bid := range canList {
		candidates = append(candidates, beelzebubCandidate{
			id:    bid,
			level: protocol.BeelzebubTrialsBossLevel(bid),
			name:  protocol.BeelzebubTrialsBossDisplayName(bid),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].level != candidates[j].level {
			return candidates[i].level < candidates[j].level
		}
		return candidates[i].id < candidates[j].id
	})

	if preferBossID > 0 {
		prioritized := make([]beelzebubCandidate, 0, len(candidates))
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
	return candidates
}

// claimBeelzebubMoralAward 查询并领取道行等级宝箱奖励。
func (s *RoleSession) claimBeelzebubMoralAward(ctx context.Context) {
	moral, err := s.BeelzebubTrialsOpenMoral(ctx)
	if err != nil {
		slog.Debug("获取魔王试炼道行信息失败", "role_id", s.RoleID(), "error", err)
		return
	}
	if moral == nil || !moral.CanClaim || moral.Level <= 0 {
		return
	}
	res, err := s.BeelzebubTrialsGetMoralAward(ctx, moral.Level)
	if err != nil {
		slog.Warn("领取魔王试炼道行等级奖励失败", "role_id", s.RoleID(), "moral_lv", moral.Level, "error", err)
		return
	}
	if res == protocol.BeelzebubTrialsSuccess {
		slog.Info("已成功领取魔王试炼道行等级宝箱奖励", "role_id", s.RoleID(), "moral_lv", moral.Level)
	}
}
