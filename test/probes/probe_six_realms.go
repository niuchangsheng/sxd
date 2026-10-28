//go:build probe

// 目标协议: [Module 32, Action 0/2/3/4/5/6/7/8] Mod_Tower_Base 六道轮回塔 & [Module 34, Action 1/7/8] Mod_SealSoul_Base 封灵系统
// 基准对齐: 对齐官方 AS3 字节码 (TowerView, TowerData, TowerController, TowerType) 与真实抓包对齐
// 运行命令: go run -tags=probe test/probes/probe_six_realms.go
// 预期表现: 连接服务器并查询六道轮回塔层数状态、当前道、待挑战层数、执行单层挑战与重置
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}

	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置文件中未找到有效账号或角色")
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		slog.Error("启动长连接会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(10 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在预期时间内激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活，开始执行六道轮回探测")

	// 1. 查询六道轮回面板
	info, err := sess.GetTowerInfo(ctx)
	if err != nil {
		slog.Error("获取六道轮回塔面板失败", "err", err)
		return
	}

	layerName := protocol.TowerLayerName(info.Layer)
	slog.Info("六道轮回塔状态报告",
		"layer", info.Layer,
		"layer_name", layerName,
		"sequence_war", info.SequenceWar,
		"sequence_max", info.SequenceMax,
		"bag_num", info.BagNum,
		"reset_num", info.ResetNum,
		"reset_ingot", info.ResetIngot,
		"high_stone", info.HighStone,
	)

	// 2. 检查等级门槛
	minLevel := protocol.TowerLayerLevelLimit(info.Layer)
	roleLevel := sess.GetPlayerState().Level
	if roleLevel > 0 && roleLevel < minLevel {
		slog.Warn("当前等级不满足挑战门槛", "role_level", roleLevel, "required", minLevel)
		return
	}

	// 3. 若当前待挑战关卡合法，发起单次挑战探测
	if info.SequenceWar <= 25 {
		slog.Info("发起关卡挑战测试", "floor", info.SequenceWar)
		res, chErr := sess.ChallengeTower(ctx, info.SequenceWar, true, false)
		if chErr != nil {
			slog.Error("挑战关卡失败", "err", chErr)
			return
		}
		slog.Info("关卡挑战响应",
			"result_code", res.Result,
			"soul_id", res.WarSoulID,
			"gain_exp", res.WarExp,
			"gain_fame", res.WarFame,
			"gain_coin", res.WarCoin,
		)
	} else if info.ResetNum > 0 {
		slog.Info("当前道已全通，测试免费重置")
		resetRes, rErr := sess.ResetTowerLayer(ctx)
		if rErr != nil {
			slog.Error("重置六道失败", "err", rErr)
			return
		}
		slog.Info("重置响应", "result", resetRes.Result, "layer", resetRes.Layer, "reset_num", resetRes.ResetNum)
	}

	slog.Info("六道轮回探测完成")
}
