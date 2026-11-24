//go:build probe

// 目标: 判定服务端「登录后约 225 秒断链」是否由「缺少业务请求」触发，
// 并比较官方抓包中出现的三种空闲保活方式。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_idle_keepalive_live.go
//
// 可选环境变量:
//
//	SXD_PROBE_MODES=none,heart075,time022   要跑的模式（默认全部）
//	SXD_PROBE_WAIT=720s                     每个模式最长观察时间
//
// 背景 (2026-11-24 /tmp/shengyujingjichang.pcapng 与 /tmp/wangushenxu.pcapng):
//   - 8200 主服上行在 23 秒空闲期一个字节都没有（只有 TCP ACK），会话仍然存活；
//   - 服务端每 15 秒主动下发 646/21，辅助连接下发 0/75（1 字节 01）；
//   - 官方客户端从不回 0/75，而是周期性发 0/22 get_server_time（王古抓包间隔 30.57s）；
//   - 我们的 RoleSession 却把 0/75 当心跳上行，且只在空闲 15 秒时发送。
//
// 四种模式:
//
//	none     只登录，之后不发任何应用层包（内置心跳关闭）
//	heart075 空闲时每 15 秒上行 0/75（当前线上行为）
//	time022  空闲时每 30 秒发 0/22 get_server_time（官方客户端行为，纯只读）
//	town     空闲时每 60 秒进一次城镇（场景操作，零消耗）
//
// 2026-11-24 首轮结果：none / heart075 / time022 全部在 4m33s~4m35s 被服务端
// 断链，说明查询类请求与 0/75 都不刷新会话；而生产日志中的长会话都穿插了
// 战斗/场景切换（跨服PK、无量劫海、取经打劫、副本扫荡），故追加 town 模式
// 验证「场景操作刷新会话」。
//
// 只读探测：不进入副本、不领奖、不消耗元宝与次数。运行前请确认账号没有在玩，
// 否则会把外部登录顶掉。
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

	modes := strings.Split(envOr("SXD_PROBE_MODES", "none,heart075,time022"), ",")
	wait, err := time.ParseDuration(envOr("SXD_PROBE_WAIT", "12m"))
	if err != nil {
		slog.Error("SXD_PROBE_WAIT 解析失败", "err", err)
		return
	}

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	roleID := fmt.Sprintf("%s-%s", role.ServerID, role.RoleName)
	results := make(map[string]time.Duration)
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		if mode == "" {
			continue
		}
		alive, ok := runMode(ticketCache, acc, role, roleID, mode, wait)
		if ok {
			results[mode] = alive
			slog.Info("模式结果", "mode", mode, "alive", alive.Round(time.Second))
		}
		// 网关释放旧会话需要时间；留出窗口再登录下一个模式。
		time.Sleep(20 * time.Second)
	}

	slog.Info("===== 汇总 =====")
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		if d, ok := results[mode]; ok {
			slog.Info("保活模式存活时长", "mode", mode, "alive", d.Round(time.Second),
				"survived_12m", d >= wait)
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func int32Bytes(v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return b[:]
}

// runMode 登录一次并在 wait 内维持指定保活策略，返回从登录成功到断链的时长。
func runMode(tc *platform.TicketCache, acc config.AccountConfig, role config.RoleConfig,
	roleID, mode string, wait time.Duration) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), wait+2*time.Minute)
	defer cancel()

	tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "mode", mode, "err", err)
		return 0, false
	}

	roleCfg := client.SessionConfig{
		RoleID:               roleID,
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 1,
		// 关闭内置 0/75 心跳：本探针自己控制上行，避免干扰对照组。
		HeartbeatInterval: 24 * time.Hour,
		HeartbeatTimeout:  24 * time.Hour,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "mode", mode, "err", err)
		return 0, false
	}
	defer sess.Close()

	deadline := time.Now().Add(30 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "mode", mode, "state", sess.State())
		return 0, false
	}

	start := time.Now()
	slog.Info("会话已激活，开始观察", "mode", mode, "wait", wait)

	next := start
	for time.Since(start) < wait {
		time.Sleep(500 * time.Millisecond)
		if sess.State() != client.StateActive {
			alive := time.Since(start)
			slog.Info("检测到断链", "mode", mode, "alive", alive.Round(time.Second),
				"state", sess.State())
			return alive, true
		}
		if time.Now().Before(next) {
			continue
		}
		switch mode {
		case "none":
			next = time.Now().Add(time.Hour) // 不再主动发包
		case "heart075":
			// 官方客户端用的是服务端下发；这里复刻当前线上行为做对照。
			if err := sess.Send(protocol.NewPacket(protocol.ActionHeartbeat, nil)); err != nil {
				slog.Warn("发送 0/75 失败", "mode", mode, "err", err)
			}
			next = time.Now().Add(15 * time.Second)
		case "time022":
			// 官方客户端 0/22：4 字节参数 + 自动业务尾链。
			callCtx, ccancel := context.WithTimeout(ctx, 5*time.Second)
			resp, err := sess.Call(callCtx,
				protocol.NewPacketWithAutoPrevAct(protocol.ActionIDPlayerServerTime, int32Bytes(0)),
				protocol.ActionIDPlayerServerTime)
			ccancel()
			if err != nil {
				slog.Warn("0/22 无回包", "mode", mode, "err", err)
			} else if resp != nil {
				payload := resp.Payload
				if len(payload) >= 4 {
					slog.Info("0/22 回包", "mode", mode,
						"server_time", binary.BigEndian.Uint32(payload[:4]))
				} else {
					slog.Info("0/22 回包", "mode", mode, "payload_len", len(payload))
				}
			}
			next = time.Now().Add(30 * time.Second)
		case "town":
			// 场景操作：进入当前城镇（Mod_Town_Base.enter_town，不消耗任何资源）。
			// 直接调用底层方法记录结果码，区分“被静默丢弃”与“已在城镇”两种情况。
			townID := int32(sess.GetPlayerState().TownMapID)
			if townID <= 0 {
				townID = 3
			}
			anchor := sess.LastAckActionID()
			if anchor == 0 {
				anchor = sess.LastBusinessActionID()
			}
			callCtx, ccancel := context.WithTimeout(ctx, 8*time.Second)
			code, err := sess.EnterTownWithAnchor(callCtx, townID, anchor)
			ccancel()
			slog.Info("enter_town 结果", "mode", mode, "town_id", townID,
				"anchor", fmt.Sprintf("0x%08X", anchor), "code", code, "err", err)
			next = time.Now().Add(60 * time.Second)
		default:
			slog.Warn("未知模式", "mode", mode)
			next = time.Now().Add(time.Hour)
		}
	}
	slog.Info("观察期结束仍在线", "mode", mode, "alive", time.Since(start).Round(time.Second))
	return time.Since(start), true
}