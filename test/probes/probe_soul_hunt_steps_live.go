//go:build probe

// 目标: 定位灵域猎妖 669/3 关卡面板被静默丢弃的原因：确认请求参数 step
// 应取主面板 values 中的哪一个字段，并扫描 1..12 观察服务端是否回包。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_soul_hunt_steps_live.go
//
// 行为: 登录 → 669/0 活动状态 → 669/1 主面板(打印完整 hex) →
// 依次请求 669/3 step=1..12（每个 4 秒），只读查询，不挑战/不购买。
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
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置缺少账号或角色")
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(90 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := sess.Call(c, pkt, act)
		ccancel()
		if err != nil {
			slog.Info("请求无回包", "name", name, "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		head := resp.Payload
		if len(head) > 400 {
			head = head[:400]
		}
		slog.Info("回包", "name", name, "action", fmt.Sprintf("0x%08X", act),
			"len", len(resp.Payload), "hex", hex.EncodeToString(head))
		return resp
	}

	call(protocol.BuildSoulHuntActivityStatePacket(), protocol.ActionIDSoulHuntActivityState, "669/0 活动状态", 8*time.Second)
	panelResp := call(protocol.BuildSoulHuntMainPanelPacket(), protocol.ActionIDSoulHuntMainPanel, "669/1 主面板", 8*time.Second)
	if panelResp != nil {
		if panel, pErr := protocol.ParseSoulHuntMainPanel(panelResp.Payload); pErr == nil {
			slog.Info("主面板解析", "result", panel.Result, "values", panel.Values,
				"current_step(Values[1])", panel.CurrentStep(), "step_pairs", panel.StepPairs,
				"tail", panel.Tail)
		} else {
			slog.Info("主面板解析失败", "err", pErr)
		}
	}

	for step := int32(1); step <= 12; step++ {
		resp := call(protocol.BuildSoulHuntChallengePanelPacket(step),
			protocol.ActionIDSoulHuntChallengePanel, fmt.Sprintf("669/3 step=%d", step), 4*time.Second)
		if resp != nil {
			if code, cErr := protocol.ParseSoulHuntResult(resp.Payload); cErr == nil {
				slog.Info("关卡面板结果", "step", step, "result", code,
					"text", protocol.SoulHuntResultText(code), "len", len(resp.Payload))
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}