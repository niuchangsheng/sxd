//go:build probe

// 目标: 诊断灵域猎妖 669/3 challenge_panel 的 step 参数取值。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_soul_hunt_steps_live.go
//
// 行为: 登录后先读 669/1 主面板，再依次用候选 step（cur_step、当前 round_id、
// 上一/下一 round_id、1、129）请求 669/3，记录是否回包。只读面板查询，
// 不发起 669/5 挑战、不消耗次数/元宝。
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		MaxReconnectAttempts: 3,
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

	call := func(label string, pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := sess.CallWithPrevActResync(c, timeout, pkt, act)
		ccancel()
		ok := err == nil && resp != nil
		head := ""
		if resp != nil {
			h := resp.Payload
			if len(h) > 120 {
				h = h[:120]
			}
			head = hex.EncodeToString(h)
		}
		slog.Info("灵域猎妖诊断", "label", label, "ok", ok, "err", err,
			"resp_len", len(head), "resp_hex", head)
		return resp
	}

	panelResp := call("669/1 main", protocol.BuildSoulHuntMainPanelPacket(),
		protocol.ActionIDSoulHuntMainPanel, 8*time.Second)
	if panelResp == nil {
		return
	}
	panel, pErr := protocol.ParseSoulHuntMainPanel(panelResp.Payload)
	if pErr != nil {
		slog.Error("解析主面板失败", "err", pErr)
		return
	}
	slog.Info("主面板字段", "values", panel.Values, "step_pairs", panel.StepPairs, "tail", panel.Tail)

	curStep := panel.Values[1]
	roundID := panel.Values[2]
	candidates := []struct {
		label string
		step  int32
	}{
		{"cur_step(Values[1])", curStep},
		{"round_id(Values[2])", roundID},
		{"next_round(round_id+1)", roundID + 1},
		{"prev_round(round_id-1)", roundID - 1},
		{"first_pair_id", panel.StepPairs[0][0]},
		{"one", 1},
		{"tail[1]", panel.Tail[1]},
	}
	for _, cand := range candidates {
		call(fmt.Sprintf("669/3 step=%d %s", cand.step, cand.label),
			protocol.BuildSoulHuntChallengePanelPacket(cand.step),
			protocol.ActionIDSoulHuntChallengePanel, 5*time.Second)
		time.Sleep(300 * time.Millisecond)
	}
}