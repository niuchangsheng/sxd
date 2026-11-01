//go:build probe

// 目标协议: [Module 282, Action 0] 打开淬炼池 + [Module 534, Action 5] 资源面板 + [Module 282, Action 1] 淬炼执行
// 运行命令: go run -tags=probe test/probes/probe_spirit_pool_live.go
// 可选执行: SPIRIT_POOL_EXECUTE=1 go run -tags=probe test/probes/probe_spirit_pool_live.go
// 可选参数: SPIRIT_POOL_QUALITY=5(铜钱)/6(元宝)  SPIRIT_POOL_TYPE=7(普通)/8(一键)  SPIRIT_POOL_COUNT=槽位数量
// 预期表现: 默认只读打开面板并打印铁槽状态; 显式设置 SPIRIT_POOL_EXECUTE=1 后才发起淬炼写操作。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load("configs/config.yaml")
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

	callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
		cCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		return sess.Call(cCtx, pkt, expectedAct)
	}

	// 1. 打开淬炼池面板 (只读), 前序动作号必须与网关实际序列一致
	openPkt := protocol.BuildRefinePoolOpenPacket(sess.LastActionID())
	slog.Info("发送打开淬炼池面板",
		"action", fmt.Sprintf("0x%08X", openPkt.ActionID),
		"payload_hex", hex.EncodeToString(openPkt.Payload),
	)
	openResp, err := callWithTimeout(openPkt, protocol.ActionIDRefinePoolOpen, 3*time.Second)
	if err != nil {
		slog.Warn("淬炼池面板无应答", "err", err)
		return
	}
	dumpPacket("淬炼池 open_panel 回包", openResp)
	panel, err := protocol.ParseRefinePoolOpenResponse(openResp.Payload)
	if err != nil {
		slog.Warn("解析 open_panel 回包失败", "err", err)
		return
	}
	printPanel(panel)

	// 2. 资源面板查询 (Mod 534, Act 5)
	panelPkt := protocol.BuildRefinePoolPanelPacket(protocol.ActionIDRefinePoolOpen)
	slog.Info("发送淬炼池资源面板查询",
		"action", fmt.Sprintf("0x%08X", panelPkt.ActionID),
		"payload_hex", hex.EncodeToString(panelPkt.Payload),
	)
	panelResp, err := callWithTimeout(panelPkt, protocol.ActionIDRefinePoolPanel, 3*time.Second)
	if err != nil {
		slog.Warn("淬炼池资源面板无应答", "err", err)
	} else {
		dumpPacket("淬炼池资源面板回包", panelResp)
	}

	// 3. 仅在显式授权时执行淬炼写操作
	if os.Getenv("SPIRIT_POOL_EXECUTE") != "1" {
		slog.Info("只读模式结束 (设置 SPIRIT_POOL_EXECUTE=1 可发起淬炼执行)", "done_slots", panel.DoneCount())
		return
	}

	done := panel.DoneCount()
	if done == 0 {
		slog.Info("没有已完成的铁槽, 不发起淬炼", "iron_count", len(panel.Irons))
		return
	}

	quality := envByte("SPIRIT_POOL_QUALITY", protocol.RefinePoolQualityCoins)
	refineType := envByte("SPIRIT_POOL_TYPE", protocol.RefinePoolTypeOneKey)
	count := envByte("SPIRIT_POOL_COUNT", uint8(done))
	execPkt := protocol.BuildRefineExecutePacket(quality, refineType, count, protocol.ActionIDRefinePoolPanel)
	slog.Info("发送淬炼执行",
		"action", fmt.Sprintf("0x%08X", execPkt.ActionID),
		"quality", quality,
		"refine_type", refineType,
		"count", count,
		"payload_hex", hex.EncodeToString(execPkt.Payload),
	)
	execResp, err := callWithTimeout(execPkt, protocol.ActionIDRefineExecute, 5*time.Second)
	if err != nil {
		slog.Warn("淬炼执行无应答", "err", err)
		return
	}
	dumpPacket("淬炼执行回包", execResp)
	if result, err := protocol.ParseRefineExecuteResponse(execResp.Payload); err != nil {
		slog.Warn("解析淬炼回包失败", "err", err)
	} else {
		printRefineResult(result)
	}
}

func envByte(key string, def uint8) uint8 {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseUint(raw, 10, 8)
	if err != nil {
		slog.Warn("环境变量格式非法, 使用默认值", "key", key, "raw", raw, "default", def)
		return def
	}
	return uint8(v)
}

func printPanel(panel *protocol.RefinePoolPanel) {
	fmt.Printf("\n=== 淬炼池面板解析 ===\n")
	fmt.Printf("Level: %d  Exp: %d  CurSeq: %d  OneKeyLockFlag: %d\n",
		panel.Level, panel.Exp, panel.CurSeq, panel.OneKeyLockFlag)
	fmt.Printf("CoinsNeed: %d  IngotNeed: %d  LianQiJingHua: %d  XuanTie: %d\n",
		panel.CoinsNeed, panel.IngotNeed, panel.LianQiJingHua, panel.XuanTie)
	fmt.Printf("DoneSlots: %d / %d\n", panel.DoneCount(), len(panel.Irons))
	for _, iron := range panel.Irons {
		fmt.Printf("  slot=%2d status=%d need=%6d remain=%6d quality=%d\n",
			iron.Seq, iron.ProduceStatus, iron.ProduceNeedTime, iron.ProduceRemainTime, iron.Quality)
	}
	fmt.Println("==============================")
}

func printRefineResult(result *protocol.RefinePoolRefineResult) {
	fmt.Printf("\n=== 淬炼回包解析 ===\n")
	fmt.Printf("Result: %d  Level: %d  Exp: %d  OneKeyLockFlag: %d  LianQiJingHua: %d  XuanTie: %d\n",
		result.Result, result.Level, result.Exp, result.OneKeyLockFlag, result.LianQiJingHua, result.XuanTie)
	fmt.Printf("Awards: %d\n", len(result.Awards))
	for _, award := range result.Awards {
		fmt.Printf("  slot=%2d result=%d quality=%d exp=%d items=%v extra=%v\n",
			award.Seq, award.Result, award.Quality, award.Exp, award.Items, award.ExtraItems)
	}
	fmt.Println("==============================")
}

func dumpPacket(label string, pkt *protocol.Packet) {
	if pkt == nil {
		fmt.Printf("%s: <nil>\n", label)
		return
	}
	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("ActionID: 0x%08X (Mod %d, Act %d)\n", pkt.ActionID, pkt.ActionID>>16, pkt.ActionID&0xFFFF)
	fmt.Printf("PayloadLen: %d\n", len(pkt.Payload))
	fmt.Printf("PayloadHex: %s\n", hex.EncodeToString(pkt.Payload))
	// 逐字节 + 逐 uint32 视图，便于人工推断字段布局
	fmt.Printf("Bytes:")
	for i, b := range pkt.Payload {
		if i%16 == 0 {
			fmt.Printf("\n  %04x:", i)
		}
		fmt.Printf(" %02x", b)
	}
	fmt.Println()
	fmt.Printf("Uint32View:")
	for i := 0; i+4 <= len(pkt.Payload); i += 4 {
		v := uint32(pkt.Payload[i])<<24 | uint32(pkt.Payload[i+1])<<16 | uint32(pkt.Payload[i+2])<<8 | uint32(pkt.Payload[i+3])
		fmt.Printf("\n  [%02d] 0x%08X (%d)", i/4, v, v)
	}
	fmt.Println()
	fmt.Println("==============================")
}
