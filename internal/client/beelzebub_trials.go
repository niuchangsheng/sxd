package client

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/protocol"
)

const (
	// beelzebubFightSettleDelay 战斗指令发出后等待服务端生成战报的结算时间。
	beelzebubFightSettleDelay = 500 * time.Millisecond
	// beelzebubTeamSettleDelay 两关之间等待服务端同步队伍/战场状态的时间。
	beelzebubTeamSettleDelay = 200 * time.Millisecond
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
	canChallenge, err := s.BeelzebubTrialsCheckCanChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查魔王试炼挑战状态失败: %w", err)
	}
	if !canChallenge {
		slog.Info("今日魔王试炼挑战次数已用尽，无需重复挑战", "role_id", s.RoleID())
		s.claimBeelzebubMoralAward(ctx)
		return nil, nil
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
	queue := append([]int32(nil), canList...)
	seen := make(map[int32]bool, len(queue))
	results := make([]*protocol.BeelzebubTrialsWarResult, 0, len(queue))
	slog.Info("魔王试炼开始逐关挑战", "role_id", s.RoleID(), "total", len(queue))

	for attempts := 0; attempts < beelzebubMaxChallenges; {
		if ctx.Err() != nil {
			break
		}

		// 每关开始前确认剩余挑战次数，避免次数耗尽后仍盲目发起战斗。
		ok, cErr := s.BeelzebubTrialsCheckCanChallenge(ctx)
		if cErr != nil {
			slog.Warn("复检魔王试炼挑战次数失败，停止后续关卡", "role_id", s.RoleID(), "error", cErr)
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
			slog.Warn("魔王试炼关卡挑战失败，继续尝试后续关卡",
				"role_id", s.RoleID(),
				"boss_id", cand.id,
				"boss_name", cand.name,
				"level", cand.level,
				"error", cErr,
			)
		} else if warRes != nil {
			results = append(results, warRes)
		}

		// 每关结束后立即刷新列表，尽早纳入新解锁的隐藏/神秘层关卡。
		if fresh, fErr := s.BeelzebubTrialsGetCanChallengeList(ctx); fErr == nil {
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
