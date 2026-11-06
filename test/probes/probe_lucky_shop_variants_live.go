//go:build probe

// 目标协议: 琳琅阁/神秘商人商品列表 (Mod_Item_Base.lucky_shop_item_list)
//
//	module 2  action 76   request=[IntUtil npc_id]
//	  response=[[Int item_id, Int coin_price], Int refresh_hour, Int refresh_seconds, Int refresh_ingot]
//	module 2  action 78   request=[IntUtil npc_id, IntUtil item_id]
//	  response=[UByte result]
//	旧版移动入口: module 100 action 80 request=[] 同名协议
//
// 线上此前以 0x0002004C + PrevAct 请求全部超时，本探针只做只读变体排查：
// 分别测试「仅 Int32、Int32+占位、带 PrevAct、空请求」四种编码，定位服务端可接受的形态。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_lucky_shop_variants_live.go
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

type variant struct {
	name    string
	action  uint32
	payload []byte
	prevAct bool
	timeout time.Duration
}

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
	state := sess.GetPlayerState()
	slog.Info("角色地图", "town_id", state.TownID, "town_map_id", state.TownMapID,
		"ingots", state.Ingots, "coins", state.Coins)

	w := protocol.NewWriter()
	w.WriteInt32(38)
	npcOnly := append([]byte(nil), w.Bytes()...)
	w2 := protocol.NewWriter()
	w2.WriteInt32(38)
	w2.WriteUint32(0)
	npcPad := w2.Bytes()

	variants := []variant{
		{"m2a76_npc38_only", 0x0002004C, npcOnly, false, 6 * time.Second},
		{"m2a76_npc38_pad", 0x0002004C, npcPad, false, 6 * time.Second},
		{"m2a76_npc38_prevact", 0x0002004C, npcPad, true, 6 * time.Second},
		{"m2a76_empty", 0x0002004C, nil, false, 6 * time.Second},
		{"m100a80_empty", 0x00640050, nil, false, 6 * time.Second},
		{"m100a80_empty_prevact", 0x00640050, nil, true, 6 * time.Second},
	}
	for _, v := range variants {
		pkt := protocol.NewPacket(v.action, v.payload)
		pkt.HasPrevAct = v.prevAct
		cCtx, cCancel := context.WithTimeout(ctx, v.timeout)
		resp, cErr := sess.Call(cCtx, pkt, v.action)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("琳琅阁变体无回包", "variant", v.name, "err", cErr)
			continue
		}
		head := resp.Payload
		if len(head) > 64 {
			head = head[:64]
		}
		slog.Info("琳琅阁变体回包", "variant", v.name, "len", len(resp.Payload),
			"head", hex.EncodeToString(head))
	}
}
