//go:build probe

// 目标: 轮回净地快速找回 (Mod_EraRebirthLandAuto_Base, 852/4 与 852/5)
//
//	quick_auto_back_panel (Act 4, 0x03540004) request=[] response=[[Int,Int,Int]]
//	quick_auto_back       (Act 5, 0x03540005) request=[Int id] response=[UByte,[Int,Long]]
//
// 每日活跃度任务 70「轮回净地领取挂机奖励」；本探针在面板中挑选一个 id 调用，
// 并校验元宝不变。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_rebirth_quick_live.go
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
	act := func() (int, int) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if err != nil || resp == nil {
			return 0, -1
		}
		p, err := protocol.ParseGameAssistantInfo(resp.Payload)
		if err != nil {
			return 0, -1
		}
		return p.DailyTotal(), p.RemainingTimesFor(70)
	}
	before, remain := act()
	slog.Info("快速找回前", "daily_total", before, "task70_remain", remain)
	panel := call(0x03540004, []byte{0, 0, 0, 0})
	if panel == nil {
		slog.Info("快速找回面板不可用")
		return
	}
	slog.Info("快速找回面板", "hex", hex.EncodeToString(panel.Payload))
	r := protocol.NewReader(panel.Payload)
	n, _ := r.ReadUint16()
	ids := make([]int32, 0, n)
	for i := 0; i < int(n); i++ {
		id, _ := r.ReadInt32()
		_, _ = r.ReadInt32()
		_, _ = r.ReadInt32()
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		slog.Info("无可快速找回条目")
		return
	}
	st := sess.GetPlayerState()
	var got *protocol.Packet
	for _, id := range ids {
		if ctx.Err() != nil || got != nil {
			break
		}
		for _, withPrev := range []bool{false, true} {
			w := protocol.NewWriter()
			w.WriteInt32(id)
			if withPrev {
				w.WriteUint32(0)
			}
			pkt := protocol.NewPacket(0x03540005, w.Bytes())
			pkt.HasPrevAct = withPrev
			c, cc := context.WithTimeout(ctx, 6*time.Second)
			r, err := sess.Call(c, pkt, 0x03540005)
			cc()
			if err != nil {
				continue
			}
			slog.Info("快速找回回包", "id", id, "with_prev", withPrev, "hex", hex.EncodeToString(r.Payload))
			got = r
			break
		}
	}
	_ = got
	time.Sleep(300 * time.Millisecond)
	after, remain2 := act()
	fin := sess.GetPlayerState()
	slog.Info("快速找回后", "daily_total", after, "task70_remain", remain2,
		"activity_delta", after-before, "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
