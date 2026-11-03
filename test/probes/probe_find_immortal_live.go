//go:build probe

// 目标协议: [Module 68, Action 1] open_find_immortal 查询喜从天降面板
//
//	[Module 68, Action 5] get_xun_xian_ling_number 查询龙鱼令数量
//	[Module 68, Action 2] start_find_immortal 召唤龙鱼 (消耗 1 枚龙鱼令)
//	[Module 68, Action 4] pickup_award 收取龙鱼奖励
//
// 基准对齐: 原版客户端 Main.swf Mod_FindImmortal_Base 协议描述表
//
// 背景: 2026-11-03 线上日志出现 "召唤前龙鱼令=2 / 召唤回包 remain_number=4" 的
// 矛盾回包，且 pickup 回包的累计仙令出现 3 -> 0 的回退，需抓取真实字节核对
// act2/act4 的字段偏移与语义。
//
// 运行命令: go run -tags=probe test/probes/probe_find_immortal_live.go
// 预期表现: 登录后依次打印 Act 1/5/2/4 的服务端原始回包十六进制与生产解析结果；
//
//	持有龙鱼令时最多消耗 1 枚并领取 1 条龙鱼。
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	serverID := flag.String("server-id", "", "目标区服 ID (留空取配置文件首个角色)")
	roleName := flag.String("role", "", "目标角色名 (留空取该区服首个角色)")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	if len(cfg.Accounts) == 0 {
		slog.Error("配置文件缺少账号")
		return
	}

	acc := cfg.Accounts[0]
	var role *config.RoleConfig
	for i := range acc.Roles {
		r := &acc.Roles[i]
		if *serverID != "" && r.ServerID != *serverID {
			continue
		}
		if *roleName != "" && r.RoleName != *roleName {
			continue
		}
		role = r
		break
	}
	if role == nil {
		slog.Error("配置文件中未找到匹配角色", "server_id", *serverID, "role", *roleName)
		return
	}

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
			fmt.Printf("  [SNIFF %s] len=%d payload=%s\n",
				tag, len(pkt.Payload), hex.EncodeToString(pkt.Payload))
		}
	}
	sess.RegisterHandler(protocol.ActionIDFindImmortalOpen, sniff("open_find_immortal"))
	sess.RegisterHandler(protocol.ActionIDFindImmortalGetXunXianLing, sniff("get_xun_xian_ling_number"))
	sess.RegisterHandler(protocol.ActionIDFindImmortalStart, sniff("start_find_immortal"))
	sess.RegisterHandler(protocol.ActionIDFindImmortalPickupAward, sniff("pickup_award"))

	prevAct := sess.LastActionID()

	call := func(action uint32, pkt *protocol.Packet) *protocol.Packet {
		cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
		defer ccancel()
		resp, cErr := sess.Call(cctx, pkt, action)
		if cErr != nil || resp == nil {
			slog.Error("请求无回包", "action", fmt.Sprintf("0x%08X", action), "err", cErr)
			return nil
		}
		prevAct = action
		return resp
	}

	fmt.Printf("\n============================================================\n")

	// 1. 面板
	panelResp := call(protocol.ActionIDFindImmortalOpen, protocol.BuildFindImmortalOpenPacket(prevAct))
	if panelResp != nil {
		if panel, pErr := protocol.ParseFindImmortalPanelResult(panelResp.Payload); pErr != nil {
			slog.Warn("解析面板失败", "err", pErr)
		} else {
			fmt.Printf("[parse open] can_buy=%d award=%+v state=%d god_id=%d cultivation=%d total=%d\n",
				panel.CanBuyNumber, panel.Award, panel.State, panel.GodID, panel.Cultivation, panel.TotalCount)
		}
	}

	// 2. 龙鱼令
	queryTokens := func() (int16, bool) {
		resp := call(protocol.ActionIDFindImmortalGetXunXianLing, protocol.BuildFindImmortalGetXunXianLingPacket(prevAct))
		if resp == nil {
			return 0, false
		}
		n, qErr := protocol.ParseFindImmortalXunXianLingResult(resp.Payload)
		if qErr != nil {
			slog.Warn("解析龙鱼令失败", "err", qErr)
			return 0, false
		}
		fmt.Printf("[parse tokens] xun_xian_ling=%d\n", n)
		return n, true
	}
	tokens, ok := queryTokens()
	if !ok {
		fmt.Printf("============================================================\n")
		return
	}

	summon := func() *protocol.FindImmortalStartResult {
		resp := call(protocol.ActionIDFindImmortalStart, protocol.BuildFindImmortalStartPacket(prevAct))
		if resp == nil {
			return nil
		}
		start, sErr := protocol.ParseFindImmortalStartResult(resp.Payload)
		if sErr != nil {
			slog.Warn("解析召唤回包失败", "err", sErr)
			return nil
		}
		fmt.Printf("[parse start] result=%d award=%+v state=%d god_id=%d remain_number=%d total=%d\n",
			start.Result, start.Award, start.State, start.GodID, start.RemainNumber, start.TotalCount)
		return start
	}

	pickup := func() *protocol.FindImmortalPickupResult {
		resp := call(protocol.ActionIDFindImmortalPickupAward, protocol.BuildFindImmortalPickupAwardPacket(prevAct))
		if resp == nil {
			return nil
		}
		p, kErr := protocol.ParseFindImmortalPickupResult(resp.Payload)
		if kErr != nil {
			slog.Warn("解析领奖回包失败", "err", kErr)
			return nil
		}
		fmt.Printf("[parse pickup] result=%d award=%+v cultivation=%d\n", p.Result, p.Award, p.Cultivation)
		return p
	}

	// 3. 召唤一只龙鱼；若回包为 GOADING，说明存在上一轮遗留的未领取龙鱼，
	//    先收取清空后再重试一次 (对应 2026-11-03 12:57 线上熔断遗留的孤儿龙鱼)。
	start := summon()
	if start == nil {
		return
	}
	if start.Result == protocol.FindImmortalResultGoading {
		fmt.Printf("[goading] 存在未领取龙鱼，先收取清空再重试召唤\n")
		pickup()
		if tokens, ok = queryTokens(); !ok || tokens <= 0 {
			fmt.Printf("[skip] 清空遗留龙鱼后龙鱼令=%d，不再召唤\n", tokens)
			fmt.Printf("============================================================\n")
			return
		}
		start = summon()
		if start == nil {
			return
		}
	}
	if start.Result != protocol.FindImmortalResultSuccess {
		fmt.Printf("============================================================\n")
		return
	}

	// 4. 回查龙鱼令，验证召唤后回包的 remain_number 是否可信
	if n, qOK := queryTokens(); qOK {
		fmt.Printf("[recheck tokens] after_summon_xun_xian_ling=%d (start.remain_number=%d)\n", n, start.RemainNumber)
	}

	// 5. 领取刚召唤的龙鱼
	pickup()

	fmt.Printf("============================================================\n\n")
}
