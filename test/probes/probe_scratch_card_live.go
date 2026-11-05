//go:build probe

// 目标协议: [Module 185 / 0xB9] Mod_ScratchCard_Base 幸运刮刮卡
//
//	scratch_card_info Act 0 -> 0x00B90000 空请求
//	  response: UByte is_in_activity, Int end_time, Short active_degree, Byte card_times
//	shave_card        Act 1 -> 0x00B90001 空请求
//	  response: UByte result, Int item_id
//
// 基准对齐: com.protocols.Mod_ScratchCard_Base / com.datas.ScratchCardData
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_scratch_card_live.go [-execute]
//
// 预期表现: 只读打印活动状态与剩余刮卡次数；-execute 时用完全部免费次数，
//
//	结果码 7 (INGOT) 表示本次刮出元宝。全程不消耗元宝。
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
	actionScratchInfo  uint32 = 0x00B90000
	actionScratchShave uint32 = 0x00B90001
)

func main() {
	execute := flag.Bool("execute", false, "实际刮卡 (消耗每日免费次数)")
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

	resp := call(actionScratchInfo, "scratch_card_info")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	isInActivity, e1 := r.ReadUint8()
	endTime, e2 := r.ReadInt32()
	activeDegree, e3 := r.ReadInt16()
	cardTimes, e4 := r.ReadUint8()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		slog.Error("解析刮刮卡信息失败", "err", fmt.Sprint(e1, e2, e3, e4))
		return
	}
	slog.Info("刮刮卡状态",
		"is_in_activity", isInActivity,
		"end_time", endTime,
		"active_degree", activeDegree,
		"card_times", cardTimes,
		"remaining", r.Remaining(),
	)

	if !*execute {
		slog.Info("只读模式结束；如需刮卡请追加 -execute")
		return
	}
	if isInActivity == 0 || cardTimes == 0 {
		slog.Info("刮刮卡活动未开放或次数已用尽", "card_times", cardTimes)
		return
	}

	for i := 0; i < int(cardTimes); i++ {
		sResp := call(actionScratchShave, fmt.Sprintf("shave_card[%d]", i+1))
		if sResp == nil || len(sResp.Payload) < 5 {
			break
		}
		sr := protocol.NewReader(sResp.Payload)
		result, _ := sr.ReadUint8()
		itemID, _ := sr.ReadInt32()
		slog.Info("刮卡结果", "index", i+1, "result", result, "item_id", itemID,
			"reason", scratchResultName(result))
		if result == 3 { // NOT_ENOUGH_TIMES
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	after := sess.GetPlayerState().Ingots
	slog.Info("资产变化", "ingots_before", before, "ingots_after", after, "delta", after-before)
}

func scratchResultName(result uint8) string {
	switch result {
	case 2:
		return "成功"
	case 3:
		return "次数不足"
	case 4:
		return "背包已满"
	case 5:
		return "失败"
	case 6:
		return "获得积分"
	case 7:
		return "获得元宝"
	case 8:
		return "获得形象卡"
	case 9:
		return "获得奖励"
	default:
		return "未知结果"
	}
}
