//go:build probe

// 目标协议: [Module 707 / 0x2C3] Mod_CatGold_Base 招财猫存储与返利
//
//	is_open           Act 4 -> 0x02C30004  空请求, 回包 Byte
//	cat_gold_info     Act 0 -> 0x02C30000  空请求
//	  response: Int today_charge_ingot, Byte is_store, Int max_store_ingot,
//	            Int interest_rate, Int can_get_ingot, Int store_ingot,
//	            Byte store_day, Byte return_day, Byte is_can_get_return_ingot,
//	            [Byte day, Byte extra_day, Int day_interest_rate] return_day_list,
//	            [Int prop_id, Byte is_unlock] prop_list,
//	            [Int item_id, Int item_num] gift_item_list,
//	            Int buy_gift_need_ingot, Byte is_buy_gift, Int return_ingot_end_time
//	get_return_ingot  Act 3 -> 0x02C30003  空请求, 回包 UByte result
//
// 基准对齐: com.protocols.Mod_CatGold_Base / com.datas.CatGoldData
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_cat_gold_live.go [-execute]
//
// 预期表现: 默认只读打印招财猫状态；-execute 仅在 is_can_get_return_ingot!=0 时
//
//	领取已到期返利。本探针绝不调用 store / buy_gift (会消耗元宝)。
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
	actionCatGoldInfo   uint32 = 0x02C30000
	actionCatGoldReturn uint32 = 0x02C30003
	actionCatGoldOpen   uint32 = 0x02C30004
)

func main() {
	execute := flag.Bool("execute", false, "实际领取已到期招财猫返利")
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

	call := func(act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, nil)
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

	if resp := call(actionCatGoldOpen, "is_open"); resp != nil && len(resp.Payload) > 0 {
		slog.Info("招财猫开放状态", "is_open", resp.Payload[0])
	}

	resp := call(actionCatGoldInfo, "cat_gold_info")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	todayCharge, e1 := r.ReadInt32()
	isStore, e2 := r.ReadUint8()
	maxStore, e3 := r.ReadInt32()
	interestRate, e4 := r.ReadInt32()
	canGetIngot, e5 := r.ReadInt32()
	storeIngot, e6 := r.ReadInt32()
	storeDay, e7 := r.ReadUint8()
	returnDay, e8 := r.ReadUint8()
	canReturnFlag, e9 := r.ReadUint8()
	if err := firstErr(e1, e2, e3, e4, e5, e6, e7, e8, e9); err != nil {
		slog.Error("解析招财猫信息失败", "err", err, "payload_len", len(resp.Payload))
		return
	}
	slog.Info("招财猫状态",
		"today_charge_ingot", todayCharge,
		"is_store", isStore,
		"max_store_ingot", maxStore,
		"interest_rate", interestRate,
		"can_get_ingot", canGetIngot,
		"store_ingot", storeIngot,
		"store_day", storeDay,
		"return_day", returnDay,
		"is_can_get_return_ingot", canReturnFlag,
		"remaining", r.Remaining(),
	)

	if !*execute {
		slog.Info("只读模式结束；如需领取到期返利请追加 -execute")
		return
	}
	if canReturnFlag == 0 {
		slog.Info("招财猫今日无可领取返利", "can_get_ingot", canGetIngot)
		return
	}
	gResp := call(actionCatGoldReturn, "get_return_ingot")
	if gResp == nil || len(gResp.Payload) == 0 {
		return
	}
	slog.Info("招财猫返利领取结果", "result", gResp.Payload[0],
		"ingots_before", before, "ingots_after", sess.GetPlayerState().Ingots,
		"delta", sess.GetPlayerState().Ingots-before)
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
