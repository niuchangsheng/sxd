//go:build probe

// 目标: 仙盟神像上香 (每日任务 49, +5)
//
//	Mod_StUnionActivity_Base (Module 179) 在仙盟城镇会话上:
//	  get_st_union_god_info (Act 10, 0x00B3000A) request=[]
//	  st_union_god_incense (Act 11, 0x00B3000B) request=[Int incense_id]
//
// 香档 1 通常为免费白檀香 (元宝档 2/3 禁用)。本探针先只读, -incense N 时上香一次。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_union_incense_live.go
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
	incense := flag.Int("incense", -1, "上香档位 (>=0 时发起一次, 建议 1=免费白檀香)")
	flag.Parse()
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
	cred, err := sess.FetchSectTownCredentials(ctx)
	if err != nil {
		slog.Error("读取仙盟凭据失败", "err", err)
		return
	}
	townCtx, townCancel := context.WithTimeout(ctx, 120*time.Second)
	defer townCancel()
	town, err := sess.OpenSectTownSessionWithCredentials(townCtx, cred)
	if err != nil {
		slog.Error("建立仙盟城镇会话失败", "err", err)
		return
	}
	defer town.Close()

	call := func(action uint32, payload []byte, timeout time.Duration, withPrev bool) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = withPrev
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := town.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "with_prev", withPrev, "err", err)
			return nil
		}
		return resp
	}
	// 面板: 依次尝试空包 / [sect_id] / [sect_id]+PrevAct。
	var info *protocol.Packet
	if r := call(0x00B3000A, nil, 8*time.Second, false); r != nil {
		info = r
	}
	if info == nil {
		w := protocol.NewWriter()
		w.WriteInt32(cred.LocalSectID)
		if r := call(0x00B3000A, w.Bytes(), 8*time.Second, false); r != nil {
			info = r
		}
	}
	if info == nil {
		w := protocol.NewWriter()
		w.WriteInt32(cred.LocalSectID)
		w.WriteUint32(0)
		if r := call(0x00B3000A, w.Bytes(), 8*time.Second, true); r != nil {
			info = r
		}
	}
	if info != nil {
		slog.Info("仙盟神像面板(城镇会话)", "len", len(info.Payload), "hex", hex.EncodeToString(info.Payload))
	} else {
		// 主服会话再试一次。
		for _, withPrev := range []bool{false, true} {
			payload := []byte(nil)
			if withPrev {
				payload = []byte{0, 0, 0, 0}
			}
			pkt := protocol.NewPacket(0x00B3000A, payload)
			pkt.HasPrevAct = withPrev
			c, cc := context.WithTimeout(ctx, 8*time.Second)
			r, err := sess.Call(c, pkt, 0x00B3000A)
			cc()
			if err != nil {
				slog.Warn("主服无回包", "with_prev", withPrev, "err", err)
				continue
			}
			slog.Info("仙盟神像面板(主服)", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
			break
		}
	}
	if *incense < 0 {
		return
	}
	st := sess.GetPlayerState()
	w := protocol.NewWriter()
	w.WriteInt32(int32(*incense))
	var resp *protocol.Packet
	if r := call(0x00B3000B, w.Bytes(), 10*time.Second, false); r != nil {
		resp = r
	} else {
		w2 := protocol.NewWriter()
		w2.WriteInt32(int32(*incense))
		w2.WriteUint32(0)
		resp = call(0x00B3000B, w2.Bytes(), 10*time.Second, true)
	}
	if resp != nil {
		slog.Info("仙盟上香回包", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}
	fin := sess.GetPlayerState()
	slog.Info("仙盟上香后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
