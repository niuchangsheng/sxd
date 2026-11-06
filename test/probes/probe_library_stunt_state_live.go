//go:build probe

// 目标协议: Module 85 (Mod_Library_Base) 只读状态核查
//
//	85/1 get_library_info            resp [[Byte job_id, Short level], Short cur_role_id, Short cur_stunt_id, Short study_type_id, UByte activate]
//	85/5 get_artifact_stunt_info     resp [[Byte job_id, Int stunt_id, Int next_stunt_id, Byte is_active, Byte is_use]]
//	85/6 get_tian_shi_stunt_info     resp [[Int stunt_id, Int next_stunt_id, Byte is_active, Byte is_use]]
//	85/7 get_master_practice_stunt_info resp [[Byte job_id, Int stunt_id, Int next_stunt_id, Byte is_active, Byte is_use]]
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_library_stunt_state_live.go
// 预期表现: 只读打印各技能库的绝技激活/使用状态，用于判断 use_stunt_skill 的前置条件。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const (
	actLibraryInfo      uint32 = 0x00550001
	actArtifactStunt    uint32 = 0x00550005
	actTianShiStunt     uint32 = 0x00550006
	actMasterStunt      uint32 = 0x00550007
	actMasterPanel      uint32 = 0x02990001
	actMasterDeployInfo uint32 = 0x02990002
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		defer cCancel()
		resp, err := sess.Call(cCtx, pkt, act)
		if err != nil || resp == nil {
			slog.Warn("查询无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		slog.Info("查询回包", "action", fmt.Sprintf("0x%08X", act), "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		return resp
	}

	if resp := call(protocol.NewPacket(actLibraryInfo, nil), actLibraryInfo); resp != nil {
		parseLibraryInfo(resp.Payload)
	}
	if resp := call(protocol.NewPacket(actArtifactStunt, nil), actArtifactStunt); resp != nil {
		parseStuntList("神器绝技", resp.Payload, 1)
	}
	if resp := call(protocol.NewPacket(actTianShiStunt, nil), actTianShiStunt); resp != nil {
		parseTianShi(resp.Payload)
	}
	if resp := call(protocol.BuildLibraryMasterPracticeStuntInfoPacket(), actMasterStunt); resp != nil {
		parseStuntList("宗师绝技", resp.Payload, 1)
	}
	if resp := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), actMasterPanel); resp != nil {
		if p, err := protocol.ParseMasterPracticePanelResult(resp.Payload); err == nil {
			for _, j := range p.Jobs {
				slog.Info("宗师面板", "job_id", j.JobID, "step", j.StepID, "stars", j.TotalStars, "today", j.TodayChallenge)
			}
		}
	}
	if resp := call(protocol.BuildMasterPracticeDeployInfoPacket(), actMasterDeployInfo); resp != nil {
		if d, err := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload); err == nil {
			for _, r := range d.Roles {
				slog.Info("宗师布阵角色", "player_role_id", r.PlayerRoleID, "is_main", r.IsMainRole, "role_id", r.RoleID, "name", r.RoleName, "is_deploy", r.IsDeploy)
			}
		}
	}
}

func parseLibraryInfo(payload []byte) {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析 library_info count 失败", "err", err)
		return
	}
	for i := 0; i < int(count); i++ {
		jobID, _ := r.ReadUint8()
		level, _ := r.ReadInt16()
		slog.Info("技能库等级", "job_id", jobID, "level", level)
	}
	curRole, err := r.ReadInt16()
	if err != nil {
		slog.Error("解析 cur_role_id 失败", "err", err)
		return
	}
	curStunt, _ := r.ReadInt16()
	studyType, _ := r.ReadInt16()
	activate, _ := r.ReadUint8()
	slog.Info("技能库当前", "cur_role_id", curRole, "cur_stunt_id", curStunt, "study_type_id", studyType, "activate_upgrade", activate)
}

// parseStuntList 解析 [[Byte job_id, Int stunt_id, Int next_stunt_id, Byte is_active, Byte is_use]]。
func parseStuntList(label string, payload []byte, _ int) {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		slog.Error(label+" count 解析失败", "err", err)
		return
	}
	for i := 0; i < int(count); i++ {
		jobID, _ := r.ReadUint8()
		stuntID, _ := r.ReadInt32()
		nextID, _ := r.ReadInt32()
		active, _ := r.ReadUint8()
		use, _ := r.ReadUint8()
		slog.Info(label, "job_id", jobID, "stunt_id", stuntID, "next_stunt_id", nextID, "is_active", active, "is_use", use)
	}
}

// parseTianShi 解析 [[Int stunt_id, Int next_stunt_id, Byte is_active, Byte is_use]]。
func parseTianShi(payload []byte) {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		slog.Error("天师绝技 count 解析失败", "err", err)
		return
	}
	for i := 0; i < int(count); i++ {
		stuntID, _ := r.ReadInt32()
		nextID, _ := r.ReadInt32()
		active, _ := r.ReadUint8()
		use, _ := r.ReadUint8()
		slog.Info("天师绝技", "stunt_id", stuntID, "next_stunt_id", nextID, "is_active", active, "is_use", use)
	}
}
