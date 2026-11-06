//go:build probe

// 目标: 将角色从山海九界 (town_map_id=153) 复位到普通城镇，恢复 NPC 玩法可用性
//
// 背景: 角色残留在特殊地图时，服务端会静默丢弃部分模块的写请求；官方客户端
// 通过 Mod_Town_Base.enter_town 回到普通城镇。本探针调用 EnterTown(4) 天剑宗，
// 并打印进入前后的 town_id/town_map_id，再用该城镇的神秘商人 NPC 101 查询琳琅阁。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_TOWN=4 go run -tags=probe test/probes/probe_enter_town_live.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	townID := int32(4)
	if raw := os.Getenv("SXD_PROBE_TOWN"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			townID = int32(v)
		}
	}

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

	before := sess.GetPlayerState()
	slog.Info("进入城镇前", "town_id", before.TownID, "town_map_id", before.TownMapID)

	// 先离开当前特殊地图，再进入普通城镇。
	leaveW := protocol.NewWriter()
	leaveW.WriteUint32(0)
	leavePkt := protocol.NewPacket(0x00010001, leaveW.Bytes())
	leavePkt.HasPrevAct = true
	lCtx, lCancel := context.WithTimeout(ctx, 5*time.Second)
	leaveResp, lErr := sess.Call(lCtx, leavePkt, 0x00010001)
	lCancel()
	if lErr != nil || leaveResp == nil {
		slog.Warn("离开当前城镇无回包，继续尝试直接进入", "err", lErr)
	} else {
		slog.Info("离开当前城镇结果", "hex", fmt.Sprintf("%x", leaveResp.Payload))
	}

	result, err := sess.EnterTown(ctx, townID)
	if err != nil {
		slog.Error("进入城镇失败", "town_id", townID, "err", err)
		return
	}
	slog.Info("进入城镇结果", "town_id", townID, "result", result)

	time.Sleep(500 * time.Millisecond)
	after := sess.GetPlayerState()
	slog.Info("进入城镇后", "town_id", after.TownID, "town_map_id", after.TownMapID)

	// 扫描常见城镇的神秘商人 NPC ID，确认当前地图接受哪一个。
	for _, npcID := range []int32{101, 100, 102, 130, 131, 132, 38, 0} {
		listW := protocol.NewWriter()
		listW.WriteInt32(npcID)
		listW.WriteUint32(0)
		pkt := protocol.NewPacket(0x0002004C, listW.Bytes())
		pkt.HasPrevAct = true
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, 0x0002004C)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("查询琳琅阁失败", "npc_id", npcID, "err", cErr)
			continue
		}
		r := protocol.NewReader(resp.Payload)
		n, _ := r.ReadUint16()
		slog.Info("琳琅阁商品数量", "npc_id", npcID, "count", n)
		for i := 0; i < int(n); i++ {
			id, _ := r.ReadInt32()
			price, _ := r.ReadInt32()
			slog.Info("商品", "item_id", id, "coin_price", price)
		}
		hour, _ := r.ReadInt32()
		sec, _ := r.ReadInt32()
		ingot, _ := r.ReadInt32()
		slog.Info("刷新信息", "hour", hour, "seconds", sec, "ingot", ingot)
		return
	}
}