//go:build probe

// 目标: 验证孩子子面板请求的 PrevAct 链要求
//
// 线上出现：主面板 0x01380000 之后紧接孩子任务 0x013B0000 会被静默丢弃，
// 而在学习信息 0x0138001C 之后调用则正常。本探针对比两种顺序。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_children_sequence_live.go
package main

import (
	"context"
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

	call := func(action uint32, payload []byte) {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 6*time.Second)
		resp, err := sess.Call(c, pkt, action)
		cc()
		if err != nil || resp == nil {
			slog.Info("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return
		}
		slog.Info("回包", "action", fmt.Sprintf("0x%08X", action), "len", len(resp.Payload))
	}
	child := protocol.NewWriter()
	child.WriteInt32(1812)
	child.WriteUint32(0)
	childPayload := child.Bytes()

	// B: 功能开放查询(Module 0 Act 69) -> 主面板 -> 孩子任务
	openW := protocol.NewWriter()
	openW.WriteInt32(protocol.FunctionIDWuFaField)
	openW.WriteUint32(0)
	call(0x00000045, openW.Bytes())
	time.Sleep(300 * time.Millisecond)
	call(0x01380000, []byte{0, 0, 0, 0})
	time.Sleep(300 * time.Millisecond)
	call(0x013B0000, childPayload)
}
