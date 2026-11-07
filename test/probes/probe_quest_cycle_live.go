//go:build probe

// 目标协议: [Module 3] Mod_Quest_Base 任务闭环 & [Module 4] Mod_Mission_Base 剧情副本 & [Module 25] 关卡扫荡
// 基准对齐: 对齐原版 pie.exe 主线/支线任务接取、推进、交付与体力自愈闭环
// 运行命令: go run -tags=probe test/probes/probe_quest_cycle_live.go
// 预期表现: 登录后执行一次 dungeon Routine (quest_cycle 开启、sweep 关闭)，输出任务条目、章节关卡状态、进入副本结果与体力结算
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/dungeon"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}

	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("挂载 Pieb.db 数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()
	slog.Info("已挂载 Pieb.db 数据字典")

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

	st := sess.GetPlayerState()
	slog.Info("=== 会话已激活，开始执行推图 Routine ===",
		"role_id", sess.RoleID(),
		"origin_name", sess.OriginNickName(),
		"player_id", sess.PlayerID(),
		"level", st.Level,
		"stamina", sess.GetStamina(),
		"town_id", st.TownID,
	)

	// 推图前先显式同步一次任务列表，便于对照 Routine 执行后的变化。
	if items, syncErr := sess.SyncQuests(ctx); syncErr != nil {
		slog.Warn("推图前同步任务列表失败", "err", syncErr)
	} else {
		for _, it := range items {
			slog.Info("推图前任务",
				"quest_id", it.QuestID,
				"state", it.State,
				"current", it.CurrentCount,
				"max", it.MaxCount,
			)
		}
	}

	execCtx, execCancel := context.WithTimeout(ctx, 150*time.Second)
	defer execCancel()

	sweepCfg := dungeon.DefaultSweepConfig()
	sweepCfg.QuestCycle.Enabled = true
	sweepCfg.QuestCycle.AutoReplenishStamina = true
	sweepCfg.QuestCycle.MaxReplenishStamina = 200
	sweepCfg.Sweep.Enabled = false
	sweepCfg.Pipeline = []string{"quest_cycle"}

	routine := dungeon.NewDungeonSweepRoutine(dictRepo, sweepCfg)
	if err := routine.Execute(execCtx, sess, nil); err != nil {
		slog.Error("推图 Routine 执行失败", "err", err)
	}

	after := sess.GetPlayerState()
	slog.Info("=== 推图 Routine 执行完成 ===",
		"state", sess.State().String(),
		"level", after.Level,
		"experience", after.Experience,
		"max_experience", after.MaxExperience,
		"stamina", sess.GetStamina(),
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
	)

	if items, syncErr := sess.SyncQuests(ctx); syncErr == nil {
		for _, it := range items {
			slog.Info("推图后任务",
				"quest_id", it.QuestID,
				"state", it.State,
				"current", it.CurrentCount,
				"max", it.MaxCount,
			)
		}
	}
}
