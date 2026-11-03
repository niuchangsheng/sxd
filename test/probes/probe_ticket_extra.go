//go:build probe

// 目标协议: 心动平台换票链路 (www.xd.com/games/play -> index_wd.php) 的 FlashVars 扩展字段诊断。
// 基准对齐: 官方微端 weiduan_shell 的 Verison/WDURI 逻辑: 客户端依赖服务端返回的
// client_url / index_url / diff_url / cur_ver / index_urls / diff_urls 构造 Game.swf 的 CDN 地址。
// 运行命令: go run -tags=probe test/probes/probe_ticket_extra.go
// 预期表现: 打印 Ticket.Extra 的键列表与版本/CDN 相关字段, 用于定位官方游戏主 SWF 的下载地址。
// 安全约束: 不打印 hash_code / password / cookie 等敏感凭据。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	keys := make([]string, 0, len(ticket.Extra))
	for k := range ticket.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("Extra keys (%d): %v\n", len(keys), keys)
	for _, k := range keys {
		switch k {
		case "hash_code", "hash", "password", "cookie", "player_name", "user", "username":
			fmt.Printf("  %s = <redacted>\n", k)
		default:
			fmt.Printf("  %s = %s\n", k, ticket.Extra[k])
		}
	}
}
