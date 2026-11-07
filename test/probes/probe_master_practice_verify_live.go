//go:build probe

// 目标协议: 宗师修行线上验证 (Mod 665 MasterPractice + Mod 85 Library)
//   - 665/1 main_panel
//   - 665/2 get_deploy_info
//   - 665/4 deploy [player_role_id Int32, grid_type_id UByte]
//   - 665/5 start_challenge [job_id Int32, battle_stars Int32]
//   - 665/8 step_up
//   - 85/5  get_artifact_stunt_info (职业 -> 已激活神器绝技)
//   - 85/3  use_stunt_skill [stunt_id Short]  (切换主角职业)
//
// 基准对齐: 官方 com.protocols.Mod_MasterPractice_Base / Mod_Library_Base,
// com.datas.LibraryData.on_use_stunt_skill (切换后更新 player.mainRoleId)。
//
// 运行命令:
//
//	SXD_CONFIG=configs/s0.yaml SXD_MP_ORDER=1 go run -tags=probe test/probes/probe_master_practice_verify_live.go
//
// 可选环境变量:
//
//	SXD_MP_ORDER=1,2,3  职业 JobId 顺序 (默认读取配置 scheduler.master_practice.practice_order；仍为空则只练当前职业)
//	SXD_MP_RESTORE=0    关闭执行后恢复原主角职业与宗师阵容 (默认 1，即恢复)
//
// 预期表现: 调用生产 Routine 完成“切主角绝技 -> 上阵同职业伙伴 -> 挑战 -> 升阶”闭环，
// 打印前后面板、主角职业、绝技映射与阵容；随后尽可能恢复执行前的主角职业与阵容。
// 注意: 这是真实写操作，会消耗当天的宗师修行挑战次数；不会消耗元宝或转职符石。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/scheduler"
)

const (
	actionPanel      = protocol.ActionIDMasterPracticePanel
	actionDeployInfo = protocol.ActionIDMasterPracticeDeployInfo
	actionDeploy     = protocol.ActionIDMasterPracticeDeploy
	actionStuntInfo  = protocol.ActionIDLibraryMasterPracticeStuntInfo
	actionUseStunt   = protocol.ActionIDLibraryUseStuntSkill
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置缺少账号或角色")
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("加载数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	roleCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
		ReconnectInterval:    5 * time.Second,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, 10*time.Second)
		defer cCancel()
		return sess.Call(cCtx, pkt, act)
	}

	before := captureState("执行前", call, dictRepo)

	order := parseOrder(os.Getenv("SXD_MP_ORDER"))
	if len(order) == 0 {
		order = cfg.Scheduler.MasterPractice.PracticeOrder
	}

	mpCfg := minigame.DefaultMasterPracticeConfig()
	mpCfg.DictRepo = dictRepo
	mpCfg.PracticeOrder = order
	if cfg.Scheduler.MasterPractice.MaxStarsPerJob > 0 {
		mpCfg.MaxStarsPerJob = cfg.Scheduler.MasterPractice.MaxStarsPerJob
	}
	if cfg.Scheduler.MasterPractice.MaxChallengesPerDay > 0 {
		mpCfg.MaxChallengesPerDay = cfg.Scheduler.MasterPractice.MaxChallengesPerDay
	}
	if cfg.Scheduler.MasterPractice.AutoNormalizeDeploy != nil {
		mpCfg.AutoNormalizeDeploy = *cfg.Scheduler.MasterPractice.AutoNormalizeDeploy
	}
	if cfg.Scheduler.MasterPractice.AllowSwitchStunt != nil {
		mpCfg.AllowSwitchStunt = *cfg.Scheduler.MasterPractice.AllowSwitchStunt
	}
	if cfg.Scheduler.MasterPractice.AllowStepUp != nil {
		mpCfg.AllowStepUp = *cfg.Scheduler.MasterPractice.AllowStepUp
	}

	slog.Info("开始执行生产宗师修行 Routine", "practice_order", order)
	rt := minigame.NewMasterPracticeRoutine(mpCfg)
	jitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 150 * time.Millisecond,
		MaxDuration: 350 * time.Millisecond,
	})
	runCtx, runCancel := context.WithTimeout(ctx, 120*time.Second)
	execErr := rt.Execute(runCtx, sess, jitter)
	runCancel()
	slog.Info("生产宗师修行 Routine 执行结束", "err", execErr)

	after := captureState("执行后", call, dictRepo)
	slog.Info("宗师修行状态对比",
		"before_job", before.mainJob, "before_role_id", before.mainRoleID,
		"after_job", after.mainJob, "after_role_id", after.mainRoleID,
		"after_deployed", after.deployedIDs,
	)

	if os.Getenv("SXD_MP_RESTORE") != "0" {
		restoreState(before, call, dictRepo)
		captureState("恢复后", call, dictRepo)
	}
}

type mpState struct {
	mainJob     int32
	mainRoleID  int32
	mainPlayer  int32
	deployedIDs []int32
	panel       *protocol.MasterPracticePanel
	deploy      *protocol.MasterPracticeDeployInfo
	stunts      []protocol.LibraryStuntInfo
}

func captureState(label string, call func(*protocol.Packet, uint32) (*protocol.Packet, error), repo dictionary.Repository) mpState {
	st := mpState{}

	if resp, err := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), actionPanel); err == nil && resp != nil {
		if p, pErr := protocol.ParseMasterPracticePanelResult(resp.Payload); pErr == nil {
			st.panel = p
			slog.Info(label+" 面板", "result", p.Result, "today_times", p.TodayChallengeTimes)
			for _, j := range p.Jobs {
				slog.Info(label+"  职业进度", "job_id", j.JobID, "step", j.StepID, "stars", j.TotalStars, "cd", j.StepUpCDTime, "today", j.TodayChallenge)
			}
		} else {
			slog.Error(label+" 面板解析失败", "err", pErr)
		}
	}

	if resp, err := call(protocol.BuildMasterPracticeDeployInfoPacket(), actionDeployInfo); err == nil && resp != nil {
		if d, dErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload); dErr == nil {
			st.deploy = d
			slog.Info(label+" 布阵", "result", d.Result, "level", d.DeployLevel, "roles", len(d.Roles), "grids", len(d.Grids))
			for _, r := range d.Roles {
				job := roleJob(repo, r.RoleID)
				slog.Info(label+"  角色", "player_role_id", r.PlayerRoleID, "is_main", r.IsMainRole, "role_id", r.RoleID, "job_id", job, "name", r.RoleName, "is_deploy", r.IsDeploy)
				if r.IsMainRole == 1 {
					st.mainJob = job
					st.mainRoleID = r.RoleID
					st.mainPlayer = r.PlayerRoleID
				}
			}
			for _, g := range d.Grids {
				if g.PlayerRoleID != 0 {
					st.deployedIDs = append(st.deployedIDs, g.PlayerRoleID)
					slog.Info(label+"  格位", "grid_type_id", g.GridTypeID, "player_role_id", g.PlayerRoleID)
				}
			}
			slog.Info(label+" 主角", "player_role_id", st.mainPlayer, "role_id", st.mainRoleID, "job_id", st.mainJob)
		} else {
			slog.Error(label+" 布阵解析失败", "err", dErr)
		}
	}

	if resp, err := call(protocol.BuildLibraryArtifactStuntInfoPacket(), protocol.ActionIDLibraryArtifactStuntInfo); err == nil && resp != nil {
		if infos, sErr := protocol.ParseLibraryStuntInfoResult(resp.Payload); sErr == nil {
			st.stunts = infos
			for _, info := range infos {
				slog.Info(label+"  神器绝技", "job_id", info.RoleJobID, "stunt_id", info.RoleStuntID, "next_stunt_id", info.NextRoleStuntID, "is_active", info.IsActive, "is_use", info.IsUse)
			}
		} else {
			slog.Error(label+" 神器绝技映射解析失败", "err", sErr)
		}
	}
	return st
}

func restoreState(before mpState, call func(*protocol.Packet, uint32) (*protocol.Packet, error), repo dictionary.Repository) {
	if before.mainJob <= 0 {
		slog.Warn("缺少执行前主角职业，跳过恢复")
		return
	}
	// 1. 切回原职业对应绝技。
	// 不依赖 deploy_info 的当前职业判断：切换后短时间内布阵信息可能仍是旧值。
	stuntID, ok := findStunt(before.stunts, before.mainJob)
	if !ok {
		slog.Warn("未找到原职业绝技，无法恢复主角职业", "job_id", before.mainJob)
	} else {
		pkt, err := protocol.BuildLibraryUseStuntSkillPacket(stuntID)
		if err == nil {
			if resp, callErr := call(pkt, actionUseStunt); callErr == nil && resp != nil {
				result, curRoleID, curStuntID, pErr := protocol.ParseLibraryUseStuntSkillResult(resp.Payload)
				slog.Info("恢复主角职业", "target_job", before.mainJob, "stunt_id", stuntID,
					"result", result, "text", protocol.LibraryUseStuntResultText(result),
					"cur_role_id", curRoleID, "cur_stunt_id", curStuntID, "err", pErr)
			} else {
				slog.Warn("恢复主角职业无回包", "err", callErr)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	// 2. 下阵与恢复目标职业不一致的伙伴，尽量还原执行前阵容。
	if resp, err := call(protocol.BuildMasterPracticeDeployInfoPacket(), actionDeployInfo); err == nil && resp != nil {
		if d, pErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload); pErr == nil {
			roleMap := make(map[int32]protocol.MasterPracticeDeployRole, len(d.Roles))
			for _, r := range d.Roles {
				roleMap[r.PlayerRoleID] = r
			}
			for _, g := range d.Grids {
				if g.PlayerRoleID == 0 {
					continue
				}
				r, ok := roleMap[g.PlayerRoleID]
				if !ok || r.IsMainRole == 1 {
					continue
				}
				if roleJob(repo, r.RoleID) != before.mainJob {
					resp2, callErr := call(protocol.BuildMasterPracticeDeployPacket(g.PlayerRoleID, 0), actionDeploy)
					if callErr == nil && resp2 != nil {
						code, _ := protocol.ParseMasterPracticeActionCode(resp2.Payload)
						slog.Info("恢复阵容: 下阵异职业伙伴", "player_role_id", g.PlayerRoleID, "result", code)
					}
					time.Sleep(150 * time.Millisecond)
				}
			}
		}
	}
}

func captureMainJob(call func(*protocol.Packet, uint32) (*protocol.Packet, error), repo dictionary.Repository) int32 {
	resp, err := call(protocol.BuildMasterPracticeDeployInfoPacket(), actionDeployInfo)
	if err != nil || resp == nil {
		return 0
	}
	d, pErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload)
	if pErr != nil {
		return 0
	}
	for _, r := range d.Roles {
		if r.IsMainRole == 1 {
			return roleJob(repo, r.RoleID)
		}
	}
	return 0
}

func roleJob(repo dictionary.Repository, roleID int32) int32 {
	if repo == nil || roleID <= 0 {
		return 0
	}
	rt, err := repo.GetRoleType(int(roleID))
	if err != nil || rt == nil {
		return 0
	}
	return int32(rt.JobID)
}

func findStunt(stunts []protocol.LibraryStuntInfo, jobID int32) (int32, bool) {
	var fallback int32
	for _, info := range stunts {
		if info.RoleJobID != jobID {
			continue
		}
		id := info.RoleStuntID
		if id <= 0 {
			id = info.NextRoleStuntID
		}
		if id <= 0 {
			continue
		}
		if info.IsUse == 1 {
			return id, true
		}
		if fallback == 0 {
			fallback = id
		}
	}
	if fallback > 0 {
		return fallback, true
	}
	return 0, false
}

func parseOrder(raw string) []int32 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []int32
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v <= 0 {
			slog.Warn("忽略非法职业顺序项", "value", part, "err", err)
			continue
		}
		out = append(out, int32(v))
	}
	return out
}
