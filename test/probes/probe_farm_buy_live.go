//go:build probe

// 目标: 确认药园 buy_new_herbs (Mod_Farm_Base, 13/23) 的请求编码。
//
// 线上例程按 [Int32 seed_id, Uint32 prev_act] 发送时被服务端静默丢弃。
// 本探针依次尝试:
//
//  1. [Int32 seed_id] 裸载荷 (不带 PrevAct)
//  2. [Int32 seed_id, Uint32 prev_act] (当前例程编码)
//
// 只购买 PriceType=3 (铜钱) 的种子，不触碰元宝档。
//
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_farm_buy_live.go
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

const (
	actionFarmBuy uint32 = 0x000D0017
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s1.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	tc := platform.NewTicketCache(90 * time.Minute)
	xd, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
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

	call := func(pkt *protocol.Packet, actionID uint32, timeout time.Duration) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, actionID)
		if err != nil {
			slog.Warn("请求无回包", "action", fmt.Sprintf("0x%08X", actionID), "err", err)
			return nil
		}
		return resp
	}

	infoPkt, _ := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{})
	if call(infoPkt, protocol.ActionIDFarmGetInfo, 8*time.Second) == nil {
		return
	}
	statePkt, _ := protocol.BuildFarmPlantStatePacket(protocol.FarmPlantStateRequest{
		PrevAct: protocol.ActionIDFarmGetInfo,
	})
	if call(statePkt, protocol.ActionIDFarmGetPlantState, 8*time.Second) == nil {
		return
	}
	seedsPkt, _ := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{
		PrevAct: protocol.ActionIDFarmGetPlantState,
	})
	seedsResp := call(seedsPkt, protocol.ActionIDFarmGetSeeds, 8*time.Second)
	if seedsResp == nil {
		return
	}
	seeds, err := protocol.ParseFarmSeedsResult(seedsResp.Payload)
	if err != nil {
		slog.Error("解析种子列表失败", "err", err)
		return
	}
	var target protocol.FarmSeedItem
	found := false
	for _, s := range seeds.Seeds {
		if s.BuyNeedIngot == 0 && s.BuyNeedCoin > 0 && s.SeedID == 176 {
			target = s
			found = true
			break
		}
	}
	if !found {
		slog.Info("未找到铜钱计价种子 176")
		return
	}
	st := sess.GetPlayerState()
	slog.Info("资产", "coins", st.Coins, "ingots", st.Ingots,
		"seed_id", target.SeedID, "buy_need_coin", target.BuyNeedCoin,
		"buy_need_ingot", target.BuyNeedIngot, "remain_times", target.RemainTimes,
		"owned", target.OwnedCount)
	if st.Coins < int64(target.BuyNeedCoin) {
		slog.Info("铜钱不足，跳过购买")
		return
	}

	// 变体 1: 裸 Int32 seed_id，不带 PrevAct。
	w := protocol.NewWriter()
	w.WriteInt32(target.SeedID)
	bare := protocol.NewPacket(actionFarmBuy, w.Bytes())
	if resp := call(bare, actionFarmBuy, 8*time.Second); resp != nil {
		slog.Info("变体1 裸载荷购买有回包", "hex", hex.EncodeToString(resp.Payload),
			"coin_delta", sess.GetPlayerState().Coins-st.Coins,
			"ingot_delta", sess.GetPlayerState().Ingots-st.Ingots)
		// 重新查询种子库存，确认拥有数量变化。
		seedsPkt2, _ := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{
			PrevAct: actionFarmBuy,
		})
		if r2 := call(seedsPkt2, protocol.ActionIDFarmGetSeeds, 8*time.Second); r2 != nil {
			if s2, e2 := protocol.ParseFarmSeedsResult(r2.Payload); e2 == nil {
				for _, s := range s2.Seeds {
					if s.SeedID == target.SeedID {
						slog.Info("购买后种子库存", "seed_id", s.SeedID, "owned", s.OwnedCount)
					}
				}
			}
		}
		return
	}

	// 变体 2: 当前例程编码 [seed_id, prev_act]。
	prevPkt, _ := protocol.BuildFarmBuySeedPacket(protocol.FarmBuySeedRequest{
		SeedID:  target.SeedID,
		PrevAct: protocol.ActionIDFarmGetSeeds,
	})
	if resp := call(prevPkt, actionFarmBuy, 8*time.Second); resp != nil {
		slog.Info("变体2 带 PrevAct 购买有回包", "hex", hex.EncodeToString(resp.Payload),
			"coin_delta", sess.GetPlayerState().Coins-st.Coins)
	}
	slog.Info("两种编码均无回包")
}
