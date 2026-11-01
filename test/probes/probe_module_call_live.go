//go:build probe

// 目标协议: 任意单发 ActionID 探测 (用于模块协议逆向时隔离单次请求, 避免 PrevAct 链被失败请求污染)
// 基准对齐: 原版 pie.exe 网关 PrevAct 原子链 (失败请求会推进 LastActionID 并导致后续请求级联静默丢弃)
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_module_call_live.go <actionHex> [payloadHex|--prevact] [respActionHex] [--append-prevact]
// 预期表现: 每个进程只发送一次请求并打印原始回包 Hex; 超时即说明该 ActionID/载荷组合不被网关接受。
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	if len(os.Args) < 2 {
		fmt.Println("usage: probe_module_call_live <actionHex> [payloadHex|--prevact] [respActionHex] [--append-prevact]")
		return
	}
	actionID, err := parseHexUint32(os.Args[1])
	if err != nil {
		slog.Error("解析 ActionID 失败", "err", err)
		return
	}

	appendPrevAct := false
	args := make([]string, 0, 3)
	for _, a := range os.Args[2:] {
		if strings.EqualFold(a, "--append-prevact") {
			appendPrevAct = true
			continue
		}
		args = append(args, a)
	}
	respAction := actionID
	if len(args) > 1 {
		if v, pErr := parseHexUint32(args[1]); pErr == nil {
			respAction = v
		}
	}

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
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
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role", sess.OriginNickName(), "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()))

	var payload []byte
	pkt := protocol.NewPacket(actionID, nil)
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "--prevact":
			payload = make([]byte, 4)
			binary.BigEndian.PutUint32(payload, sess.LastActionID())
			pkt.HasPrevAct = true
		default:
			payload, err = hex.DecodeString(strings.TrimPrefix(args[0], "0x"))
			if err != nil {
				slog.Error("解析载荷 Hex 失败", "err", err)
				return
			}
		}
	}
	if appendPrevAct {
		prev := make([]byte, 4)
		binary.BigEndian.PutUint32(prev, sess.LastActionID())
		payload = append(payload, prev...)
		pkt.HasPrevAct = true
	}
	pkt.Payload = payload

	fmt.Printf("\n>>> request action=0x%08X payload=%s\n", actionID, hex.EncodeToString(pkt.Payload))
	callCtx, callCancel := context.WithTimeout(ctx, 6*time.Second)
	resp, callErr := sess.Call(callCtx, pkt, respAction)
	callCancel()
	if callErr != nil || resp == nil {
		fmt.Printf("<<< no response: %v (session_state=%s)\n", callErr, sess.State())
		return
	}
	fmt.Printf("<<< response action=0x%08X (%dB)\n%s\n", resp.ActionID, len(resp.Payload), hex.EncodeToString(resp.Payload))
}

func parseHexUint32(s string) (uint32, error) {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	v, err := strconv.ParseUint(s, 16, 32)
	return uint32(v), err
}
