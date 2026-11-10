// Package main 提供神仙道助手服务端主入口与运行时生命周期管理。
//
// @author Ateng
// @since 2026-10-08
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/logging"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/scheduler"
	"sxd-pie-ng/internal/web"
)

var (
	// Version 编译期注入的版本号
	Version = "dev"
	// Commit 编译期注入的 Git Commit 散列
	Commit = "none"
	// Date 编译期注入的构建日期
	Date = "unknown"
)

type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var next []slog.Handler
	for _, h := range m.handlers {
		next = append(next, h.WithAttrs(attrs))
	}
	return &multiHandler{handlers: next}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	var next []slog.Handler
	for _, h := range m.handlers {
		next = append(next, h.WithGroup(name))
	}
	return &multiHandler{handlers: next}
}

type sessionManager struct {
	sessions []*client.RoleSession
	roles    []web.RoleInfo
	mu       sync.RWMutex
}

func (m *sessionManager) add(sess *client.RoleSession, info web.RoleInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = append(m.sessions, sess)
	m.roles = append(m.roles, info)
}

func (m *sessionManager) closeAll() {
	m.mu.RLock()
	sessions := append([]*client.RoleSession(nil), m.sessions...)
	m.mu.RUnlock()
	for _, sess := range sessions {
		sess.Close()
	}
}

func (m *sessionManager) ListRoles() []web.RoleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]web.RoleInfo, len(m.roles))
	for i, r := range m.roles {
		res[i] = r
		if i < len(m.sessions) && m.sessions[i] != nil {
			res[i].State = m.sessions[i].State().String()
		}
	}
	return res
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("服务异常退出", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sxd-pie-ng", flag.ContinueOnError)
	versionFlag := fs.Bool("v", false, "打印版本信息并退出")
	configPath := fs.String("config", "configs/config.yaml", "配置文件路径")
	browserLoginFlag := fs.Bool("browser-login", false, "启动前弹出浏览器交互登录以自动捕获/更新 Cookie")
	debugFlag := fs.Bool("debug", false, "是否开启Debug日志输出 (默认关闭，屏幕仅输出业务Info及以上日志)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *versionFlag {
		fmt.Printf("sxd-pie-ng version %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return nil
	}

	// 1. 初始化环形日志收集器与受管日志 (默认Info，屏幕不展示debug日志)
	logLevel := slog.LevelInfo
	if *debugFlag {
		logLevel = slog.LevelDebug
	}
	logBuf := web.NewLogBuffer(200)
	if *debugFlag {
		logBuf.SetMinLevel(slog.LevelDebug)
	}
	stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	multiH := &multiHandler{
		handlers: []slog.Handler{stdoutHandler, logBuf},
	}
	slog.SetDefault(slog.New(multiH))

	targetConfig := *configPath
	if _, err := os.Stat(targetConfig); os.IsNotExist(err) && targetConfig == "configs/config.yaml" {
		targetConfig = "configs/config.example.yaml"
	}

	if *browserLoginFlag {
		if err := runBrowserLogin(ctx, targetConfig); err != nil {
			slog.Error("浏览器辅助登录未完成", "error", err)
		}
	}

	slog.Info("正在加载系统配置...", "file", targetConfig)
	cfg, err := config.Load(targetConfig)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 2. 挂载本地文件日志 (按大小与时间双维度轮转，默认仅记录 INFO 及以上级别)
	if cfg.Log.IsEnabled() {
		fileLevel := logging.ParseLevel(cfg.Log.Level)
		if *debugFlag {
			fileLevel = slog.LevelDebug
		}
		rotateInterval, rotErr := time.ParseDuration(cfg.Log.RotateInterval)
		if rotErr != nil {
			slog.Warn("日志轮转周期配置无效，已回退为关闭时间轮转", "value", cfg.Log.RotateInterval, "error", rotErr)
			rotateInterval = 0
		}

		fileHandler, logCloser, fErr := logging.NewFileHandler(logging.Config{
			Dir:            cfg.Log.Dir,
			FileName:       cfg.Log.File,
			Level:          fileLevel,
			MaxSizeBytes:   int64(cfg.Log.MaxSizeMB) * 1024 * 1024,
			RotateInterval: rotateInterval,
			MaxBackups:     cfg.Log.MaxBackups,
			MaxAge:         time.Duration(cfg.Log.MaxAgeDays) * 24 * time.Hour,
			JSON:           strings.EqualFold(cfg.Log.Format, "json"),
		})
		if fErr != nil {
			slog.Warn("本地文件日志初始化失败，将继续仅输出到控制台", "error", fErr)
		} else {
			multiH.handlers = append(multiH.handlers, fileHandler)
			slog.SetDefault(slog.New(multiH))
			defer logCloser.Close()
			slog.Info("已启用本地文件日志",
				"dir", cfg.Log.Dir,
				"file", cfg.Log.File,
				"level", fileLevel.String(),
				"max_size_mb", cfg.Log.MaxSizeMB,
				"rotate_interval", cfg.Log.RotateInterval,
			)
		}
	}

	// 3. 初始化数据字典仓储与智能题库问答引擎
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Warn("未检测到本地游戏数据字典，部分元数据查询将降级", "error", err)
	} else {
		slog.Info("已成功挂载老版游戏数据字典 Pieb.db")
	}

	qaEngine, err := qa.NewEngine("")
	if err != nil {
		slog.Warn("未检测到本地问答题库，仙履答题将以默认模式运行", "error", err)
	} else {
		slog.Info("已成功加载智能题库问答引擎", "total_questions", qaEngine.Size())
	}

	// 4. 初始化角色会话与多角色调度器
	mgr := &sessionManager{}
	dispatcher := scheduler.NewDispatcher()

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	if fwAuth, err := platform.NewFengwanAuthenticator(); err == nil {
		ticketCache.RegisterAuthenticator(fwAuth)
	}
	xdAuth, err := platform.NewXindongAuthenticator()
	if err == nil {
		ticketCache.RegisterAuthenticator(xdAuth)
	}

	// 阶段一：纯内存构建启动计划 (去重、网关解析、区服覆盖合并、工厂装配)。
	plans := buildRolePlans(cfg, qaEngine, dictRepo)
	slog.Info("已生成多区服启动计划", "role_count", len(plans))

	// 阶段二：串行换票，避免同一账号短时间多次 Web 登录触发风控。
	fetchAllCredentials(ctx, plans, cfg.Scheduler.Startup, ticketCache, xdAuth)

	// 阶段三：装配会话与独立调度器，然后并发启动会话。
	routineMgr := newRoutineManager(buildRoutineEnabledMap(cfg.Scheduler))
	startable := registerPlans(plans, mgr, dispatcher, routineMgr)
	// 先完成登录与登录后置任务（如渡业台抽取），再启动调度器，
	// 确保对 PrevAct 链敏感的协议是会话激活后的第一批业务请求。
	startPlans(ctx, startable)
	dispatcher.StartAll(ctx)

	// 每个角色按其生效的 cron_times 独立唤醒 Cron 玩法。
	cronTimesByRole := make(map[string][]string, len(plans))
	for _, p := range plans {
		cronTimesByRole[p.roleID] = p.eff.CronTimes
	}
	go runCronTicker(ctx, dispatcher, cronTimesByRole)

	// 5. 启动 Web 控制台服务并注入玩法管理提供器
	webServer := web.NewServer(cfg.Server.Host, cfg.Server.Port, mgr, logBuf, Version)
	webServer.SetRoutineProvider(routineMgr)
	if err := webServer.Start(ctx); err != nil {
		return fmt.Errorf("启动 Web 控制台失败: %w", err)
	}

	slog.Info("服务就绪，等待运行信号 (按 Ctrl+C 退出)")
	<-ctx.Done()

	slog.Info("收到停机信号，正在优雅关闭服务...")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	_ = webServer.Shutdown(shutdownCtx)
	dispatcher.StopAll()
	mgr.closeAll()

	slog.Info("神仙道助手已安全退出")
	return nil
}

func runBrowserLogin(ctx context.Context, configPath string) error {
	slog.Info("正在拉起浏览器交互式登录助手以获取/刷新 Cookie...", "config", configPath)
	scriptPaths := []string{
		"scripts/browser_login.py",
		"../scripts/browser_login.py",
	}
	scriptPath := ""
	for _, p := range scriptPaths {
		if _, err := os.Stat(p); err == nil {
			scriptPath = p
			break
		}
	}
	if scriptPath == "" {
		return fmt.Errorf("未找到交互登录脚本 scripts/browser_login.py")
	}

	cmd := exec.CommandContext(ctx, "python3", scriptPath, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
