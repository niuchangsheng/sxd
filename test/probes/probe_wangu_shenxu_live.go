//go:build probe

// 目标协议: [Module 840, Action 0] 万古神墟主面板
//
//	[Module 840, Action 1] 膜拜大神像 [Int type]
//	[Module 840, Action 2] 领取被膜拜累计铜钱奖励
//
// 运行命令: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_wangu_shenxu_live.go
// 可选执行: WORSHIP_EXECUTE=1 时才真正发送 840/1/2；默认只读面板。
//
// 2026-11-21 抓包 + 实机结论: 840 在主服会话回包；worship 成功回包 Int 是给
// **被膜拜榜一**的元宝奖励（抓包客户端自身即榜一，故能看到 0/3 PLAYER_INGOT 增加）；
// 普通膜拜者膜拜他人成功但不增加元宝。膜拜对膜拜者免费，不消耗元宝。
// 官方客户端每次膜拜成功后刷新主面板；连续快速膜拜会被服务端静默丢弃。
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
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

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	st := sess.GetPlayerState()
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots", st.Ingots, "coins", st.Coins)

	call := func(pkt *protocol.Packet, d time.Duration) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, pkt.ActionID)
		if err != nil {
			slog.Warn("请求无应答", "action", fmt.Sprintf("0x%08X", pkt.ActionID),
				"payload_hex", hex.EncodeToString(pkt.Payload), "err", err)
			return nil
		}
		return resp
	}

	// 1. 主面板 (只读)。
	panelResp := call(protocol.BuildWanguShenxuMainInfoPacket(), 10*time.Second)
	if panelResp == nil {
		slog.Error("主面板无回包")
		return
	}
	panel, err := protocol.ParseWanguShenxuPanel(panelResp.Payload)
	if err != nil {
		slog.Error("解析主面板失败", "err", err, "payload_hex", hex.EncodeToString(panelResp.Payload))
		return
	}
	slog.Info("万古神墟主面板", "result", panel.Result,
		"result_text", protocol.WanguShenxuResultText(panel.Result),
		"statues", len(panel.Players), "can_get_coin", panel.CanGetCoin)
	for _, p := range panel.Players {
		slog.Info("大神像", "type", p.Type, "season", p.Season, "step", p.Step,
			"player_id", p.PlayerID, "server", p.ServerName, "nickname", p.Nickname,
			"role_id", p.RoleID, "suit_id", p.SuitID, "war_power", p.WarPower,
			"is_worship", p.IsWorship, "worship_num", p.WorshipNum,
			"seasons", len(p.Seasons), "weed_award_coin", p.WeedAwardCoin,
			"total_award_coin", p.TotalAwardCoin)
	}
	if panel.Result != protocol.WanguShenxuResultSuccess {
		slog.Info("面板不可用，结束")
		return
	}

	if os.Getenv("WORSHIP_EXECUTE") != "1" {
		slog.Info("只读模式结束（设置 WORSHIP_EXECUTE=1 才发送 840/1/2）")
		return
	}

	// 2. 膜拜全部未膜拜且存在对象的大神像。
	for _, p := range panel.Players {
		if p.PlayerID == 0 || p.IsWorship {
			continue
		}
		resp := call(protocol.BuildWanguShenxuWorshipPacket(p.Type), 10*time.Second)
		if resp == nil {
			continue
		}
		got, pErr := protocol.ParseWanguShenxuWorshipResult(resp.Payload)
		if pErr != nil {
			slog.Error("解析膜拜回包失败", "type", p.Type, "err", pErr,
				"payload_hex", hex.EncodeToString(resp.Payload))
			continue
		}
		slog.Info("膜拜结果", "type", p.Type, "nickname", p.Nickname,
			"result", got.Result, "result_text", protocol.WanguShenxuResultText(got.Result),
			"target_ingot", got.Ingot, "role_ingots", sess.GetPlayerState().Ingots)
		// 官方客户端每次膜拜成功后重新拉取面板，并间隔数秒进行下一次膜拜。
		time.Sleep(2 * time.Second)
		if refresh := call(protocol.BuildWanguShenxuMainInfoPacket(), 10*time.Second); refresh != nil {
			if refreshed, rErr := protocol.ParseWanguShenxuPanel(refresh.Payload); rErr == nil {
				panel = refreshed
			}
		}
	}

	// 3. 有累计铜钱奖励时领取。
	if panel.CanGetCoin > 0 {
		resp := call(protocol.BuildWanguShenxuGetAwardPacket(), 10*time.Second)
		if resp != nil {
			got, pErr := protocol.ParseWanguShenxuAwardResult(resp.Payload)
			if pErr != nil {
				slog.Error("解析领奖回包失败", "err", pErr, "payload_hex", hex.EncodeToString(resp.Payload))
			} else {
				slog.Info("领奖结果", "result", got.Result,
					"result_text", protocol.WanguShenxuResultText(got.Result),
					"coin", got.Coin, "role_coins", sess.GetPlayerState().Coins)
			}
		}
	}
}
