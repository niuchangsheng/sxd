//go:build probe

// 目标协议: [Module 419, Action 1] 圣盟膜拜面板 + [Action 2] 铜钱/免费膜拜
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_sa_union_worship_live.go
// 可选执行: WORSHIP_EXECUTE=1 时才真正发送 419/2（只使用 type=0 铜钱/免费档）。
//
// 目的: 对比三种载荷形态下 419/2 的结果码：
//   A. [UByte type]                     (当前例程)
//   B. [UByte type][UInt32 prevAct]     (official 链式风格)
//   C. [UByte type][UInt32 prevAct=面板] (显式指向面板)
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	st := sess.GetPlayerState()
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots", st.Ingots, "coins", st.Coins)

	call := func(pkt *protocol.Packet, d time.Duration) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, pkt.ActionID)
		if err != nil {
			slog.Warn("请求无应答", "action", fmt.Sprintf("0x%08X", pkt.ActionID),
				"payload_hex", hex.EncodeToString(pkt.Payload), "err", err)
			return nil
		}
		dumpPacket("回包", resp)
		return resp
	}

	// 面板
	panelResp := call(protocol.BuildSaUnionWorshipPanelPacketWithPrev(sess.LastActionID()), 8*time.Second)
	_ = panelResp

	if os.Getenv("WORSHIP_EXECUTE") != "1" {
		slog.Info("只读模式结束（设置 WORSHIP_EXECUTE=1 才发送 419/2）")
		return
	}

	run := func(label string, payload []byte) {
		pkt := protocol.NewPacket(protocol.ActionIDSaUnionWorship, payload)
		slog.Info("发送膜拜", "case", label, "payload_hex", hex.EncodeToString(payload))
		if resp := call(pkt, 8*time.Second); resp != nil && len(resp.Payload) > 0 {
			slog.Info("膜拜结果", "case", label, "result", resp.Payload[0],
				"result_text", protocol.SaUnionWorshipResultText(resp.Payload[0]))
		}
	}

	run("A-[UByte type]", []byte{protocol.SaUnionWorshipUseCoin})
	time.Sleep(500 * time.Millisecond)
	w := protocol.NewWriter()
	w.WriteUint8(protocol.SaUnionWorshipUseCoin)
	w.WriteUint32(sess.LastActionID())
	run("B-[UByte type][PrevAct]", w.Bytes())
	time.Sleep(500 * time.Millisecond)
	w2 := protocol.NewWriter()
	w2.WriteUint8(protocol.SaUnionWorshipUseCoin)
	w2.WriteUint32(protocol.ActionIDSaUnionWorshipPanel)
	run("C-[UByte type][PrevAct=面板]", w2.Bytes())

	st = sess.GetPlayerState()
	slog.Info("结束资产", "ingots", st.Ingots, "coins", st.Coins)
}

func dumpPacket(label string, pkt *protocol.Packet) {
	if pkt == nil {
		fmt.Printf("%s: <nil>\n", label)
		return
	}
	fmt.Printf("\n=== %s ===\nActionID: 0x%08X PayloadLen: %d\nPayloadHex: %s\n",
		label, pkt.ActionID, len(pkt.Payload), hex.EncodeToString(pkt.Payload))
}