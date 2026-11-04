//go:build probe

// 目标协议: [Module 10, Action 22] faction_god_info + [Module 69, Action 1] open_lucky_wheel
// 基准对齐: s0 服两个只读动作恒定 context deadline exceeded，s1 服同代码正常；
//
//	用于区分“服务端按角色状态静默丢弃”与“请求长度/PrevAct 形态不被该服接受”。
//
// 运行命令: go run -tags=probe test/probes/probe_faction_diag.go -config configs/s0.yaml
// 观察点: 空载荷 / 4 字节 PrevAct / 8 字节 / 4 字节零 等候选形态下服务端是否有任何回包。
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfgPath := flag.String("config", "configs/s0.yaml", "配置文件路径")
	timeout := flag.Duration("timeout", 3*time.Second, "单个候选载荷等待上限")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
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
		RefreshCredentials: func(c context.Context, sCfg *client.SessionConfig) error {
			ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
			freshTicket, err := ticketCache.GetOrFetch(c, acc.Platform, acc.Username, acc.Password, role.ServerID)
			if err != nil {
				return err
			}
			client.ApplyTicket(sCfg, freshTicket)
			if freshTicket.GatewayURL != "" {
				sCfg.ServerAddr = freshTicket.GatewayURL
			}
			return nil
		},
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)

	// 观察器必须在 Start 之前注册，才能捕获登录握手阶段的全量资产回包，
	// 用于对比两个服角色数据中是否存在帮派相关字段差异。
	observed := make(chan *protocol.Packet, 256)
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		if pkt == nil || pkt.ActionID == protocol.ActionHeartbeat {
			return
		}
		if pkt.ActionID == protocol.ActionIDPlayerGetInfo {
			slog.Info("登录全量资产回包", "len", len(pkt.Payload), "hex", hex.EncodeToString(pkt.Payload))
		}
		select {
		case observed <- pkt:
		default:
		}
	})

	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未在时限内激活", "state", sess.State())
		return
	}
	drain := func() {
		for {
			select {
			case <-observed:
			default:
				return
			}
		}
	}
	waitStray := func(tag string, d time.Duration) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		for {
			select {
			case p := <-observed:
				slog.Info("观测窗口内收到非心跳报文", "tag", tag,
					"action_id", fmt.Sprintf("0x%08X", p.ActionID),
					"len", len(p.Payload), "payload", hex.EncodeToString(p.Payload))
			case <-timer.C:
				return
			}
		}
	}

	slog.Info("=== 会话已激活，开始帮派模块诊断 ===", "role_id", sess.RoleID(), "server_id", role.ServerID)

	lastAct := sess.LastActionID()
	type variant struct {
		name    string
		payload []byte
	}
	variants := []variant{
		{name: "empty", payload: nil},
		{name: "prevact_last", payload: []byte{byte(lastAct >> 24), byte(lastAct >> 16), byte(lastAct >> 8), byte(lastAct)}},
		{name: "prevact_town_enter", payload: []byte{0x00, 0x01, 0x00, 0x00}},
		{name: "prevact_zero", payload: []byte{0, 0, 0, 0}},
		{name: "two_int32_zero", payload: []byte{0, 0, 0, 0, 0, 0, 0, 0}},
	}

	for _, action := range []struct {
		name     string
		act      uint32
		variants []variant
	}{
		{"faction_god_info(10,22)", protocol.ActionIDFactionGodInfo, variants},
		{"open_lucky_wheel(69,1)", protocol.ActionIDLuckyWheelOpen, variants},
		{"lucky_wheel_action0(69,0)", 0x00450000, variants[:1]},
		// 帮派面板类只读动作：仅用空载荷探测，避免向未知写动作发送伪造参数。
		{"faction_open(10,0)", 0x000A0000, variants[:1]},
		{"faction_list(10,2)", 0x000A0002, variants[:1]},
		// 其余已知只读动作：验证帮派模块是否整体不可达。
		{"faction_blessing_count(10,24)", protocol.ActionIDFactionGetBlessingCount, variants[:1]},
		{"faction_rollcake_remain(10,44)", protocol.ActionIDFactionRollCakeRemainTimes, variants[:1]},
		{"faction_rollcake_info(10,45)", protocol.ActionIDFactionRollCakeInfo, variants[:1]},
		// 非法供品编号 99 不会消耗任何资源，仅用于判定上香处理器是否可达。
		{"faction_incense_99(10,23)", protocol.ActionIDFactionIncense,
			[]variant{{name: "int32_99", payload: []byte{0, 0, 0, 99}}}},
		// 合法免费供品 (白檀香) 直接探测角色是否有帮派上下文：
		// 有帮派且今日未上香会成功，无帮派返回成员/帮派缺失类结果码。
		{"faction_incense_1(10,23)", protocol.ActionIDFactionIncense,
			[]variant{{name: "int32_1", payload: []byte{0, 0, 0, 1}}}},
	} {
		for _, v := range action.variants {
			drain()
			pkt := protocol.NewPacket(action.act, v.payload)
			cCtx, cCancel := context.WithTimeout(ctx, *timeout)
			resp, err := sess.Call(cCtx, pkt, action.act)
			cCancel()
			if err != nil {
				slog.Warn("候选载荷无目标回包", "action", action.name, "variant", v.name,
					"payload", hex.EncodeToString(v.payload), "err", err)
				waitStray(action.name+"/"+v.name, 500*time.Millisecond)
				continue
			}
			slog.Info("候选载荷收到回包", "action", action.name, "variant", v.name,
				"payload", hex.EncodeToString(v.payload),
				"len", len(resp.Payload), "resp", hex.EncodeToString(resp.Payload))
			break // 命中的形态即可作为该动作的有效请求
		}
	}

	slog.Info("=== 帮派模块诊断结束 ===")
}
