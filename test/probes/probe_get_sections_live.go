//go:build probe

// 目标: 定位任务 478 (关卡 560, 天境(6)) 的 get_sections (Mod 4, Act 0) 静默丢包原因。
//
// 线上日志: map_id=21 的 get_sections 无回包，导致任务推图直接跳过该关卡。
// 本探针依次尝试 Missions.map / mapkey / SectionId / NPC 城镇 / 当前城镇，
// 观察哪个参数能稳定回包。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_get_sections_live.go -role 1
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	missionID := flag.Int("mission", 560, "关卡 ID")
	waitBefore := flag.Duration("wait", 0, "登录后等待多久再请求 (用于验证长会话是否影响 get_sections)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	repo, rErr := dictionary.NewRepository("")
	if rErr != nil {
		slog.Error("打开字典失败", "err", rErr)
		return
	}
	defer repo.Close()
	m, mErr := repo.GetMission(*missionID)
	if mErr != nil {
		slog.Error("查询关卡失败", "mission", *missionID, "err", mErr)
		return
	}
	slog.Info("关卡数据", "mission", m.ID, "name", m.Name,
		"map_id", m.MapID, "map_key", m.MapKey, "section_id", m.SectionID,
		"mission_lock", m.MissionLock)
	slog.Info("候选参数", "map", m.MapID, "map_key", m.MapKey, "section", m.SectionID,
		"current_town", sess.GetPlayerState().TownID)

	if *waitBefore > 0 {
		slog.Info("等待后再请求", "wait", *waitBefore)
		time.Sleep(*waitBefore)
	}
	if _, sErr := sess.SyncQuests(ctx); sErr != nil {
		slog.Warn("同步任务列表失败", "err", sErr)
	}
	slog.Info("同步后锚点", "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))

	// 依次用不同 PrevAct 发送 get_sections(21)，定位任务链路丢包原因。
	candidates := []struct {
		name string
		prev uint32
	}{
		{"last_ack", sess.LastAckActionID()},
		{"last_action", sess.LastActionID()},
		{"town_enter", protocol.ActionIDTownEnter},
		{"login_anchor", 0x00000002},
	}
	for _, cand := range candidates {
		time.Sleep(600 * time.Millisecond)
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.BuildMissionGetSectionsPacket(21, cand.prev),
			protocol.ActionIDMissionGetSections)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("无回包", "prev", cand.name, "prev_act", fmt.Sprintf("0x%08X", cand.prev), "err", cErr)
			continue
		}
		limit := len(resp.Payload)
		if limit > 60 {
			limit = 60
		}
		slog.Info("有回包", "prev", cand.name, "prev_act", fmt.Sprintf("0x%08X", cand.prev),
			"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		if secs, pErr := protocol.ParseMissionGetSectionsResponse(resp.Payload); pErr == nil {
			found := false
			for _, sec := range secs {
				if sec.MissionID == int32(*missionID) {
					found = true
					slog.Info("目标关卡在列表中", "mission", sec.MissionID, "status", sec.Status)
				}
			}
			slog.Info("章节列表", "count", len(secs), "target_found", found)
		} else {
			slog.Warn("解析章节失败", "err", pErr)
		}
	}
	slog.Info("探针结束")
}
