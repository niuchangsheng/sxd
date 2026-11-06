//go:build probe

// 目标: 只读审计禁地之书各区域的探索余量与计费字段，定位元宝异常消耗
//
//	panel_info (Mod 264? -> 见 protocol.ActionIDForbiddenBookInfo) 回包:
//	  [ [UByte forbidden_type, UByte explore_type, Int cost_coin, Int cost_ingot,
//	     Short cur_number, Short total_number, Int next_free_time] ],
//	  Int consum_score
//
// 若某区域 cost_ingot > 0 且 cur_number > 0，说明"免费探索"例程可能直接扣元宝，
// 需要立即在例程中加入 cost_ingot 门禁。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_forbidden_book_audit_live.go
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	st := sess.GetPlayerState()
	slog.Info("当前资产", "ingots", st.Ingots, "coins", st.Coins)

	cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
	resp, cErr := sess.Call(cCtx, protocol.BuildForbiddenBookInfoPacket(), protocol.ActionIDForbiddenBookInfo)
	cCancel()
	if cErr != nil || resp == nil {
		slog.Error("查询禁地之书面板失败", "err", cErr)
		return
	}
	info, err := protocol.ParseForbiddenBookInfoResult(resp.Payload)
	if err != nil {
		slog.Error("解析禁地之书面板失败", "err", err)
		return
	}
	slog.Info("禁地之书面板", "areas", len(info.Areas), "consum_score", info.ConsumScore)
	for _, a := range info.Areas {
		slog.Info("禁地区域",
			"forbidden_type", a.ForbiddenType,
			"explore_type", a.ExploreType,
			"cost_coin", a.CostCoin,
			"cost_ingot", a.CostIngot,
			"cur_number", a.CurNumber,
			"total_number", a.TotalNumber,
			"next_free_time", a.NextFreeTime,
		)
	}

	xCtx, xCancel := context.WithTimeout(ctx, 8*time.Second)
	xResp, xErr := sess.Call(xCtx, protocol.BuildForbiddenBookExchangeInfoPacket(), protocol.ActionIDForbiddenBookExchangeInfo)
	xCancel()
	if xErr == nil && xResp != nil {
		if prices, pErr := protocol.ParseForbiddenBookExchangeInfoResult(xResp.Payload); pErr == nil {
			for _, p := range prices {
				slog.Info("禁地兑换价格",
					"forbidden_type", p.ForbiddenType,
					"cost_coin", p.CostCoin,
					"cost_ingot", p.CostIngot,
					"count", p.Count,
				)
			}
		}
	}
}
