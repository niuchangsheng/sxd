//go:build probe

// 目标协议: [Module 2] Mod_Item_Base.get_the_item_num (Action 107 -> 0x0002006B)
// 基准对齐: 官方 item 数量查询接口；区别于 get_player_pack_item_list (仅主背包/仓库物品，
// 不含灵珠、鸿蒙紫气等以系统货币形式存储的资源)。
//
// 运行命令: go run -tags=probe test/probes/probe_item_counts.go -config configs/s0.yaml
// 预期表现: 逐个查询给定道具 ID 的实际持有数量并打印字典名称，只读不消耗资源。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

// 天帝宝库候选奖励涉及的关键道具，以及账号常见养成资源。
var defaultItemIDs = []int32{
	2397, // 鸿蒙紫气
	3783, // 灵珠
	4954, // 血魂石
	3706, // 星魂
	1743, // 仙令
	6909, // 大法力石
	2403, // 道缘
	1740, // 境界点
	2395, // 内丹
	4975, // 黄金圣水
	5397, // 普通进阶丹
	5183, // 命宫碎片
	5117, // 兽魂碎片
	6910, // 八卦阵碎片
	2407, // 超级火龙珠
	5323, // 育灵丹
	2010, // 永恒灵宝
	6499, // 星石
	1411, // 女娲石碎片
	1411, // 重复项仅用于校验去重
}

func main() {
	cfgPath := flag.String("config", "configs/s0.yaml", "配置文件路径")
	itemsFlag := flag.String("items", "", "逗号分隔的道具 ID 列表 (留空使用内置清单)")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("挂载数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

	itemIDs := defaultItemIDs
	if strings.TrimSpace(*itemsFlag) != "" {
		itemIDs = itemIDs[:0]
		for _, part := range strings.Split(*itemsFlag, ",") {
			var id int32
			if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &id); err == nil && id > 0 {
				itemIDs = append(itemIDs, id)
			}
		}
	}

	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	ticket, err := xdAuth.Login(ctx, acc.Username, acc.Password, role.ServerID)
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
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	seen := make(map[int32]bool, len(itemIDs))
	for _, itemID := range itemIDs {
		if seen[itemID] {
			continue
		}
		seen[itemID] = true

		pkt := protocol.BuildGetItemNumPacket(itemID, sess.LastActionID())

		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, protocol.ActionIDItemGetTheItemNum)
		cCancel()

		name := "?"
		if it, dErr := dictRepo.GetItem(int(itemID)); dErr == nil && it != nil {
			name = it.Name
		}
		count := int32(-1)
		if cErr == nil && resp != nil {
			if v, pErr := protocol.ParseGetItemNumResponse(resp.Payload); pErr == nil {
				count = v
			}
		}
		fmt.Printf("item=%-6d %-18s count=%-12d err=%v\n", itemID, name, count, cErr)
	}
}
