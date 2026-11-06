//go:build probe

// 目标: 批量查询角色当前可用的功能开放状态 (Mod_Player_Base.check_function_open, 0x00000045)
//
// 服务端对未解锁模块的业务请求会静默丢弃（表现为超时）。本探针在发起任何业务
// 请求前先列出候选玩法是否开放，用于判定哪些活跃度任务对当前角色可达。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_function_open_matrix_live.go
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

type functionProbe struct {
	id   int32
	name string
}

var functionProbes = []functionProbe{
	{42, "琳琅阁/神秘商人"},
	{43, "发财树(使用仙露)"},
	{56, "摘仙桃"},
	{88, "聚宝盆"},
	{113, "五行天仪"},
	{119, "仙盟/帮派"},
	{131, "家园"},
	{132, "仙界竞技场"},
	{140, "结缘"},
	{147, "九界仙葫"},
	{152, "奇门遁甲"},
	{154, "主角成圣"},
	{166, "天缘/缘魂宝箱"},
	{172, "孩子"},
	{173, "劫镖"},
	{175, "符石宝殿"},
	{192, "命格客栈升级"},
	{216, "回忆之境"},
	{228, "宗门仙境"},
	{235, "法力星图"},
	{243, "山海九界"},
	{245, "悟法领域"},
	{256, "山海幻兽"},
	{259, "六道轮回"},
	{272, "幻灵伙伴(仙幽猎境)"},
	{281, "诸法器冢"},
	{292, "轮回净地"},
	{294, "纪元湮劫"},
	{90, "取经"},
	{96, "混沌虚空"},
	{24, "猎命"},
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
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
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	open, closed, unknown := 0, 0, 0
	for _, fp := range functionProbes {
		if ctx.Err() != nil {
			break
		}
		pkt := protocol.BuildCheckFunctionOpenPacket(fp.id, sess.LastActionID())
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, protocol.ActionIDPlayerCheckFunctionOpen)
		cCancel()
		if cErr != nil || resp == nil {
			unknown++
			slog.Info("功能开放状态未知(无回包)", "func_id", fp.id, "name", fp.name, "err", cErr)
			continue
		}
		state, pErr := protocol.ParseCheckFunctionOpenResponse(resp.Payload)
		if pErr != nil {
			unknown++
			slog.Warn("功能开放状态解析失败", "func_id", fp.id, "name", fp.name, "err", pErr)
			continue
		}
		switch state {
		case protocol.PlayerFunctionOpenTrue:
			open++
			slog.Info("功能已开放", "func_id", fp.id, "name", fp.name)
		case protocol.PlayerFunctionOpenFalse:
			closed++
			slog.Info("功能未开放", "func_id", fp.id, "name", fp.name)
		default:
			unknown++
			slog.Info("功能开放状态未知值", "func_id", fp.id, "name", fp.name, "state", state)
		}
	}
	slog.Info("功能开放矩阵汇总", "open", open, "closed", closed, "unknown", unknown)
}
