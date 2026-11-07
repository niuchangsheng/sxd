//go:build probe

// 目标: 神龙祭坛临时格状态与逐颗收取 (Mod_Dragonball_Base 22/1 与 22/2)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_dragonball_tmp_live.go
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
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Second)
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	info := call(protocol.ActionIDDragonballTmpInfo, nil)
	if info == nil {
		slog.Error("临时格查询失败")
		return
	}
	balls, err := protocol.ParseDragonballTmpInfoResult(info.Payload)
	if err != nil {
		slog.Error("解析临时格失败", "err", err)
		return
	}
	slog.Info("临时格龙珠", "count", len(balls))
	collected, invalid := 0, 0
	for _, b := range balls {
		if ctx.Err() != nil {
			break
		}
		resp := call(protocol.ActionIDDragonballCollect, protocol.BuildDragonballCollectPacket([]int32{b.PlayerDBID}).Payload)
		if resp == nil {
			continue
		}
		result, ids, perr := protocol.ParseDragonballCollectResult(resp.Payload)
		if perr != nil {
			continue
		}
		switch result {
		case protocol.DragonballResultSuccess, protocol.DragonballResultPackageFull:
			collected++
		case protocol.DragonballResultInvalidDragonball:
			invalid++
		}
		slog.Info("收取", "player_d_b_id", b.PlayerDBID, "result", result, "ids", ids)
		time.Sleep(100 * time.Millisecond)
	}
	slog.Info("收取汇总", "collected", collected, "invalid", invalid)
}
