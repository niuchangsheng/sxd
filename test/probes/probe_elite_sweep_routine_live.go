//go:build probe

// 目标协议: [Module 35] Mod_HeroMission_Base 全城镇精英副本扫荡 + 背包掉落自动出售
// 基准对齐: 原版客户端 HeroPracticeView (start_practice -> quickly -> notify) 与 PackView 出售流程
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_elite_sweep_routine_live.go
// 预期表现: 遍历 Pieb.db 中所有含精英副本的城镇，逐城镇 start_practice/quickly，
//           并在每城镇结束后对比背包数量差异自动卖出新增掉落。
// 注意: 该探针会真实消耗体力并卖出扫荡掉落，属于写操作；仅在需要实机验证时运行。
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

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("加载数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

	towns, err := dictRepo.GetEliteMissionTowns()
	if err != nil {
		slog.Error("查询精英副本城镇失败", "err", err)
		return
	}
	fmt.Printf("数据字典精英副本城镇: %v (共 %d 个)\n", towns, len(towns))

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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
	slog.Info("会话已激活", "role", sess.RoleName(), "level", st.Level, "stamina", sess.GetStamina(), "coins", st.Coins, "bag_capacity", st.BagCapacity)

	sweepCfg := dungeon.DefaultSweepConfig()
	sweepCfg.Sweep.Enabled = true
	sweepCfg.Sweep.Pipeline = []string{"sweep_elite"}
	sweepCfg.Sweep.SellSweptItems = true
	sweepCfg.Sweep.PreferPracticeQuickly = true
	sweepCfg.Pipeline = []string{"sweep_elite"}

	routine := dungeon.NewDungeonSweepRoutine(dictRepo, sweepCfg)
	err = routine.Execute(ctx, sess, nil)
	after := sess.GetPlayerState()
	fmt.Printf("Execute 返回: %v\n", err)
	fmt.Printf("扫荡后: stamina=%d coins=%d\n", sess.GetStamina(), after.Coins)
}