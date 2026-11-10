//go:build probe

// 目标协议: [Module 306, Action 0] 幻境修炼面板 + [Action 1] 选择伙伴 + [Action 2] 开始修炼
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_partner_practice_live.go
// 可选执行: PARTNER_PRACTICE_EXECUTE=1 时才发送 306/1、306/2（均为免费动作）。
//
// 目的: 验证 start_practice 返回 10(FAILURE) 是否因为缺少同会话的 select_partner。
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(pkt *protocol.Packet, d time.Duration) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, pkt.ActionID)
		if err != nil {
			slog.Warn("请求无应答", "action", fmt.Sprintf("0x%08X", pkt.ActionID),
				"payload_hex", hex.EncodeToString(pkt.Payload), "err", err)
			return nil
		}
		return resp
	}

	infoResp := call(protocol.BuildPartnerPracticeInfoPacket(), 8*time.Second)
	partnerID := int32(0)
	if infoResp != nil {
		slog.Info("面板回包", "payload_hex", hex.EncodeToString(infoResp.Payload))
		if info, e := protocol.ParsePartnerPracticeInfo(infoResp.Payload); e == nil {
			partnerID = info.PartnerRoleID
			slog.Info("当前伙伴", "partner_role_id", partnerID)
		}
	}

	if os.Getenv("PARTNER_PRACTICE_EXECUTE") != "1" {
		slog.Info("只读模式结束（设置 PARTNER_PRACTICE_EXECUTE=1 才执行选择/开始）")
		return
	}
	if partnerID <= 0 {
		slog.Warn("面板未返回伙伴 ID，无法选择")
		return
	}

	// 306/1 选择伙伴 [Int role_id]
	w := protocol.NewWriter()
	w.WriteInt32(partnerID)
	selResp := call(protocol.NewPacket(protocol.ActionIDPartnerPracticeSelect, w.Bytes()), 8*time.Second)
	if selResp != nil && len(selResp.Payload) > 0 {
		slog.Info("选择伙伴结果", "result", selResp.Payload[0],
			"result_text", protocol.PartnerPracticeResultText(selResp.Payload[0]))
	}

	time.Sleep(500 * time.Millisecond)

	// 306/2 开始修炼
	var startPkt *protocol.Packet
	if os.Getenv("PARTNER_PRACTICE_PREV") == "1" {
		pw := protocol.NewWriter()
		pw.WriteUint32(sess.LastActionID())
		startPkt = protocol.NewPacket(protocol.ActionIDPartnerPracticeStart, pw.Bytes())
	} else {
		startPkt = protocol.BuildPartnerPracticeStartPacket()
	}
	startResp := call(startPkt, 8*time.Second)
	if startResp != nil && len(startResp.Payload) > 0 {
		slog.Info("开始修炼结果", "result", startResp.Payload[0],
			"result_text", protocol.PartnerPracticeResultText(startResp.Payload[0]))
	}

	if os.Getenv("PARTNER_PRACTICE_STOP") == "1" {
		time.Sleep(500 * time.Millisecond)
		stopResp := call(protocol.BuildPartnerPracticeStopPacket(), 8*time.Second)
		if stopResp != nil && len(stopResp.Payload) > 0 {
			slog.Info("结算修炼结果", "result", stopResp.Payload[0],
				"result_text", protocol.PartnerPracticeResultText(stopResp.Payload[0]),
				"payload_hex", hex.EncodeToString(stopResp.Payload))
		}
	}
}
