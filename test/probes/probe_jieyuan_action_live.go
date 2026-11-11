//go:build probe

// 目标: 结缘宝箱单开动作号验证。
//
// 官方 Mod_LinkFate.as 注册顺序显示 get_link_fate_box=30 时，紧随其后的
// open_link_fate_box 应为 31（而非当前使用的 34=one_key_open_box）。
// 本探针对 232/31 依次尝试官方参数形态 [Int box, Int autoMerge] /
// [Int box, Byte autoMerge]，仅对非传说宝箱发送。
//
// 运行: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_jieyuan_action_live.go
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
	actionList    uint32 = 0x00E8001E // 232/30
	actionOpenNew uint32 = 0x00E8001F // 232/31 open_link_fate_box
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

	w := protocol.NewWriter().WriteUint32(0x00040000)
	listCtx, listCancel := context.WithTimeout(ctx, 8*time.Second)
	listResp, lErr := sess.Call(listCtx, protocol.NewPacket(actionList, w.Bytes()), actionList)
	listCancel()
	if lErr != nil || listResp == nil {
		slog.Error("查询结缘宝箱列表失败", "err", lErr)
		return
	}
	boxes, pErr := protocol.ParseJieYuanBoxListResult(listResp.Payload)
	if pErr != nil {
		slog.Error("解析结缘宝箱列表失败", "err", pErr)
		return
	}
	var itemID int32
	for _, b := range boxes {
		if b.Count > 0 && b.ItemID != protocol.ItemJieYuanBoxLegendary {
			itemID = b.ItemID
			break
		}
	}
	if itemID == 0 {
		slog.Info("没有可开启的非传说结缘宝箱", "boxes", boxes)
		return
	}
	slog.Info("准备测试 232/31", "item_id", itemID)

	call := func(payload []byte, name string) bool {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, protocol.NewPacket(actionOpenNew, payload), actionOpenNew)
		if cErr != nil {
			slog.Warn("无回包", "form", name, "err", cErr)
			return false
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "form", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return true
	}

	// A: [Int item_id, Int auto_merge]
	wa := protocol.NewWriter().WriteInt32(itemID).WriteInt32(0)
	if call(wa.Bytes(), "A_int_int") {
		return
	}
	// B: [Int item_id, Byte auto_merge]
	wb := protocol.NewWriter().WriteInt32(itemID).WriteUint8(0)
	if call(wb.Bytes(), "B_int_byte") {
		return
	}
	// C: [Int 0, Int auto_merge] (box index 0)
	wc := protocol.NewWriter().WriteInt32(0).WriteInt32(0)
	if call(wc.Bytes(), "C_zero_int") {
		return
	}
	// D: [Int 0, Byte auto_merge]
	wd := protocol.NewWriter().WriteInt32(0).WriteUint8(0)
	if call(wd.Bytes(), "D_zero_byte") {
		return
	}
	// E: 仅 [Int 0] (box index 0)
	we := protocol.NewWriter().WriteInt32(0)
	if call(we.Bytes(), "E_int0") {
		return
	}
	// F: 仅 [Int 1]
	wf := protocol.NewWriter().WriteInt32(1)
	if call(wf.Bytes(), "F_int1") {
		return
	}
	slog.Info("232/31 六种形态均无回包")
}
