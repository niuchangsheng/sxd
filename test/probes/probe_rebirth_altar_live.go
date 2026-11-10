//go:build probe

// 目标协议: [Module 852, Action 12] 渡业仙池面板 + [Module 852, Action 16] 渡业台抽取
// 运行命令: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_rebirth_altar_live.go
// 可选执行: REBIRTH_ALTAR_DRAW=1 时才真正发送 852/16 抽取写操作。
//
// 目的: 对比两种 PrevAct 形态下 852/16 的回包差异：
//
//	A. 先发空载荷 852/12（当前例程做法），再发 852/16；
//	B. 先发带 [prevAct] 载荷的 852/12（官方抓包形态），再发 852/16。
//
// 全程不调用任何元宝动作，只记录抽取前后的元宝/铜钱。
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
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

	if ms := os.Getenv("REBIRTH_ALTAR_PRE_WAIT_MS"); ms != "" {
		if d, e := time.ParseDuration(ms + "ms"); e == nil {
			slog.Info("登录后等待再测试", "wait", d)
			time.Sleep(d)
		}
	}
	if os.Getenv("REBIRTH_ALTAR_DUMMY") == "1" {
		mode := os.Getenv("REBIRTH_ALTAR_DUMMY_MODE")
		if mode == "" {
			mode = "both"
		}
		slog.Info("先发送若干普通业务请求模拟长会话上下文", "mode", mode)
		for i := 0; i < 3; i++ {
			if mode == "both" || mode == "assistant" {
				call(protocol.BuildGameAssistantInfoPacket(), 5*time.Second)
			}
			if mode == "both" || mode == "bag" {
				call(protocol.BuildGetPackItemListPacket(), 5*time.Second)
			}
		}
		if ms := os.Getenv("REBIRTH_ALTAR_DUMMY_WAIT_MS"); ms != "" {
			if d, e := time.ParseDuration(ms + "ms"); e == nil {
				slog.Info("模拟操作间隔", "wait", d)
				time.Sleep(d)
			}
		}
	}

	if os.Getenv("REBIRTH_ALTAR_RESYNC") == "1" {
		anchor := sess.LastAckActionID()
		if anchor == 0 {
			anchor = sess.LastActionID()
		}
		slog.Info("尝试用 0/2 player_info 重建 PrevAct 链", "anchor", fmt.Sprintf("0x%08X", anchor))
		call(protocol.BuildPlayerGetInfoPacket(int32(anchor)), 8*time.Second)
	}

	// A. 当前例程形态：空载荷 852/12 -> 852/16
	// 设置 REBIRTH_ALTAR_SKIP_A=1 可跳过 A，直接从干净的 PrevAct 链测试 B。
	if os.Getenv("REBIRTH_ALTAR_SKIP_A") == "1" {
		slog.Info("跳过 A（REBIRTH_ALTAR_SKIP_A=1），直接测试 B")
	} else {
		slog.Info("A: 空载荷 852/12")
		call(protocol.BuildEraRebirthLandPoolPanelPacket(), 8*time.Second)
	}

	if os.Getenv("REBIRTH_ALTAR_DRAW") != "1" {
		slog.Info("只读模式结束（设置 REBIRTH_ALTAR_DRAW=1 才发送 852/16）")
		return
	}

	if os.Getenv("REBIRTH_ALTAR_SKIP_A") != "1" {
		slog.Info("A: 852/16 抽取")
		drawA := protocol.BuildEraRebirthLandDrawPacket(0, sess.LastActionID())
		if resp := call(drawA, 8*time.Second); resp != nil {
			if res, e := protocol.ParseEraRebirthDrawResult(resp.Payload); e == nil {
				slog.Info("A 抽取结果", "result", res.Result, "first", res.First, "second", res.Second)
			}
		}
		time.Sleep(700 * time.Millisecond)
	}

	// B. 官方形态：带 PrevAct 的 852/12 -> 852/16
	anchor := sess.LastAckActionID()
	if raw := os.Getenv("REBIRTH_ALTAR_ANCHOR_HEX"); raw != "" {
		if v, e := strconv.ParseUint(raw, 16, 32); e == nil {
			anchor = uint32(v)
		}
	}
	w := protocol.NewWriter()
	w.WriteUint32(anchor)
	poolWithPrev := &protocol.Packet{
		ActionID:   protocol.ActionIDEraRebirthLandPoolPanel,
		Payload:    w.Bytes(),
		HasPrevAct: true,
	}
	slog.Info("B: 带 PrevAct 的 852/12", "payload_hex", hex.EncodeToString(poolWithPrev.Payload))
	call(poolWithPrev, 8*time.Second)

	if ms := os.Getenv("REBIRTH_ALTAR_DELAY_MS"); ms != "" {
		if d, e := time.ParseDuration(ms + "ms"); e == nil {
			slog.Info("B: 面板后延迟再抽取", "delay", d)
			time.Sleep(d)
		}
	}

	slog.Info("B: 852/16 抽取")
	drawB := protocol.BuildEraRebirthLandDrawPacket(0, sess.LastActionID())
	if resp := call(drawB, 8*time.Second); resp != nil {
		if res, e := protocol.ParseEraRebirthDrawResult(resp.Payload); e == nil {
			slog.Info("B 抽取结果", "result", res.Result, "first", res.First, "second", res.Second)
		}
	}

	st = sess.GetPlayerState()
	slog.Info("结束资产", "ingots", st.Ingots, "coins", st.Coins)
}

func dumpPacket(label string, pkt *protocol.Packet) {
	if pkt == nil {
		fmt.Printf("%s: <nil>\n", label)
		return
	}
	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("ActionID: 0x%08X (Mod %d, Act %d) PayloadLen: %d\n",
		pkt.ActionID, pkt.ActionID>>16, pkt.ActionID&0xFFFF, len(pkt.Payload))
	fmt.Printf("PayloadHex: %s\n", hex.EncodeToString(pkt.Payload))
}
