//go:build probe

// 目标协议: [Module 232, Action 30/34] 结缘宝箱列表/批量开启 + [Module 528, Action 1] 开箱前置握手
// 基准对齐: 对齐原版 Mod_JieYuan_Base 结缘宝箱开箱闭环
// 运行命令: go run -tags=probe test/probes/probe_jieyuan_box_live.go
// 运行命令 (只读): go run -tags=probe test/probes/probe_jieyuan_box_live.go -dry-run
// 预期表现: 登录 s0 后先只读查询背包结缘宝箱并打印原始载荷，随后执行 jieyuan_box Routine 批量开启非传说宝箱，最后复查剩余数量
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
	"sxd-pie-ng/internal/routines/social"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "只读查询结缘宝箱，不执行开箱")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
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

	for i := 0; i < 60; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("=== 会话已激活，开始结缘宝箱测试 ===",
		"role_id", sess.RoleID(),
		"server_id", role.ServerID,
		"player_id", sess.PlayerID(),
	)

	// 1. 只读查询：打印原始载荷，便于核对列表解析格式
	items, err := queryJieYuanBoxes(ctx, sess, "查询-执行前")
	if err != nil {
		slog.Error("查询结缘宝箱列表失败", "err", err)
		return
	}
	printBoxes(items)

	openable := protocol.FilterOpenableJieYuanBoxes(items)
	if *dryRun {
		slog.Info("dry-run 模式：仅查询，未执行开箱", "total_kinds", len(items), "openable_kinds", len(openable))
		return
	}
	if len(openable) == 0 {
		slog.Info("无待开启的非传说结缘宝箱，跳过开箱执行", "total_kinds", len(items))
		return
	}

	// 2. 执行真实 Routine (会消耗非传说结缘宝箱)
	boxCfg := social.DefaultJieYuanBoxConfig()
	if cfg.Scheduler.JieYuanBox.MaxBatchSize > 0 {
		boxCfg.MaxBatchSize = cfg.Scheduler.JieYuanBox.MaxBatchSize
	}
	execCtx, execCancel := context.WithTimeout(ctx, 60*time.Second)
	defer execCancel()

	routine := social.NewJieYuanBoxRoutine(boxCfg)
	if err := routine.Execute(execCtx, sess, nil); err != nil {
		slog.Error("jieyuan_box Routine 执行失败", "err", err)
		return
	}

	// 3. 复查剩余数量
	remaining, err := queryJieYuanBoxes(ctx, sess, "查询-执行后")
	if err != nil {
		slog.Error("复查结缘宝箱列表失败", "err", err)
		return
	}
	printBoxes(remaining)

	fmt.Printf("\n------------------------------------------------------------\n")
	fmt.Printf("结缘宝箱测试完成: 开箱前 %d 种, 可开启 %d 种, 开箱后剩 %d 种\n",
		len(items), len(openable), len(remaining))
	fmt.Printf("------------------------------------------------------------\n\n")
}

func queryJieYuanBoxes(ctx context.Context, sess *client.RoleSession, label string) ([]protocol.JieYuanBoxItem, error) {
	pkt := protocol.BuildJieYuanBoxListPacket(sess.LastActionID())
	cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := sess.Call(cCtx, pkt, protocol.ActionIDJieYuanBoxList)
	if err != nil {
		return nil, err
	}
	fmt.Printf("\n[%s] resp action=0x%08X len=%d payload=%s\n",
		label, resp.ActionID, len(resp.Payload), hex.EncodeToString(resp.Payload))

	return protocol.ParseJieYuanBoxListResult(resp.Payload)
}

func printBoxes(items []protocol.JieYuanBoxItem) {
	if len(items) == 0 {
		fmt.Println("  (无结缘宝箱)")
		return
	}
	for _, b := range items {
		skip := ""
		if b.ItemID == protocol.ItemJieYuanBoxLegendary {
			skip = " [传说-跳过]"
		}
		fmt.Printf("  item_id=%d count=%d%s\n", b.ItemID, b.Count, skip)
	}
}
