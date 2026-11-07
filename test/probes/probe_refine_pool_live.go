//go:build probe

// 目标协议: 粗铁淬炼池 (Mod_RefinePool 282 / Mod 534 资源面板)
// 基准对齐: docs/protocol-spec.md 4.7 与 haha.pcapng 三步链路
//
//	open_panel  282/0 0x011A0000
//	panel_534   534/5 0x02160005
//	refine      282/1 0x011A0001 [target, cost, mode, prevAct]
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_REFINE_VARIANT=A go run -tags=probe test/probes/probe_refine_pool_live.go
//
// 变体:
//
//	A = open -> refine（跳过 534）
//	B = open -> 534 -> refine（官方三步链路）
//
// 仅在 open 面板显示 DoneCount>0 时执行一次一键淬炼（铜钱档），不消耗元宝。
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
		cfgPath = "configs/niuchangsheng.yaml"
	}
	variant := os.Getenv("SXD_PROBE_REFINE_VARIANT")
	if variant == "" {
		variant = "A"
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
	rc := client.SessionConfig{
		RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3,
	}
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
	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		slog.Info("回包", "action", fmt.Sprintf("0x%08X", act), "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:min(40, len(resp.Payload))]))
		return resp
	}

	openResp := call(protocol.BuildRefinePoolOpenPacket(sess.LastActionID()), protocol.ActionIDRefinePoolOpen, 8*time.Second)
	if openResp == nil {
		return
	}
	panel, err := protocol.ParseRefinePoolOpenResponse(openResp.Payload)
	if err != nil {
		slog.Error("解析面板失败", "err", err)
		return
	}
	counts := map[uint8]int{}
	for _, iron := range panel.Irons {
		counts[iron.ProduceStatus]++
	}
	slog.Info("面板", "irons", len(panel.Irons), "status_counts", counts,
		"done", panel.DoneCount(), "coins_need", panel.CoinsNeed, "level", panel.Level)
	if panel.DoneCount() == 0 {
		slog.Info("无已完成槽位，不执行淬炼")
		return
	}

	prev := uint32(protocol.ActionIDRefinePoolOpen)
	if variant == "B" {
		if r := call(protocol.BuildRefinePoolPanelPacket(protocol.ActionIDRefinePoolOpen), protocol.ActionIDRefinePoolPanel, 8*time.Second); r == nil {
			slog.Warn("534 资源面板无回包，仍尝试执行")
		} else {
			prev = protocol.ActionIDRefinePoolPanel
		}
	}
	execPkt := protocol.BuildRefineExecutePacket(protocol.RefinePoolTargetXuanTie, protocol.RefinePoolCostCoins, protocol.RefinePoolTypeOneKey, prev)
	resp := call(execPkt, protocol.ActionIDRefineExecute, 25*time.Second)
	if resp == nil {
		return
	}
	res, err := protocol.ParseRefineExecuteResponse(resp.Payload)
	if err != nil {
		slog.Warn("解析执行回包失败", "err", err)
		return
	}
	slog.Info("执行结果", "result", res.Result, "awards", len(res.Awards),
		"lian_qi_jing_hua", res.LianQiJingHua, "level", res.Level)
}
