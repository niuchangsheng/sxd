//go:build probe

// 目标: 结缘宝箱 232/34 请求编码变体验证。
//
// 线上 s1 当前 [item_id, count, PrevAct] 被静默丢弃。官方 Mod_LinkFate_Base
// open_link_fate_box 调用参数为 [box_index, autoMergeQuality]；本探针依次测试：
//
//	变体 A: [Int item_id, Int count, Int auto_merge=0]  (12 字节)
//	变体 B: [Int item_id, Int count]                    (8 字节)
//	变体 C: [Int item_id, Byte auto_merge=0]            (5 字节)
//
// 只对非传说宝箱发送，任意变体成功即停止，避免多开。
//
// 运行: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_jieyuan_open_variants_live.go
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
	actionList uint32 = 0x00E8001E
	actionOpen uint32 = 0x00E80022
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

	call := func(payload []byte, name string) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, protocol.NewPacket(actionOpen, payload), actionOpen)
		if cErr != nil {
			slog.Warn("无回包", "variant", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "variant", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	// 先查询库存，选第一个非传说宝箱。
	w := protocol.NewWriter().WriteUint32(0x00040000)
	listResp, lErr := func() (*protocol.Packet, error) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		return sess.Call(c, protocol.NewPacket(actionList, w.Bytes()), actionList)
	}()
	if lErr != nil || listResp == nil {
		slog.Error("查询结缘宝箱列表失败", "err", lErr)
		return
	}
	boxes, pErr := protocol.ParseJieYuanBoxListResult(listResp.Payload)
	if pErr != nil {
		slog.Error("解析结缘宝箱列表失败", "err", pErr)
		return
	}
	var itemID, count int32
	for _, b := range boxes {
		if b.Count > 0 && b.ItemID != protocol.ItemJieYuanBoxLegendary {
			itemID, count = b.ItemID, b.Count
			break
		}
	}
	if itemID == 0 {
		slog.Info("没有可开启的非传说结缘宝箱", "boxes", boxes)
		return
	}
	slog.Info("准备测试结缘宝箱", "item_id", itemID, "count", count)

	// 变体 A: [Int item_id, Int count, Int auto_merge]
	wa := protocol.NewWriter()
	wa.WriteInt32(itemID).WriteInt32(count).WriteInt32(0)
	if call(wa.Bytes(), "A_int_int_int") != nil {
		return
	}

	// 变体 B: [Int item_id, Int count]
	wb := protocol.NewWriter()
	wb.WriteInt32(itemID).WriteInt32(count)
	if call(wb.Bytes(), "B_int_int") != nil {
		return
	}

	// 变体 C: [Int item_id, Byte auto_merge]
	wc := protocol.NewWriter()
	wc.WriteInt32(itemID).WriteUint8(0)
	if call(wc.Bytes(), "C_int_byte") != nil {
		return
	}
	slog.Info("三种变体均无回包")
}