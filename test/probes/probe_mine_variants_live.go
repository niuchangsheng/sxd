//go:build probe

// 目标: 排查仙界矿山 (Module 264, Mod_StMine) 恒定返回 FAILED(13) 的原因。
//
//	get_mine_mountain_info (264/0, 0x01080000)
//	enter_mine_mountain    (264/1, 0x01080001)
//
// 对 264/0 依次尝试三种载荷形态并打印回包：
//  1. 裸载荷；
//  2. HasPrevAct（由会话用最后发送动作号填充尾部）；
//  3. 显式 LastAckActionID 锚点（不置 HasPrevAct，避免被本地 lastActionID 覆盖）。
//
// 任一形态返回 result=9(SUCCESS) 时，再用同一形态尝试进入当前矿脉并打印完整回包。
// 本探针不调用 enter 之外的写操作，不消耗元宝。
//
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_mine_variants_live.go
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
	actMineInfo  uint32 = 0x01080000
	actMineEnter uint32 = 0x01080001
)

func mineInfoPacket(variant int, anchor uint32) *protocol.Packet {
	switch variant {
	case 0:
		return protocol.NewPacket(actMineInfo, nil)
	case 1:
		return &protocol.Packet{ActionID: actMineInfo, Payload: make([]byte, 4), HasPrevAct: true}
	default:
		w := protocol.NewWriter()
		w.WriteUint32(anchor)
		return protocol.NewPacket(actMineInfo, w.Bytes())
	}
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置缺少账号或角色")
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	deadline := time.Now().Add(25 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID(),
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))

	// FunctionTypeData.List 157 = StMine（通关天庭女儿国(1)开启）。
	if open, fErr := sess.IsFunctionOpen(ctx, 157); fErr != nil {
		slog.Warn("矿山功能开放状态查询失败", "err", fErr)
	} else {
		slog.Info("矿山功能开放状态", "function_id", 157, "open", open)
	}

	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "act", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	names := []string{"bare", "has_prev_act", "explicit_ack_anchor"}
	var chosen = -1
	var curMountain uint8
	for variant := 0; variant < 3; variant++ {
		anchor := sess.LastAckActionID()
		resp := call(mineInfoPacket(variant, anchor), actMineInfo)
		if resp == nil {
			continue
		}
		head := resp.Payload
		if len(head) > 48 {
			head = head[:48]
		}
		slog.Info("矿山信息回包", "variant", names[variant], "anchor", fmt.Sprintf("0x%08X", anchor),
			"len", len(resp.Payload), "head_hex", hex.EncodeToString(head))
		if len(resp.Payload) >= 1 && resp.Payload[0] == 9 {
			chosen = variant
			if info, pErr := protocol.ParseStMineInfoResult(resp.Payload); pErr == nil {
				curMountain = info.CurMountain
				slog.Info("矿山信息解析成功", "level", info.Level, "cur_mountain", info.CurMountain)
			}
			break
		}
	}
	if chosen < 0 {
		slog.Info("主服会话矿山不可用，改在仙界跨服会话重试")
		cross, cErr := sess.OpenCrossServerSession(ctx)
		if cErr != nil {
			slog.Error("建立仙界跨服会话失败", "err", cErr)
			return
		}
		defer cross.Close()
		crossCall := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
			defer cCancel()
			resp, cErr := cross.Call(cCtx, pkt, act)
			if cErr != nil {
				slog.Warn("跨服无回包", "act", fmt.Sprintf("0x%08X", act), "err", cErr)
				return nil
			}
			return resp
		}
		for variant := 0; variant < 3; variant++ {
			anchor := cross.LastAckActionID()
			resp := crossCall(mineInfoPacket(variant, anchor), actMineInfo)
			if resp == nil {
				continue
			}
			head := resp.Payload
			if len(head) > 48 {
				head = head[:48]
			}
			slog.Info("跨服矿山信息回包", "variant", names[variant], "anchor", fmt.Sprintf("0x%08X", anchor),
				"len", len(resp.Payload), "head_hex", hex.EncodeToString(head))
			if len(resp.Payload) >= 1 && resp.Payload[0] == 9 {
				chosen = variant
				if info, pErr := protocol.ParseStMineInfoResult(resp.Payload); pErr == nil {
					curMountain = info.CurMountain
					slog.Info("跨服矿山信息解析成功", "level", info.Level, "cur_mountain", info.CurMountain)
				}
				break
			}
		}
		if chosen < 0 {
			slog.Error("主服与跨服均未返回 SUCCESS(9)，矿山当前不可进入")
			return
		}
		enterPkt := protocol.NewPacket(actMineEnter, []byte{curMountain})
		if chosen == 1 {
			enterPkt = &protocol.Packet{ActionID: actMineEnter, Payload: []byte{curMountain, 0, 0, 0, 0}, HasPrevAct: true}
		} else if chosen == 2 {
			w := protocol.NewWriter()
			w.WriteUint8(curMountain)
			w.WriteUint32(cross.LastAckActionID())
			enterPkt = protocol.NewPacket(actMineEnter, w.Bytes())
		}
		enterResp := crossCall(enterPkt, actMineEnter)
		if enterResp == nil {
			slog.Error("跨服进入矿山无回包")
			return
		}
		slog.Info("跨服进入矿山回包", "variant", names[chosen], "len", len(enterResp.Payload),
			"hex", hex.EncodeToString(enterResp.Payload))
		return
	}

	enterPkt := protocol.NewPacket(actMineEnter, []byte{curMountain})
	if chosen == 1 {
		enterPkt = &protocol.Packet{ActionID: actMineEnter, Payload: []byte{curMountain, 0, 0, 0, 0}, HasPrevAct: true}
	} else if chosen == 2 {
		w := protocol.NewWriter()
		w.WriteUint8(curMountain)
		w.WriteUint32(sess.LastAckActionID())
		enterPkt = protocol.NewPacket(actMineEnter, w.Bytes())
	}
	enterResp := call(enterPkt, actMineEnter)
	if enterResp == nil {
		slog.Error("进入矿山无回包")
		return
	}
	slog.Info("进入矿山回包", "variant", names[chosen], "len", len(enterResp.Payload),
		"hex", hex.EncodeToString(enterResp.Payload))
}
