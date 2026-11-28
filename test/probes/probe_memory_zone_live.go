//go:build probe

// 目标: 实机探测「回忆之境」(Mod_MemoryZone_Base, Module 526 / 0x020E) 的
//       功能开放状态 (0/69 function_id=216)、主面板 (526/1) 与分区伙伴碎片面板 (526/2)。
//
// 运行命令:
//   SXD_CONFIG=configs/niuchangsheng.yaml SXD_SERVER=s0 go run -tags=probe test/probes/probe_memory_zone_live.go
//
// 预期表现:
//   - 默认仅执行只读探测（0/69 -> 526/1 -> 526/2），打印原始 hex 与解码结果；
//   - 仅当显式设置 SXD_DO_BACKTRACK=1 时才调用 NewMemoryZoneRoutine() 执行每日免费回溯（0 元宝）。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/daily"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	wantServer := os.Getenv("SXD_SERVER")
	if wantServer == "" {
		wantServer = "s0"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}

	var acc config.AccountConfig
	var role config.RoleConfig
	found := false
	for _, a := range cfg.Accounts {
		for _, r := range a.Roles {
			if strings.EqualFold(r.ServerID, wantServer) {
				acc = a
				role = r
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		slog.Error("配置中未找到目标区服角色", "server", wantServer)
		return
	}

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建心动认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	runCtx, cancelRun := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelRun()
	ticket, err := ticketCache.GetOrFetch(runCtx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("获取登录票据失败", "err", err)
		return
	}

	sessCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&sessCfg, ticket)

	sess := client.NewRoleSession(sessCfg)
	if err := sess.Start(runCtx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	if err := sess.AwaitActive(runCtx); err != nil {
		slog.Error("等待主服会话激活失败", "err", err)
		return
	}

	open, err := sess.CheckFunctionOpen(runCtx, protocol.FunctionIDMemoryZone)
	slog.Info("功能开放探测", "function_id", protocol.FunctionIDMemoryZone, "open", open, "err", err)
	if !open {
		return
	}

	mainResp, err := sess.CallWithPrevActResync(runCtx, 8*time.Second, protocol.BuildMemoryZoneMainPanelPacket(), protocol.ActionIDMemoryZoneMainPanel)
	if err != nil {
		slog.Error("526/1 main_panel 失败", "err", err)
		return
	}
	slog.Info("526/1 main_panel 原始回包", "len", len(mainResp.Payload), "hex", hex.EncodeToString(mainResp.Payload))
	mainPanel, err := protocol.ParseMemoryZoneMainPanel(mainResp.Payload)
	if err != nil {
		slog.Error("526/1 解析失败", "err", err)
		return
	}
	slog.Info("526/1 解析结果", "result", mainPanel.Result, "categories", mainPanel.Categories, "award_daoyuan", mainPanel.AwardDaoYuan)

	for _, c := range mainPanel.Categories {
		backResp, err := sess.CallWithPrevActResync(runCtx, 8*time.Second, protocol.BuildMemoryZoneBackPanelPacket(c.Type), protocol.ActionIDMemoryZoneBackPanel)
		if err != nil {
			slog.Error("526/2 back_panel 失败", "type", c.Type, "err", err)
			continue
		}
		backPanel, err := protocol.ParseMemoryZoneBackPanel(backResp.Payload)
		if err != nil {
			slog.Error("526/2 解析失败", "type", c.Type, "err", err)
			continue
		}
		slog.Info("526/2 解析结果",
			"type", c.Type,
			"result", backPanel.Result,
			"today", backPanel.TodayBackTimes,
			"free", backPanel.DailyFreeTimes,
			"ingot_limit", backPanel.DailyIngotTimes,
			"cost_ingot", backPanel.CostIngot,
			"partners", backPanel.Partners)
	}

	if os.Getenv("SXD_DO_BACKTRACK") == "1" {
		err := daily.NewMemoryZoneRoutine().Execute(runCtx, sess, nil)
		slog.Info("NewMemoryZoneRoutine 执行完成", "err", err)
	}
}
