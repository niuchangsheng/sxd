//go:build probe

// 目标: 直接执行 travel_event 例程, 验证「答题满一轮先开宝箱再继续」链路。
//
// 背景: get_event_and_answer 回包尾部携带 cur/tol_answer_times; 当 cur>=tol
// 时必须先调用 0x00150004 lottery_draw (免费), 否则服务端静默丢弃答题请求。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_travel_routine_live.go
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
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/routines/minigame"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	st := sess.GetPlayerState()
	qaEngine, err := qa.NewEngine("")
	if err != nil {
		slog.Warn("题库引擎加载失败, 回退首个分支", "err", err)
	}
	r := minigame.NewTravelEventRoutine(qaEngine)
	if err := r.Execute(ctx, sess, nil); err != nil {
		slog.Error("仙旅奇缘例程执行失败", "err", err)
	}
	fin := sess.GetPlayerState()
	slog.Info("仙旅奇缘例程结束", "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
