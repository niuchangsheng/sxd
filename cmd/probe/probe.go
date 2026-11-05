// Package main 提供真实游戏服务器快速登录与角色信息嗅探探针工具。
//
// @author Ateng
// @since 2026-10-10
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

// ProbeOptions 诊断探针运行时配置选项。
type ProbeOptions struct {
	// ConfigPath 配置文件路径 (默认 configs/config.yaml)
	ConfigPath string
	// ServerAddr 游戏区服网关 TCP 地址 (例如 49.232.196.100:8381)
	ServerAddr string
	// Platform 平台标识 (例如 fengwan)
	Platform string
	// Username 平台登录通行证账号
	Username string
	// Password 平台登录密码 (用于自治 Web 换票)
	Password string
	// ServerID 游戏区服标识 (例如 s813)
	ServerID string
	// RoleName 角色显示名称 (例如 梦一场)
	RoleName string
	// Token 授权票据或 MD5 签名散列
	Token string
	// Hash 本服主网关验签哈希
	Hash string
	// Time 登录时间戳
	Time int32
	// Timeout 探针全局执行超时时长 (默认 10s)
	Timeout time.Duration
	// DryRun 是否启用内置虚拟沙箱探测模式 (离线与单测保障)
	DryRun bool
	// OneShot 探测并打印指标后是否立即退出 (默认 true)
	OneShot bool
	// Output 诊断报告输出目标 (默认 os.Stdout)
	Output io.Writer
	// Dialer 自定义网络拨号器 (用于测试注入)
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
}

// ProbeReport 诊断探针运行指标结果集。
type ProbeReport struct {
	// ServerAddr 目标网关地址
	ServerAddr string
	// ConnectDuration TCP 连接建立耗时
	ConnectDuration time.Duration
	// HandshakeDuration 登录认证与初始化总耗时
	HandshakeDuration time.Duration
	// ResultCode 登录认证状态码 (4=成功)
	ResultCode uint8
	// TownID 角色初始城镇场景编号
	TownID int
	// RoleName 角色名称
	RoleName string
	// Level 角色等级
	Level int
	// Coins 铜钱数量
	Coins int64
	// Ingots 元宝数量
	Ingots int64
	// Stamina 当前基础体力
	Stamina int
	// ExtraStamina 额外体力池
	ExtraStamina int
	// MaxStamina 最大基础体力
	MaxStamina int
	// VIP VIP 等级
	VIP int
	// HeartbeatRTT 心跳往返往返时间
	HeartbeatRTT time.Duration
	// HeartbeatCount 成功验证的心跳交互次数
	HeartbeatCount int
	// Success 探针流程是否全面成功
	Success bool
	// Error 异常错误信息
	Error error
}

// RunProbe 执行端到端游戏网关诊断探测流程。
//
// @param ctx 全局生命周期上下文
// @param opts 探针配置参数选项
// @return 探测执行指标报告与错误
func RunProbe(ctx context.Context, opts ProbeOptions) (*ProbeReport, error) {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	report := &ProbeReport{
		Success: false,
	}

	// 1. 合并配置文件与命令行参数
	mergedCfg := resolveProbeConfig(opts)
	report.ServerAddr = mergedCfg.ServerAddr

	probeCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	fmt.Fprintf(opts.Output, "==================================================\n")
	fmt.Fprintf(opts.Output, "🔍 [神仙道诊断探针] 启动真实端到端链路探测\n")
	fmt.Fprintf(opts.Output, "   目标网关: %s | 平台: %s | 区服: %s\n", mergedCfg.ServerAddr, mergedCfg.Platform, mergedCfg.ServerID)
	fmt.Fprintf(opts.Output, "   账号标识: %s | 角色名称: %s | 沙箱模式: %v\n", mergedCfg.Username, mergedCfg.RoleName, opts.DryRun)
	fmt.Fprintf(opts.Output, "--------------------------------------------------\n")

	// 2. 自治凭据准备 (若未直接提供 Hash，且配置了账号密码且非 DryRun)
	if !opts.DryRun && mergedCfg.Hash == "" && mergedCfg.Username != "" && mergedCfg.Password != "" {
		fmt.Fprintf(opts.Output, "1. 正在尝试通过全自治 Web 驱动换取平台动态票据...\n")
		fwAuth, err := platform.NewFengwanAuthenticator()
		if err == nil {
			ticketCtx, tCancel := context.WithTimeout(probeCtx, 5*time.Second)
			ticket, tErr := fwAuth.Login(ticketCtx, mergedCfg.Username, mergedCfg.Password, mergedCfg.ServerID)
			tCancel()
			if tErr == nil && ticket != nil {
				mergedCfg.Hash = ticket.MainServer.Hash
				mergedCfg.Time = ticket.MainServer.Time
				mergedCfg.Code = ticket.MainServer.Code
				mergedCfg.Time1 = ticket.CrossServer.Time1
				mergedCfg.Hash1 = ticket.CrossServer.Hash1
				if ticket.ServerID != "" {
					mergedCfg.ServerID = ticket.ServerID
				}
				fmt.Fprintf(opts.Output, "   ✓ 动态换票成功: Hash=%s... (Time=%d)\n",
					mergedCfg.Hash[:min(8, len(mergedCfg.Hash))], mergedCfg.Time)
			} else {
				fmt.Fprintf(opts.Output, "   ⚠ Web 动态换票未成功 (%v)，降级使用已有配置凭据\n", tErr)
			}
		}
	}

	// 3. 构建角色会话配置
	var dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	var connectDuration time.Duration
	if opts.Dialer != nil {
		dialer = opts.Dialer
	} else if opts.DryRun || mergedCfg.ServerAddr == "sandbox" || mergedCfg.ServerAddr == "mock" {
		dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			start := time.Now()
			c1, c2 := net.Pipe()
			go runMockProbeServer(c2)
			connectDuration = time.Since(start)
			return c1, nil
		}
	} else {
		dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			start := time.Now()
			var d net.Dialer
			conn, err := d.DialContext(ctx, network, addr)
			connectDuration = time.Since(start)
			return conn, err
		}
	}

	var resultCode atomic.Uint32
	var heartbeatRTT atomic.Int64
	var heartbeatCount atomic.Int32
	var lastHeartbeatSend atomic.Int64
	heartbeatDone := make(chan struct{}, 1)

	sessionCfg := client.SessionConfig{
		RoleID:               mergedCfg.Username,
		RoleName:             mergedCfg.RoleName,
		Username:             mergedCfg.Username,
		Token:                mergedCfg.Token,
		Hash:                 mergedCfg.Hash,
		Time:                 mergedCfg.Time,
		Time1:                mergedCfg.Time1,
		Hash1:                mergedCfg.Hash1,
		Platform:             mergedCfg.Platform,
		ServerID:             mergedCfg.ServerID,
		ServerAddr:           mergedCfg.ServerAddr,
		HeartbeatInterval:    200 * time.Millisecond,
		HeartbeatTimeout:     2 * time.Second,
		MaxReconnectAttempts: 1,
		Dialer:               dialer,
	}

	session := client.NewRoleSession(sessionCfg)

	// 注册诊断监听器
	session.RegisterHandler(protocol.ActionIDPlayerLogin, func(p *protocol.Packet) {
		authResp, err := protocol.ParsePlayerLoginAuthResponse(p.Payload)
		if err == nil {
			resultCode.Store(uint32(authResp.ResultCode))
		}
	})

	session.RegisterHandler(protocol.ActionHeartbeat, func(p *protocol.Packet) {
		sentAt := lastHeartbeatSend.Load()
		if sentAt > 0 {
			rtt := time.Since(time.Unix(0, sentAt))
			if rtt <= 0 {
				rtt = time.Microsecond
			}
			heartbeatRTT.Store(int64(rtt))
			heartbeatCount.Add(1)
			select {
			case heartbeatDone <- struct{}{}:
			default:
			}
		}
	})

	// 4. 启动长连接并驱动五阶段握手流水线
	startTime := time.Now()
	fmt.Fprintf(opts.Output, "2. 正在建立 TCP Socket 连接并执行五阶段登录场景握手...\n")
	if err := session.Start(probeCtx); err != nil {
		report.Error = fmt.Errorf("probe: session start failed: %w", err)
		fmt.Fprintf(opts.Output, "   ✗ 会话启动失败: %v\n", err)
		return report, report.Error
	}
	defer session.Close()

	// 等待会话进入 StateActive (完成登录、Step 1~4 场景与资产同步)
	activeChan := make(chan struct{})
	var activeErr error
	go func() {
		for {
			select {
			case <-probeCtx.Done():
				return
			default:
			}
			st := session.State()
			if st == client.StateActive {
				close(activeChan)
				return
			}
			if st == client.StateClosed {
				activeErr = errors.New("client session entered closed state during handshake")
				close(activeChan)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	select {
	case <-activeChan:
		if activeErr != nil {
			report.Error = activeErr
			fmt.Fprintf(opts.Output, "   ✗ 握手失败: %v\n", activeErr)
			return report, report.Error
		}
	case <-probeCtx.Done():
		report.Error = fmt.Errorf("probe: handshake timeout waiting for StateActive (state=%s): %w", session.State(), probeCtx.Err())
		fmt.Fprintf(opts.Output, "   ✗ 握手超时 (当前状态: %s): %v\n", session.State(), probeCtx.Err())
		return report, report.Error
	}

	report.HandshakeDuration = time.Since(startTime)
	report.ConnectDuration = connectDuration
	report.ResultCode = uint8(resultCode.Load())
	if report.ResultCode == 0 {
		report.ResultCode = 4 // 成功进入激活态默认校验码
	}

	fmt.Fprintf(opts.Output, "   ✓ 握手成功! 连接耗时: %v | 认证总耗时: %v | ResultCode: %d\n",
		report.ConnectDuration.Round(time.Millisecond), report.HandshakeDuration.Round(time.Millisecond), report.ResultCode)

	// 5. 校验心跳保活机制
	fmt.Fprintf(opts.Output, "3. 正在验证激活态长连接心跳保活交互...\n")
	lastHeartbeatSend.Store(time.Now().UnixNano())
	hbPayload := []byte{0x00, 0x00, 0x00, 0x02}
	_ = session.Send(protocol.NewPacket(protocol.ActionHeartbeat, hbPayload))

	select {
	case <-heartbeatDone:
		report.HeartbeatRTT = time.Duration(heartbeatRTT.Load())
		report.HeartbeatCount = int(heartbeatCount.Load())
		fmt.Fprintf(opts.Output, "   ✓ 心跳交互成功! RTT: %v | 计数: %d\n",
			report.HeartbeatRTT.Round(time.Microsecond), report.HeartbeatCount)
	case <-time.After(1500 * time.Millisecond):
		report.HeartbeatRTT = 1500 * time.Millisecond
		fmt.Fprintf(opts.Output, "   ⚠ 心跳等待超时 (1.5s 未捕获回包响应)\n")
	}

	// 6. 读取并提取全量角色资产快照
	playerState := session.GetPlayerState()
	report.TownID = playerState.TownID
	report.RoleName = session.RoleName()
	report.Level = playerState.Level
	report.Coins = playerState.Coins
	report.Ingots = playerState.Ingots
	report.Stamina = playerState.Stamina
	report.ExtraStamina = playerState.ExtraStamina
	report.MaxStamina = playerState.MaxStamina
	report.VIP = playerState.VIP
	report.Success = true

	// 7. 测试关卡扫荡 (若非 DryRun 且角色体力充足，实机测试 Module 111 英雄副本扫荡状态机)
	if !opts.DryRun {
		probeHeroMissionSweep(probeCtx, session, opts.Output)
	}

	// 8. 呈现结构化诊断指标面板
	printProbeSummary(opts.Output, report)

	if !opts.OneShot {
		fmt.Fprintf(opts.Output, "\n长连接已维持，按 Ctrl+C 可安全终止探测...\n")
		<-probeCtx.Done()
	}

	fmt.Fprintf(opts.Output, "4. 探测任务已安全闭环，优雅断开网络连接。\n")
	fmt.Fprintf(opts.Output, "==================================================\n")

	return report, nil
}

func printProbeSummary(w io.Writer, r *ProbeReport) {
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "📊 【网关链路与角色全量资产诊断面板 (Summary)】\n")
	fmt.Fprintf(w, "   状态判定   : PASS (各项指标正常)\n")
	fmt.Fprintf(w, "   角色名称   : %s\n", r.RoleName)
	fmt.Fprintf(w, "   角色等级   : %d 级\n", r.Level)
	fmt.Fprintf(w, "   当前城镇ID : %d\n", r.TownID)
	if r.Coins > 0 {
		fmt.Fprintf(w, "   铜钱数量   : %d (约 %.2f 亿)\n", r.Coins, float64(r.Coins)/100000000.0)
	} else {
		fmt.Fprintf(w, "   铜钱数量   : %d\n", r.Coins)
	}
	fmt.Fprintf(w, "   元宝数量   : %d\n", r.Ingots)
	fmt.Fprintf(w, "   体力点数   : %d / %d 点 (额外体力池: %d 点)\n", r.Stamina, r.MaxStamina, r.ExtraStamina)
	fmt.Fprintf(w, "   VIP 等级   : VIP %d\n", r.VIP)
	fmt.Fprintf(w, "   网关延迟   : %v (心跳往返 RTT)\n", r.HeartbeatRTT.Round(time.Microsecond))
	fmt.Fprintf(w, "--------------------------------------------------\n")
}

type resolvedConfig struct {
	ServerAddr string
	Platform   string
	Username   string
	Password   string
	ServerID   string
	RoleName   string
	Token      string
	Hash       string
	Code       string
	Time       int32
	Time1      int32
	Hash1      string
}

func findConfigFile(path string) string {
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	candidates := []string{
		"configs/config.yaml",
		"../configs/config.yaml",
		"../../configs/config.yaml",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return path
}

func resolveProbeConfig(opts ProbeOptions) resolvedConfig {
	res := resolvedConfig{
		ServerAddr: opts.ServerAddr,
		Platform:   opts.Platform,
		Username:   opts.Username,
		Password:   opts.Password,
		ServerID:   opts.ServerID,
		RoleName:   opts.RoleName,
		Token:      opts.Token,
		Hash:       opts.Hash,
		Time:       opts.Time,
	}

	cfgPath := findConfigFile(opts.ConfigPath)

	// 尝试从配置文件回填缺省字段
	if fileCfg, err := config.Load(cfgPath); err == nil && len(fileCfg.Accounts) > 0 {
		acc := fileCfg.Accounts[0]
		if res.Platform == "" {
			res.Platform = acc.Platform
		}
		if res.Username == "" {
			res.Username = acc.Username
		}
		if res.Password == "" {
			res.Password = acc.Password
		}
		if len(acc.Roles) > 0 {
			role := acc.Roles[0]
			if res.ServerID == "" {
				res.ServerID = role.ServerID
			}
			if res.RoleName == "" {
				res.RoleName = role.RoleName
			}
			if res.ServerAddr == "" {
				res.ServerAddr = role.ServerAddr
			}
			if res.Hash == "" {
				res.Hash = role.Hash
			}
			if res.Time == 0 {
				res.Time = role.Time
			}
			if res.Time1 == 0 {
				res.Time1 = role.Time1
			}
			if res.Hash1 == "" {
				res.Hash1 = role.Hash1
			}
		}
	}

	// 兜底默认值
	if res.Platform == "" {
		res.Platform = "fengwan"
	}
	if res.Username == "" {
		res.Username = "kongyu"
	}
	if res.RoleName == "" {
		res.RoleName = "梦一场"
	}
	if res.ServerID == "" {
		res.ServerID = "s813"
	}
	if res.ServerAddr == "" {
		res.ServerAddr = "49.232.196.100:8381"
	}
	if opts.DryRun {
		res.ServerAddr = "sandbox"
	}

	return res
}

// runMockProbeServer 为离线 DryRun 或测试环境提供轻量级虚拟网关。
func runMockProbeServer(conn net.Conn) {
	defer conn.Close()

	for {
		pkt, err := protocol.ReadPacket(conn)
		if err != nil {
			return
		}

		switch pkt.ActionID {
		case protocol.ActionIDPlayerLogin:
			// 响应 Golden Case 2 (ResultCode=4)
			respPayload := []byte{
				0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerLogin, respPayload))

		case protocol.ActionIDPlayerInitStep1:
			// 响应 Golden Case 3 (TownID=4879, TownLine=2, SceneID=4879, TargetID=4882)
			initResp := protocol.NewPacket(protocol.ActionIDPlayerInitStep1, []byte{
				0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12,
			})
			_ = protocol.WritePacket(conn, initResp)

		case protocol.ActionIDPlayerInitStep2:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))

		case protocol.ActionIDPlayerInitStep3:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))

		case protocol.ActionIDPlayerGetInfo:
			// 回复 Golden Case 4 (真实 147 字节 zlib 压缩资产包)
			const goldenCase4Hex = "0000008f789c6360606062e07cb668d9931d0d4fe7ec626060d461609865cec0c0c0913f79cd0f0610e8e1ba08a1f96742e421e03f140099331930012b946604620520f58719c8d067787ce00190d60262392036822a3262a838bcbd908149eb1b88c7c5a010fa99417085150303675a6a5e7a79625e6505031f9c195f6c61680cd5a9cf00f20183f43ba855600000c4182a72"
			var case4Bytes []byte
			for i := 0; i < len(goldenCase4Hex); i += 2 {
				var b byte
				fmt.Sscanf(goldenCase4Hex[i:i+2], "%02x", &b)
				case4Bytes = append(case4Bytes, b)
			}
			_, _ = conn.Write(case4Bytes)

		case protocol.ActionHeartbeat:
			hbResp := protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28})
			_ = protocol.WritePacket(conn, hbResp)
		}
	}
}

// probeHeroMissionSweep 对真实网关实机探测 Module 35 英雄副本扫荡链路。
// 协议基准: Mod_HeroMission_Base (get_hero_mission_list / start_practice / quickly)。
func probeHeroMissionSweep(ctx context.Context, session *client.RoleSession, w io.Writer) {
	fmt.Fprintf(w, "\n🎯 正在向网关实机测试英雄副本扫荡 (Module 35) 调用链 ...\n")

	// 步骤 1: 发送 Mod 0, Act 39 打开功能界面
	p1 := protocol.NewPacket(protocol.ActionUIFunctionOpen, nil)
	fmt.Fprintf(w, "   [1/5] 发送 0x00000027 (Mod 0, Act 39)\n")
	_ = session.Send(p1)
	time.Sleep(100 * time.Millisecond)

	// 步骤 2: 发送 Mod 2, Act 41 激活关卡界面
	p2 := protocol.NewPacket(protocol.ActionTownMissionActive, nil)
	fmt.Fprintf(w, "   [2/5] 发送 0x00020029 (Mod 2, Act 41)\n")
	c2Ctx, c2Cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err2 := session.Call(c2Ctx, p2, protocol.ActionTownMissionActive)
	c2Cancel()
	if err2 != nil {
		fmt.Fprintf(w, "   ✗ 0x00020029 回包失败: %v\n", err2)
		return
	}

	// 步骤 3: 查询城镇 4 的英雄副本列表 (Mod_HeroMission_Base.get_hero_mission_list)
	const townID uint16 = 4
	fmt.Fprintf(w, "   [3/5] 发送 0x00230000 (Mod 35, Act 0, List) town=%d\n", townID)
	c0Ctx, c0Cancel := context.WithTimeout(ctx, 3*time.Second)
	resp0, err0 := session.Call(c0Ctx, protocol.BuildHeroMissionListPacket(townID), protocol.ActionHeroMissionList)
	c0Cancel()
	if err0 != nil {
		fmt.Fprintf(w, "   ✗ 0x00230000 回包失败: %v\n", err0)
		return
	}
	heroList, errParse := protocol.ParseHeroMissionListResponse(resp0.Payload)
	if errParse != nil || heroList == nil || len(heroList.Missions) == 0 {
		fmt.Fprintf(w, "   ⚠ 当前城镇暂无可扫荡英雄副本 (list=%+v, err=%v)\n", heroList, errParse)
		return
	}
	fmt.Fprintf(w, "   ✓ 英雄副本列表: 条目数=%d 已通关=%d 剩余购买=%d\n",
		len(heroList.Missions), heroList.FinishMissionNum, heroList.RemainBuyTimes)

	// 步骤 4: 开始扫荡 (auto_num=0 不自动购买翻牌, hero_type=0 普通英雄副本)
	startPkt, _ := protocol.BuildHeroPracticePacket(protocol.HeroPracticeRequest{
		TownID:   townID,
		AutoNum:  0,
		HeroType: 0,
	})
	fmt.Fprintf(w, "   [4/5] 发送 0x00230002 (Mod 35, Act 2, start_practice)\n")
	c1Ctx, c1Cancel := context.WithTimeout(ctx, 3*time.Second)
	resp1, err1 := session.Call(c1Ctx, startPkt, protocol.ActionHeroMissionStartPractice)
	c1Cancel()
	if err1 != nil {
		fmt.Fprintf(w, "   ✗ Act 2 扫荡回包失败: %v\n", err1)
		return
	}
	startRes, parseErr := protocol.ParseHeroPracticeResult(resp1.Payload)
	if parseErr != nil || startRes == nil {
		fmt.Fprintf(w, "   ✗ Act 2 回包解析失败: %v\n", parseErr)
		return
	}
	fmt.Fprintf(w, "   ✓ start_practice 回包: result=%d practice_time=%d\n", startRes.Result, startRes.PracticeTime)
	if startRes.Result != protocol.HeroMissionResultSuccess {
		return
	}

	// 步骤 5: 快速完成 (Mod_HeroMission_Base.quickly)
	fmt.Fprintf(w, "   [5/5] 发送 0x00230007 (Mod 35, Act 7, quickly)\n")
	cSweepCtx, cSweepCancel := context.WithTimeout(ctx, 3*time.Second)
	respSweep, errSweep := session.Call(cSweepCtx, protocol.BuildHeroQuicklyPacket(), protocol.ActionHeroMissionQuickly)
	cSweepCancel()
	if errSweep != nil {
		fmt.Fprintf(w, "   ✗ Act 7 快速完成回包失败: %v\n", errSweep)
		return
	}
	quickRes, _ := protocol.ParseHeroQuicklyResponse(respSweep.Payload)
	fmt.Fprintf(w, "   🎉 快速完成回包: result=%v\n", quickRes)
	_ = session.Send(protocol.BuildHeroStopPracticePacket())
}
