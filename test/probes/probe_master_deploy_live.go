//go:build probe

// 目标: 抓取宗师修行布阵信息 (0x299/2) 的真实回包字节，用于修正字段布局。
//
// 背景: 2026-11-26 线上修复 PrevAct 尾链后，0x299/2 从静默丢弃变为正常回包
// (1077 字节)，但现有解析器在 deploy_role[1].string[0] 处 EOF，说明字段
// 定义与实包不一致。本探针登录一次、依次查询面板与布阵信息，输出完整载荷。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_master_deploy_live.go
//
// 注意: 会顶掉该角色正在运行的会话，运行前请先停服。只读探测，不消耗资源。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	idx := 0
	if v := os.Getenv("SXD_PROBE_ROLE_INDEX"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 && n < len(acc.Roles) {
			idx = n
		}
	}
	role := acc.Roles[idx]

	tc := platform.NewTicketCache(90 * time.Minute)
	auth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	auth.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(auth)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
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

	deadline := time.Now().Add(30 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", roleCfg.RoleID)

	call := func(pkt *protocol.Packet, act uint32, label string) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, 8*time.Second)
		defer ccancel()
		resp, err := sess.Call(c, pkt, act)
		if err != nil || resp == nil {
			slog.Warn("请求失败", "label", label, "err", err)
			return nil
		}
		slog.Info("请求成功", "label", label, "payload_len", len(resp.Payload),
			"payload_hex", hex.EncodeToString(resp.Payload))
		return resp
	}

	panel := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter),
		protocol.ActionIDMasterPracticePanel, "panel 0x299/1")
	if panel != nil {
		if p, pErr := protocol.ParseMasterPracticePanelResult(panel.Payload); pErr == nil {
			slog.Info("面板解析", "result", p.Result, "challenge_times", p.TodayChallengeTimes,
				"jobs", len(p.Jobs))
		}
	}

	deploy := call(protocol.BuildMasterPracticeDeployInfoPacket(),
		protocol.ActionIDMasterPracticeDeployInfo, "deploy_info 0x299/2")
	if deploy != nil {
		info, pErr := protocol.ParseMasterPracticeDeployInfoResult(deploy.Payload)
		if pErr != nil {
			slog.Warn("布阵信息解析失败（预期复现）", "err", pErr)
		} else {
			slog.Info("布阵信息解析成功", "deploy_level", info.DeployLevel,
				"roles", len(info.Roles), "grids", len(info.Grids))
		}
	}
}