//go:build probe

// 目标: 确认药园 buy_new_herbs (13/23) 需要哪个会话上下文。
//
// 背景: 主服裸载荷 [Int32 seed_id] 与 [Int32, Uint32 prev_act] 均被静默丢弃。
// 本探针依次在主服、圣域会话、仙盟城镇会话上执行:
//
//	13/21 get_new_farmlandinfo_list
//	13/22 new_farmland_herbs_list
//	13/23 buy_new_herbs [Int32 seed_id]  (官方只有 4 字节)
//
// 只购买 buy_need_ingot=0 的种子；任意会话成功后立即停止，避免重复扣铜钱。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_farm_buy_sessions_live.go
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
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
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
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	tryOn := func(s *client.RoleSession, label string) bool {
		call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
			cCtx, cCancel := context.WithTimeout(ctx, timeout)
			defer cCancel()
			resp, cErr := s.Call(cCtx, pkt, act)
			if cErr != nil {
				slog.Warn("无回包", "session", label, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
				return nil
			}
			limit := len(resp.Payload)
			if limit > 80 {
				limit = 80
			}
			slog.Info("回包", "session", label, "action", fmt.Sprintf("0x%08X", act),
				"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
			return resp
		}

		// 先走一次新农田列表，模拟官方客户端打开面板的顺序。
		call(protocol.NewPacket(protocol.ActionIDFarmGetPlantState, []byte{0, 0, 0, 0}),
			protocol.ActionIDFarmGetPlantState, 8*time.Second)
		seedsResp := call(protocol.NewPacket(protocol.ActionIDFarmGetSeeds, []byte{0, 0, 0, 0}),
			protocol.ActionIDFarmGetSeeds, 8*time.Second)
		if seedsResp == nil {
			return false
		}
		seeds, pErr := protocol.ParseFarmSeedsResult(seedsResp.Payload)
		if pErr != nil {
			slog.Warn("解析种子列表失败", "session", label, "err", pErr)
			return false
		}
		var target *protocol.FarmSeedItem
		for i := range seeds.Seeds {
			seed := &seeds.Seeds[i]
			if seed.BuyNeedIngot == 0 && seed.BuyNeedCoin > 0 && seed.RemainTimes > 0 {
				target = seed
				break
			}
		}
		if target == nil {
			slog.Info("没有铜钱可购种子", "session", label)
			return false
		}
		before := s.GetPlayerState()
		slog.Info("准备购买种子", "session", label, "seed_id", target.SeedID,
			"buy_need_coin", target.BuyNeedCoin, "remain_times", target.RemainTimes,
			"owned", target.OwnedCount, "coins", before.Coins)

		w := protocol.NewWriter()
		w.WriteInt32(target.SeedID)
		resp := call(protocol.NewPacket(protocol.ActionIDFarmBuySeed, w.Bytes()),
			protocol.ActionIDFarmBuySeed, 8*time.Second)
		if resp == nil {
			return false
		}
		after := s.GetPlayerState()
		slog.Info("购买有回包", "session", label, "hex", hex.EncodeToString(resp.Payload),
			"coin_delta", after.Coins-before.Coins, "ingot_delta", after.Ingots-before.Ingots)

		// 重新读取库存确认是否真的到账。
		if r2 := call(protocol.NewPacket(protocol.ActionIDFarmGetSeeds, []byte{0, 0, 0, 0}),
			protocol.ActionIDFarmGetSeeds, 8*time.Second); r2 != nil {
			if s2, e2 := protocol.ParseFarmSeedsResult(r2.Payload); e2 == nil {
				for _, seed := range s2.Seeds {
					if seed.SeedID == target.SeedID {
						slog.Info("购买后库存", "session", label, "seed_id", seed.SeedID, "owned", seed.OwnedCount)
					}
				}
			}
		}
		return true
	}

	if tryOn(sess, "main") {
		slog.Info("主服购买成功")
		return
	}

	sa, err := sess.OpenSaintAreaSession(ctx)
	if err == nil {
		defer sa.Close()
		if tryOn(sa, "saint_area") {
			slog.Info("圣域会话购买成功")
			return
		}
	} else {
		slog.Warn("建立圣域会话失败", "err", err)
	}

	town, err := sess.OpenSectTownSession(ctx)
	if err == nil {
		defer town.Close()
		if tryOn(town, "sect_town") {
			slog.Info("仙盟城镇会话购买成功")
			return
		}
	} else {
		slog.Warn("建立仙盟城镇会话失败", "err", err)
	}
	slog.Info("三种会话均无法购买药园种子")
}