//go:build probe

// 目标: 每日活跃度任务 14「使用仙露」(道具 5145 观音仙露) 可用性
//
// 读取背包中的 5145 格位, 仅在 -use 时调用 player_use_grid_item (Mod 2 Act 53)。
// 使用背包已有道具不消耗元宝。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_xianlu_live.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const xianLuItemID int32 = 5145

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	use := flag.Int("use", 0, "使用观音仙露的个数 (0=只读)")
	flag.Parse()
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

	numCtx, numCancel := context.WithTimeout(ctx, 8*time.Second)
	num, numErr := sess.GetItemNum(numCtx, xianLuItemID)
	numCancel()
	if numErr != nil {
		slog.Warn("查询观音仙露数量失败", "err", numErr)
	} else {
		slog.Info("观音仙露数量", "item_id", xianLuItemID, "num", num)
	}

	packCtx, packCancel := context.WithTimeout(ctx, 8*time.Second)
	packPkt, err := sess.Call(packCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	packCancel()
	if err != nil {
		slog.Error("拉取背包失败", "err", err)
		return
	}
	pack, err := protocol.ParsePackItemListResponse(packPkt.Payload)
	if err != nil {
		slog.Error("解析背包失败", "err", err)
		return
	}
	grids := make([]int32, 0, 4)
	for _, it := range pack.PackItems {
		if it.ItemID == xianLuItemID {
			grids = append(grids, it.GridID)
		}
	}
	slog.Info("背包中的仙露格位", "grids", grids)
	if *use <= 0 || len(grids) == 0 {
		return
	}
	st := sess.GetPlayerState()
	done := 0
	for i := 0; i < len(grids) && done < *use; i++ {
		pkt, pErr := protocol.BuildUseGridItemPacket(int16(grids[i]))
		if pErr != nil {
			slog.Warn("构造使用封包失败", "err", pErr)
			break
		}
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		resp, uErr := sess.Call(c, pkt, protocol.ActionIDItemUseGrid)
		cc()
		if uErr != nil {
			slog.Warn("使用仙露无回包", "grid", grids[i], "err", uErr)
			break
		}
		ok, code, pErr := protocol.ParseUseGridItemResponse(resp.Payload)
		slog.Info("使用仙露", "grid", grids[i], "ok", ok, "code", code, "err", pErr)
		if !ok {
			break
		}
		done++
	}
	fin := sess.GetPlayerState()
	slog.Info("使用仙露结束", "used", done, "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
