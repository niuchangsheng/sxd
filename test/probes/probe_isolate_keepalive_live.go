//go:build probe

// 目标协议:
//   - 对照组 pure06: 仅主服 0/6 (每 60s) + 被动 0/75->0/22，不建 8219、不重换票，验证是否被 9x050 5 分钟扫描踢线
//   - 实验组 shared8219: 对齐原版 Main.swf (View.as:16639 chatConnect + ServerTownMapView.as:614 leave_town)，
//     常驻单条 8219 连接、跨服业务复用同一 8219 连接 (95/0 enter_town -> 114/2 -> 95/2 leave_town 不关连接)，
//     验证: (1) 主服 ReturnToTown/EnterTown 正常 (code=0)；(2) 跨越 2 个 5 分钟扫描点 (10m30s) 零断线。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_isolate_keepalive_live.go
//
// 预期表现: 输出 pure06 与 shared8219 在 5 分钟扫描点 (:X0:33 / :X5:33) 的精确存活对照结果
// 安全性: 纯只读与场景进出，零资源消耗。
package main

import (
	"context"
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
	actStTownEnter uint32 = 0x005F0000
	actStTownLeave uint32 = 0x005F0002
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
		os.Exit(1)
	}
	acc := cfg.Accounts[0]

	tc := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		os.Exit(1)
	}
	if acc.Cookie != "" {
		xdAuth.SetCookie(acc.Cookie)
	}
	tc.RegisterAuthenticator(xdAuth)

	mode := os.Getenv("SXD_ISOLATE_MODE")
	if mode == "" || mode == "pure06" {
		slog.Info("========== 阶段 1: 运行 pure06 对照组 (无重换票、无 8219，仅每 60s 发 0/6，观察 7m30s) ==========")
		survived, dur := runArm(tc, acc, false, 450*time.Second)
		slog.Info("========== 阶段 1 结果 ==========", "mode", "pure06", "survived", survived, "alive", dur.Round(time.Second))
		if mode == "pure06" {
			return
		}
		slog.Info("等待 22 秒释放网关会话后启动阶段 2 (shared8219)...")
		time.Sleep(22 * time.Second)
	}

	slog.Info("========== 阶段 2: 运行 shared8219 实验组 (常驻单条 8219 + 复用进出仙界 95/0->95/2，观察 10m30s) ==========")
	survived2, dur2 := runArm(tc, acc, true, 630*time.Second)
	slog.Info("========== 阶段 2 结果 ==========", "mode", "shared8219", "survived", survived2, "alive", dur2.Round(time.Second))
}

func runArm(tc *platform.TicketCache, acc config.AccountConfig, withShared8219 bool, maxWait time.Duration) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), maxWait+2*time.Minute)
	defer cancel()

	var sessions []*client.RoleSession
	defer func() {
		for _, s := range sessions {
			s.Close()
		}
	}()

	for i, role := range acc.Roles {
		if i > 0 {
			time.Sleep(3 * time.Second)
		}
		tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
		tk, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
		if err != nil {
			slog.Error("换票失败", "server", role.ServerID, "err", err)
			return false, 0
		}
		roleID := fmt.Sprintf("%s-%s", role.ServerID, role.RoleName)
		pName, uName, pWord, sID := acc.Platform, acc.Username, acc.Password, role.ServerID
		sCfg := client.SessionConfig{
			RoleID:               roleID,
			RoleName:             role.RoleName,
			Username:             acc.Username,
			ServerAddr:           tk.GatewayURL,
			ServerID:             role.ServerID,
			Platform:             acc.Platform,
			Authenticator:        client.DefaultAuthenticator,
			MaxReconnectAttempts: 3,
			HeartbeatInterval:    24 * time.Hour,
			HeartbeatTimeout:     24 * time.Hour,
			RefreshCredentials: func(c context.Context, cfg *client.SessionConfig) error {
				tc.Invalidate(pName, uName, sID)
				ft, rErr := tc.GetOrFetch(c, pName, uName, pWord, sID)
				if rErr != nil {
					return rErr
				}
				client.ApplyTicket(cfg, ft)
				return nil
			},
		}
		client.ApplyTicket(&sCfg, tk)
		sess := client.NewRoleSession(sCfg)
		sess.SetPacketObserver(func(pkt *protocol.Packet) {
			if pkt != nil && pkt.ActionID == protocol.ActionHeartbeat {
				go func() {
					_ = sess.Send(protocol.NewPacketWithAutoPrevAct(protocol.ActionIDPlayerServerTime, nil))
				}()
			}
		})
		if err := sess.Start(ctx); err != nil {
			slog.Error("启动失败", "err", err)
			return false, 0
		}
		sessions = append(sessions, sess)
	}

	deadline := time.Now().Add(95 * time.Second)
	for _, s := range sessions {
		for s.State() != client.StateActive && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if s.State() != client.StateActive {
			slog.Error("激活超时", "role_id", s.RoleID(), "state", s.State())
			return false, 0
		}
		_ = s.KeepaliveBusinessRead(ctx)
	}

	initGens := make([]uint64, len(sessions))
	for i, s := range sessions {
		initGens[i] = s.ConnectionGeneration()
	}

	s0 := sessions[0]
	var sharedCross *client.RoleSession
	if withShared8219 {
		for _, s := range sessions {
			if err := s.EnsureCrossSessionAlive(ctx); err != nil {
				slog.Warn("EnsureCrossSessionAlive 失败", "role_id", s.RoleID(), "err", err)
			} else {
				slog.Info("常驻跨服辅助会话 (login-only) 已建立", "role_id", s.RoleID(), "has_cross", s.HasCrossSession())
			}
		}

		// 测试主城有效 town_id (对比 153 vs 3/2/1)
		for _, tid := range []int32{int32(s0.GetPlayerState().TownMapID), 3, 2, 1} {
			if tid <= 0 {
				continue
			}
			eCtx, eCancel := context.WithTimeout(ctx, 5*time.Second)
			code, eErr := s0.EnterTown(eCtx, tid)
			eCancel()
			slog.Info("主服探测 EnterTown", "town_id", tid, "code", code, "err", eErr)
			if eErr == nil && code == 0 {
				break
			}
		}
	}
	_ = sharedCross

	start := time.Now()
	last06 := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for time.Since(start) < maxWait {
		<-ticker.C
		elapsed := time.Since(start)
		for i, s := range sessions {
			if s.State() != client.StateActive || s.ConnectionGeneration() != initGens[i] {
				slog.Warn("检测到断线!",
					"role_id", s.RoleID(),
					"elapsed", elapsed.Round(time.Second),
					"wall", time.Now().Format("15:04:05.000"),
					"has_cross", s.HasCrossSession(),
				)
				return false, elapsed
			}
		}
		if time.Since(last06) >= 60*time.Second {
			last06 = time.Now()
			for _, s := range sessions {
				err := s.KeepaliveBusinessRead(ctx)
				slog.Info("60s 保活 0/6",
					"role_id", s.RoleID(),
					"elapsed", elapsed.Round(time.Second),
					"wall", time.Now().Format("15:04:05"),
					"ok", err == nil,
					"has_cross", s.HasCrossSession(),
				)
			}
		}
	}
	return true, time.Since(start)
}
