//go:build probe

// 目标协议: [Module 89 / 0x59] Mod_SignIn_Base 每日签到 (官方 Main.swf 反编译基准)
//
//	get_sign_in_status   Act 5 -> 0x00590005  空请求, 回包 Byte×3 (活动开启/有奖/状态)
//	get_sign_in_info     Act 0 -> 0x00590000  空请求
//	player_sign_in       Act 2 -> 0x00590002  空请求
//	get_continue_award   Act 1 -> 0x00590001  空请求
//	get_total_award      Act 4 -> 0x00590004  UByte 档位 (5/15/25/35/45 天)
//
// 基准对齐: com.protocols.Mod_SignIn_Base 与 com.datas.SignInData
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_daily_signin_live.go [-execute]
//
// 预期表现: 默认只读打印签到活动状态与签到信息; 显式 -execute 时依次签到、
// 领取连签奖励与累计签到档位奖励，并打印每一步的原始回包与解析结果。
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

const (
	actionSignInStatus     uint32 = 0x00590005
	actionSignInInfo       uint32 = 0x00590000
	actionSignInPlayer     uint32 = 0x00590002
	actionSignInContinue   uint32 = 0x00590001
	actionSignInTotalAward uint32 = 0x00590004
)

func main() {
	execute := flag.Bool("execute", false, "实际签到/领奖 (会改变账号状态)")
	flag.Parse()
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置文件缺少账号或角色")
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

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	before := sess.GetPlayerState()
	slog.Info("会话已激活",
		"role_id", sess.RoleID(),
		"player_id", sess.PlayerID(),
		"level", before.Level,
		"ingots", before.Ingots,
	)

	call := func(payload []byte, act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("调用失败: "+name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		slog.Info("回包: "+name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		return resp
	}

	// 1. 签到活动状态 (只读)
	if resp := call(nil, actionSignInStatus, "get_sign_in_status"); resp != nil {
		r := protocol.NewReader(resp.Payload)
		a, _ := r.ReadInt8()
		b, _ := r.ReadInt8()
		c, _ := r.ReadInt8()
		slog.Info("签到活动状态", "activity_open", a == 1 || b == 1, "has_award", b == 1, "flag", c)
	}

	// 2. 签到详情 (只读)
	var info *protocol.Packet
	info = call(nil, actionSignInInfo, "get_sign_in_info")
	if info != nil {
		r := protocol.NewReader(info.Payload)
		continueState, _ := r.ReadUint8()
		continueDay, _ := r.ReadInt32()
		totalSignIn := readIntList(r)
		totalInfo := readU8PairList(r)
		totalDay, _ := r.ReadInt32()
		signState, _ := r.ReadUint8()
		repairSign, _ := r.ReadInt32()
		curDay, _ := r.ReadInt32()
		slog.Info("签到详情",
			"continue_state", continueState,
			"continue_day", continueDay,
			"total_sign_in", totalSignIn,
			"total_info", totalInfo,
			"total_day", totalDay,
			"sign_state", signState,
			"repair_sign", repairSign,
			"cur_day", curDay,
			"remaining", r.Remaining(),
		)
	}

	if !*execute {
		slog.Info("只读模式结束；如需实际签到/领奖请追加 -execute")
		return
	}

	// 3. 签到
	if resp := call(nil, actionSignInPlayer, "player_sign_in"); resp != nil {
		r := protocol.NewReader(resp.Payload)
		result, _ := r.ReadUint8()
		totalSignIn := readIntList(r)
		totalInfo := readU8PairList(r)
		totalDay, _ := r.ReadInt32()
		slog.Info("签到结果",
			"result", result,
			"total_sign_in", totalSignIn,
			"total_info", totalInfo,
			"total_day", totalDay,
			"remaining", r.Remaining(),
		)
	}

	// 4. 连签奖励
	if resp := call(nil, actionSignInContinue, "get_continue_award"); resp != nil {
		r := protocol.NewReader(resp.Payload)
		result, _ := r.ReadUint8()
		day, _ := r.ReadInt32()
		slog.Info("连签奖励结果", "result", result, "continue_day", day, "remaining", r.Remaining())
	}

	// 5. 累计签到档位奖励 (5/15/25/35/45 天)
	for _, tier := range []uint8{2, 3, 4, 5, 6} {
		payload := []byte{tier}
		if resp := call(payload, actionSignInTotalAward, fmt.Sprintf("get_total_award(tier=%d)", tier)); resp != nil {
			if len(resp.Payload) >= 1 {
				slog.Info("累计签到奖励结果", "tier", tier, "result", resp.Payload[0], "payload_len", len(resp.Payload))
			}
		}
	}

	after := sess.GetPlayerState()
	slog.Info("资产变化",
		"ingots_before", before.Ingots,
		"ingots_after", after.Ingots,
		"delta", after.Ingots-before.Ingots,
	)
}

// readIntList 读取 [Int] 形式列表：Uint16 count + count × Int32。
func readIntList(r *protocol.Reader) []int32 {
	count, err := r.ReadUint16()
	if err != nil {
		return nil
	}
	out := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return out
		}
		out = append(out, v)
	}
	return out
}

// readU8PairList 读取 [UByte,UByte] 形式列表：Uint16 count + count × (UByte,UByte)。
func readU8PairList(r *protocol.Reader) [][2]uint8 {
	count, err := r.ReadUint16()
	if err != nil {
		return nil
	}
	out := make([][2]uint8, 0, count)
	for i := 0; i < int(count); i++ {
		a, err := r.ReadUint8()
		if err != nil {
			return out
		}
		b, err := r.ReadUint8()
		if err != nil {
			return out
		}
		out = append(out, [2]uint8{a, b})
	}
	return out
}
