// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现宗师修行试炼与境界突破 (Mod_MasterPractice_Base, Module 665) 自动化。
package minigame

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// MasterPracticeConfig 宗师修行策略配置。
type MasterPracticeConfig struct {
	// DictRepo 静态数据字典，用于把主角/伙伴 role_id 映射为职业 JobId。
	DictRepo dictionary.Repository
	// PracticeOrder 期望修行的职业 JobId 顺序。服务端要求整队同职业；
	// 切换主角神器绝技 (Mod 85 Act 3，映射来自 Act 5) 会改写主角当前 role_id/职业，
	// 因此可依次切换职业并上阵同职业伙伴，逐个方向修行。
	PracticeOrder []int32
	// MaxStarsPerJob 单方向累计星数上限 (默认 30)。
	MaxStarsPerJob int32
	// MaxChallengesPerDay 每日挑战次数上限 (默认 3，线上实测每天 3 次)。
	MaxChallengesPerDay int32
	// AutoNormalizeDeploy 是否自动下阵异职业伙伴、上阵同职业伙伴 (默认 true)。
	AutoNormalizeDeploy bool
	// AllowSwitchStunt 是否允许通过切换主角神器绝技来转换职业 (默认 true)。
	AllowSwitchStunt bool
	// AllowStepUp 是否在挑战后自动升阶 (默认 true)。
	AllowStepUp bool
	// MaxRetriesPerStar 单个星级挑战失败后的重试次数 (默认 2)。
	// 仅在 SwitchJobOnFailure=false 时生效；开启失败切换时首次失败即退避换职业。
	MaxRetriesPerStar int
	// SwitchJobOnFailure 当前职业重试耗尽仍失败时，是否切换到其他可修行职业 (默认 true)。
	SwitchJobOnFailure bool
}

// DefaultMasterPracticeConfig 返回宗师修行默认策略配置。
func DefaultMasterPracticeConfig() MasterPracticeConfig {
	return MasterPracticeConfig{
		MaxStarsPerJob:      30,
		MaxChallengesPerDay: 3,
		AutoNormalizeDeploy: true,
		AllowSwitchStunt:    true,
		AllowStepUp:         true,
		MaxRetriesPerStar:   2,
		SwitchJobOnFailure:  true,
	}
}

// NewMasterPracticeRoutine 构造宗师修行挑战与突破任务 (Feature 15: 宗师修行)。
func NewMasterPracticeRoutine(cfgs ...MasterPracticeConfig) scheduler.ActivityRoutine {
	cfg := DefaultMasterPracticeConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
		if cfg.MaxStarsPerJob <= 0 {
			cfg.MaxStarsPerJob = 30
		}
		if cfg.MaxChallengesPerDay <= 0 {
			cfg.MaxChallengesPerDay = 3
		}
	}
	return scheduler.NewBaseRoutine(
		"master_practice",
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
			slog.Info("正在执行益智任务: 宗师修行试炼与境界突破", "role_id", session.RoleID())

			caller := kit.NewCaller(ctx, session, 8*time.Second)
			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				return caller.Call(pkt, act)
			}

			// 1. 查询宗师修行主面板 (Mod 665, Act 1)
			resp, err := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), protocol.ActionIDMasterPracticePanel)
			if err != nil || resp == nil {
				if abortErr, ok := kit.IsAbortErr(err); ok {
					return abortErr
				}
				slog.Info("宗师修行功能未开放/不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "宗师修行面板无回包")
			}
			panel, err := protocol.ParseMasterPracticePanelResult(resp.Payload)
			if err != nil {
				slog.Info("解析宗师修行面板失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "宗师修行面板解析失败")
			}
			if panel.Result != protocol.MasterPracticeResultSuccess {
				slog.Info("宗师修行面板返回失败，稍后重试", "role_id", session.RoleID(), "result", panel.Result)
				return scheduler.NewCooldown(time.Hour, "宗师修行面板返回失败")
			}
			slog.Info("宗师修行面板",
				"role_id", session.RoleID(),
				"today_challenge_times", panel.TodayChallengeTimes,
				"jobs", len(panel.Jobs),
			)

			// 2. 查询神器绝技映射 (职业 -> 已激活绝技) 与当前布阵。
			//   主角通过切换神器绝技转换职业 (Mod 85 Act 3)，映射来自 Mod 85 Act 5；
			//   Mod 85 Act 7 的未激活宗师绝技会被服务端静默丢弃，不能用于切换。
			stunts, stErr := masterPracticeFetchArtifactStunts(call)
			if stErr != nil {
				slog.Warn("宗师修行绝技映射查询失败，跳过", "role_id", session.RoleID(), "err", stErr)
				return nil
			}
			deploy := masterPracticeFetchDeploy(call)
			if deploy == nil {
				if session.State() != client.StateActive {
					return client.ErrNotConnected
				}
				// 布阵面板被静默丢弃时不要直接等到次日，10 分钟后重试。
				slog.Info("宗师修行布阵信息不可用，10 分钟后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(10*time.Minute, "宗师修行布阵信息不可用")
			}
			_, currentJob, hasMain := masterPracticeProtagonistJob(deploy, cfg.DictRepo)
			if !hasMain {
				slog.Info("宗师修行无法解析主角当前职业，跳过", "role_id", session.RoleID())
				return nil
			}

			order := masterPracticeBuildOrder(cfg, panel, currentJob, stunts)
			if len(cfg.PracticeOrder) == 0 {
				slog.Info("宗师修行未配置职业顺序，以主角当前职业为主并按需切换备选职业",
					"role_id", session.RoleID(), "job_id", currentJob, "order", order)
			} else {
				slog.Info("宗师修行职业顺序",
					"role_id", session.RoleID(), "order", order, "current_job", currentJob)
			}

			totalChallenged := int32(0)
			switchedAny := false
			// 面板 today_challenge_times 实测不随挑战变化，不能作为门禁；
			// 以服务端 NO_CHALLENGE_TIMES 为准，本地仅保留宽松上限防死循环。
			maxTotalChallenges := cfg.MaxChallengesPerDay * 4
			if maxTotalChallenges < 6 {
				maxTotalChallenges = 6
			}

			for _, requestedJob := range order {
				if err := ctx.Err(); err != nil {
					return nil
				}

				// 每个职业开始前刷新面板，确保星数与今日完成状态是最新的。
				if fresh := masterPracticeFetchPanel(call); fresh != nil {
					panel = fresh
				}
				if totalChallenged >= maxTotalChallenges {
					slog.Info("宗师修行达到本地尝试上限，结束本轮",
						"role_id", session.RoleID(),
						"challenged", totalChallenged, "cap", maxTotalChallenges)
					return nil
				}
				job, found := masterPracticeJobByID(panel, requestedJob)
				if !found {
					slog.Info("宗师修行方向不在面板中，跳过", "role_id", session.RoleID(), "job_id", requestedJob)
					continue
				}
				if masterPracticeJobFull(job, cfg.MaxStarsPerJob) {
					slog.Info("宗师修行方向累计星数已满，切换下一职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "total_stars", job.TotalStars)
					continue
				}

				// 刷新布阵，读取主角当前职业。
				deploy = masterPracticeFetchDeploy(call)
				if deploy == nil {
					slog.Warn("宗师修行布阵信息刷新失败，跳过该职业", "role_id", session.RoleID(), "job_id", requestedJob)
					continue
				}
				_, currentJob, hasMain = masterPracticeProtagonistJob(deploy, cfg.DictRepo)
				if !hasMain {
					slog.Warn("宗师修行无法解析主角当前职业，跳过该职业", "role_id", session.RoleID(), "job_id", requestedJob)
					continue
				}

				// 今日已完成但尚未满星：先升阶，然后尝试下一候选职业。
				if masterPracticeJobDoneToday(job) {
					if requestedJob == currentJob && cfg.AllowStepUp {
						if refreshed := masterPracticeStepUpJob(requestedJob, cfg, session.RoleID(), call); refreshed != nil {
							panel = refreshed
						}
					}
					slog.Info("宗师修行方向今日已完成，尝试下一职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "total_stars", job.TotalStars)
					continue
				}

				// 3. 切换主角神器绝技 (职业)。成功后服务端会改写主角 role_id。
				switched, swErr := masterPracticeEnsureStunt(ctx, currentJob, requestedJob, stunts, cfg, session.RoleID(), call)
				if swErr != nil {
					slog.Warn("宗师修行切换主角绝技失败，跳过该职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "err", swErr)
					continue
				}
				if switched {
					switchedAny = true
					verified := false
					for attempt := 0; attempt < 2; attempt++ {
						if err := masterPracticeDeployPause(ctx); err != nil {
							return nil
						}
						deploy = masterPracticeFetchDeploy(call)
						if deploy == nil {
							continue
						}
						_, currentJob, hasMain = masterPracticeProtagonistJob(deploy, cfg.DictRepo)
						if hasMain && currentJob == requestedJob {
							verified = true
							break
						}
					}
					if !verified {
						slog.Warn("切换绝技后主角职业未生效，跳过该职业",
							"role_id", session.RoleID(), "expect_job", requestedJob, "actual_job", currentJob)
						continue
					}
				}

				// 4. 调整阵容为整队同职业。JOB_ERROR(11) 的官方文案是
				//   “请上阵相同职业的角色”，因此异职业伙伴必须先下阵。
				if cfg.AutoNormalizeDeploy {
					var normErr error
					deploy, normErr = masterPracticeNormalizeDeploy(ctx, deploy, requestedJob, cfg.DictRepo, call)
					if normErr != nil {
						slog.Warn("宗师修行阵容整理失败，跳过该职业",
							"role_id", session.RoleID(), "job_id", requestedJob, "err", normErr)
						continue
					}
				}
				if ok, reason := masterPracticeDeployMatches(deploy, requestedJob, cfg.DictRepo); !ok {
					slog.Warn("宗师修行阵容未满足整队同职业，跳过该职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "reason", reason)
					continue
				}

				// 5. 挑战尚未完成的星级；服务端次数不足时返回 NO_CHALLENGE_TIMES。
				remaining := int32(maxTotalChallenges - totalChallenged)
				if remaining <= 0 {
					return nil
				}
				// 开启失败切换时，单星级不做同职业重试：失败立即退避，
				// 把剩余次数用于其他职业，保证「先用光次数」。
				maxRetries := cfg.MaxRetriesPerStar
				if cfg.SwitchJobOnFailure {
					maxRetries = 0
				}
				challenged, full, failed, chErr := masterPracticeChallengeJob(
					ctx, job, remaining, maxRetries, session.RoleID(), call)
				totalChallenged += challenged
				if chErr != nil {
					if errors.Is(chErr, scheduler.ErrAttemptsExhausted) {
						slog.Info("宗师修行今日挑战次数已用尽", "role_id", session.RoleID())
					}
					return chErr
				}
				if failed {
					slog.Warn("宗师修行当前职业重试耗尽仍挑战失败，准备切换职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "challenged", challenged)
					if !cfg.SwitchJobOnFailure {
						return nil
					}
					// 刷新面板，下一候选职业据此更新剩余次数与今日进度。
					if fresh := masterPracticeFetchPanel(call); fresh != nil {
						panel = fresh
					}
					continue
				}

				// 6. 刷新面板后升阶；只有当前职业满星才继续修行顺序中的下一个职业。
				if refreshed := masterPracticeStepUpJob(requestedJob, cfg, session.RoleID(), call); refreshed != nil {
					panel = refreshed
					if freshJob, ok := masterPracticeJobByID(panel, requestedJob); ok {
						full = full || masterPracticeJobFull(freshJob, cfg.MaxStarsPerJob)
					}
				}
				if !full {
					// 当前职业今天可挑战的星级已打完但未满星，继续下一职业用完次数。
					slog.Info("宗师修行当前职业本次可挑战星级已完成，切换下一职业以用光次数",
						"role_id", session.RoleID(), "job_id", requestedJob,
						"challenged", challenged, "total_challenged", totalChallenged)
					continue
				}
				slog.Info("宗师修行当前职业已满星，继续下一职业",
					"role_id", session.RoleID(), "job_id", requestedJob)
			}

			if totalChallenged == 0 && !switchedAny {
				slog.Info("宗师修行本轮没有可执行方向",
					"role_id", session.RoleID(), "order", order,
					"reason", "全部方向已满星、今日已完成，或未激活对应神器绝技")
			}
			return nil
		},
	)
}

// masterPracticeFetchArtifactStunts 查询职业 -> 已激活神器绝技映射 (Mod 85 Act 5)。
//
// 注意: Mod 85 Act 7 的“宗师绝技”列表在未激活时 role_stunt_id=0/is_active=0，
// 服务端会静默丢弃这类 use_stunt_skill 请求。切换主角职业必须使用
// Mod 85 Act 5 返回的已激活神器绝技 ID。
func masterPracticeFetchArtifactStunts(call func(*protocol.Packet, uint32) (*protocol.Packet, error)) ([]protocol.LibraryStuntInfo, error) {
	resp, err := call(protocol.BuildLibraryArtifactStuntInfoPacket(), protocol.ActionIDLibraryArtifactStuntInfo)
	if err != nil {
		return nil, fmt.Errorf("查询神器绝技映射无回包: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("查询神器绝技映射无回包")
	}
	infos, pErr := protocol.ParseLibraryStuntInfoResult(resp.Payload)
	if pErr != nil {
		return nil, pErr
	}
	return infos, nil
}

// masterPracticeFindStunt 从神器绝技映射中选择目标职业可用的绝技。
// 优先 is_use=1，其次 is_active=1；未激活的绝技服务端会静默丢弃，不能用于切换。
func masterPracticeFindStunt(stunts []protocol.LibraryStuntInfo, jobID int32) (int32, bool) {
	var fallback int32
	for _, info := range stunts {
		if info.RoleJobID != jobID {
			continue
		}
		stuntID := info.RoleStuntID
		if stuntID <= 0 {
			stuntID = info.NextRoleStuntID
		}
		if stuntID <= 0 {
			continue
		}
		if info.IsUse == 1 {
			return stuntID, true
		}
		if info.IsActive == 1 && fallback == 0 {
			fallback = stuntID
		}
	}
	if fallback > 0 {
		return fallback, true
	}
	return 0, false
}

// masterPracticeEnsureStunt 通过切换主角神器绝技把职业转换为 targetJob。
// 返回 switched=true 表示已发送切换请求并成功。
func masterPracticeEnsureStunt(
	ctx context.Context,
	currentJob, targetJob int32,
	stunts []protocol.LibraryStuntInfo,
	cfg MasterPracticeConfig,
	roleID string,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) (bool, error) {
	if currentJob == targetJob {
		return false, nil
	}
	if !cfg.AllowSwitchStunt {
		return false, fmt.Errorf("主角当前职业 %d != 目标 %d，且配置关闭了绝技切换", currentJob, targetJob)
	}
	stuntID, ok := masterPracticeFindStunt(stunts, targetJob)
	if !ok {
		return false, fmt.Errorf("职业 %d 没有已激活的神器绝技 (请先学习对应绝技)", targetJob)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	pkt, err := protocol.BuildLibraryUseStuntSkillPacket(stuntID)
	if err != nil {
		return false, err
	}
	resp, err := call(pkt, protocol.ActionIDLibraryUseStuntSkill)
	if err != nil || resp == nil {
		return false, fmt.Errorf("切换神器绝技无回包: %w", err)
	}
	result, curRoleID, curStuntID, pErr := protocol.ParseLibraryUseStuntSkillResult(resp.Payload)
	if pErr != nil {
		return false, pErr
	}
	if result != protocol.LibraryResultSuccess {
		return false, fmt.Errorf("切换神器绝技失败: %s (result=%d)", protocol.LibraryUseStuntResultText(result), result)
	}
	slog.Info("已切换主角神器绝技",
		"role_id", roleID,
		"target_job", targetJob,
		"stunt_id", stuntID,
		"cur_role_id", curRoleID,
		"cur_stunt_id", curStuntID,
	)
	return true, nil
}

// masterPracticeRetryDelay 宗师修行战斗失败后重试同层前的等待。
const masterPracticeRetryDelay = 400 * time.Millisecond

// masterPracticeChallengeJob 挑战指定职业今日尚未完成的星级。
//
// remaining 为今日剩余挑战次数；只有服务端返回 SUCCESS 才扣减一次。
// maxRetries 为单个星级挑战失败 (FAILED) 后的重试次数。
// 返回:
//   - consumed: 本次实际消耗的挑战次数
//   - full: 该方向已累计满星
//   - failed: 重试耗尽仍打不过，调用方可切换其他职业继续
func masterPracticeChallengeJob(
	ctx context.Context,
	job protocol.MasterPracticeJobInfo,
	remaining int32,
	maxRetries int,
	roleID string,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) (consumed int32, full bool, failed bool, err error) {
	challengedToday := make(map[int32]bool, len(job.TodayChallenge))
	for _, star := range job.TodayChallenge {
		challengedToday[star] = true
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	for star := int32(1); star <= 3; star++ {
		if challengedToday[star] {
			continue
		}
		retries := 0
		for {
			if remaining > 0 && consumed >= remaining {
				slog.Info("宗师修行今日挑战次数已用尽，停止发送挑战",
					"role_id", roleID, "job_id", job.JobID, "remaining", remaining)
				return consumed, false, false, scheduler.ErrAttemptsExhausted
			}
			if ctx.Err() != nil {
				return consumed, false, false, nil
			}
			resp, callErr := call(
				protocol.BuildMasterPracticeChallengePacket(uint32(job.JobID), uint32(star), 0),
				protocol.ActionIDMasterPracticeChallenge,
			)
			if callErr != nil || resp == nil {
				slog.Info("宗师修行挑战无回包", "role_id", roleID, "job_id", job.JobID, "star", star, "err", callErr)
				return consumed, false, false, nil
			}
			code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
			if pErr != nil {
				slog.Warn("解析宗师修行挑战回包失败", "role_id", roleID, "err", pErr)
				return consumed, false, false, nil
			}
			outcome, outcomeErr := classifyMasterPracticeChallengeResult(code)
			switch outcome {
			case masterPracticeChallengeContinue:
				if code == protocol.MasterPracticeResultSuccess {
					consumed++
					slog.Info("宗师修行挑战成功", "role_id", roleID, "job_id", job.JobID, "star", star)
				} else {
					// 今日已挑战过该星级，不再重复消耗全局次数。
					slog.Info("宗师修行该星级今日已挑战", "role_id", roleID, "job_id", job.JobID, "star", star)
				}
			case masterPracticeChallengeFull:
				slog.Info("宗师修行方向累计星数已满", "role_id", roleID, "job_id", job.JobID, "result", code)
				return consumed, true, false, nil
			case masterPracticeChallengeNoTimes:
				return consumed, false, false, scheduler.ErrAttemptsExhausted
			case masterPracticeChallengeJobError:
				slog.Warn("宗师修行职业不匹配 (请上阵相同职业的角色)",
					"role_id", roleID, "job_id", job.JobID, "result", code)
				return consumed, false, false, scheduler.ErrConditionNotMet
			default:
				if code == protocol.MasterPracticeResultFailed {
					if retries < maxRetries {
						retries++
						slog.Warn("宗师修行挑战失败，重试同星级",
							"role_id", roleID, "job_id", job.JobID, "star", star,
							"retry", retries, "max_retries", maxRetries)
						select {
						case <-ctx.Done():
							return consumed, false, false, nil
						case <-time.After(masterPracticeRetryDelay):
						}
						continue
					}
					slog.Warn("宗师修行挑战失败且重试耗尽",
						"role_id", roleID, "job_id", job.JobID, "star", star,
						"retries", retries, "max_retries", maxRetries)
					return consumed, false, true, nil
				}
				if outcomeErr != nil {
					slog.Warn("宗师修行挑战业务失败",
						"role_id", roleID, "job_id", job.JobID, "star", star, "result", code, "err", outcomeErr)
					return consumed, false, false, outcomeErr
				}
				return consumed, false, false, nil
			}
			select {
			case <-ctx.Done():
				return consumed, false, false, nil
			case <-time.After(200 * time.Millisecond):
			}
			break
		}
	}
	return consumed, false, false, nil
}

// masterPracticeBuildOrder 构建本轮职业修行顺序：
// 先按配置/当前职业确定主方向；开启失败切换时，再追加面板中可修行且已激活神器绝技的备选职业。
func masterPracticeBuildOrder(
	cfg MasterPracticeConfig,
	panel *protocol.MasterPracticePanel,
	currentJob int32,
	stunts []protocol.LibraryStuntInfo,
) []int32 {
	seen := make(map[int32]bool)
	order := make([]int32, 0, len(panel.Jobs)+1)
	add := func(jobID int32) {
		if jobID <= 0 || seen[jobID] {
			return
		}
		seen[jobID] = true
		order = append(order, jobID)
	}

	if len(cfg.PracticeOrder) > 0 {
		for _, jobID := range cfg.PracticeOrder {
			add(jobID)
		}
	} else {
		add(currentJob)
	}

	if !cfg.SwitchJobOnFailure || !cfg.AllowSwitchStunt {
		return order
	}
	for _, job := range panel.Jobs {
		if job.JobID == currentJob || seen[job.JobID] {
			continue
		}
		if masterPracticeJobFull(job, cfg.MaxStarsPerJob) || masterPracticeJobDoneToday(job) {
			continue
		}
		if _, ok := masterPracticeFindStunt(stunts, job.JobID); !ok {
			continue
		}
		add(job.JobID)
	}
	return order
}

// masterPracticeStepUpJob 刷新面板并在星数满足时尝试升阶，返回刷新后的面板。
func masterPracticeStepUpJob(
	jobID int32,
	cfg MasterPracticeConfig,
	roleID string,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) *protocol.MasterPracticePanel {
	panel := masterPracticeFetchPanel(call)
	if panel == nil {
		slog.Warn("宗师修行升阶前刷新面板失败", "role_id", roleID, "job_id", jobID)
		return nil
	}
	if !cfg.AllowStepUp {
		return panel
	}
	job, found := masterPracticeJobByID(panel, jobID)
	if !found || job.TotalStars <= 0 {
		return panel
	}
	resp, err := call(
		protocol.BuildMasterPracticeStepUpPacket(uint32(jobID), 0),
		protocol.ActionIDMasterPracticeStepUp,
	)
	if err != nil || resp == nil {
		slog.Info("宗师境界突破无回包", "role_id", roleID, "job_id", jobID, "err", err)
		return panel
	}
	code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
	if pErr != nil {
		slog.Warn("解析宗师突破回包失败", "role_id", roleID, "err", pErr)
		return panel
	}
	switch code {
	case protocol.MasterPracticeResultSuccess:
		slog.Info("宗师境界突破成功", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultStepUpCD:
		slog.Info("宗师境界突破冷却中", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultLessStars:
		slog.Info("宗师境界突破星数不足", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultLessItem:
		slog.Info("宗师境界突破道具不足", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultNotActiveAll:
		slog.Info("宗师境界突破需先激活全部炼魂", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultTopStep:
		slog.Info("宗师境界已达最高", "role_id", roleID, "job_id", jobID)
	default:
		slog.Info("宗师境界突破未成功", "role_id", roleID, "job_id", jobID, "result", code)
	}
	return panel
}

// masterPracticeFetchPanel 查询并解析宗师修行主面板。
func masterPracticeFetchPanel(call func(*protocol.Packet, uint32) (*protocol.Packet, error)) *protocol.MasterPracticePanel {
	resp, err := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), protocol.ActionIDMasterPracticePanel)
	if err != nil || resp == nil {
		return nil
	}
	panel, pErr := protocol.ParseMasterPracticePanelResult(resp.Payload)
	if pErr != nil || panel.Result != protocol.MasterPracticeResultSuccess {
		return nil
	}
	return panel
}

// masterPracticeFetchDeploy 查询并解析宗师修行布阵信息。
func masterPracticeFetchDeploy(call func(*protocol.Packet, uint32) (*protocol.Packet, error)) *protocol.MasterPracticeDeployInfo {
	resp, err := call(protocol.BuildMasterPracticeDeployInfoPacket(), protocol.ActionIDMasterPracticeDeployInfo)
	if err != nil || resp == nil {
		slog.Warn("宗师修行布阵信息无回包", "err", err)
		return nil
	}
	info, pErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload)
	if pErr != nil {
		slog.Warn("宗师修行布阵信息解析失败", "err", pErr, "payload_len", len(resp.Payload),
			"payload_hex", hex.EncodeToString(resp.Payload))
		return nil
	}
	if info.Result != protocol.MasterPracticeResultSuccess {
		slog.Info("宗师修行布阵信息返回业务失败", "result", info.Result)
		return nil
	}
	return info
}

// masterPracticeJobByID 按 JobID 从面板中查找方向。
func masterPracticeJobByID(panel *protocol.MasterPracticePanel, jobID int32) (protocol.MasterPracticeJobInfo, bool) {
	for _, job := range panel.Jobs {
		if job.JobID == jobID {
			return job, true
		}
	}
	return protocol.MasterPracticeJobInfo{}, false
}

// masterPracticeRoleJob 把角色 role_id 映射为职业 JobId；字典缺失时返回 0。
func masterPracticeRoleJob(repo dictionary.Repository, roleID int32) int32 {
	if repo == nil || roleID <= 0 {
		return 0
	}
	rt, err := repo.GetRoleType(int(roleID))
	if err != nil || rt == nil {
		return 0
	}
	return int32(rt.JobID)
}

// masterPracticeProtagonistJob 返回阵容中的主角及其当前 JobId。
func masterPracticeProtagonistJob(deploy *protocol.MasterPracticeDeployInfo, repo dictionary.Repository) (playerRoleID, jobID int32, ok bool) {
	if deploy == nil {
		return 0, 0, false
	}
	for _, role := range deploy.Roles {
		if role.IsMainRole != 1 {
			continue
		}
		job := masterPracticeRoleJob(repo, role.RoleID)
		if job <= 0 {
			return role.PlayerRoleID, 0, false
		}
		return role.PlayerRoleID, job, true
	}
	return 0, 0, false
}

// masterPracticeJobFull 判断方向累计星数是否已达上限。
func masterPracticeJobFull(job protocol.MasterPracticeJobInfo, maxStars int32) bool {
	return maxStars > 0 && job.TotalStars >= maxStars
}

// masterPracticeJobDoneToday 判断方向今日 1~3 星是否均已挑战。
func masterPracticeJobDoneToday(job protocol.MasterPracticeJobInfo) bool {
	var seen [4]bool
	for _, star := range job.TodayChallenge {
		if star >= 1 && star <= 3 {
			seen[star] = true
		}
	}
	return seen[1] && seen[2] && seen[3]
}

// masterPracticeMismatchedPartners 返回需要下阵的异职业伙伴 player_role_id。
// 主角 (IsMainRole==1) 永不下阵。
func masterPracticeMismatchedPartners(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) []int32 {
	if deploy == nil {
		return nil
	}
	roleByPlayerID := make(map[int32]protocol.MasterPracticeDeployRole, len(deploy.Roles))
	for _, role := range deploy.Roles {
		roleByPlayerID[role.PlayerRoleID] = role
	}
	var out []int32
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID == 0 {
			continue
		}
		role, ok := roleByPlayerID[grid.PlayerRoleID]
		if !ok || role.IsMainRole == 1 {
			continue
		}
		if masterPracticeRoleJob(repo, role.RoleID) != targetJob {
			out = append(out, grid.PlayerRoleID)
		}
	}
	return out
}

// masterPracticeHasDeployedPartner 判断阵容中是否已有同职业伙伴。
func masterPracticeHasDeployedPartner(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) bool {
	if deploy == nil {
		return false
	}
	roleByPlayerID := make(map[int32]protocol.MasterPracticeDeployRole, len(deploy.Roles))
	for _, role := range deploy.Roles {
		roleByPlayerID[role.PlayerRoleID] = role
	}
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID == 0 {
			continue
		}
		role, ok := roleByPlayerID[grid.PlayerRoleID]
		if !ok || role.IsMainRole == 1 {
			continue
		}
		if masterPracticeRoleJob(repo, role.RoleID) == targetJob {
			return true
		}
	}
	return false
}

// masterPracticeDeployCandidate 选择一名未上阵的同职业伙伴及空余格位。
// 返回 ok=false 表示没有可上阵的同职业伙伴、当前已有同职业伙伴或没有空位。
func masterPracticeDeployCandidate(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) (playerRoleID int32, gridTypeID uint8, ok bool) {
	if deploy == nil {
		return 0, 0, false
	}
	if masterPracticeHasDeployedPartner(deploy, targetJob, repo) {
		return 0, 0, false
	}
	deployed := make(map[int32]bool, len(deploy.Grids))
	empty := make(map[int32]bool, len(deploy.Grids))
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID != 0 {
			deployed[grid.PlayerRoleID] = true
		} else if isMasterPracticeNormalGrid(grid.GridTypeID) {
			empty[grid.GridTypeID] = true
		}
	}

	for _, role := range deploy.Roles {
		if role.IsMainRole == 1 || deployed[role.PlayerRoleID] {
			continue
		}
		if masterPracticeRoleJob(repo, role.RoleID) == targetJob {
			playerRoleID = role.PlayerRoleID
			break
		}
	}
	if playerRoleID == 0 {
		return 0, 0, false
	}

	// 优先使用 9 号常规格，其次 2~8；不使用 1 号主角位与 51/52 替补位。
	for _, preferred := range []int32{9, 2, 3, 4, 5, 6, 7, 8} {
		if empty[preferred] {
			return playerRoleID, uint8(preferred), true
		}
	}
	return 0, 0, false
}

func isMasterPracticeNormalGrid(gridTypeID int32) bool {
	return gridTypeID >= 1 && gridTypeID <= 9
}

// masterPracticeDeployPause 在连续布阵操作之间注入短间隔，降低触发风控的概率。
func masterPracticeDeployPause(ctx context.Context) error {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// masterPracticeDeployMatches 校验所有已上阵角色 (含主角) 是否都与 targetJob 同职业。
func masterPracticeDeployMatches(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) (bool, string) {
	if deploy == nil {
		return false, "布阵信息为空"
	}
	roleByPlayerID := make(map[int32]protocol.MasterPracticeDeployRole, len(deploy.Roles))
	for _, role := range deploy.Roles {
		roleByPlayerID[role.PlayerRoleID] = role
	}
	mainDeployed := false
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID == 0 {
			continue
		}
		role, ok := roleByPlayerID[grid.PlayerRoleID]
		if !ok {
			return false, fmt.Sprintf("格位 %d 的角色 %d 不在角色列表", grid.GridTypeID, grid.PlayerRoleID)
		}
		job := masterPracticeRoleJob(repo, role.RoleID)
		if role.IsMainRole == 1 {
			mainDeployed = true
		}
		if job != targetJob {
			return false, fmt.Sprintf("角色 %d (%s) 职业 %d != 目标 %d", role.PlayerRoleID, role.RoleName, job, targetJob)
		}
	}
	if !mainDeployed {
		return false, "主角未上阵"
	}
	return true, ""
}

// masterPracticeNormalizeDeploy 下阵异职业伙伴，并在需要时上阵一名同职业伙伴。
func masterPracticeNormalizeDeploy(
	ctx context.Context,
	deploy *protocol.MasterPracticeDeployInfo,
	targetJob int32,
	repo dictionary.Repository,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) (*protocol.MasterPracticeDeployInfo, error) {
	mismatched := masterPracticeMismatchedPartners(deploy, targetJob, repo)
	for _, playerRoleID := range mismatched {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := call(protocol.BuildMasterPracticeDeployPacket(playerRoleID, 0), protocol.ActionIDMasterPracticeDeploy)
		if err != nil || resp == nil {
			return nil, fmt.Errorf("下阵角色 %d 无回包: %w", playerRoleID, err)
		}
		code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
		if pErr != nil || code != protocol.MasterPracticeResultSuccess {
			return nil, fmt.Errorf("下阵角色 %d 失败: result=%d err=%v", playerRoleID, code, pErr)
		}
		if err := masterPracticeDeployPause(ctx); err != nil {
			return nil, err
		}
	}
	if len(mismatched) > 0 {
		refreshed := masterPracticeFetchDeploy(call)
		if refreshed == nil {
			return nil, fmt.Errorf("下阵后刷新布阵信息失败")
		}
		deploy = refreshed
	}

	if masterPracticeHasDeployedPartner(deploy, targetJob, repo) {
		return deploy, nil
	}
	playerRoleID, gridTypeID, ok := masterPracticeDeployCandidate(deploy, targetJob, repo)
	if !ok {
		// 没有同职业伙伴时仍允许以主角单人阵容挑战；若服务端要求伙伴，
		// 后续挑战会返回 JOB_ERROR 并由调用方熔断。
		return deploy, nil
	}
	resp, err := call(protocol.BuildMasterPracticeDeployPacket(playerRoleID, gridTypeID), protocol.ActionIDMasterPracticeDeploy)
	if err != nil || resp == nil {
		return nil, fmt.Errorf("上阵角色 %d 到格位 %d 无回包: %w", playerRoleID, gridTypeID, err)
	}
	code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
	if pErr != nil || code != protocol.MasterPracticeResultSuccess {
		return nil, fmt.Errorf("上阵角色 %d 到格位 %d 失败: result=%d err=%v", playerRoleID, gridTypeID, code, pErr)
	}
	if err := masterPracticeDeployPause(ctx); err != nil {
		return nil, err
	}
	refreshed := masterPracticeFetchDeploy(call)
	if refreshed == nil {
		return nil, fmt.Errorf("上阵后刷新布阵信息失败")
	}
	return refreshed, nil
}

// masterPracticeChallengeOutcome 挑战回包的业务分类。
type masterPracticeChallengeOutcome int

const (
	// masterPracticeChallengeContinue 已挑战或挑战成功，继续下一星。
	masterPracticeChallengeContinue masterPracticeChallengeOutcome = iota
	// masterPracticeChallengeStop 正常停止当前方向 (如星数不足)。
	masterPracticeChallengeStop
	// masterPracticeChallengeFull 该方向累计星数已满。
	masterPracticeChallengeFull
	// masterPracticeChallengeNoTimes 今日挑战次数用尽。
	masterPracticeChallengeNoTimes
	// masterPracticeChallengeJobError 阵容职业与方向不匹配。
	masterPracticeChallengeJobError
)

// classifyMasterPracticeChallengeResult 将挑战结果码映射为业务动作与调度错误。
func classifyMasterPracticeChallengeResult(code uint8) (masterPracticeChallengeOutcome, error) {
	switch code {
	case protocol.MasterPracticeResultSuccess, protocol.MasterPracticeResultIsChallenged:
		return masterPracticeChallengeContinue, nil
	case protocol.MasterPracticeResultMaxStars:
		return masterPracticeChallengeFull, nil
	case protocol.MasterPracticeResultNoChallengeTimes:
		return masterPracticeChallengeNoTimes, scheduler.ErrAttemptsExhausted
	case protocol.MasterPracticeResultJobError:
		return masterPracticeChallengeJobError, scheduler.ErrConditionNotMet
	case protocol.MasterPracticeResultLessStars, protocol.MasterPracticeResultFailed:
		return masterPracticeChallengeStop, nil
	case protocol.MasterPracticeResultConditionLimited, protocol.MasterPracticeResultIsLocked:
		return masterPracticeChallengeStop, scheduler.ErrConditionNotMet
	default:
		return masterPracticeChallengeStop, fmt.Errorf("宗师修行挑战未知结果码: %d", code)
	}
}
