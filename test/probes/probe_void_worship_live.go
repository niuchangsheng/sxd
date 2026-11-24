//go:build probe

// 目标: 定位大神榜膜拜 500/1 固定返回 FAILED(5) 的原因，验证是否需要先通过
// 95/0 进入封神之境 (Mapkey: FengShenZhiJing = 102) 再膜拜。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_void_worship_live.go
//
// 行为: 主服 500/1 → 跨服 95/0(102) → 主服 500/1 → 跨服 500/1，
// 打印各步结果码。膜拜免费，不消耗元宝。
package main

import (
	"context"
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
	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
	deadline := time.Now().Add(90 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := s.Call(c, pkt, act)
		ccancel()
		if err != nil {
			slog.Info("无回包", "scope", name, "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		code, _ := protocol.ParseGodRankResult(resp.Payload)
		if act != protocol.ActionIDGodRankWorship {
			code = 0
		}
		slog.Info("回包", "scope", name, "action", fmt.Sprintf("0x%08X", act),
			"len", len(resp.Payload), "result", code,
			"head", fmt.Sprintf("%x", resp.Payload[:min(16, len(resp.Payload))]))
		return resp
	}

	call(sess, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship, "main/500/1 前置", 10*time.Second)

	cross, cErr := sess.OpenCrossServerSession(ctx)
	if cErr != nil {
		slog.Error("建立跨服会话失败", "err", cErr)
		return
	}
	defer cross.Close()

	call(cross, protocol.BuildStTownEnterPacket(102), protocol.ActionIDStTownEnter, "cross/95/0 enter 102", 10*time.Second)
	call(sess, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship, "main/500/1 进入后", 10*time.Second)
	call(cross, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship, "cross/500/1", 10*time.Second)

	// 在主服会话上直接进入封神之境后再膜拜。
	call(sess, protocol.BuildStTownEnterPacket(102), protocol.ActionIDStTownEnter, "main/95/0 enter 102", 10*time.Second)
	call(sess, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship, "main/500/1 after main enter", 10*time.Second)

	// 500/2 最佳玩家信息：无膜拜对象时 500/1 会返回 FAILED(5)。
	bestPkt := protocol.NewPacket(protocol.ActionIDGodRankBestPlayer, []byte{0, 0, 0, 0})
	bestPkt.HasPrevAct = true
	call(sess, bestPkt, protocol.ActionIDGodRankBestPlayer, "main/500/2 best_player", 8*time.Second)

	// 尝试先按榜单类型调用 500/0 get_rank_list，再膜拜。
	for _, typ := range []uint8{0, 1, 2} {
		w := protocol.NewWriter()
		w.WriteUint8(typ)
		w.WriteUint32(0)
		listPkt := protocol.NewPacket(0x01F40000, w.Bytes())
		listPkt.HasPrevAct = true
		name := fmt.Sprintf("main/500/0 type=%d", typ)
		listResp := call(sess, listPkt, 0x01F40000, name, 8*time.Second)
		if listResp != nil {
			slog.Info("榜单回包", "type", typ, "len", len(listResp.Payload))
		}
		call(sess, protocol.BuildGodRankWorshipPacket(), protocol.ActionIDGodRankWorship,
			fmt.Sprintf("main/500/1 after type=%d", typ), 10*time.Second)
		time.Sleep(300 * time.Millisecond)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
