//go:build probe

// 目标: 验证 PrevAct 链污染导致业务请求被静默丢弃时，改用「最后收到的包」
// (LastRxActionID) 作为显式锚点（不置 HasPrevAct）能否恢复。以修真境界
// 851/4→851/5 为例。结论见 docs/research/prevact-chain-rx-anchor.md。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_prevact_resync_live.go
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
		cfgPath = "configs/qq.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("配置加载失败", "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	auth, err := platform.NewXindongAuthenticator()
	if err != nil {
		return
	}
	auth.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(auth)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动失败", "err", err)
		return
	}
	defer sess.Close()
	dl := time.Now().Add(25 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(dl) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活")
		return
	}
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, e := sess.Call(c, pkt, act)
		if e != nil {
			slog.Warn("无回包", "act", act, "err", e)
			return nil
		}
		return resp
	}
	// 851/4 资源列表
	resp := call(protocol.BuildEraRebirthAttrResourcePackagePacket(), protocol.ActionIDEraRebirthAttrResourcePackage)
	if resp == nil {
		slog.Error("851/4 无回包")
		return
	}
	items, _, perr := protocol.ParseEraRebirthAttrTimeItems(resp.Payload)
	if perr != nil || len(items) == 0 {
		slog.Error("851/4 解析失败/无道具", "err", perr)
		return
	}
	it := items[0]
	slog.Info("准备使用时间道具", "item_id", it.ItemID, "num", it.Num,
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))
	// 正常 HasPrevAct
	r1 := call(protocol.BuildEraRebirthAttrUseTimeItemPacket(it.ItemID, it.Num), protocol.ActionIDEraRebirthAttrUseTimeItem)
	if r1 != nil {
		slog.Info("正常 PrevAct 成功", "hex", hex.EncodeToString(r1.Payload))
		return
	}
	// 显式 LastAckActionID 锚点
	anchor := sess.LastAckActionID()
	w := protocol.NewWriter()
	w.WriteInt32(it.ItemID)
	w.WriteInt32(it.Num)
	w.WriteUint32(anchor)
	pkt := protocol.NewPacket(protocol.ActionIDEraRebirthAttrUseTimeItem, w.Bytes())
	r2 := call(pkt, protocol.ActionIDEraRebirthAttrUseTimeItem)
	if r2 == nil {
		slog.Error("显式锚点仍无回包", "anchor", fmt.Sprintf("0x%08X", anchor))
		return
	}
	slog.Info("显式锚点成功", "anchor", fmt.Sprintf("0x%08X", anchor), "hex", hex.EncodeToString(r2.Payload))
}
