//go:build probe

// 目标协议: Mod_Item_Base.player_sell_item (Module 2, Action 73, 0x00020049)
//
//	request:  [ShortUtil grid_id] (+ 会话层自动追加 4 字节 PrevAct)
//	response: [UByte result, ...]
//
// 用途: 诊断精英扫荡物自动出售超时。对比两种 PrevAct 环境:
//
//	A. 出售前等待 3 秒，末次发送为心跳 (0x00000017)；
//	B. 出售前紧接一次背包查询 (0x00020029)，末次发送为业务动作。
//
// 若 A 超时、B 成功，则可确认 PrevAct 链不能以心跳结尾。
//
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_sell_item_diag_live.go
// 注意: 该探针会真实出售最多 2 件可出售物品 (价格 > 0 的精英副本制作卷)，属于写操作。
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
		MaxReconnectAttempts: 2,
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	// A. 让心跳成为末次发送动作后出售。
	slog.Info("场景 A: 等待 3 秒使心跳成为末次发送动作")
	time.Sleep(3 * time.Second)
	targetA := findSellable(ctx, sess, nil)
	if targetA == nil {
		slog.Error("未找到可出售物品 A")
		return
	}
	slog.Info("场景 A 目标", "grid_id", targetA.GridID, "item_id", targetA.ItemID)
	okA, codeA, errA := sellOnce(ctx, sess, targetA.GridID)
	slog.Info("场景 A 结果", "ok", okA, "code", codeA, "err", errA)

	// B. 出售前先做一次背包查询，使业务动作成为末次发送动作。
	slog.Info("场景 B: 背包查询后立即出售")
	targetB := findSellable(ctx, sess, &targetA.GridID)
	if targetB == nil {
		slog.Error("未找到可出售物品 B")
		return
	}
	slog.Info("场景 B 目标", "grid_id", targetB.GridID, "item_id", targetB.ItemID)
	okB, codeB, errB := sellOnce(ctx, sess, targetB.GridID)
	slog.Info("场景 B 结果", "ok", okB, "code", codeB, "err", errB)

	slog.Info("对比完成", "A_with_heartbeat_prevact", okA, "B_with_business_prevact", okB)
}

// findSellable 查询背包并返回一个价格 > 0 的精英副本制作卷。
func findSellable(ctx context.Context, sess *client.RoleSession, excludeGrid *int32) *protocol.ItemRecord {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	if err != nil || resp == nil {
		slog.Error("查询背包失败", "err", err)
		return nil
	}
	pack, pErr := protocol.ParsePackItemListResponse(resp.Payload)
	if pErr != nil || pack == nil {
		slog.Error("解析背包失败", "err", pErr)
		return nil
	}
	for i := range pack.PackItems {
		it := pack.PackItems[i]
		if it.ItemID < 900 || it.ItemID > 1000 || it.Field4 <= 0 || it.Field5 <= 0 {
			continue
		}
		if excludeGrid != nil && it.GridID == *excludeGrid {
			continue
		}
		return &pack.PackItems[i]
	}
	return nil
}

func sellOnce(ctx context.Context, sess *client.RoleSession, gridID int32) (bool, uint8, error) {
	pkt, err := protocol.BuildSellItemPacket(int16(gridID))
	if err != nil {
		return false, 0, err
	}
	slog.Info("发送出售封包", "grid_id", gridID, "payload_hex", hex.EncodeToString(pkt.Payload))

	sCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(sCtx, pkt, protocol.ActionIDItemSell)
	if err != nil {
		return false, 0, err
	}
	if resp == nil {
		return false, 0, fmt.Errorf("空回包")
	}
	slog.Info("出售回包", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	return protocol.ParseSellItemResponse(resp.Payload)
}
