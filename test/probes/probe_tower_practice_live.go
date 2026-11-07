//go:build probe

// 目标: 通天塔修行 (Mod_TongTianTower_Base.practice, 648/2) 参数探测
//
//	panel_info (0) 读取 heaven/floor
//	practice (2) request=[Int,Int,Int] response=[UByte,Int,Int]
//
// 使用当前 heaven/floor 与 type=0 尝试一次，观察结果码。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_tower_practice_live.go
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	panelPkt := protocol.NewPacket(0x02880000, []byte{0, 0, 0, 0})
	panelPkt.HasPrevAct = true
	panel := call(panelPkt, 0x02880000)
	if panel == nil {
		slog.Error("通天塔面板无回包")
		return
	}
	// panel_info: [Int x12, Byte, Int x4] -> 读取前 4 个 Int 作为 heaven/floor 候选
	r := protocol.NewReader(panel.Payload)
	vals := make([]int32, 0, 12)
	for i := 0; i < 12; i++ {
		v, _ := r.ReadInt32()
		vals = append(vals, v)
	}
	slog.Info("通天塔面板", "ints", vals, "hex", hex.EncodeToString(panel.Payload[:48]))
	heaven, floor, curHeaven, curFloor := vals[0], vals[1], vals[9], vals[10]
	slog.Info("面板字段", "heaven", heaven, "floor", floor, "cur_heaven", curHeaven, "cur_floor", curFloor)
	// practice = 扫荡: [heaven, floor, battle_timer 秒]
	for _, target := range [][2]int32{{curHeaven, curFloor}, {heaven, floor}} {
		for _, timer := range []int32{1, 5} {
			w := protocol.NewWriter()
			w.WriteInt32(target[0])
			w.WriteInt32(target[1])
			w.WriteInt32(timer)
			w.WriteUint32(0)
			pkt := protocol.NewPacket(0x02880002, w.Bytes())
			pkt.HasPrevAct = true
			if resp := call(pkt, 0x02880002); resp != nil {
				slog.Info("扫荡回包", "heaven", target[0], "floor", target[1], "timer", timer,
					"hex", hex.EncodeToString(resp.Payload))
				return
			}
		}
	}
}
