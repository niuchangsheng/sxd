//go:build probe

// 目标: 定位炼宝炉 586/4 start_refine 静默丢包的真实参数/前序语义。
//
// 官方流程 (Mod_RefineFurnace_Base): open_panel(586/1) -> select_type(586/2)
// -> select_air(586/3) -> start_refine(586/4)。面板 response 第 4 个 Int 为
// 当前选中项 (selected)，借此观察 select_type / select_air 是否真正改变服务端状态。
//
// 验证项:
//  1. select_type 候选 (1/2/3) 后重开面板，观察 selected 变化；
//  2. select_air 候选 (7923/7924/7925) 后重开面板，观察 selected 变化；
//  3. start_refine 分别用裸 4 字节与带 PrevAct 的 8 字节载荷，比较回包。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_furnace_air_live.go -role 0
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
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
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

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 60 {
			limit = 60
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	openPanel := func(tag string) *protocol.RefineFurnacePanel {
		time.Sleep(800 * time.Millisecond)
		resp := call(protocol.BuildRefineFurnacePanelPacket(), protocol.ActionIDRefineFurnacePanel, "panel_"+tag)
		if resp == nil {
			return nil
		}
		p, pErr := protocol.ParseRefineFurnacePanel(resp.Payload)
		if pErr != nil {
			slog.Warn("解析面板失败", "tag", tag, "err", pErr)
			return nil
		}
		slog.Info("面板状态", "tag", tag, "level", p.Level, "selected", p.Selected, "materials", p.Materials)
		return p
	}
	selectType := func(t int32) bool {
		pkt := protocol.NewPacket(protocol.ActionIDRefineFurnaceType, protocol.NewWriter().WriteInt32(t).Bytes())
		resp := call(pkt, protocol.ActionIDRefineFurnaceType, fmt.Sprintf("select_type_%d", t))
		time.Sleep(600 * time.Millisecond)
		return resp != nil && len(resp.Payload) > 0 && resp.Payload[0] == protocol.RefineFurnaceSuccess
	}
	selectAir := func(a int32) bool {
		pkt := protocol.NewPacket(protocol.ActionIDRefineFurnaceAir, protocol.NewWriter().WriteInt32(a).Bytes())
		resp := call(pkt, protocol.ActionIDRefineFurnaceAir, fmt.Sprintf("select_air_%d", a))
		time.Sleep(600 * time.Millisecond)
		return resp != nil && len(resp.Payload) > 0 && resp.Payload[0] == protocol.RefineFurnaceSuccess
	}

	if openPanel("init") == nil {
		return
	}

	// 1. select_type 语义：每次成功后重开面板看 selected。
	for _, t := range []int32{1, 2, 3} {
		ok := selectType(t)
		slog.Info("select_type 回包", "type", t, "ok", ok)
		openPanel(fmt.Sprintf("after_type_%d", t))
	}

	// 2. select_air 语义：每次成功后重开面板看 selected。
	for _, a := range []int32{7923, 7924, 7925, 1} {
		ok := selectAir(a)
		slog.Info("select_air 回包", "air", a, "ok", ok)
		openPanel(fmt.Sprintf("after_air_%d", a))
	}

	// 3. start_refine 载荷形态对比：裸 4 字节 vs 带 PrevAct 占位 8 字节。
	bare := protocol.NewPacket(protocol.ActionIDRefineFurnaceStart,
		protocol.NewWriter().WriteInt32(1).Bytes())
	if resp := call(bare, protocol.ActionIDRefineFurnaceStart, "start_bare_1"); resp != nil && len(resp.Payload) > 0 {
		slog.Info("start 裸载荷结果", "result", resp.Payload[0])
	}
	if resp := call(protocol.BuildRefineFurnaceStartPacket(1), protocol.ActionIDRefineFurnaceStart, "start_prevact_1"); resp != nil && len(resp.Payload) > 0 {
		slog.Info("start PrevAct 载荷结果", "result", resp.Payload[0])
	}
	slog.Info("探针结束")
}
