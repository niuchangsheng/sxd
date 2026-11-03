//go:build probe

// 目标协议: 仙界竞技场 (Mod 214 / 0x00D6) 跨服面板与挑战验证
// 基准对齐: 官方 AS3 Mod_StArena_Base.open_st_arena / refresh_player_list / challenge 描述符
// 运行命令: go run -tags=probe test/probes/probe_st_arena.go
// 可选开关: SXD_PROBE_ST_ARENA_CHALLENGE=1 时对面板中等级最低的对手发起一次真实挑战
// 可选开关: SXD_PROBE_ST_ARENA_DELAY_MS=N 在开面板与挑战之间等待 N 毫秒
// 可选开关: SXD_PROBE_ST_ARENA_DEBUG=1 打印全部收发报文，便于核对 PrevAct 线格式
// 预期表现: 建立仙界跨服会话，打印面板积分/剩余次数/对手与战报；挑战开关开启时打印结果码
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	if os.Getenv("SXD_PROBE_ST_ARENA_DEBUG") == "1" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	cfg, err := config.Load("configs/config.yaml")
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

	mainCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&mainCfg, ticket)
	mainSess := client.NewRoleSession(mainCfg)
	if err := mainSess.Start(ctx); err != nil {
		slog.Error("主服会话启动失败", "err", err)
		return
	}
	defer mainSess.Close()
	for i := 0; i < 60 && mainSess.State() != client.StateActive; i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if mainSess.State() != client.StateActive {
		slog.Error("主服未激活", "state", mainSess.State())
		return
	}

	credCtx, credCancel := context.WithTimeout(ctx, 5*time.Second)
	cred, err := mainSess.FetchCrossServerCredentials(credCtx)
	credCancel()
	if err != nil {
		slog.Error("拉取仙界凭据失败", "err", err)
		return
	}
	crossSess, err := mainSess.OpenCrossServerSessionWithInfo(ctx, cred)
	if err != nil {
		slog.Error("仙界跨服会话建立失败", "err", err)
		return
	}
	defer crossSess.Close()

	// 记录全部入站报文，用于确认挑战回包是否真的没有下发。
	var inboundMu sync.Mutex
	var inboundActions []string
	crossSess.SetPacketObserver(func(p *protocol.Packet) {
		inboundMu.Lock()
		inboundActions = append(inboundActions,
			fmt.Sprintf("0x%08X(len=%d)", p.ActionID, len(p.Payload)))
		inboundMu.Unlock()
	})

	slog.Info("仙界会话已激活",
		"player_id", crossSess.PlayerID(),
		"last_action", fmt.Sprintf("0x%08X", crossSess.LastActionID()),
	)

	call := func(pkt *protocol.Packet, actionID uint32, d time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, d)
		defer cCancel()
		return crossSess.Call(cCtx, pkt, actionID)
	}

	// 1. 打开面板（只读）。
	openResp, err := call(protocol.BuildStArenaOpenPacket(), protocol.ActionIDStArenaOpen, 5*time.Second)
	if err != nil {
		slog.Error("打开仙界竞技场面板失败", "err", err)
		return
	}
	slog.Info("open_st_arena 原始回包", "len", len(openResp.Payload), "hex", hex.EncodeToString(openResp.Payload))
	panel, err := protocol.ParseStArenaPanelResult(openResp.Payload)
	if err != nil {
		slog.Error("解析仙界竞技场面板失败", "err", err)
		return
	}
	slog.Info("仙界竞技场面板",
		"integral", panel.Integral,
		"remain_challenge_times", panel.ReduceChallengeCount,
		"award_feats", panel.AwardFeats,
		"award_get_time", panel.AwardGetTime,
		"remain_refresh_times", panel.ReduceRefreshTime,
		"opponents", len(panel.ChallengeList),
		"reports", len(panel.ReportList),
		"rank_entries", len(panel.IntegralRankList),
	)
	for i, opp := range panel.ChallengeList {
		slog.Info("可挑战对手", "index", i+1, "st_player_id", opp.StPlayerID,
			"integral", opp.Integral, "role_id", opp.RoleID, "level", opp.Level)
	}
	for _, rep := range panel.ReportList {
		slog.Info("历史战报", "report_id", rep.WarReportID,
			"attack", rep.AttackName, "defense", rep.DefenseName,
			"win_id", rep.WinID, "integral", rep.Integral, "time", rep.Time)
	}

	// 2. 可选真实挑战（默认关闭，避免误消耗次数）。
	if op := os.Getenv("SXD_PROBE_ST_ARENA_CHALLENGE"); op != "1" {
		slog.Info("未开启挑战开关，仅完成只读验证 (SXD_PROBE_ST_ARENA_CHALLENGE=1 可实测)")
		return
	}
	if len(panel.ChallengeList) == 0 || panel.ReduceChallengeCount <= 0 {
		slog.Info("无可挑战对手或次数不足", "opponents", len(panel.ChallengeList),
			"remain_times", panel.ReduceChallengeCount)
		return
	}
	target := panel.ChallengeList[0]
	for _, opp := range panel.ChallengeList[1:] {
		if opp.Level < target.Level || (opp.Level == target.Level && opp.Integral < target.Integral) {
			target = opp
		}
	}
	// 可选：在开面板与挑战之间插入等待，验证服务端是否存在操作间隔校验。
	delayMs, _ := strconv.Atoi(os.Getenv("SXD_PROBE_ST_ARENA_DELAY_MS"))
	if delayMs > 0 {
		slog.Info("挑战前等待", "delay_ms", delayMs)
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}

	challengePkt, err := protocol.BuildStArenaChallengePacket(protocol.StArenaChallengeRequest{
		StPlayerID: target.StPlayerID,
		Integral:   target.Integral,
	})
	if err != nil {
		slog.Error("构造挑战封包失败", "err", err)
		return
	}
	slog.Info("发起仙界竞技场挑战", "st_player_id", target.StPlayerID,
		"integral", target.Integral, "level", target.Level,
		"request_hex", hex.EncodeToString(challengePkt.Payload),
		"last_action", fmt.Sprintf("0x%08X", crossSess.LastActionID()))
	chResp, err := call(challengePkt, protocol.ActionIDStArenaChallenge, 20*time.Second)
	if err != nil {
		inboundMu.Lock()
		actions := append([]string(nil), inboundActions...)
		inboundMu.Unlock()
		slog.Error("仙界竞技场挑战失败", "err", err,
			"last_action", fmt.Sprintf("0x%08X", crossSess.LastActionID()),
			"inbound_actions", actions)
		return
	}
	slog.Info("challenge 原始回包", "len", len(chResp.Payload), "hex", hex.EncodeToString(chResp.Payload))
	if len(chResp.Payload) > 0 {
		slog.Info("challenge 结果码", "result", chResp.Payload[0])
	}
}
