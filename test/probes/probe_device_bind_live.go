//go:build probe

// 目标协议: [Module 814 / 0x32E] Mod_DeviceBindRewards_Base 设备/手机绑定奖励
//
//	get_bind_info       Act 0 -> 0x032E0000 空请求
//	  response: UByte result, Byte is_bind, String phone_num, Byte first_reward,
//	            Byte monthly_reward, Int verify_cd, Byte need_verify_bind, Int start_time
//	get_first_reward    Act 5 -> 0x032E0005 空请求, 回包 UByte result (200元宝)
//	get_monthly_reward  Act 6 -> 0x032E0006 空请求, 回包 UByte result (50元宝)
//
// 基准对齐: com.protocols.Mod_DeviceBindRewards_Base /
//
//	com.datas.DeviceBindRewardsData / com.assist.server.DeviceBindRewardsType
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_device_bind_live.go [-execute]
//
// 预期表现: 只读打印绑定状态与首绑/月度奖励领取标记；-execute 仅在已绑定且
//
//	可领取时调用领取接口。绑定/解绑/验证码接口需要手机短信，探针不调用。
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
)

const (
	actionDeviceBindInfo    uint32 = 0x032E0000
	actionDeviceBindFirst   uint32 = 0x032E0005
	actionDeviceBindMonthly uint32 = 0x032E0006
)

func main() {
	execute := flag.Bool("execute", false, "实际领取已绑定设备的首绑/月度奖励")
	flag.Parse()
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	before := sess.GetPlayerState().Ingots
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID(), "ingots", before)

	call := func(act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, nil)
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("调用失败: "+name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		slog.Info("回包: "+name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		return resp
	}

	resp := call(actionDeviceBindInfo, "get_bind_info")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	result, e1 := r.ReadUint8()
	isBind, e2 := r.ReadUint8()
	phone, e3 := r.ReadString()
	firstReward, e4 := r.ReadUint8()
	monthlyReward, e5 := r.ReadUint8()
	verifyCD, e6 := r.ReadInt32()
	needVerify, e7 := r.ReadUint8()
	startTime, e8 := r.ReadInt32()
	if err := firstErr(e1, e2, e3, e4, e5, e6, e7, e8); err != nil {
		slog.Error("解析绑定信息失败", "err", err, "payload_len", len(resp.Payload))
		return
	}
	slog.Info("设备绑定状态",
		"result", result,
		"is_bind", isBind,
		"phone", phone,
		"first_reward", firstReward,
		"monthly_reward", monthlyReward,
		"verify_cd", verifyCD,
		"need_verify_bind", needVerify,
		"start_time", startTime,
		"remaining", r.Remaining(),
	)

	if !*execute {
		slog.Info("只读模式结束；如需领取请追加 -execute")
		return
	}
	if result != 0 || isBind == 0 || needVerify == 1 {
		slog.Info("账号未完成绑定/验证，无法领取 (需手机短信，探针不代操作)",
			"is_bind", isBind, "need_verify_bind", needVerify, "result", result)
		return
	}
	if firstReward == 0 {
		if resp := call(actionDeviceBindFirst, "get_first_reward"); resp != nil && len(resp.Payload) > 0 {
			slog.Info("首绑奖励领取结果", "result", resp.Payload[0])
		}
	}
	if monthlyReward == 0 {
		if resp := call(actionDeviceBindMonthly, "get_monthly_reward"); resp != nil && len(resp.Payload) > 0 {
			slog.Info("月度奖励领取结果", "result", resp.Payload[0])
		}
	}
	slog.Info("资产变化", "ingots_before", before, "ingots_after", sess.GetPlayerState().Ingots,
		"delta", sess.GetPlayerState().Ingots-before)
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
