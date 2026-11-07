//go:build probe

// 目标: 锁妖塔 challenge 参数语义验证。
//
//	panel_info 705/0、get_rank 705/5 读取 self_layer，
//	然后依次尝试 challenge(self_layer) 与 challenge(self_layer+1)，
//	打印结果码，确认“下一层”到底该传哪个值。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_demon_tower_layer_live.go
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
	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		slog.Info("回包", "action", fmt.Sprintf("0x%08X", act), "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:min(16, len(resp.Payload))]))
		return resp
	}

	// 只读 get_rank 取 self_layer。
	if r := call(protocol.BuildDemonTowerRankPacket(), protocol.ActionIDDemonTowerRank, 8*time.Second); r != nil {
		result, selfLayer, err := protocol.ParseDemonTowerRankSelfLayer(r.Payload)
		slog.Info("排行榜", "result", result, "self_layer", selfLayer, "err", err)
		for _, layer := range []int32{selfLayer, selfLayer + 1} {
			if layer <= 0 {
				continue
			}
			resp := call(protocol.BuildDemonTowerChallengePacket(layer),
				protocol.ActionIDDemonTowerChallenge, 20*time.Second)
			if resp != nil {
				code, _ := protocol.ParseDemonTowerChallengeResult(resp.Payload)
				slog.Info("挑战结果", "layer", layer, "result", code)
			}
		}
	}

	// 只读 panel_info 对照 now_finish_layer / layer_list。
	if r := call(protocol.BuildDemonTowerPanelPacket(), protocol.ActionIDDemonTowerPanel, 8*time.Second); r != nil {
		p, err := protocol.ParseDemonTowerPanelResult(r.Payload)
		if err == nil {
			slog.Info("面板", "result", p.Result, "now_step", p.NowStep,
				"now_finish_layer", p.NowFinishLayer, "tickets", p.NowTicketNum,
				"layers", len(p.LayerList), "next", p.NextLayerID(), "err", err)
		}
	}
}
