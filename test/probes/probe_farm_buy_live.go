//go:build probe

// 目标: 定位药园买种 13/23 在 niuchangsheng 服务器静默丢包的载荷形态。
//
// 背景: qq s0 上裸 [Int32 seed] 可购买；ncs s1 相同载荷 8 秒超时。
// 官方 request=[IntUtil]。本探针依序尝试裸 4 字节 / 带 PrevAct 8 字节，
// 成功即停止（只买一颗铜钱计价种子）。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_farm_buy_live.go -role 1
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "coins", sess.GetPlayerState().Coins,
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 30 {
			limit = 30
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	seedsPkt, _ := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{PrevAct: sess.LastActionID()})
	seedsResp := call(seedsPkt, protocol.ActionIDFarmGetSeeds, "seeds")
	if seedsResp == nil {
		return
	}
	seeds, pErr := protocol.ParseFarmSeedsResult(seedsResp.Payload)
	if pErr != nil {
		slog.Warn("解析种子失败", "err", pErr)
		return
	}
	var target *protocol.FarmSeedItem
	for i := range seeds.Seeds {
		it := &seeds.Seeds[i]
		if it.BuyNeedIngot == 0 && it.BuyNeedCoin > 0 && it.RemainTimes > 0 {
			if target == nil || it.BuyNeedCoin < target.BuyNeedCoin {
				target = it
			}
		}
	}
	if target == nil {
		slog.Info("无铜钱计价可购种子")
		return
	}
	slog.Info("测试目标种子", "seed_id", target.SeedID, "coin", target.BuyNeedCoin,
		"remain", target.RemainTimes, "owned", target.OwnedCount)

	seedBytes := []byte{
		byte(target.SeedID >> 24), byte(target.SeedID >> 16), byte(target.SeedID >> 8), byte(target.SeedID),
	}
	ack := sess.LastAckActionID()
	build := func(prev uint32) []byte {
		out := append([]byte{}, seedBytes...)
		if prev != 0 {
			out = append(out, byte(prev>>24), byte(prev>>16), byte(prev>>8), byte(prev))
		}
		return out
	}
	for _, v := range []struct {
		name string
		prev uint32
	}{
		{"buy_bare", 0},
		{"buy_prev_ack", ack},
		{"buy_prev_seeds", protocol.ActionIDFarmGetSeeds},
		{"buy_prev0", 0xFFFFFFFF}, // 双字节全 1 占位，便于观察
	} {
		if v.name == "buy_prev0" {
			if resp := call(protocol.NewPacket(protocol.ActionIDFarmBuySeed,
				append(append([]byte{}, seedBytes...), 0, 0, 0, 0)),
				protocol.ActionIDFarmBuySeed, v.name); resp != nil {
				break
			}
			continue
		}
		if resp := call(protocol.NewPacket(protocol.ActionIDFarmBuySeed, build(v.prev)),
			protocol.ActionIDFarmBuySeed, v.name); resp != nil {
			break
		}
		time.Sleep(time.Second)
	}
	slog.Info("探针结束")
}
