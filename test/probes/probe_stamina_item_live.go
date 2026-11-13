//go:build probe

// 目标: 定位体力道具使用 2/53 静默丢包的载荷形态。
//
// 背景: s0 体力不足时连续尝试包子/粽子/茶叶蛋，全部 3 秒超时。
// 2/53 官方 request=[Short(grid_id)]；会话层对 2 字节 use_grid 会自动
// 追加 4 字节 PrevAct，疑似服务端只接受裸 2 字节（与 25/0、35/0 一致）。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_stamina_item_live.go -role 0
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
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "stamina", sess.GetPlayerState().Stamina)

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 30 {
			limit = 30
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	packResp := call(protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList, "pack_list")
	if packResp == nil {
		return
	}
	pack, pErr := protocol.ParsePackItemListResponse(packResp.Payload)
	if pErr != nil {
		slog.Warn("解析背包失败", "err", pErr)
		return
	}
	grid := int16(-1)
	itemName := ""
	for _, it := range pack.PackItems {
		switch it.ItemID {
		case protocol.ItemIDSteamedBun:
			grid, itemName = int16(it.GridID), "包子"
		case protocol.ItemIDRiceDumpling:
			if grid < 0 {
				grid, itemName = int16(it.GridID), "粽子"
			}
		case protocol.ItemIDTeaEgg:
			if grid < 0 {
				grid, itemName = int16(it.GridID), "茶叶蛋"
			}
		}
		if grid >= 0 {
			break
		}
	}
	if grid < 0 {
		slog.Info("背包中未找到体力道具")
		return
	}
	slog.Info("选定体力道具", "item", itemName, "grid_id", grid)

	gridBytes := []byte{byte(uint16(grid) >> 8), byte(grid)}
	prev := sess.LastAckActionID()
	variants := []struct {
		name    string
		payload []byte
	}{
		{"use_bare", gridBytes},
		{"use_prev_ack", append(append([]byte{}, gridBytes...), byte(prev>>24), byte(prev>>16), byte(prev>>8), byte(prev))},
		{"use_prev0", append(append([]byte{}, gridBytes...), 0, 0, 0, 0)},
		{"use_int32_grid", []byte{0, 0, byte(uint16(grid) >> 8), byte(grid)}},
	}
	for _, v := range variants {
		sess.SetLastActionID(0) // 禁止会话层自动追加 PrevAct
		if call(protocol.NewPacket(protocol.ActionIDItemUseGrid, v.payload), protocol.ActionIDItemUseGrid, v.name) != nil {
			break
		}
		time.Sleep(1500 * time.Millisecond)
	}
	slog.Info("探针结束")
}