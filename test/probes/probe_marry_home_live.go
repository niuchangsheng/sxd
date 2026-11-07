//go:build probe

// 目标: 家园每日奖励 (Mod_MarryHome_Base, Module 210, Act 19 get_cake)
//
//	get_cake request=[Int player_id] response=[UByte result, [Int item_id, Int item_num] award_list]
//
// 官方客户端在自家小蛋糕处调用 get_cake(自己的 player_id)。本探针先只读
// get_self_home_info (210/0)，再尝试领取一次免费家园奖励，并核对元宝不变。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_marry_home_live.go
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
	actionMarryHomeSelfInfo uint32 = 0x00D20000 // 210/0
	actionMarryHomeGetCake  uint32 = 0x00D20013 // 210/19
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	claim := flag.Bool("claim", false, "调用 get_cake 领取一次家园奖励")
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
	if info := call(actionMarryHomeSelfInfo, []byte{0, 0, 0, 0}); info != nil {
		slog.Info("自家家园信息", "len", len(info.Payload), "hex", hex.EncodeToString(info.Payload))
	}
	pid := sess.PlayerID()
	slog.Info("角色 player_id", "player_id", pid)
	if !*claim {
		return
	}
	st := sess.GetPlayerState()
	w := protocol.NewWriter()
	w.WriteInt32(int32(pid))
	resp := call(actionMarryHomeGetCake, w.Bytes())
	if resp == nil {
		return
	}
	slog.Info("家园奖励回包", "hex", hex.EncodeToString(resp.Payload))
	fin := sess.GetPlayerState()
	slog.Info("家园奖励后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
