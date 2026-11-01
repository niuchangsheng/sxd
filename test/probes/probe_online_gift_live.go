//go:build probe

// 目标协议: [Module 0, Action 31] get_player_current_online_gift 查询在线礼包状态
//
//	[Module 0, Action 30] player_get_online_gift 领取在线礼包
//
// 基准对齐: 原版客户端 Main.swf Mod_Player_Base.cinit 协议描述表 (请求体均为空)
// 运行命令: go run -tags=probe test/probes/probe_online_gift_live.go
// 预期表现: 登录 s0 后打印查询与领取的服务端原始回包、结果码与奖励列表，
//           直接复用生产 routine `daily.NewOnlineGiftRoutine()` 验证真实代码路径。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/daily"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置文件缺少账号或角色")
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	// 注册原始回包嗅探器，完整打印查询与领取的服务端负载
	sniff := func(tag string) func(*protocol.Packet) {
		return func(pkt *protocol.Packet) {
			payload := pkt.Payload
			if len(payload) > 128 {
				payload = payload[:128]
			}
			fmt.Printf("  [SNIFF %s] action=0x%08X len=%d payload=%s\n",
				tag, pkt.ActionID, len(pkt.Payload), hex.EncodeToString(payload))
		}
	}
	sess.RegisterHandler(protocol.ActionIDPlayerGetCurrentOnlineGift, sniff("get_current_online_gift"))
	sess.RegisterHandler(protocol.ActionIDPlayerGetOnlineGift, sniff("player_get_online_gift"))

	st := sess.GetPlayerState()
	fmt.Printf("\n============================================================\n")
	fmt.Printf("角色: %s (Lv.%d) 区服: %s\n", sess.OriginNickName(), st.Level, role.ServerID)
	fmt.Printf("执行前资产: 铜钱=%d 元宝=%d 背包容量=%d\n", st.Coins, st.Ingots, st.BagCapacity)
	fmt.Printf("============================================================\n")

	// 直接执行生产 routine，验证真实代码路径
	routine := daily.NewOnlineGiftRoutine()
	start := time.Now()
	if err := routine.Execute(ctx, sess, nil); err != nil {
		slog.Error("online_gift routine 执行失败", "err", err)
	} else {
		fmt.Printf("\n[routine] online_gift 执行完成，耗时 %s\n", time.Since(start).Round(time.Millisecond))
	}

	// 给服务端异步下发留出窗口后打印最终快照
	time.Sleep(1500 * time.Millisecond)
	after := sess.GetPlayerState()
	fmt.Printf("[after] 铜钱=%d 元宝=%d\n", after.Coins, after.Ingots)
	fmt.Printf("============================================================\n\n")
}