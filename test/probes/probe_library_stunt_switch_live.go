//go:build probe

// 目标协议: Mod_Library_Base.use_stunt_skill (Module 85 / Action 3, 0x00550003)
// 基准对齐: 官方 com.protocols.Mod_Library_Base
//
//	use_stunt_skill request=[Short stunt_id], response=[UByte result, Short cur_role_id, Short cur_stunt_id]
//	同面板依赖: Mod_Library_Base.get_library_info (0x00550001)
//	           get_master_practice_stunt_info (0x00550007)
//
// 运行命令:
//
//	SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_library_stunt_switch_live.go
//
// 预期表现: 依次尝试三种载荷格式，打印服务端所有入站动作，判定服务端实际接受的
// use_stunt_skill 请求格式；若任一格式成功，则把主角职业切回原职业。
// 注意: 这是真实写操作，但只切换主角绝技；不会消耗元宝或转职符石。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const (
	actLibraryInfo   = protocol.ActionIDLibraryMasterPracticeStuntInfo // 85/7 仅作诊断
	actArtifactStunt = protocol.ActionIDLibraryArtifactStuntInfo       // 85/5 职业 -> 神器绝技
	actUseStunt      = protocol.ActionIDLibraryUseStuntSkill
	actDeployInfo    = protocol.ActionIDMasterPracticeDeployInfo
	actPanel         = protocol.ActionIDMasterPracticePanel
	targetJobID      = 1
	originalFallback = 15
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

	repo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("加载数据字典失败", "err", err)
		return
	}
	defer repo.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	cache := platform.NewTicketCache(90 * time.Minute)
	auth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	auth.SetCookie(acc.Cookie)
	cache.RegisterAuthenticator(auth)
	cache.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := cache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
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
		MaxReconnectAttempts: 1,
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	var obsMu sync.Mutex
	var inbound []string
	sess.SetPacketObserver(func(p *protocol.Packet) {
		obsMu.Lock()
		inbound = append(inbound, fmt.Sprintf("0x%08X len=%d hex=%s", p.ActionID, len(p.Payload), hex.EncodeToString(p.Payload)))
		obsMu.Unlock()
	})
	drain := func(label string) {
		time.Sleep(300 * time.Millisecond)
		obsMu.Lock()
		defer obsMu.Unlock()
		if len(inbound) == 0 {
			slog.Warn(label + " 期间无任何入站报文")
			return
		}
		for _, line := range inbound {
			slog.Info(label+" 入站", "packet", line)
		}
		inbound = nil
	}
	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		return sess.Call(cCtx, pkt, act)
	}

	// 基线查询。
	if resp, err := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), actPanel, 8*time.Second); err == nil && resp != nil {
		if p, pErr := protocol.ParseMasterPracticePanelResult(resp.Payload); pErr == nil {
			for _, j := range p.Jobs {
				slog.Info("面板", "job_id", j.JobID, "step", j.StepID, "stars", j.TotalStars, "today", j.TodayChallenge)
			}
		}
	}
	if resp, err := call(protocol.BuildLibraryMasterPracticeStuntInfoPacket(), actLibraryInfo, 8*time.Second); err == nil && resp != nil {
		if infos, pErr := protocol.ParseLibraryMasterPracticeStuntInfoResult(resp.Payload); pErr == nil {
			for _, info := range infos {
				slog.Info("宗师绝技映射", "job_id", info.RoleJobID, "stunt_id", info.RoleStuntID,
					"next_stunt_id", info.NextRoleStuntID, "is_active", info.IsActive, "is_use", info.IsUse)
			}
		}
	}
	if resp, err := call(protocol.NewPacket(actArtifactStunt, nil), actArtifactStunt, 8*time.Second); err == nil && resp != nil {
		if infos, pErr := protocol.ParseLibraryMasterPracticeStuntInfoResult(resp.Payload); pErr == nil {
			for _, info := range infos {
				slog.Info("神器绝技映射", "job_id", info.RoleJobID, "stunt_id", info.RoleStuntID,
					"next_stunt_id", info.NextRoleStuntID, "is_active", info.IsActive, "is_use", info.IsUse)
			}
		}
	}
	originalJob, originalRoleID := currentMainRole(call, repo)
	slog.Info("执行前主角", "job_id", originalJob, "role_id", originalRoleID)

	stuntID := int32(532)
	if infos, ok := fetchArtifactStunts(call); ok {
		if id, found := findStunt(infos, targetJobID); found {
			stuntID = id
		}
	}
	slog.Info("准备切换绝技", "target_job", targetJobID, "stunt_id", stuntID)

	// 变体 A: 官方 request=[Short stunt_id]，2 字节。
	attempt("A: Short(2B)", func() (*protocol.Packet, error) {
		return protocol.BuildLibraryUseStuntSkillPacket(stuntID)
	}, call)
	// 变体 B: Short + PrevAct(4B)，并显式把 PrevAct 设为绝技映射查询。
	attempt("B: Short+PrevAct(6B)", func() (*protocol.Packet, error) {
		w := protocol.NewWriter()
		w.WriteInt16(int16(stuntID))
		w.WriteUint32(actLibraryInfo)
		pkt := protocol.NewPacket(actUseStunt, w.Bytes())
		pkt.HasPrevAct = true
		sess.SetLastActionID(actLibraryInfo)
		return pkt, nil
	}, call)
	// 变体 C: Int(4B)。
	attempt("C: Int(4B)", func() (*protocol.Packet, error) {
		w := protocol.NewWriter()
		w.WriteInt32(stuntID)
		return protocol.NewPacket(actUseStunt, w.Bytes()), nil
	}, call)

	drain("切换尝试结束后")

	afterJob, afterRoleID := currentMainRole(call, repo)
	slog.Info("切换尝试后主角", "job_id", afterJob, "role_id", afterRoleID)

	// 无论是否切换成功，都尝试切回原职业，避免账号停留在目标职业。
	if afterJob > 0 && afterJob != originalJob {
		restoreJob := originalJob
		if restoreJob <= 0 {
			restoreJob = originalFallback
		}
		if infos, ok := fetchArtifactStunts(call); ok {
			if id, found := findStunt(infos, restoreJob); found {
				// 使用变体 A 先试；若失败再试 B。
				restoreOne(restoreJob, id, false, call)
				time.Sleep(500 * time.Millisecond)
				if job, _ := currentMainRole(call, repo); job != restoreJob {
					restoreOne(restoreJob, id, true, call)
				}
			}
		}
		drain("恢复尝试结束后")
		job, roleID := currentMainRole(call, repo)
		slog.Info("恢复后主角", "job_id", job, "role_id", roleID)
	}
}

func attempt(label string, build func() (*protocol.Packet, error), call func(*protocol.Packet, uint32, time.Duration) (*protocol.Packet, error)) {
	pkt, err := build()
	if err != nil {
		slog.Error(label+" 构造失败", "err", err)
		return
	}
	slog.Info(label+" 发送", "payload", hex.EncodeToString(pkt.Payload), "has_prev_act", pkt.HasPrevAct)
	resp, err := call(pkt, actUseStunt, 6*time.Second)
	if err != nil || resp == nil {
		slog.Warn(label+" 无回包", "err", err)
		return
	}
	result, roleID, curStunt, pErr := protocol.ParseLibraryUseStuntSkillResult(resp.Payload)
	slog.Info(label+" 回包", "result", result, "text", protocol.LibraryUseStuntResultText(result),
		"cur_role_id", roleID, "cur_stunt_id", curStunt, "parse_err", pErr)
}

func restoreOne(jobID, stuntID int32, withPrevAct bool, call func(*protocol.Packet, uint32, time.Duration) (*protocol.Packet, error)) {
	var pkt *protocol.Packet
	if withPrevAct {
		w := protocol.NewWriter()
		w.WriteInt16(int16(stuntID))
		w.WriteUint32(actLibraryInfo)
		pkt = protocol.NewPacket(actUseStunt, w.Bytes())
		pkt.HasPrevAct = true
	} else {
		pkt, _ = protocol.BuildLibraryUseStuntSkillPacket(stuntID)
	}
	slog.Info("恢复主角职业尝试", "job_id", jobID, "stunt_id", stuntID, "with_prev_act", withPrevAct)
	resp, err := call(pkt, actUseStunt, 8*time.Second)
	if err != nil || resp == nil {
		slog.Warn("恢复主角职业无回包", "err", err)
		return
	}
	result, roleID, curStunt, pErr := protocol.ParseLibraryUseStuntSkillResult(resp.Payload)
	slog.Info("恢复主角职业回包", "result", result, "text", protocol.LibraryUseStuntResultText(result),
		"cur_role_id", roleID, "cur_stunt_id", curStunt, "parse_err", pErr)
}

func fetchStunts(call func(*protocol.Packet, uint32, time.Duration) (*protocol.Packet, error)) ([]protocol.LibraryStuntInfo, bool) {
	resp, err := call(protocol.BuildLibraryMasterPracticeStuntInfoPacket(), actLibraryInfo, 8*time.Second)
	if err != nil || resp == nil {
		return nil, false
	}
	infos, pErr := protocol.ParseLibraryMasterPracticeStuntInfoResult(resp.Payload)
	if pErr != nil {
		return nil, false
	}
	return infos, true
}

func fetchArtifactStunts(call func(*protocol.Packet, uint32, time.Duration) (*protocol.Packet, error)) ([]protocol.LibraryStuntInfo, bool) {
	resp, err := call(protocol.NewPacket(actArtifactStunt, nil), actArtifactStunt, 8*time.Second)
	if err != nil || resp == nil {
		return nil, false
	}
	infos, pErr := protocol.ParseLibraryMasterPracticeStuntInfoResult(resp.Payload)
	if pErr != nil {
		return nil, false
	}
	return infos, true
}

func currentMainRole(call func(*protocol.Packet, uint32, time.Duration) (*protocol.Packet, error), repo dictionary.Repository) (jobID, roleID int32) {
	resp, err := call(protocol.BuildMasterPracticeDeployInfoPacket(), actDeployInfo, 8*time.Second)
	if err != nil || resp == nil {
		return 0, 0
	}
	info, pErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload)
	if pErr != nil {
		return 0, 0
	}
	for _, r := range info.Roles {
		if r.IsMainRole == 1 {
			return roleJob(repo, r.RoleID), r.RoleID
		}
	}
	return 0, 0
}

func roleJob(repo dictionary.Repository, roleID int32) int32 {
	rt, err := repo.GetRoleType(int(roleID))
	if err != nil || rt == nil {
		return 0
	}
	return int32(rt.JobID)
}

func findStunt(infos []protocol.LibraryStuntInfo, jobID int32) (int32, bool) {
	var fallback int32
	for _, info := range infos {
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
		if info.IsActive == 1 && fallback == 0 {
			fallback = id
		}
	}
	return fallback, fallback > 0
}
