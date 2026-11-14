//go:build probe

// 目标: 实机验证仙界矿山 Routine (Module 264, Mod_StMine_Base)
//
//	信息: Mod 264 get_mine_mountain_info (0x01080000)
//	进入: Mod 264 enter_mine_mountain    (0x01080001)
//	领奖: Mod 264 receive_award          (0x01080007)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_crystal_mine_routine_live.go
//
// 目标会话: 仙界跨服会话 (主服 264/0 恒返回 FAILED(13))。
//
// 预期表现: 功能开放时进入矿脉，领取已有收益并占领/抢夺一个矿洞；
// 未开放/关闭时按结果码进入冷却。
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
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/scheduler"
)

func main() {
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	logMineTask := func(stage string) {
		cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		resp, cErr := sess.Call(cCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if cErr != nil || resp == nil {
			slog.Warn("活跃度查询失败", "stage", stage, "err", cErr)
			return
		}
		panel, pErr := protocol.ParseGameAssistantInfo(resp.Payload)
		if pErr != nil {
			slog.Warn("活跃度解析失败", "stage", stage, "err", pErr)
			return
		}
		if task, ok := panel.FindTask(51); ok {
			slog.Info("抢夺仙界矿山活跃度", "stage", stage,
				"cur", task.CurTimes, "max", task.MaxTimes, "per", task.PerActiveAdd)
		} else {
			slog.Info("活跃度面板未包含任务51", "stage", stage)
		}
	}
	logMineTask("before")

	for _, r := range []struct {
		name string
		rt   scheduler.ActivityRoutine
	}{
		{"仙界矿山", farming.NewCrystalMineRoutine()},
	} {
		if err := r.rt.Execute(ctx, sess, nil); err != nil {
			slog.Warn("Routine 执行返回错误", "name", r.name, "err", err)
		} else {
			slog.Info("Routine 执行完成", "name", r.name)
		}
	}
	logMineTask("after")
}
