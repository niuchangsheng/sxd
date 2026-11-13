//go:build probe

// 目标: 定位粗铁淬炼池 282/0 open_panel 静默丢包的载荷形态。
//
// 背景: niuchangsheng s0/s1 每 10 分钟尝试打开淬炼池面板均 8 秒超时；
// 官方 request=[]，现行构造追加 4 字节 PrevAct。结合 25/0、35/0、2/53 的
// 实机结论，怀疑服务端只接受空载荷。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_refine_pool_live.go -role 0
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
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

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
	}
	client.ApplyTicket(&roleCfg, ticket)
	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(),
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	ack := sess.LastAckActionID()
	prev := []byte{byte(ack >> 24), byte(ack >> 16), byte(ack >> 8), byte(ack)}
	var openResp *protocol.Packet
	for _, v := range []struct {
		name    string
		payload []byte
	}{
		{"open_bare", nil},
		{"open_prev_ack", prev},
		{"open_prev0", []byte{0, 0, 0, 0}},
	} {
		sess.SetLastActionID(0)
		if resp := call(protocol.NewPacket(protocol.ActionIDRefinePoolOpen, v.payload),
			protocol.ActionIDRefinePoolOpen, v.name); resp != nil {
			openResp = resp
			break
		}
		time.Sleep(time.Second)
	}
	if openResp == nil {
		slog.Info("282/0 三种形态均无回包")
		return
	}
	panel, pErr := protocol.ParseRefinePoolOpenResponse(openResp.Payload)
	if pErr != nil {
		slog.Warn("解析淬炼池面板失败", "err", pErr)
		return
	}
	done := 0
	for _, iron := range panel.Irons {
		if iron.ProduceStatus == protocol.RefinePoolProduceDone {
			done++
		}
	}
	slog.Info("淬炼池面板", "irons", len(panel.Irons), "done", done, "level", panel.Level)
	if done == 0 {
		slog.Info("无已完成槽位，跳过 282/1 载荷测试")
		return
	}

	// 282/1 refine 载荷对比：裸 3 字节 vs 7 字节带 PrevAct。
	base := []byte{protocol.RefinePoolTargetXuanTie, protocol.RefinePoolCostCoins, protocol.RefinePoolTypeOneKey}
	execPrev := append(append([]byte{}, base...), byte((protocol.ActionIDRefinePoolOpen>>24)&0xFF), byte((protocol.ActionIDRefinePoolOpen>>16)&0xFF), byte((protocol.ActionIDRefinePoolOpen>>8)&0xFF), byte(protocol.ActionIDRefinePoolOpen&0xFF))
	for _, v := range []struct {
		name    string
		payload []byte
	}{
		{"exec_bare", base},
		{"exec_prev_open", execPrev},
	} {
		sess.SetLastActionID(0)
		if call(protocol.NewPacket(protocol.ActionIDRefineExecute, v.payload),
			protocol.ActionIDRefineExecute, v.name) != nil {
			break
		}
		time.Sleep(time.Second)
	}
	slog.Info("探针结束")
}
