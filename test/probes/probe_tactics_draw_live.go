//go:build probe

// 目标: 兵法圣水浇灌 (Mod_Tactics_Base, Module 299)
//
//	get_tactics_info  (Act 0, 0x012B0000) request=[]
//	draw_tactics_exp  (Act 1, 0x012B0001) request=[UByte type, Int count]
//	  type: XIAO_SHENG_SHUI=0 (湛蓝圣水), DA_SHENG_SHUI=1 (黄金圣水), COIN=2, INGOT=3(禁用)
//
// 每日活跃度任务 54「兵法」每次 +5，上限 25 次。
// 本探针只使用圣水道具备用次数，绝不使用元宝档。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_tactics_draw_live.go
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
	act := func(tag string) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if err != nil || resp == nil {
			slog.Warn("活跃度读取失败", "tag", tag)
			return
		}
		p, err := protocol.ParseGameAssistantInfo(resp.Payload)
		if err != nil {
			return
		}
		slog.Info("活跃度", "tag", tag, "daily_total", p.DailyTotal(), "task54_remain", p.RemainingTimesFor(54))
	}
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	infoResp := call(0x012B0000, []byte{0, 0, 0, 0})
	if infoResp == nil {
		slog.Error("兵法面板无回包")
		return
	}
	slog.Info("兵法面板", "len", len(infoResp.Payload), "hex", hex.EncodeToString(infoResp.Payload))
	act("before")
	st := sess.GetPlayerState()
	// 5 字节原样请求 (不带 PrevAct)，type=1/0，count=1
	drawOnce := func(t uint8) *protocol.Packet {
		w := protocol.NewWriter()
		w.WriteUint8(t)
		w.WriteInt32(1)
		pkt := protocol.NewPacket(0x012B0001, w.Bytes())
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, 0x012B0001)
		if err != nil {
			slog.Warn("浇灌无回包", "type", t, "err", err)
			return nil
		}
		return resp
	}
	for _, t := range []uint8{1, 0} {
		if resp := drawOnce(t); resp != nil {
			slog.Info("浇灌回包", "type", t, "hex", hex.EncodeToString(resp.Payload))
			break
		}
	}
	time.Sleep(300 * time.Millisecond)
	act("after")
	fin := sess.GetPlayerState()
	slog.Info("浇灌后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
