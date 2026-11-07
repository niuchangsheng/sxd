//go:build probe

// 目标: 小助手好礼馈赠 (task 82) 与琳琅阁 (task 74, Mod_Mobile_Base, Module 100)
//
//	info_assistant       (Act 78, 0x0064004E) request=[]           -> 16 × Byte
//	get_award_assistant  (Act 79, 0x0064004F) request=[Int index]  -> UByte result
//	lucky_shop_item_list (Act 80, 0x00640050) request=[]           -> [[Int,Int],Int,Int,Int]
//	buy_lucky_shop_item  (Act 81, 0x00640051) request=[Int itemID] -> UByte result
//
// 本探针默认只读; -claim 时按 info_assistant 中非零条目逐个尝试领取 (免费).
//
// 背景: 旧探针误用 Act 123/127 (LuckyStore 超级宝箱), 与琳琅阁/好礼馈赠无关。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_assistant_gift_live.go
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
	claim := flag.Bool("claim", false, "按 info_assistant 非零条目尝试领取好礼馈赠")
	all := flag.Bool("all", false, "无论 info_assistant 标记如何, 依次尝试索引 0..15 (仅免费好礼)")
	flag.Parse()
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	info := call(0x0064004E, []byte{0, 0, 0, 0})
	if info != nil {
		slog.Info("小助手信息", "len", len(info.Payload), "hex", hex.EncodeToString(info.Payload))
	}
	if shop := call(0x00640050, []byte{0, 0, 0, 0}); shop != nil {
		slog.Info("琳琅阁商品", "len", len(shop.Payload), "hex", hex.EncodeToString(shop.Payload))
	}
	if *all {
		for i := 0; i < 16; i++ {
			w := protocol.NewWriter()
			w.WriteInt32(int32(i))
			slog.Info("尝试领取好礼(全索引)", "index", i)
			if resp := call(0x0064004F, w.Bytes()); resp != nil {
				slog.Info("好礼领取回包", "index", i, "hex", hex.EncodeToString(resp.Payload))
			}
		}
		return
	}
	if !*claim || info == nil {
		return
	}
	for i, b := range info.Payload {
		if i >= 15 || b == 0 {
			continue
		}
		w := protocol.NewWriter()
		w.WriteInt32(int32(i))
		slog.Info("尝试领取好礼", "index", i, "flag", b)
		if resp := call(0x0064004F, w.Bytes()); resp != nil {
			slog.Info("好礼领取回包", "index", i, "hex", hex.EncodeToString(resp.Payload))
		}
	}
}
