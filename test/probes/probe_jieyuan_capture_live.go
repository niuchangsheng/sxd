//go:build probe

// 目标: 原样复现结缘宝箱抓包链路。
//
// 抓包顺序 (TCP 段长度):
//
//	14B C→S : 8B 帧头 + [07 04 + Uint32 PrevAct]      (528/1 握手)
//	 9B S→C : 握手结果
//	20B C→S : 8B 帧头 + [Int item_id, Int count, Uint32 PrevAct=0x02100001]
//	53B S→C : 开箱结果
//
// 本探针在 s1（有 3937 × 10）上严格按此顺序发送并打印回包。
//
// 运行: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_jieyuan_capture_live.go
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

const (
	actionList    uint32 = 0x00E8001E // 232/30
	actionContext uint32 = 0x02100001 // 528/1
	actionOpen    uint32 = 0x00E80022 // 232/34
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s1.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
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

	call := func(payload []byte, act uint32, name string) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, protocol.NewPacket(act, payload), act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 60 {
			limit = 60
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	// 1. 列表：[PrevAct]
	listPayload := protocol.NewWriter().WriteUint32(0x00040000).Bytes()
	if call(listPayload, actionList, "232_30_list") == nil {
		return
	}

	// 2. 528/1 握手：[0x0704, PrevAct=232/30]
	hs := protocol.NewWriter()
	hs.WriteUint16(0x0704)
	hs.WriteUint32(actionList)
	if call(hs.Bytes(), actionContext, "528_1_context") == nil {
		slog.Info("528/1 握手无回包")
		return
	}

	// 3. 开箱：[item_id, count, PrevAct=528/1]
	open := protocol.NewWriter()
	open.WriteInt32(3937)
	open.WriteInt32(10)
	open.WriteUint32(actionContext)
	if call(open.Bytes(), actionOpen, "232_34_open_after_context") == nil {
		slog.Info("握手后开箱仍无回包")
		return
	}
	slog.Info("抓包链路复现成功")
}