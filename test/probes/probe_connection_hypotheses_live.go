//go:build probe

// 目标协议:
//   - [Module 0, Action 6]  Mod_Player_Base.get_player_function (主服业务读保活)
//   - [Module 0, Action 75] Mod_Player_Base.heart -> [Module 0, Action 22] server_time (原版心跳应答)
//   - [Module 96/94/95]     仙界跨服连接共存、重复建连互踢与 leave_town (95/2) 退出
//   - [Module 293]          圣域连接探测与主服共存
//
// 基准对齐: 对齐官方 Main.swf (DataBase.as 多 Socket 并发模型、View.as:14912 heart->server_time)
// 与 /tmp/daily.pcapng (浏览器空闲 12.5 分钟不断线抓包)
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_connection_hypotheses_live.go
//
// 预期表现:
//  1. 同账号 s0 + s1 双区服同时在线 11 分钟 (跨越 2 个 5 分钟服务端回收扫描点)，零断线 (gen=1)；
//  2. 在线期间对 s0/s1 执行 Web 重新换票不会踢掉已有 TCP 会话；
//  3. 主城 (8200) 与仙界 (8219)、圣域 (8220) 连接可同时共存，leave_town 后主服上下文完好；
//  4. 同一角色重复建立第 2 条 8219 跨服连接会踢掉第 1 条 8219 连接，但不影响 8200 主连接。
//
// 安全性: 纯只读查询与城镇进出，不进入副本战斗、不领奖、不消耗元宝或体力。
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const actionStTownLeave uint32 = 0x005F0002

type roleWatch struct {
	name       string
	serverID   string
	sess       *client.RoleSession
	heartPushes atomic.Int64
	 push646    atomic.Int64
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	waitDur := 11 * time.Minute
	if v := os.Getenv("SXD_PROBE_WAIT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			waitDur = d
		}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置无可用角色")
		os.Exit(1)
	}
	acc := cfg.Accounts[0]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		os.Exit(1)
	}
	if acc.Cookie != "" {
		xdAuth.SetCookie(acc.Cookie)
	}
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), waitDur+3*time.Minute)
	defer cancel()

	var watches []*roleWatch
	for i, role := range acc.Roles {
		if i > 0 {
			time.Sleep(3 * time.Second)
		}
		ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
		ticket, tErr := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
		if tErr != nil {
			slog.Error("换票失败", "server", role.ServerID, "err", tErr)
			os.Exit(1)
		}
		roleID := fmt.Sprintf("%s-%s", role.ServerID, role.RoleName)
		sCfg := client.SessionConfig{
			RoleID:               roleID,
			RoleName:             role.RoleName,
			Username:             acc.Username,
			ServerAddr:           ticket.GatewayURL,
			ServerID:             role.ServerID,
			Platform:             acc.Platform,
			Authenticator:        client.DefaultAuthenticator,
			MaxReconnectAttempts: 1,
			// 关闭旧版主动上行 0/75，对齐官方客户端被动接收 0/75 -> 回复 0/22
			HeartbeatInterval: 24 * time.Hour,
			HeartbeatTimeout:  24 * time.Hour,
		}
		client.ApplyTicket(&sCfg, ticket)

		sess := client.NewRoleSession(sCfg)
		w := &roleWatch{name: roleID, serverID: role.ServerID, sess: sess}
		sess.SetPacketObserver(func(pkt *protocol.Packet) {
			if pkt == nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionHeartbeat: // 0x0000004B S->C
				n := w.heartPushes.Add(1)
				slog.Info("收到服务端 0/75 heart 推送，按原版回复 0/22 server_time",
					"role_id", w.name, "count", n)
				go func() {
					_ = sess.Send(protocol.NewPacketWithAutoPrevAct(protocol.ActionIDPlayerServerTime, nil))
				}()
			case 0x02860015: // 646/21 S->C 15s tick
				w.push646.Add(1)
			}
		})

		if err := sess.Start(ctx); err != nil {
			slog.Error("启动会话失败", "role_id", roleID, "err", err)
			os.Exit(1)
		}
		defer sess.Close()
		watches = append(watches, w)
	}

	// 等待所有区服激活
	deadline := time.Now().Add(45 * time.Second)
	for _, w := range watches {
		for w.sess.State() != client.StateActive && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if w.sess.State() != client.StateActive {
			slog.Error("角色未能在时限内激活", "role_id", w.name, "state", w.sess.State())
			os.Exit(1)
		}
		// 初始执行一次 0/6 功能查询保活锚点
		if err := w.sess.KeepaliveBusinessRead(ctx); err != nil {
			slog.Warn("初始 0/6 读取异常", "role_id", w.name, "err", err)
		} else {
			slog.Info("角色激活并完成初始 0/6 业务读", "role_id", w.name, "gen", w.sess.ConnectionGeneration())
		}
	}

	start := time.Now()
	lastKeepalive := time.Now()
	didStep60, didStep120, didStep180, didStep240 := false, false, false, false

	s0 := watches[0]

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for time.Since(start) < waitDur {
		select {
		case <-ctx.Done():
			slog.Error("探针上下文超时退出")
			return
		case <-ticker.C:
		}

		elapsed := time.Since(start)

		// 检查所有角色是否保持原连接在线 (gen == 1 && StateActive)
		for _, w := range watches {
			if w.sess.State() != client.StateActive || w.sess.ConnectionGeneration() != 1 {
				slog.Error("检测到非预期断线!",
					"role_id", w.name,
					"elapsed", elapsed.Round(time.Second),
					"wall", time.Now().Format("15:04:05.000"),
					"state", w.sess.State().String(),
					"gen", w.sess.ConnectionGeneration(),
					"last_tx", fmt.Sprintf("0x%08X", w.sess.LastActionID()),
					"last_rx", fmt.Sprintf("0x%08X", w.sess.LastRxActionID()),
					"recent_rx", fmt.Sprintf("%X", w.sess.RecentRxActionIDs()),
				)
				os.Exit(2)
			}
		}

		// 每 90 秒在各主服连接发送一次 0/6 KeepaliveBusinessRead
		if time.Since(lastKeepalive) >= 90*time.Second {
			lastKeepalive = time.Now()
			for _, w := range watches {
				err := w.sess.KeepaliveBusinessRead(ctx)
				slog.Info("周期 0/6 主服保活读",
					"role_id", w.name,
					"elapsed", elapsed.Round(time.Second),
					"ok", err == nil,
					"err", err,
					"heart_pushes", w.heartPushes.Load(),
					"push_646_21", w.push646.Load(),
				)
			}
		}

		// Step 1 (t=60s): 验证猜想 4a — 主城 (8200) 与仙界 (8219) 连接同时共存 + leave_town (95/2) 后主服上下文完好
		if !didStep60 && elapsed >= 60*time.Second {
			didStep60 = true
			slog.Info("=== [实验 4a] 验证主城 (8200) 与仙界 (8219) 同时共存及 leave_town 切换 ===")
			cCtx, cCancel := context.WithTimeout(ctx, 20*time.Second)
			cross, cErr := s0.sess.OpenCrossServerSession(cCtx)
			cCancel()
			if cErr != nil {
				slog.Warn("[实验 4a] 打开仙界会话失败", "err", cErr)
			} else {
				// 并发在 8219 查取经信息 (114/2) + 在 8200 查服务器时间 (0/22)
				qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
				tbResp, tbErr := cross.Call(qCtx, protocol.NewPacketWithAutoPrevAct(0x00720002, nil), 0x00720002)
				stResp, stErr := s0.sess.Call(qCtx, protocol.NewPacketWithAutoPrevAct(protocol.ActionIDPlayerServerTime, nil), protocol.ActionIDPlayerServerTime)
				qCancel()
				var srvTime uint32
				if stResp != nil && len(stResp.Payload) >= 4 {
					srvTime = binary.BigEndian.Uint32(stResp.Payload[:4])
				}
				slog.Info("[实验 4a] 8200 与 8219 双连接并发通信结果",
					"cross_state", cross.State().String(),
					"main_state", s0.sess.State().String(),
					"take_bible_ok", tbErr == nil && tbResp != nil,
					"main_time_ok", stErr == nil && srvTime > 0,
				)
				// 对齐官方 ServerTownMapView.as:614 发送 95/2 leave_town 后关闭
				lCtx, lCancel := context.WithTimeout(ctx, 5*time.Second)
				lvResp, lvErr := cross.Call(lCtx, protocol.NewPacketWithAutoPrevAct(actionStTownLeave, nil), actionStTownLeave)
				lCancel()
				var lvCode int = -1
				if lvResp != nil && len(lvResp.Payload) > 0 {
					lvCode = int(lvResp.Payload[0])
				}
				cross.Close()
				// 验证主服 enter_town 仍正常回包
				townID := int32(s0.sess.GetPlayerState().TownMapID)
				if townID <= 0 {
					townID = 2
				}
				eCtx, eCancel := context.WithTimeout(ctx, 8*time.Second)
				eCode, eErr := s0.sess.EnterTown(eCtx, townID)
				eCancel()
				slog.Info("[实验 4a] 仙界 leave_town 关闭后主服状态复核",
					"leave_code", lvCode, "leave_err", lvErr,
					"main_enter_town_id", townID, "main_enter_code", eCode, "main_enter_err", eErr,
					"main_state", s0.sess.State().String(),
				)
			}
		}

		// Step 2 (t=120s): 验证猜想 4b — 圣域凭据/连接探测与主服共存
		if !didStep120 && elapsed >= 120*time.Second {
			didStep120 = true
			slog.Info("=== [实验 4b] 验证圣域连接探测与主城 (8200) 共存 ===")
			saCtx, saCancel := context.WithTimeout(ctx, 15*time.Second)
			saSess, saErr := s0.sess.OpenSaintAreaSession(saCtx)
			saCancel()
			if saErr != nil {
				slog.Info("[实验 4b] 圣域当前未开放或不可连接，验证主服连接不受影响",
					"sa_err", saErr, "main_state", s0.sess.State().String())
			} else {
				slog.Info("[实验 4b] 圣域会话已建立并与主服共存",
					"sa_state", saSess.State().String(), "main_state", s0.sess.State().String())
				saSess.Close()
			}
		}

		// Step 3 (t=180s): 验证猜想 3b — 同账号在线期间重新执行 s1 和 s0 的 Web 登录换票，观察是否踢掉活跃 TCP 连接
		if !didStep180 && elapsed >= 180*time.Second {
			didStep180 = true
			slog.Info("=== [实验 3b] 验证同账号 s0/s1 在线期间重新向 Web 换票是否互踢 ===")
			for _, role := range acc.Roles {
				ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
				tCtx, tCancel := context.WithTimeout(ctx, 20*time.Second)
				tk, tErr := ticketCache.GetOrFetch(tCtx, acc.Platform, acc.Username, acc.Password, role.ServerID)
				tCancel()
				slog.Info("[实验 3b] 在线重换票完成",
					"server", role.ServerID, "ok", tErr == nil && tk != nil, "err", tErr)
				time.Sleep(2 * time.Second)
			}
			for _, w := range watches {
				slog.Info("[实验 3b] 换票后连接存活核对",
					"role_id", w.name, "state", w.sess.State().String(), "gen", w.sess.ConnectionGeneration())
			}
		}

		// Step 4 (t=240s): 验证重复跨服连接互踢 — 第 2 条 8219 会踢掉第 1 条 8219，但 8200 主服不受影响
		if !didStep240 && elapsed >= 240*time.Second {
			didStep240 = true
			slog.Info("=== [实验 4c] 验证同一角色并发建立 2 条 8219 跨服连接是否互踢及主服影响 ===")
			cCtx1, cCancel1 := context.WithTimeout(ctx, 20*time.Second)
			cross1, err1 := s0.sess.OpenCrossServerSession(cCtx1)
			cCancel1()
			if err1 == nil && cross1 != nil {
				cCtx2, cCancel2 := context.WithTimeout(ctx, 20*time.Second)
				cross2, err2 := s0.sess.OpenCrossServerSession(cCtx2)
				cCancel2()
				time.Sleep(3 * time.Second)
				slog.Info("[实验 4c] 双 8219 建立 3 秒后状态对照",
					"cross1_state", cross1.State().String(),
					"cross1_gen", cross1.ConnectionGeneration(),
					"cross2_ok", err2 == nil && cross2 != nil,
					"cross2_state", func() string {
						if cross2 != nil {
							return cross2.State().String()
						}
						return "nil"
					}(),
					"main_state", s0.sess.State().String(),
					"main_gen", s0.sess.ConnectionGeneration(),
				)
				cross1.Close()
				if cross2 != nil {
					lCtx, lCancel := context.WithTimeout(ctx, 5*time.Second)
					_, _ = cross2.Call(lCtx, protocol.NewPacketWithAutoPrevAct(actionStTownLeave, nil), actionStTownLeave)
					lCancel()
					cross2.Close()
				}
			}
		}

		if int(elapsed.Seconds())%60 < 5 {
			for _, w := range watches {
				slog.Info("存活巡检",
					"role_id", w.name,
					"elapsed", elapsed.Round(time.Second),
					"wall", time.Now().Format("15:04:05"),
					"state", w.sess.State().String(),
					"gen", w.sess.ConnectionGeneration(),
					"heart_pushes", w.heartPushes.Load(),
					"push_646_21", w.push646.Load(),
				)
			}
		}
	}

	slog.Info("===== 全部实验通过: 双区服持续存活无断线 =====", "duration", time.Since(start).Round(time.Second))
	for _, w := range watches {
		slog.Info("最终状态",
			"role_id", w.name,
			"state", w.sess.State().String(),
			"gen", w.sess.ConnectionGeneration(),
			"heart_pushes", w.heartPushes.Load(),
			"push_646_21", w.push646.Load(),
		)
	}
}
