//go:build probe

// 目标协议: 仙界跨服西天取经打劫 (Mod 114 / 0x0072, Action 0/9/11/16/19)
// 基准对齐: 官方 AS3 Mod_StTakeBible_Base 与 StTakeBibleRoadView 打劫链路
// 运行命令: go run -tags=probe test/probes/probe_pilgrimage_rob.go [-execute] [-player 123456] [-max 1]
// 预期表现: 默认只读打印路况队伍、打劫冷却与目标详情；显式 -execute 时消耗每日次数发起真实打劫。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	execute := flag.Bool("execute", false, "实际发起打劫 (会消耗每日打劫次数)")
	targetPlayerID := flag.Int64("player", 0, "指定目标玩家 ID，0 表示自动选择收益最高的队伍")
	maxAttempts := flag.Int("max", 1, "最多尝试打劫次数")
	debug := flag.Bool("debug", false, "打印全部收发报文 (定位静默丢包)")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelInfo)
	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置中缺少账号或角色")
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
	slog.Info("仙界跨服凭据",
		"addr", net.JoinHostPort(cred.Host, strconv.Itoa(int(cred.Port))),
		"server_id", cred.ServerName,
		"player_id", mainSess.PlayerID(),
	)

	cross, err := mainSess.OpenCrossServerSessionWithInfo(ctx, cred)
	if err != nil {
		slog.Error("仙界跨服会话建立失败", "err", err)
		return
	}
	defer cross.Close()

	call := func(pkt *protocol.Packet, act uint32, d time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, d)
		defer cCancel()
		return cross.Call(cCtx, pkt, act)
	}

	// 1. 只读拉取取经路况
	openResp, err := call(protocol.BuildStTakeBibleCrossOpenPacket(), protocol.ActionIDStTakeBibleCrossOpen, 5*time.Second)
	if err != nil {
		slog.Error("open_take_bible 失败", "err", err)
		return
	}
	road, err := protocol.ParseStTakeBibleCrossRoadResult(openResp.Payload)
	if err != nil {
		slog.Error("解析取经路况失败", "err", err, "hex_len", len(openResp.Payload))
		return
	}
	slog.Info("取经路况",
		"teams", len(road.Teams),
		"can_rob_times", road.CanRobTimes,
		"can_take_times", road.CanTakeBibleTimes,
		"last_robs", len(road.LastRobs),
	)
	for i, team := range road.Teams {
		if i >= 20 {
			break
		}
		slog.Info("路况队伍",
			"player_id", team.PlayerID,
			"protection", protocol.StTakeBibleCrossProtectionName(team.Protection),
			"sequence_id", team.SequenceID,
			"protect_player_id", team.ProtectPlayerID,
			"is_deeds_so", team.IsDeedsSo,
			"distance", team.Distance,
		)
	}
	defer func() {
		if _, cErr := call(protocol.BuildStTakeBibleCrossClosePacket(), protocol.ActionIDStTakeBibleCrossClose, time.Second); cErr != nil {
			slog.Debug("close_take_bible 失败", "err", cErr)
		}
	}()

	// 2. 只读查询打劫冷却
	cdResp, err := call(protocol.BuildStTakeBibleCrossGetRobCDPacket(), protocol.ActionIDStTakeBibleCrossGetRobCD, 3*time.Second)
	if err != nil {
		slog.Error("get_rob_take_bible_cd_time 失败", "err", err)
		return
	}
	cd, err := protocol.ParseStTakeBibleCrossRobCDResult(cdResp.Payload)
	if err != nil {
		slog.Error("解析打劫冷却失败", "err", err)
		return
	}
	slog.Info("打劫冷却", "cd_seconds", cd.CDSeconds, "clear_ingot", cd.ClearIngot)

	// 2.1 只读核对剩余次数增量同步 (官方客户端每次 rob 后都会调用)
	updateResp, err := call(protocol.BuildStTakeBibleCrossUpdateDataPacket(), protocol.ActionIDStTakeBibleCrossUpdateData, 3*time.Second)
	if err != nil {
		slog.Warn("get_take_bible_updata_data 失败", "err", err)
	} else if update, uErr := protocol.ParseStTakeBibleCrossUpdateDataResult(updateResp.Payload); uErr != nil {
		slog.Warn("解析剩余次数失败", "err", uErr)
	} else {
		slog.Info("剩余次数同步",
			"can_rob_times", update.CanRobTimes,
			"can_take_times", update.CanTakeBibleTimes,
			"road_can_rob_times", road.CanRobTimes,
		)
	}

	// 3. 选择目标
	var selected *protocol.StTakeBibleCrossRoadTeam
	for i := range road.Teams {
		team := road.Teams[i]
		if team.PlayerID == cross.PlayerID() || team.PlayerID <= 0 {
			continue
		}
		if *targetPlayerID != 0 && int64(team.PlayerID) != *targetPlayerID {
			continue
		}
		selected = &road.Teams[i]
		break
	}
	if selected == nil {
		slog.Info("没有可探测的取经队伍")
		return
	}

	infoResp, err := call(
		protocol.BuildStTakeBibleCrossGetPlayerPacket(
			selected.PlayerID,
			int16(mainSess.GetPlayerState().Level),
			0,
			selected.SequenceID,
		),
		protocol.ActionIDStTakeBibleCrossGetPlayer,
		3*time.Second,
	)
	if err != nil {
		slog.Error("get_player_take_bible 失败", "err", err)
		return
	}
	info, err := protocol.ParseStTakeBibleCrossPlayerInfoResult(infoResp.Payload)
	if err != nil {
		slog.Error("解析目标详情失败", "err", err)
		return
	}
	slog.Info("目标详情",
		"player_id", info.PlayerID,
		"nickname", info.NickName,
		"stage_name", info.StageName,
		"server_name", info.ServerName,
		"level", info.Level,
		"is_robbed", info.IsRobbed,
		"be_rob_times", info.BeRobTimes,
		"total_can_rob_times", info.TotalCanRobTimes,
		"protect_time", info.ProtectTime,
		"rob_coin", info.RobCoin,
		"rob_fame", info.RobFame,
		"sequence_id", info.SequenceID,
	)

	if !*execute {
		slog.Info("只读模式结束；如需实际打劫请追加 -execute")
		return
	}
	if cd.CDSeconds > 0 {
		slog.Warn("打劫冷却中，跳过执行", "cd_seconds", cd.CDSeconds)
		return
	}
	if info.IsRobbed != 0 {
		slog.Warn("该队伍已被打劫过，跳过执行", "player_id", info.PlayerID)
		return
	}

	attempts := *maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		// 官方客户端使用 get_player_take_bible 回包的 sequence_id 发起打劫。
		sequenceID := info.SequenceID
		if sequenceID == 0 {
			sequenceID = selected.SequenceID
		}
		robResp, rErr := call(
			protocol.BuildStTakeBibleCrossRobPacket(selected.PlayerID, sequenceID),
			protocol.ActionIDStTakeBibleCrossRob,
			15*time.Second, // 回包含完整战报，需等待跨服节点完成战斗计算
		)
		if rErr != nil {
			slog.Error("rob 失败", "err", rErr)
			return
		}
		result, pErr := protocol.ParseStTakeBibleCrossRobResult(robResp.Payload)
		if pErr != nil {
			slog.Error("解析 rob 回包失败", "err", pErr)
			return
		}
		slog.Info("rob 结果",
			"msg", result.Msg,
			"reason", protocol.StTakeBibleCrossRobMsgName(result.Msg),
			"target_player_id", result.TargetPlayerID,
			"target_nickname", result.TargetNickName,
			"rob_coin", result.RobCoin,
			"rob_fame", result.RobFame,
			"rob_st_exp", result.RobStExp,
			"war_report_id", result.WarReportID,
			"win_player_id", result.WinPlayerID,
			"war_parsed", result.WarParsed,
			"win", result.IsWin(cross.PlayerID()),
		)
		if result.Msg != protocol.StTakeBibleCrossMsgSuccess {
			return
		}
		cdResp, rErr = call(protocol.BuildStTakeBibleCrossGetRobCDPacket(), protocol.ActionIDStTakeBibleCrossGetRobCD, 3*time.Second)
		if rErr != nil {
			return
		}
		cd, pErr = protocol.ParseStTakeBibleCrossRobCDResult(cdResp.Payload)
		if pErr != nil || cd.CDSeconds > 0 {
			return
		}
	}
}
