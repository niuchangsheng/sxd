//go:build probe

// 目标: 只读查询药园种子商城的库存与计价货币 (Mod_Farm_Base, Module 13)
//
//	get_info      (Act 0,  0x000D0000) request=[UInt32 prevAct]
//	get_plant_state (Act 21, 0x000D0015) request=[UInt32 prevAct]
//	get_seeds     (Act 22, 0x000D0016) request=[UInt32 prevAct]
//	  response=[Uint16 count, count × (Int32 seedID, Int32 owned, Int32 priceType, Int32 price, Int32 reserved)]
//
// 背景: 线上发现药园自动补仓可能消耗元宝。本探针只读，不下发任何购买请求，
// 用于确认每种种子的 priceType (3=铜钱) 与实际单价。
//
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_farm_seeds_live.go -role 1
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	tc := platform.NewTicketCache(90 * time.Minute)
	xd, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
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
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	call := func(pkt *protocol.Packet, actionID uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, actionID)
		if err != nil {
			slog.Warn("请求无回包", "action", fmt.Sprintf("0x%08X", actionID), "err", err)
			return nil
		}
		return resp
	}

	infoPkt, _ := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{})
	if call(infoPkt, protocol.ActionIDFarmGetInfo) == nil {
		return
	}
	statePkt, _ := protocol.BuildFarmPlantStatePacket(protocol.FarmPlantStateRequest{
		PrevAct: protocol.ActionIDFarmGetInfo,
	})
	if call(statePkt, protocol.ActionIDFarmGetPlantState) == nil {
		return
	}
	seedsPkt, _ := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{
		PrevAct: protocol.ActionIDFarmGetPlantState,
	})
	resp := call(seedsPkt, protocol.ActionIDFarmGetSeeds)
	if resp == nil {
		return
	}
	res, err := protocol.ParseFarmSeedsResult(resp.Payload)
	if err != nil {
		slog.Error("解析种子列表失败", "err", err)
		return
	}
	st := sess.GetPlayerState()
	slog.Info("当前资产", "coins", st.Coins, "ingots", st.Ingots)
	for _, s := range res.Seeds {
		slog.Info("种子商城条目",
			"seed_id", s.SeedID,
			"owned", s.OwnedCount,
			"remain_times", s.RemainTimes,
			"buy_need_coin", s.BuyNeedCoin,
			"buy_need_ingot", s.BuyNeedIngot,
		)
	}
}
