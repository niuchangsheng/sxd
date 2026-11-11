//go:build probe

// 目标: 验证结缘宝箱 232 动作是否在仙盟城镇会话上可达。
//
// 主服 232/30 列表可回包，但 232/31、232/34 开箱均静默丢弃；抓包中同时出现
// :8251 第二条连接。本探针在 s0 的仙盟城镇会话上重复：
//
//	232/30 列表
//	232/34 [Int item_id, Int count, Uint32 PrevAct]
//
// 只读/开箱请求，不购买任何元宝档。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_jieyuan_sect_live.go
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
		cfgPath = "configs/niuchangsheng.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}

	town, err := sess.OpenSectTownSession(ctx)
	if err != nil {
		slog.Error("建立仙盟城镇会话失败", "err", err)
		return
	}
	defer town.Close()
	slog.Info("仙盟城镇会话已建立", "player_id", town.PlayerID(), "role_id", town.RoleID())

	call := func(s *client.RoleSession, payload []byte, act uint32, name string) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := s.Call(c, protocol.NewPacket(act, payload), act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 60 {
			limit = 60
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	listPayload := protocol.NewWriter().WriteUint32(0x00040000).Bytes()
	call(town, listPayload, actionList, "town_232_30_list")
	openPayload := protocol.NewWriter().WriteInt32(3937).WriteInt32(1).WriteUint32(actionList).Bytes()
	call(town, openPayload, actionOpen, "town_232_34_open")
}