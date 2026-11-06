//go:build probe

// 目标协议: Mod_Dragonball_Base.collect_dragonball 单颗收取 (Module 111, Action 1)
//
// 线上结论: 批次中只要混有失效记录，服务端会对整批返回
// INVALID_DRAGONBALL(2)；逐颗发送时有效记录可成功收取。
// 本探针只发送已验证可接受的扁平格式 [Uint16 count=1, Int32 player_d_b_id]，
// 逐颗跳过失效记录，用于清空祭坛临时格。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_dragonball_collect_single_live.go
// 预期表现: 每次成功打印 SUCCESS 与 collected id；失效记录打印跳过；最终打印剩余数量。
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

type ball struct {
	playerDBID int32
	dbID       int16
	lv         uint8
}

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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	fetch := func() []ball {
		resp := call(protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo, 6*time.Second)
		if resp == nil {
			return nil
		}
		r := protocol.NewReader(resp.Payload)
		_, _ = r.ReadInt16()
		_, _ = r.ReadInt16()
		_, _ = r.ReadUint8()
		for i := 0; i < 3; i++ {
			_, _ = r.ReadInt16()
		}
		n, err := r.ReadUint16()
		if err != nil {
			return nil
		}
		out := make([]ball, 0, n)
		for i := 0; i < int(n); i++ {
			var b ball
			b.playerDBID, _ = r.ReadInt32()
			b.dbID, _ = r.ReadInt16()
			b.lv, _ = r.ReadUint8()
			out = append(out, b)
		}
		return out
	}

	before := fetch()
	slog.Info("祭坛临时格", "count", len(before))
	for _, b := range before {
		slog.Info("龙珠", "player_d_b_id", b.playerDBID, "d_b_id", b.dbID, "lv", b.lv)
	}

	ok, invalid := 0, 0
	for _, b := range before {
		if ctx.Err() != nil {
			break
		}
		w := protocol.NewWriter()
		w.WriteUint16(1)
		w.WriteInt32(b.playerDBID)
		resp := call(protocol.NewPacket(protocol.ActionIDDragonballCollect, w.Bytes()),
			protocol.ActionIDDragonballCollect, 8*time.Second)
		if resp == nil {
			continue
		}
		code, ids, pErr := protocol.ParseDragonballCollectResult(resp.Payload)
		if pErr != nil {
			slog.Warn("解析收取回包失败", "player_d_b_id", b.playerDBID, "err", pErr)
			continue
		}
		switch code {
		case protocol.DragonballResultSuccess, protocol.DragonballResultPackageFull:
			ok++
			slog.Info("收取成功", "player_d_b_id", b.playerDBID, "collected", ids, "result", code)
		case protocol.DragonballResultInvalidDragonball:
			invalid++
			slog.Info("跳过失效龙珠", "player_d_b_id", b.playerDBID, "d_b_id", b.dbID)
		default:
			slog.Info("收取未成功", "player_d_b_id", b.playerDBID, "result", code)
		}
		time.Sleep(150 * time.Millisecond)
	}
	slog.Info("收取完成", "before", len(before), "success", ok, "invalid", invalid, "remain", len(fetch()))
}