//go:build probe

// 目标协议: [Module 503 / 0x1F7] Mod_AskFairy_Base 问灵/回归福利
//
//	get_recall_info  Act 0 -> 0x01F70000 空请求
//	  response: Int first_login_time, Int total_login_day, Int partner_num, Int level,
//	            Long fight_power, Int has_gain_award, Int is_shared, Int max_ask_num,
//	            Int has_ask_num, Int random_sign_id,
//	            [Int award_id, Int item_id, Int item_count] random_award,
//	            Int left_day, Int mount_count, Int title_count, Int cur_level
//	gain_recall_award Act 1 -> 0x01F70001 空请求
//	do_ask_fairy      Act 3 -> 0x01F70003 空请求
//	gain_ask_award    Act 4 -> 0x01F70004  Int award_id
//	gain_ask_level_award Act 7 -> 0x01F70007 空请求
//
// 基准对齐: com.protocols.Mod_AskFairy_Base / com.datas.AskFairyData /
//
//	com.assist.server.source.AskFairyTypeData
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_ask_fairy_live.go [-execute]
//
// 预期表现: 只读打印问灵状态；-execute 在仍有次数时问灵，刷新出随机奖励后
//
//	优先选择元宝 (item_id=3345)，否则选第一项；并领取可领的回归/等级奖励。
package main

import (
	"context"
	"encoding/binary"
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
	actionAskFairyInfo       uint32 = 0x01F70000
	actionAskFairyRecall     uint32 = 0x01F70001
	actionAskFairyDo         uint32 = 0x01F70003
	actionAskFairyGain       uint32 = 0x01F70004
	actionAskFairyLevelAward uint32 = 0x01F70007
)

type askFairyInfo struct {
	HasGainAward int32
	MaxAskNum    int32
	HasAskNum    int32
	LeftDay      int32
	CurLevel     int32
	RandomAwards [][3]int32
}

func main() {
	execute := flag.Bool("execute", false, "实际问灵并领取奖励")
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

	call := func(payload []byte, act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, payload)
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

	fetchInfo := func() *askFairyInfo {
		resp := call(nil, actionAskFairyInfo, "get_recall_info")
		if resp == nil {
			return nil
		}
		r := protocol.NewReader(resp.Payload)
		if _, err := r.ReadInt32(); err != nil { // first_login_time
			return nil
		}
		if _, err := r.ReadInt32(); err != nil { // total_login_day
			return nil
		}
		if _, err := r.ReadInt32(); err != nil { // partner_num
			return nil
		}
		if _, err := r.ReadInt32(); err != nil { // level
			return nil
		}
		if _, err := r.ReadInt64(); err != nil { // fight_power
			return nil
		}
		info := &askFairyInfo{}
		info.HasGainAward, _ = r.ReadInt32()
		_, _ = r.ReadInt32() // is_shared
		info.MaxAskNum, _ = r.ReadInt32()
		info.HasAskNum, _ = r.ReadInt32()
		_, _ = r.ReadInt32() // random_sign_id
		count, _ := r.ReadUint16()
		for i := 0; i < int(count); i++ {
			awardID, _ := r.ReadInt32()
			itemID, _ := r.ReadInt32()
			itemCount, _ := r.ReadInt32()
			info.RandomAwards = append(info.RandomAwards, [3]int32{awardID, itemID, itemCount})
		}
		info.LeftDay, _ = r.ReadInt32()
		_, _ = r.ReadInt32() // mount_count
		_, _ = r.ReadInt32() // title_count
		info.CurLevel, _ = r.ReadInt32()
		slog.Info("问灵状态",
			"has_gain_award", info.HasGainAward,
			"max_ask_num", info.MaxAskNum,
			"has_ask_num", info.HasAskNum,
			"left_day", info.LeftDay,
			"cur_level", info.CurLevel,
			"random_awards", info.RandomAwards,
		)
		return info
	}

	info := fetchInfo()
	if info == nil {
		return
	}
	if !*execute {
		slog.Info("只读模式结束；如需问灵请追加 -execute")
		return
	}

	if info.HasGainAward == 0 {
		if resp := call(nil, actionAskFairyRecall, "gain_recall_award"); resp != nil && len(resp.Payload) > 0 {
			slog.Info("回归奖励领取结果", "result", resp.Payload[0])
		}
	}
	if resp := call(nil, actionAskFairyLevelAward, "gain_ask_level_award"); resp != nil && len(resp.Payload) > 0 {
		slog.Info("问灵等级奖励领取结果", "result", resp.Payload[0])
	}

	if info.HasAskNum <= info.MaxAskNum { // 语义待实机确认: 尝试一次问灵
		if resp := call(nil, actionAskFairyDo, "do_ask_fairy"); resp != nil && len(resp.Payload) > 0 {
			slog.Info("问灵结果", "result", resp.Payload[0])
			if resp.Payload[0] == 0 { // SUCCESS
				info = fetchInfo()
			}
		}
	} else {
		slog.Info("今日问灵次数已用尽", "has_ask", info.HasAskNum, "max_ask", info.MaxAskNum)
	}

	if info != nil && len(info.RandomAwards) > 0 {
		chosen := info.RandomAwards[0]
		for _, a := range info.RandomAwards {
			if a[1] == 3345 { // 优先元宝
				chosen = a
				break
			}
		}
		payload := make([]byte, 4)
		binary.BigEndian.PutUint32(payload, uint32(chosen[0]))
		if resp := call(payload, actionAskFairyGain, fmt.Sprintf("gain_ask_award(award_id=%d)", chosen[0])); resp != nil && len(resp.Payload) > 0 {
			slog.Info("问灵奖励领取结果", "result", resp.Payload[0], "award_id", chosen[0], "item_id", chosen[1], "count", chosen[2])
		}
	}
	after := sess.GetPlayerState().Ingots
	slog.Info("资产变化", "ingots_before", before, "ingots_after", after, "delta", after-before)
}
