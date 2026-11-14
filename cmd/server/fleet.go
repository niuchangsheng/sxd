// Package main 提供神仙道助手服务端主入口与运行时生命周期管理。
//
// 本文件实现多区服启动编排：计划构建 -> 串行换票 -> 并发启动会话与独立调度器。
//
// @author Ateng
// @since 2026-11-06
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/routines"
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/scheduler"
	"sxd-pie-ng/internal/web"
)

// rolePlan 描述一个待启动角色的完整运行计划。
type rolePlan struct {
	acc  *config.AccountConfig
	role config.RoleConfig

	roleID string
	cfg    client.SessionConfig

	eff     config.SchedulerConfig
	factory *routines.RegistryFactory
	enabled map[string]bool

	dictRepo dictionary.Repository

	sess *client.RoleSession
}

// buildRolePlans 构建启动计划：去重、解析网关、合并区服覆盖、创建玩法工厂。
// 此阶段只做纯内存计算，不发起任何网络请求。
func buildRolePlans(
	cfg *config.Config,
	qaEngine qa.Engine,
	dictRepo dictionary.Repository,
) []*rolePlan {
	seen := make(map[string]bool)
	factories := make(map[string]*routines.RegistryFactory)
	plans := make([]*rolePlan, 0, len(cfg.Accounts))

	for i := range cfg.Accounts {
		acc := &cfg.Accounts[i]
		for _, role := range acc.Roles {
			roleID := fmt.Sprintf("%s-%s", role.ServerID, role.RoleName)
			if seen[roleID] {
				slog.Warn("检测到重复角色配置，已跳过后续条目",
					"role_id", roleID, "username", acc.Username)
				continue
			}
			seen[roleID] = true

			eff, err := cfg.EffectiveScheduler(role.ServerID)
			if err != nil {
				slog.Warn("区服覆盖配置合并失败，回退全局调度策略",
					"server_id", role.ServerID, "error", err)
				eff = cfg.Scheduler
			}

			factoryKey := strings.ToLower(role.ServerID)
			factory := factories[factoryKey]
			if factory == nil {
				factory = newRegistryFactory(eff, qaEngine, dictRepo)
				factories[factoryKey] = factory
			}

			plans = append(plans, &rolePlan{
				acc:      acc,
				role:     role,
				roleID:   roleID,
				cfg:      buildSessionConfig(acc, role, roleID),
				eff:      eff,
				factory:  factory,
				enabled:  buildRoutineEnabledMap(eff),
				dictRepo: dictRepo,
			})
		}
	}

	// 提示拼写错误或已移除区服的覆盖配置，避免策略静默不生效。
	activeServers := make(map[string]bool, len(plans))
	for _, p := range plans {
		activeServers[strings.ToLower(p.role.ServerID)] = true
	}
	for serverID := range cfg.Scheduler.ServerOverrides {
		if !activeServers[strings.ToLower(serverID)] {
			slog.Warn("server_overrides 配置的区服未出现在 accounts.roles 中，该覆盖不会生效",
				"server_id", serverID)
		}
	}
	return plans
}

func buildSessionConfig(acc *config.AccountConfig, role config.RoleConfig, roleID string) client.SessionConfig {
	return client.SessionConfig{
		RoleID:               roleID,
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           resolveServerAddr(role),
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Code:                 role.Code,
		Time:                 role.Time,
		Hash:                 role.Hash,
		Time1:                role.Time1,
		Hash1:                role.Hash1,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
}

// fetchAllCredentials 串行换取所有角色的平台凭据。
// 平台 Web 登录对同一账号存在频控，因此按配置间隔 (默认 2~5 秒) 拉开；
// 单个角色失败只记录日志，不影响其他区服。
func fetchAllCredentials(
	ctx context.Context,
	plans []*rolePlan,
	startup config.StartupConfig,
	ticketCache *platform.TicketCache,
	xdAuth *platform.XindongAuthenticator,
) {
	minDelay, maxDelay := startup.TicketDelayRange()
	for i, plan := range plans {
		if ctx.Err() != nil {
			return
		}
		if i > 0 {
			if err := waitTicketInterval(ctx, minDelay, maxDelay); err != nil {
				return
			}
		}
		if xdAuth != nil && plan.acc.Cookie != "" && isXindongPlatform(plan.acc.Platform) {
			xdAuth.SetCookie(plan.acc.Cookie)
		}
		plan.fetchCredentials(ctx, ticketCache)
	}
}

func waitTicketInterval(ctx context.Context, minDelay, maxDelay time.Duration) error {
	if maxDelay <= 0 {
		return nil
	}
	delay := minDelay
	if maxDelay > minDelay {
		delay += time.Duration(rand.Int63n(int64(maxDelay-minDelay) + 1))
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// fetchCredentials 为单个计划换取双轨凭据并装配自愈刷新回调。
func (p *rolePlan) fetchCredentials(ctx context.Context, ticketCache *platform.TicketCache) {
	acc := p.acc

	// 主通道：全自治 Web 登录换取最新双轨凭据。
	if acc.Platform != "" && (acc.Cookie != "" || (acc.Username != "" && acc.Password != "")) {
		slog.Info("正在通过全自治 Web 驱动自动换取平台最新凭据...",
			"platform", acc.Platform, "username", acc.Username, "server", p.role.ServerID)
		ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, p.role.ServerID)
		if err != nil {
			slog.Warn("原生平台换票未成功，尝试备用降级通道",
				"role_id", p.roleID, "server_id", p.role.ServerID, "error", err)
		} else {
			client.ApplyTicket(&p.cfg, ticket)
			if ticket.GatewayURL != "" {
				p.cfg.ServerAddr = ticket.GatewayURL
			}
			slog.Info("已成功全自动换取最新双轨凭据并直注会话",
				"role_id", p.roleID, "server_id", p.cfg.ServerID,
				"role_name", p.role.RoleName, "server_addr", p.cfg.ServerAddr)
		}
	}

	// 备用降级通道：仅当主服 Hash 缺失时，才尝试从外部 user.ini 摄取。
	if p.cfg.Hash == "" {
		if ticket, err := platform.LoadTicketFromIni("", p.role.RoleName); err == nil {
			client.ApplyTicket(&p.cfg, ticket)
			slog.Info("已通过备用 Ticket Ingress 成功摄取本地凭据",
				"role_id", p.roleID, "role_name", p.role.RoleName, "server_id", p.cfg.ServerID)
		}
	}

	// 自愈凭据刷新管道：断线时使旧票失效并重新向 Web 换票。
	// 每个计划独立捕获自身的 server_id，避免多区服互相覆盖。
	if acc.Platform != "" && acc.Username != "" && acc.Password != "" {
		pName := acc.Platform
		uName := acc.Username
		pWord := acc.Password
		sID := p.role.ServerID
		roleID := p.roleID
		p.cfg.MaxReconnectAttempts = 10
		p.cfg.RefreshCredentials = func(c context.Context, sCfg *client.SessionConfig) error {
			ticketCache.Invalidate(pName, uName, sID)
			freshTicket, err := ticketCache.GetOrFetch(c, pName, uName, pWord, sID)
			if err != nil {
				slog.Warn("自愈换票失败", "role_id", roleID, "server_id", sID, "error", err)
				return err
			}
			client.ApplyTicket(sCfg, freshTicket)
			if freshTicket.GatewayURL != "" {
				sCfg.ServerAddr = freshTicket.GatewayURL
			}
			return nil
		}
	}
}

// registerPlans 装配会话、调度器与玩法矩阵，并返回可自动登录的计划。
func registerPlans(
	plans []*rolePlan,
	mgr *sessionManager,
	dispatcher *scheduler.Dispatcher,
	routineMgr *routineManager,
) []*rolePlan {
	startable := make([]*rolePlan, 0, len(plans))

	for _, p := range plans {
		sess := client.NewRoleSession(p.cfg)
		p.sess = sess

		jitterCfg := scheduler.JitterConfig{
			MinDuration: time.Duration(p.eff.Jitter.MinSeconds * float64(time.Second)),
			MaxDuration: time.Duration(p.eff.Jitter.MaxSeconds * float64(time.Second)),
		}
		roleSched, err := dispatcher.AddRole(p.roleID, sess, jitterCfg)
		if err != nil {
			slog.Error("注册角色调度器失败，已跳过该角色",
				"role_id", p.roleID, "server_id", p.role.ServerID, "error", err)
			continue
		}
		mgr.add(sess, web.RoleInfo{
			RoleID:   p.roleID,
			RoleName: p.role.RoleName,
			ServerID: p.cfg.ServerID,
			State:    sess.State().String(),
		})

		regCount := p.factory.RegisterAll(roleSched, p.enabled)
		slog.Info("已为角色装配自动化玩法矩阵",
			"role_id", p.roleID, "server_id", p.role.ServerID, "registered_count", regCount)
		routineMgr.registerRole(p.roleID, p.role.ServerID, roleSched, p.enabled, p.factory)

		if !p.role.AutoLogin {
			continue
		}
		if p.cfg.Hash == "" && p.cfg.Hash1 == "" && p.cfg.ServerAddr != "sandbox" {
			slog.Error("角色缺失有效网关认证票据 (Hash 为空)，已跳过自动连接",
				"role_id", p.roleID, "server_id", p.role.ServerID)
			continue
		}
		startable = append(startable, p)
	}
	return startable
}

// startPlans 并发启动已就绪的角色会话。
// 每个 RoleSession 拥有独立 TCP 连接、心跳与状态机，互不共享。
func startPlans(ctx context.Context, plans []*rolePlan) {
	var wg sync.WaitGroup
	for _, p := range plans {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.sess.Start(ctx); err != nil {
				slog.Error("启动角色会话失败",
					"role_id", p.roleID, "server_id", p.role.ServerID, "error", err)
				return
			}
			slog.Info("角色会话启动中",
				"role_id", p.roleID, "server_id", p.role.ServerID, "server_addr", p.cfg.ServerAddr)
			// 登录后置任务（调度器在 startPlans 之后才启动）：
			// 只保留渡业台 852/16 —— 它要求“登录后第一个业务请求”。
			//
			// 淬炼池 282 与英雄副本 35/0 不再在登录钩子中执行：
			// 2026-11-14 线上探针确认，发送 35/0（即使全部有回包）后，
			// 任务推图的 get_sections (4/0) 会被服务端静默丢弃；
			// 反过来先 get_sections 再发 35/0 两者都正常。
			// 因此由调度器按优先级执行：dungeon(200) 先完成一次任务推图，
			// 之后的淬炼池与英雄副本由各自例程处理。
			if p.enabled["rebirth_land"] {
				daily.RunRebirthAltarOnLogin(ctx, p.sess)
			}
		}()
	}
	wg.Wait()
}

// runCronTicker 按每个角色生效的 cron_times 定点唤醒其 Cron 玩法。
// 使用 per-role 触发，确保 server_overrides.cron_times 也能独立生效。
func runCronTicker(ctx context.Context, dispatcher *scheduler.Dispatcher, cronTimes map[string][]string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	lastTriggerMinute := make(map[string]string, len(cronTimes))
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			currentMinute := now.Format("15:04")
			for roleID, times := range cronTimes {
				if lastTriggerMinute[roleID] == currentMinute {
					continue
				}
				matched := false
				for _, t := range times {
					if currentMinute == t {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
				lastTriggerMinute[roleID] = currentMinute
				roleSched := dispatcher.GetRole(roleID)
				if roleSched == nil {
					continue
				}
				triggered := roleSched.TriggerBatch(scheduler.ScheduleCron)
				slog.Info("触发定点日常重置批量唤醒",
					"role_id", roleID, "time", currentMinute, "triggered_count", triggered)
			}
		}
	}
}

func isXindongPlatform(platformName string) bool {
	return strings.EqualFold(platformName, "xindong") || strings.EqualFold(platformName, "xd")
}
