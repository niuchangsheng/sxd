//go:build probe

// 目标协议: [Module 341, Action 1] 入口/面板 + [Action 4] 创建房间 + [Action 12] 解除封印 + [Action 13] 待确认
// 运行命令: go run -tags=probe test/probes/probe_tower_room_live.go
// 可选执行: TOWER_ROOM_EXECUTE=1 go run -tags=probe test/probes/probe_tower_room_live.go
// 可选配置: SXD_CONFIG=configs/qq.yaml (默认 configs/config.yaml)
// 预期表现: 默认只读 341/1 并打印回包; 显式设置 TOWER_ROOM_EXECUTE=1 后依次执行
//
//	创建房间(341/4) → 解除封印(341/12) → 下一步(341/13)，并打印每次结果码。
//
// 说明: 所有请求载荷仅 4 字节 PrevAct，由会话在发送时填充最新动作号；
//
//	本探针不调用任何元宝购买类动作。
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots", sess.GetPlayerState().Ingots, "coins", sess.GetPlayerState().Coins)

	call := func(pkt *protocol.Packet, d time.Duration) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, pkt.ActionID)
		if err != nil {
			slog.Warn("请求无应答", "action", fmt.Sprintf("0x%08X", pkt.ActionID), "err", err)
			return nil
		}
		dumpPacket("回包", resp)
		return resp
	}

	// 1. 只读入口/面板 (341/1)
	openPkt := protocol.BuildTowerRoomOpenPacket(sess.LastActionID())
	slog.Info("发送房间入口查询", "action", fmt.Sprintf("0x%08X", openPkt.ActionID),
		"payload_hex", hex.EncodeToString(openPkt.Payload))
	call(openPkt, 8*time.Second)

	if os.Getenv("TOWER_ROOM_EXECUTE") != "1" {
		slog.Info("只读模式结束 (设置 TOWER_ROOM_EXECUTE=1 可执行创建/解除封印)")
		return
	}

	// 2. 创建房间 (341/4)
	createPkt := protocol.BuildTowerRoomCreatePacket(sess.LastActionID())
	slog.Info("发送创建房间", "action", fmt.Sprintf("0x%08X", createPkt.ActionID),
		"payload_hex", hex.EncodeToString(createPkt.Payload))
	if resp := call(createPkt, 8*time.Second); resp != nil {
		if res, err := protocol.ParseTowerRoomCreateResult(resp.Payload); err != nil {
			slog.Warn("解析创建房间回包失败", "err", err)
		} else {
			slog.Info("创建房间结果", "result", res.Result,
				"result_text", protocol.TowerRoomResultText(res.Result), "room_id", res.RoomID)
		}
	}

	time.Sleep(500 * time.Millisecond)

	// 3. 解除封印 (341/12)
	unsealPkt := protocol.BuildTowerRoomUnsealPacket(sess.LastActionID())
	slog.Info("发送解除封印", "action", fmt.Sprintf("0x%08X", unsealPkt.ActionID),
		"payload_hex", hex.EncodeToString(unsealPkt.Payload))
	if resp := call(unsealPkt, 8*time.Second); resp != nil && len(resp.Payload) > 0 {
		slog.Info("解除封印结果", "result", resp.Payload[0],
			"result_text", protocol.TowerRoomResultText(resp.Payload[0]))
	}

	time.Sleep(500 * time.Millisecond)

	// 4. 解除封印后的下一步 (341/13，语义待确认)
	nextPkt := protocol.BuildTowerRoomNextPacket(sess.LastActionID())
	slog.Info("发送下一步动作", "action", fmt.Sprintf("0x%08X", nextPkt.ActionID),
		"payload_hex", hex.EncodeToString(nextPkt.Payload))
	call(nextPkt, 15*time.Second)

	slog.Info("结束资产", "ingots", sess.GetPlayerState().Ingots, "coins", sess.GetPlayerState().Coins)
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
	fmt.Printf("Bytes:")
	for i, b := range pkt.Payload {
		if i%16 == 0 {
			fmt.Printf("\n  %04x:", i)
		}
		fmt.Printf(" %02x", b)
	}
	fmt.Println()
}
