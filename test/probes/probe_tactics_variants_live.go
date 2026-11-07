//go:build probe

// 目标: 兵法浇灌参数矩阵 (Mod_Tactics_Base, 299/1)
//
//	draw_tactics_exp request=[UByte type, Int ?]
//	type: 湛蓝圣水=0, 黄金圣水=1, 铜钱=2, 元宝=3(禁用)
//	成功结果码 SUCCEED=7
//
// 依次尝试 water 类型 0/1/2 与第二参数 0/1/2，命中 SUCCEED 即停止；
// 每次调用后校验元宝不变。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_tactics_variants_live.go
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
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	// 面板
	if r := call(0x012B0000, []byte{0, 0, 0, 0}); r != nil {
		slog.Info("兵法面板", "hex", hex.EncodeToString(r.Payload))
	}
	st := sess.GetPlayerState()
	for _, typ := range []uint8{0, 1, 2} {
		for _, arg := range []int32{0, 1, 2} {
			if ctx.Err() != nil {
				break
			}
			w := protocol.NewWriter()
			w.WriteUint8(typ)
			w.WriteInt32(arg)
			resp := call(0x012B0001, w.Bytes())
			if resp == nil {
				continue
			}
			code := byte(0)
			if len(resp.Payload) > 0 {
				code = resp.Payload[0]
			}
			slog.Info("浇灌返回", "type", typ, "arg", arg, "result", code,
				"hex", hex.EncodeToString(resp.Payload))
			if code == 7 {
				time.Sleep(300 * time.Millisecond)
				fin := sess.GetPlayerState()
				slog.Info("浇灌成功", "type", typ, "arg", arg,
					"coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
				c, cc := context.WithTimeout(ctx, 8*time.Second)
				ar, aerr := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
				cc()
				if aerr == nil && ar != nil {
					if p, perr := protocol.ParseGameAssistantInfo(ar.Payload); perr == nil {
						slog.Info("活跃度", "daily_total", p.DailyTotal(), "task54_remain", p.RemainingTimesFor(54))
					}
				}
				return
			}
		}
	}
	fin := sess.GetPlayerState()
	slog.Info("全部变体未成功", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
