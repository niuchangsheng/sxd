//go:build probe

// 目标: 验证琳琅阁在线/关闭面板是否触发「每日领取好礼馈赠」(任务 82)
//
//	lucky_store_online     (100/127, 0x0064007F) request=[] response=[UByte]
//	close_lucky_store_panel(100/126, 0x0064007E) request=[] response=[UByte]
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_lucky_store_gift_live.go
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
	act := func(tag string) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if err != nil || resp == nil {
			slog.Warn("活跃度读取失败", "tag", tag)
			return
		}
		p, err := protocol.ParseGameAssistantInfo(resp.Payload)
		if err != nil {
			return
		}
		slog.Info("活跃度", "tag", tag, "daily_total", p.DailyTotal(),
			"task82_remain", p.RemainingTimesFor(82), "task74_remain", p.RemainingTimesFor(74))
	}
	call := func(action uint32) {
		pkt := protocol.NewPacket(action, []byte{0, 0, 0, 0})
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return
		}
		slog.Info("回包", "action", fmt.Sprintf("0x%08X", action), "hex", hex.EncodeToString(resp.Payload))
	}
	act("before")
	call(0x0064007F) // lucky_store_online
	act("after_online")
	call(0x0064007E) // close_panel
	act("after_close")
}
