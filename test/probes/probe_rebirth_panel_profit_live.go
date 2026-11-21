//go:build probe

// 目标: 只读验证轮回净地渡业台 852/1 面板与 852/9 挂机收益，不调用 852/8。
//
// 2026-11-21 线上发现 852/8 第 3 次起会扣 10 元宝，本探针用于隔离验证
// 852/9 是否免费，并输出完整面板 Hex 以便定位免费次数/收益字段。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_rebirth_panel_profit_live.go
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
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	call := func(act uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		pkt := protocol.NewPacket(act, []byte{0, 0, 0, 0})
		pkt.HasPrevAct = true
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	st := sess.GetPlayerState()
	slog.Info("探测前", "coins", st.Coins, "ingots", st.Ingots)
	if r := call(protocol.ActionIDEraRebirthLandPanel); r != nil {
		slog.Info("852/1 面板", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
	}
	if r := call(protocol.ActionIDEraRebirthLandAutoProfit); r != nil {
		slog.Info("852/9 收益", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
		if p, err := protocol.ParseEraRebirthAutoProfit(r.Payload); err == nil {
			slog.Info("852/9 解析", "result", p.Result, "profit", p.Profit, "rewards", p.Rewards)
		}
	}
	time.Sleep(2 * time.Second)
	fin := sess.GetPlayerState()
	slog.Info("探测后", "coins", fin.Coins, "ingots", fin.Ingots,
		"coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
