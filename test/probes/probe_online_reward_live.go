//go:build probe

// 目标协议: [Module 712 / 0x2C8] Mod_OnlineReward_Base 在线奖励 (官方 Main.swf 反编译基准)
//
//	main_panel   Act 0 -> 0x02C80000  空请求
//	  response: Int start_time, Int end_time, Byte day,
//	            [ award_id Int, award_item_list [item_id Int, item_num Long, is_big_award Byte],
//	              is_get Byte, need_online_time Int ],
//	            Int online_time
//	one_key_get  Act 1 -> 0x02C80001  空请求, 回包 UByte result + [Int award_id]
//
// 基准对齐: com.protocols.Mod_OnlineReward_Base / com.datas.OnlineRewardData
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_online_reward_live.go [-execute]
//
// 预期表现: 默认只读打印活动时间、在线时长与各档奖励领取状态；
// 显式 -execute 时调用一键领取并打印结果码与领取到的档位。
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
	actionOnlineRewardPanel  uint32 = 0x02C80000
	actionOnlineRewardOneKey uint32 = 0x02C80001
)

func main() {
	execute := flag.Bool("execute", false, "实际一键领取在线奖励")
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

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

	resp := call(actionOnlineRewardPanel, "main_panel")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	startTime, e1 := r.ReadInt32()
	endTime, e2 := r.ReadInt32()
	day, e3 := r.ReadUint8()
	count, e4 := r.ReadUint16()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		slog.Error("解析在线奖励主面板失败", "err", fmt.Sprint(e1, e2, e3, e4))
		return
	}
	type rewardItem struct {
		ItemID     int32
		Num        int64
		IsBigAward uint8
	}
	type rewardTier struct {
		AwardID        int32
		IsGet          uint8
		NeedOnlineTime int32
		Items          []rewardItem
	}
	tiers := make([]rewardTier, 0, count)
	for i := 0; i < int(count); i++ {
		var tier rewardTier
		if tier.AwardID, err = r.ReadInt32(); err != nil {
			break
		}
		itemCount, err := r.ReadUint16()
		if err != nil {
			break
		}
		for j := 0; j < int(itemCount); j++ {
			var it rewardItem
			if it.ItemID, err = r.ReadInt32(); err != nil {
				break
			}
			if it.Num, err = r.ReadInt64(); err != nil {
				break
			}
			if it.IsBigAward, err = r.ReadUint8(); err != nil {
				break
			}
			tier.Items = append(tier.Items, it)
		}
		if tier.IsGet, err = r.ReadUint8(); err != nil {
			break
		}
		if tier.NeedOnlineTime, err = r.ReadInt32(); err != nil {
			break
		}
		tiers = append(tiers, tier)
	}
	onlineTime, _ := r.ReadInt32()
	slog.Info("在线奖励面板",
		"start_time", startTime,
		"end_time", endTime,
		"day", day,
		"online_time", onlineTime,
		"tiers", tiers,
		"remaining", r.Remaining(),
	)

	if !*execute {
		slog.Info("只读模式结束；如需一键领取在线奖励请追加 -execute")
		return
	}
	if startTime == 0 && endTime == 0 {
		slog.Info("在线奖励活动未开放，跳过领取")
		return
	}
	gResp := call(actionOnlineRewardOneKey, "one_key_get")
	if gResp == nil {
		return
	}
	gr := protocol.NewReader(gResp.Payload)
	result, _ := gr.ReadUint8()
	awardCount, _ := gr.ReadUint16()
	awardIDs := make([]int32, 0, awardCount)
	for i := 0; i < int(awardCount); i++ {
		id, err := gr.ReadInt32()
		if err != nil {
			break
		}
		awardIDs = append(awardIDs, id)
	}
	slog.Info("在线奖励一键领取结果", "result", result, "award_ids", awardIDs, "remaining", gr.Remaining())
}
