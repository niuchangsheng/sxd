//go:build probe

// 目标协议: [Module 4] 精英任务 (Quests.Type=4) 生产推图闭环: quest_cycle 自动接取/首通/交付
// 基准对齐: 原版 pie.exe 精英副本任务与剧情副本共用 Mod_Mission_Base 首通状态机
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_elite_quest_cycle_live.go
// 预期表现: 直接执行生产 dungeon routine 的 quest_cycle 流水线, 日志中精英任务应走
//
//	"精英副本任务直接进入首通挑战" -> plot_enter/fight_monster -> "任务达成并成功交付结算"。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/dungeon"
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("打开数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

	routine := dungeon.NewDungeonSweepRoutine(dictRepo, dungeon.SweepConfig{
		Pipeline: []string{"quest_cycle"},
	})
	if err := routine.Execute(ctx, sess, nil); err != nil {
		slog.Warn("quest_cycle 流水线返回错误", "err", err)
	}

	items, err := sess.SyncQuests(ctx)
	if err != nil {
		slog.Warn("通关后同步任务列表失败", "err", err)
		return
	}
	fmt.Println("--- 最终任务状态 ---")
	for _, it := range items {
		fmt.Printf("quest=%d state=%d current=%d max=%d\n", it.QuestID, it.State, it.CurrentCount, it.MaxCount)
	}
}
