//go:build probe

// 目标协议: [Module 0, Action 23] 心跳保活与 [Module 4, Action 0] get_sections 的 PrevAct 链冲突
// 基准对齐: 原版 pie.exe 心跳黄金包 c2s_m0_a23.bin (payload=00000030) 与 mod 4 原子链语义
// 运行命令: SXD_CONFIG=configs/config.yaml go run -tags=probe test/probes/probe_heartbeat_chain_live.go <mode>
//
//	mode:
//	  idle            控制组: 空闲 2s (心跳已禁用, 不发包) 后发 get_sections
//	  idle-auto       空闲 2s 且保留 1.5s 自动心跳, 观察自动心跳是否同样污染链
//	  hb-golden       心跳载荷取官方黄金包 0x00000030 后发 get_sections
//	  hb-prevact      心跳载荷取客户端 lastActionID (现有实现) 后发 get_sections
//	  hb-prevact-sync 现有实现 + 心跳后把 lastActionID 置为 0x00000017 再发 get_sections
//	  service-sequence 复刻线上时序: 登录后空闲 4s 触发自动心跳 -> SyncQuests -> get_sections
//
// 预期表现:
//   - 修复前 (心跳不推进 lastActionID): hb-* 模式下 get_sections 6s 超时, idle 模式正常;
//   - 修复后: 心跳发出后 lastActionID 自动推进为 0x00000017, 所有模式 get_sections 均 ~150ms 成功。
//
// 安全性: 仅只读查询, 不进入副本、不消耗体力。
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

const probeMapID int32 = 13

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	mode := "idle"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
		MaxReconnectAttempts: 2,
	}
	if mode == "idle-auto" || mode == "service-sequence" {
		// 保留默认 1.5s 自动心跳, 用于验证调度器自身心跳是否同样污染 Mod 4 链。
		roleCfg.HeartbeatInterval = 1500 * time.Millisecond
	} else {
		// 其余模式禁用自动心跳, 由实验显式发送, 避免自动心跳污染对照。
		roleCfg.HeartbeatInterval = time.Hour
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	var inbound []string
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		inbound = append(inbound, fmt.Sprintf("0x%08X", pkt.ActionID))
	})
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	callSections := func(label string) bool {
		inbound = inbound[:0]
		pkt := protocol.BuildMissionGetSectionsPacket(probeMapID, sess.LastActionID())
		start := time.Now()
		callCtx, callCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, callErr := sess.Call(callCtx, pkt, protocol.ActionIDMissionGetSections)
		callCancel()
		fmt.Printf("[%s] payload=%s elapsed=%s -> ", label, hex.EncodeToString(pkt.Payload), time.Since(start).Round(time.Millisecond))
		if callErr != nil || resp == nil {
			fmt.Printf("FAILED (%v)\n", callErr)
			return false
		}
		fmt.Printf("OK (%dB) inbound_before=%v\n", len(resp.Payload), inbound)
		return true
	}

	// 控制组: 先确认基线链路可用。
	if !callSections("control-before-heartbeat") {
		fmt.Println("基线 get_sections 即失败, 本轮实验放弃")
		return
	}

	switch mode {
	case "idle":
		fmt.Println("空闲 2s (无心跳), 观察 Mod 4 链是否可用...")
		time.Sleep(2 * time.Second)
	case "idle-auto":
		fmt.Println("空闲 2s (保留 1.5s 自动心跳), 观察自动心跳是否影响 Mod 4 链...")
		time.Sleep(2 * time.Second)
	case "service-sequence":
		fmt.Println("复刻线上时序: 空闲 4s (自动心跳 1.5s) -> SyncQuests -> get_sections ...")
		time.Sleep(4 * time.Second)
		fmt.Printf("空闲后 client_last_action=0x%08X\n", sess.LastActionID())
		if _, qErr := sess.SyncQuests(ctx); qErr != nil {
			fmt.Printf("SyncQuests FAILED: %v\n", qErr)
			return
		}
		fmt.Printf("SyncQuests OK, last_action=0x%08X\n", sess.LastActionID())
		callSections("after-service-sequence")
		return
	case "hb-golden", "hb-prevact", "hb-prevact-sync", "hb-golden-sync":
		time.Sleep(1600 * time.Millisecond) // 等自动心跳窗口过去, 确保手动心跳单独生效
		var hbPayload []byte
		if mode == "hb-golden" || mode == "hb-golden-sync" {
			hbPayload = []byte{0x00, 0x00, 0x00, 0x30}
		} else {
			prev := sess.LastActionID()
			hbPayload = []byte{byte(prev >> 24), byte(prev >> 16), byte(prev >> 8), byte(prev)}
		}
		inbound = inbound[:0]
		fmt.Printf("发送心跳 mode=%s payload=%s ...\n", mode, hex.EncodeToString(hbPayload))
		if err := sess.Send(protocol.NewPacket(protocol.ActionHeartbeat, hbPayload)); err != nil {
			fmt.Printf("心跳发送失败: %v\n", err)
			return
		}
		time.Sleep(400 * time.Millisecond)
		fmt.Printf("心跳后 inbound=%v client_last_action=0x%08X\n", inbound, sess.LastActionID())
		if mode == "hb-prevact-sync" || mode == "hb-golden-sync" {
			sess.SetLastActionID(protocol.ActionHeartbeat)
			fmt.Printf("已将客户端 lastActionID 同步为心跳 0x%08X\n", protocol.ActionHeartbeat)
		}
	default:
		fmt.Printf("未知 mode: %s\n", mode)
		return
	}

	callSections("after-" + mode)
	fmt.Printf("最终 session_state=%s\n", sess.State())
}
