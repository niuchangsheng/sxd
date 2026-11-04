//go:build probe

// 目标协议: [Module 127, Action 0] is_game_function_end 查询已终结功能列表
//
//	[Module 127, Action 1] game_function_end_gift 查询功能终结礼包列表
//	[Module 127, Action 2] get_game_function_end_gift 领取指定终结礼包
//	[Module 127, Action 3] random_award 预览指定终结礼包奖励
//
// 基准对齐: 原版客户端 Main.swf com.protocols.Mod_FunctionEnd_Base 协议描述表
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_function_end_live.go
// 预期表现: 登录后打印已终结功能、待领取礼包清单以及每个礼包的奖励预览；
//
//	设置 SXD_CLAIM=1 时领取全部免费礼包并打印结果码与 is_have_next，
//	服务端标记需要消耗元宝的礼包一律跳过。
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
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	fmt.Printf("\n角色: %s 区服: %s\n", sess.OriginNickName(), role.ServerID)

	// 0. 已终结功能列表
	resp, err := call(sess, ctx, protocol.Packet{ActionID: 0x007F0000})
	if err != nil {
		slog.Error("查询已终结功能失败", "err", err)
		return
	}
	fmt.Printf("\n[is_game_function_end] %s\n", hex.EncodeToString(resp))

	// 1. 待领取礼包清单
	resp, err = call(sess, ctx, protocol.Packet{ActionID: 0x007F0001})
	if err != nil {
		slog.Error("查询终结礼包列表失败", "err", err)
		return
	}
	fmt.Printf("[game_function_end_gift] %s\n", hex.EncodeToString(resp))

	gifts, err := protocol.ParseFunctionEndGiftListResult(resp)
	if err != nil {
		slog.Error("解析终结礼包列表失败", "err", err)
		return
	}
	fmt.Printf("待领取礼包 %d 个:\n", len(gifts))
	for _, g := range gifts {
		fmt.Printf("  gift_id=%d bei=%d %+v\n", g.GiftID, g.Bei, g)
	}

	// 2. 逐个预览奖励 (action 3, 无副作用)
	for _, g := range gifts {
		payload := make([]byte, 2)
		payload[0] = byte(g.GiftID >> 8)
		payload[1] = byte(g.GiftID)
		r, err := call(sess, ctx, protocol.Packet{ActionID: 0x007F0003, Payload: payload})
		if err != nil {
			fmt.Printf("  [preview gift=%d] 失败: %v\n", g.GiftID, err)
			continue
		}
		p, err := protocol.ParseFunctionEndRandomAwardResult(r)
		if err != nil {
			fmt.Printf("  [preview gift=%d] 解析失败: %v raw=%s\n", g.GiftID, err, hex.EncodeToString(r))
			continue
		}
		fmt.Printf("  [preview gift=%d] %+v\n", g.GiftID, p)
	}

	// 3. 可选: 逐个领取全部礼包 (action 2)
	if os.Getenv("SXD_CLAIM") != "1" {
		fmt.Println("\n未设置 SXD_CLAIM=1, 仅预览不领取。")
		return
	}
	for _, g := range gifts {
		if g.IngotCost > 0 {
			fmt.Printf("  [claim gift=%d] 跳过: 需要消耗 %d 元宝\n", g.GiftID, g.IngotCost)
			continue
		}
		payload := make([]byte, 2)
		payload[0] = byte(g.GiftID >> 8)
		payload[1] = byte(g.GiftID)
		r, err := call(sess, ctx, protocol.Packet{ActionID: 0x007F0002, Payload: payload})
		if err != nil {
			fmt.Printf("  [claim gift=%d] 失败: %v\n", g.GiftID, err)
			continue
		}
		c, err := protocol.ParseGetGameFunctionEndGiftResult(r)
		if err != nil {
			fmt.Printf("  [claim gift=%d] 解析失败: %v raw=%s\n", g.GiftID, err, hex.EncodeToString(r))
			continue
		}
		fmt.Printf("  [claim gift=%d] result=%d is_have_next=%v %+v\n", g.GiftID, c.Result, c.IsHaveNext, c)
	}
}

func call(sess *client.RoleSession, ctx context.Context, pkt protocol.Packet) ([]byte, error) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, &pkt, pkt.ActionID)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response")
	}
	return resp.Payload, nil
}
