//go:build probe

// 目标: 验证魔王试炼 create_team (57/4) 是否只能在圣域会话上成功。
//
// 背景: s0 主服对 create_team 恒定静默丢弃（任意载荷/bossID），
// 而同连接内其它 57 动作用正常回包。本探针在主服与 Mod 293 圣域会话上
// 分别执行「面板 -> 资格 -> 建队 -> 队伍信息」，对比回包行为。
//
// 运行: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_beelzebub_sa_create_live.go
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

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
	}
	client.ApplyTicket(&roleCfg, ticket)
	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}
	slog.Info("主服会话已激活", "role_id", sess.RoleID())

	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		defer cCancel()
		resp, cErr := s.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	tryCreate := func(s *client.RoleSession, prefix string, bossID int32) {
		call(s, protocol.BuildBeelzebubTrialsOpenPacket(s.LastActionID()),
			protocol.ActionIDBeelzebubTrialsOpen, prefix+"_open")
		call(s, protocol.BuildBeelzebubTrialsCheckCanChallengePacket(s.LastActionID()),
			protocol.ActionIDBeelzebubTrialsCheckCanChallenge, prefix+"_check")
		resp := call(s, protocol.BuildBeelzebubTrialsCreateTeamPacket(bossID, protocol.BeelzebubTrialsNotLimit, s.LastActionID()),
			protocol.ActionIDBeelzebubTrialsCreateTeam, prefix+"_create")
		if resp != nil {
			code, _ := protocol.ParseBeelzebubTrialsCreateTeamResponse(resp.Payload)
			slog.Info("建队回包解析", "prefix", prefix, "boss_id", bossID, "result", code)
			_ = s.BeelzebubTrialsExitTeam(ctx)
		}
		call(s, protocol.BuildBeelzebubTrialsGetPlayerTeamInfoPacket(s.LastActionID()),
			protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, prefix+"_team_info")
	}

	// 1. 主服基线（预期静默丢弃）。
	tryCreate(sess, "main", 144)

	// 2. 圣域会话。
	sa, err := sess.OpenSaintAreaSession(ctx)
	if err != nil {
		slog.Error("建立圣域会话失败", "err", err)
		return
	}
	defer sa.Close()
	slog.Info("圣域会话已建立", "player_id", sa.PlayerID())
	tryCreate(sa, "sa", 144)
	tryCreate(sa, "sa2", 139)
	slog.Info("探针结束")
}