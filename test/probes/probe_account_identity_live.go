//go:build probe

// 目标: 诊断心动平台换票结果对应的角色/UID，确认 Cookie 是否漂移到其它账号。
//
// 运行命令:
//   SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_account_identity_live.go
//   SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_account_identity_live.go
//
// 行为: 只访问 Web 换票链路（games/play、index_wd.php），不连接游戏网关、不登录游戏。
// 打印 ticket.RoleName、区服与若干非敏感 FlashVars，用于判断 Cookie 当前绑定的账号。
// 安全约束: 不打印 hash_code / cookie / password。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
)

func main() {
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	fmt.Printf("config=%s username=%s expected_role=%s server=%s\n",
		cfgPath, acc.Username, role.RoleName, role.ServerID)
	fmt.Printf("ticket.role_name=%q server_id=%q gateway=%s\n",
		ticket.RoleName, ticket.ServerID, ticket.GatewayURL)
	keys := make([]string, 0, len(ticket.Extra))
	for k := range ticket.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch k {
		case "hash_code", "hash", "password", "cookie":
			fmt.Printf("  %s = <redacted>\n", k)
		default:
			fmt.Printf("  %s = %s\n", k, ticket.Extra[k])
		}
	}
}