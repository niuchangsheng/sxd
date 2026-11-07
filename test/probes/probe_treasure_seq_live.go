//go:build probe

// 目标: 定位天帝宝库 set_use_free_flag (Act 4) 被服务端静默丢弃的时序条件。
//
// 生产例程顺序: Act1 -> Act8 -> Act2 -> Act3 -> Act4 (Act4 恒定超时)
// 官方客户端/人工探针顺序: Act1 -> Act2 -> Act9 -> Act8 -> Act1 -> Act3 -> Act4 (Act4 成功)
// 本探针按 -variant 复放不同顺序, 只发送只读面板请求与免费 set_award/Act4, 不抽奖。
//
// 运行命令: go run -tags=probe test/probes/probe_treasure_seq_live.go -config configs/s0.yaml -variant 4
package main

import (
	"context"
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := flag.String("config", "configs/s0.yaml", "配置文件路径")
	variant := flag.Int("variant", 0, "时序变体: 0=1,8,2,3,4 1=1,8,2,9,3,4 2=1,8,2,1,3,4 3=1,2,9,8,1,3,4")
	gap := flag.Duration("gap", 150*time.Millisecond, "动作之间的间隔, 0 表示紧随回包发送")
	beelzebub := flag.Bool("beelzebub", false, "序列前先查询魔王试炼队伍信息 (Mod 57 Act 3), 复现例程前置调用")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

	if *beelzebub {
		c, cc := context.WithTimeout(ctx, 3*time.Second)
		teamID, _, tErr := sess.BeelzebubTrialsGetPlayerTeamInfo(c)
		cc()
		slog.Info("魔王试炼队伍信息", "team_id", teamID, "err", tErr)
		time.Sleep(*gap)
	}

	seqs := map[int][]uint32{
		0: {protocol.ActionIDTreasureHouseMainPanel, protocol.ActionIDTreasureHouseGetFreeItem,
			protocol.ActionIDTreasureHouseSelectPanel, protocol.ActionIDTreasureHouseSetAward,
			protocol.ActionIDTreasureHouseSetUseFreeFlag},
		1: {protocol.ActionIDTreasureHouseMainPanel, protocol.ActionIDTreasureHouseGetFreeItem,
			protocol.ActionIDTreasureHouseSelectPanel, protocol.ActionIDTreasureHouseGetAwardList,
			protocol.ActionIDTreasureHouseSetAward, protocol.ActionIDTreasureHouseSetUseFreeFlag},
		2: {protocol.ActionIDTreasureHouseMainPanel, protocol.ActionIDTreasureHouseGetFreeItem,
			protocol.ActionIDTreasureHouseSelectPanel, protocol.ActionIDTreasureHouseMainPanel,
			protocol.ActionIDTreasureHouseSetAward, protocol.ActionIDTreasureHouseSetUseFreeFlag},
		3: {protocol.ActionIDTreasureHouseMainPanel, protocol.ActionIDTreasureHouseSelectPanel,
			protocol.ActionIDTreasureHouseGetAwardList, protocol.ActionIDTreasureHouseGetFreeItem,
			protocol.ActionIDTreasureHouseMainPanel, protocol.ActionIDTreasureHouseSetAward,
			protocol.ActionIDTreasureHouseSetUseFreeFlag},
		// 4: 先设置免费优先, 再查询/设定自选大奖 (生产修复顺序)
		4: {protocol.ActionIDTreasureHouseMainPanel, protocol.ActionIDTreasureHouseGetFreeItem,
			protocol.ActionIDTreasureHouseSetUseFreeFlag, protocol.ActionIDTreasureHouseSelectPanel,
			protocol.ActionIDTreasureHouseSetAward},
	}
	seq, ok := seqs[*variant]
	if !ok {
		slog.Error("未知变体", "variant", *variant)
		return
	}
	for _, act := range seq {
		var pkt *protocol.Packet
		switch act {
		case protocol.ActionIDTreasureHouseMainPanel:
			pkt = protocol.BuildTreasureHouseMainPanelPacket(sess.LastActionID())
		case protocol.ActionIDTreasureHouseGetFreeItem:
			pkt = protocol.BuildTreasureHouseGetFreeItemPacket(sess.LastActionID())
		case protocol.ActionIDTreasureHouseSelectPanel:
			pkt = protocol.BuildTreasureHouseInfoPacket(sess.LastActionID())
		case protocol.ActionIDTreasureHouseGetAwardList:
			pkt = protocol.BuildTreasureHousePoolPacket(sess.LastActionID())
		case protocol.ActionIDTreasureHouseSetAward:
			pkt = protocol.BuildTreasureHouseSelectPacket([]protocol.TreasureAwardSelect{
				{TierID: 1, ItemID: 9}, {TierID: 2, ItemID: 18}, {TierID: 3, ItemID: 36},
			}, sess.LastActionID())
		case protocol.ActionIDTreasureHouseSetUseFreeFlag:
			pkt = protocol.BuildTreasureHouseUseFreeFlagPacket(1, sess.LastActionID())
		}
		if pkt == nil {
			slog.Error("无法构造封包", "action", fmt.Sprintf("0x%08X", act))
			return
		}
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		start := time.Now()
		resp, err := sess.Call(c, pkt, act)
		cc()
		if err != nil {
			slog.Warn("变体请求无回包", "variant", *variant, "action", fmt.Sprintf("0x%08X", act),
				"prev", fmt.Sprintf("0x%08X", sess.LastActionID()), "err", err)
		} else {
			slog.Info("变体请求回包", "variant", *variant, "action", fmt.Sprintf("0x%08X", act),
				"resp_len", len(resp.Payload), "elapsed_ms", time.Since(start).Milliseconds())
		}
		time.Sleep(*gap)
	}
	slog.Info("变体结束", "variant", *variant)
	_ = os.Stdout
}
