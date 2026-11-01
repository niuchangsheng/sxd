//go:build probe

// 目标协议: 平台换票与主服网关地址诊断 (只读，不发生任何游戏业务动作)
// 基准对齐: 对齐账号登录链路取得 GatewayURL，用于比对抓包服务器地址
// 运行命令: go run -tags=probe test/probes/probe_gateway_diag.go
// 预期表现: 输出 ticket.GatewayURL 与区服标识后立即退出
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
)

func main() {
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	fmt.Printf("GATEWAY=%s SERVER_ID=%s ROLE=%s CODE=%d\n",
		ticket.GatewayURL, role.ServerID, role.RoleName, ticket.MainServer.Code)
}