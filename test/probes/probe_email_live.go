//go:build probe

// 目标协议: [Module 345, Action 0] get_email_info 查询邮箱面板; [Module 345, Action 6] one_key_get_award 邮箱附件一键收取
// 基准对齐: 官方客户端 Main.swf 的 com.protocols.Mod_Email_Base.cinit 协议描述表; 空请求在 DataBase.netCall 中会自动追加 4 字节 PrevAct
// 运行命令: go run -tags=probe test/probes/probe_email_live.go
// 预期表现: 登录主服后先同步邮箱面板，再触发一键收取; 打印服务端原始回包字节与业务结果码 (8=SUCCESS, 22=NO_AWARD, 13~16=对应背包已满)
// 复用说明: 直接执行生产 routine `daily.NewMailCollectRoutine()` 验证真实代码路径。
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
	sess.RegisterHandler(protocol.ActionIDEmailGetInfo, sniff("get_email_info"))
	sess.RegisterHandler(protocol.ActionIDEmailOneKeyGetAward, sniff("one_key_get_award"))

	st := sess.GetPlayerState()
	fmt.Printf("\n============================================================\n")
	fmt.Printf("角色: %s (Lv.%d) 区服: %s 背包容量: %d\n",
		sess.OriginNickName(), st.Level, role.ServerID, st.BagCapacity)
	fmt.Printf("============================================================\n")

	routine := daily.NewMailCollectRoutine()
	start := time.Now()
	if err := routine.Execute(ctx, sess, nil); err != nil {
		slog.Error("mail_collect routine 执行失败", "err", err)
	} else {
		fmt.Printf("\n[routine] mail_collect 执行完成，耗时 %s\n", time.Since(start).Round(time.Millisecond))
	}
}
