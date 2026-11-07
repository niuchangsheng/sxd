//go:build probe

// 目标: 宗门灵兽喂养 (Mod_SectMonster_Base, Module 576)
//
//	egg_panel_info          (Act 0, 0x02400000) request=[Int]
//	get_monster_list        (Act 1, 0x02400001) request=[Int]
//	feed                    (Act 3, 0x02400003) request=[Int, Int] response=[UByte,[Int,Int]]
//	get_common_monster_list (Act 4, 0x02400004) request=[]
//
// 每日活跃度任务 77「每日宗门灵兽喂养」每次 +20。
// 本探针先只读探测；仅在能解析出灵兽 id 时调用一次 feed (不涉及元宝档
// NOT_ENOUGH_INGOT 仅表示资源不足，不会自动扣元宝)。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_sect_monster_live.go
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 6*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	zero := []byte{0, 0, 0, 0}
	if r := call(0x02400004, zero); r != nil {
		slog.Info("通用灵兽列表", "hex", hex.EncodeToString(r.Payload))
	}
	if r := call(0x02400001, zero); r != nil {
		slog.Info("宗门灵兽列表", "hex", hex.EncodeToString(r.Payload))
	}
	if r := call(0x02400000, zero); r != nil {
		slog.Info("灵兽蛋面板", "hex", hex.EncodeToString(r.Payload))
	}
}
