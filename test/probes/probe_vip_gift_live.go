//go:build probe

// 目标协议: [Module 18 / 0x12] Mod_Vip_Base VIP 每日礼包 (官方 Main.swf 反编译基准)
//
//	get_player_vip_info   Act 0 -> 0x00120000  空请求
//	                     回包 Int player_vip_level, Int money_to_next_level,
//	                          [Byte vip_level, Byte send_gift_is_get, Byte buy_gift_is_get]
//	get_send_gift         Act 1 -> 0x00120001  Byte vip_level (领取免费VIP礼包)
//	buy_send_gift         Act 2 -> 0x00120002  Byte vip_level (消耗元宝购买，本探针不调用)
//
// 基准对齐: com.protocols.Mod_Vip_Base / com.datas.VipData / VipView
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_vip_gift_live.go [-execute]
//
// 预期表现: 默认只读打印 VIP 等级与每个等级礼包的领取状态；
// 显式 -execute 时对尚未领取的等级逐个调用 get_send_gift，打印结果码与元宝变化。
package main

import (
	"context"
	"encoding/hex"
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

const (
	actionVipInfo     uint32 = 0x00120000
	actionVipSendGift uint32 = 0x00120001
)

func main() {
	execute := flag.Bool("execute", false, "实际领取免费VIP礼包")
	flag.Parse()
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
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	before := sess.GetPlayerState().Ingots
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID(), "ingots", before)

	call := func(payload []byte, act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("调用失败: "+name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		slog.Info("回包: "+name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		return resp
	}

	resp := call(nil, actionVipInfo, "get_player_vip_info")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	vipLevel, e1 := r.ReadInt32()
	moneyToNext, e2 := r.ReadInt32()
	count, e3 := r.ReadUint16()
	if e1 != nil || e2 != nil || e3 != nil {
		slog.Error("解析VIP信息失败", "err", fmt.Sprint(e1, e2, e3))
		return
	}
	type vipGiftState struct {
		Level   uint8
		SendGet uint8
		BuyGet  uint8
	}
	states := make([]vipGiftState, 0, count)
	for i := 0; i < int(count); i++ {
		level, err := r.ReadUint8()
		if err != nil {
			break
		}
		sendGet, err := r.ReadUint8()
		if err != nil {
			break
		}
		buyGet, err := r.ReadUint8()
		if err != nil {
			break
		}
		states = append(states, vipGiftState{Level: level, SendGet: sendGet, BuyGet: buyGet})
	}
	slog.Info("VIP信息",
		"vip_level", vipLevel,
		"money_to_next_level", moneyToNext,
		"gift_states", states,
		"remaining", r.Remaining(),
	)

	if !*execute {
		slog.Info("只读模式结束；如需领取免费VIP礼包请追加 -execute")
		return
	}

	claimed := 0
	for _, st := range states {
		// 未领取状态通常为 0；已领取为 1/2，两种都直接跳过。
		if st.SendGet != 0 {
			continue
		}
		payload := []byte{st.Level}
		gResp := call(payload, actionVipSendGift, fmt.Sprintf("get_send_gift(level=%d)", st.Level))
		if gResp != nil && len(gResp.Payload) > 0 {
			slog.Info("VIP礼包领取结果", "level", st.Level, "result", gResp.Payload[0])
			claimed++
		}
	}
	slog.Info("VIP礼包领取完成", "attempted", claimed, "ingots_before", before, "ingots_after", sess.GetPlayerState().Ingots)
}
