//go:build probe

// 目标: 结缘宝箱以列表索引 + PrevAct 形态发送开箱。
//
// 运行: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_jieyuan_index_prev_live.go
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
	action31   uint32 = 0x00E8001F
	action34   uint32 = 0x00E80022
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
		RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3,
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

	call := func(payload []byte, act uint32, name string) bool {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, protocol.NewPacket(act, payload), act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return false
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return true
	}

	if !call(protocol.NewWriter().WriteUint32(0x00040000).Bytes(), actionList, "list") {
		return
	}
	forms := []struct {
		act uint32
		idx int32
		cnt int32
	}{{action34, 1, 10}, {action34, 0, 10}, {action34, 1, 1}, {action31, 1, 1}}
	for _, f := range forms {
		w := protocol.NewWriter()
		w.WriteInt32(f.idx).WriteInt32(f.cnt).WriteUint32(actionList)
		if call(w.Bytes(), f.act, fmt.Sprintf("act%d_idx%d_cnt%d", f.act&0xFFFF, f.idx, f.cnt)) {
			return
		}
	}
	slog.Info("索引+PrevAct 形态均无回包")
}
