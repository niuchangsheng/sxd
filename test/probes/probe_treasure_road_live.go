//go:build probe

// 目标协议: [Module 624 / 0x270] Mod_TreasureRoad_Base 仙途秘宝
//
//	activity_info Act 1 -> 0x02700001 空请求, 回包 UByte status (0=OPEN,1=CLOSE)
//	open_panel    Act 2 -> 0x02700002 空请求
//	  response: UByte result, UByte step, Int first_end_time, Int second_end_time,
//	            Int cur_level, Int cur_exp, Int total_charge,
//	            [Int] have_role_list, [Int] get_award_list
//	get_award     Act 3 -> 0x02700003  Int award_id, 回包 UByte result
//	get_all_award Act 5 -> 0x02700005 空请求, 回包 UByte result + [Int award_id]
//
// 基准对齐: com.protocols.Mod_TreasureRoad_Base / com.datas.TreasureRoadData /
//
//	com.assist.server.TreasureRoadType
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_treasure_road_live.go [-execute]
//
// 预期表现: 只读打印当前等级与已领取奖励；-execute 仅领取普通奖励轨上
//
//	已达到等级且未领取的元宝档 (id 51..56 对应 1/5/10/15/20/25 级，
//	每档 200 元宝)。不调用消耗元宝的 buy_advance。
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
	actionTreasureRoadInfo  uint32 = 0x02700001
	actionTreasureRoadPanel uint32 = 0x02700002
	actionTreasureRoadAward uint32 = 0x02700003
)

func main() {
	execute := flag.Bool("execute", false, "实际领取已达成等级的普通奖励")
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

	if resp := call(nil, actionTreasureRoadInfo, "activity_info"); resp != nil && len(resp.Payload) > 0 {
		slog.Info("仙途秘宝活动状态", "status", resp.Payload[0], "status_name", treasureRoadStatusName(resp.Payload[0]))
		if resp.Payload[0] != 0 {
			slog.Info("仙途秘宝未开放，跳过")
			return
		}
	}

	resp := call(nil, actionTreasureRoadPanel, "open_panel")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	result, e1 := r.ReadUint8()
	step, e2 := r.ReadUint8()
	firstEnd, e3 := r.ReadInt32()
	curLevel, e4 := r.ReadInt32()
	curExp, e5 := r.ReadInt32()
	totalCharge, e6 := r.ReadInt32()
	buyFlag, e7 := r.ReadInt32()
	if err := firstErr(e1, e2, e3, e4, e5, e6, e7); err != nil {
		slog.Error("解析仙途秘宝面板失败", "err", err)
		return
	}
	haveRoles := readIntList(r)
	gotAwards := readIntList(r)
	got := make(map[int32]bool, len(gotAwards))
	for _, id := range gotAwards {
		got[id] = true
	}
	slog.Info("仙途秘宝面板",
		"result", result,
		"step", step,
		"first_end_time", firstEnd,
		"cur_level", curLevel,
		"cur_exp", curExp,
		"total_charge", totalCharge,
		"buy_flag", buyFlag,
		"cur_exp", curExp,
		"total_charge", totalCharge,
		"have_roles", haveRoles,
		"got_awards", gotAwards,
	)

	if !*execute {
		slog.Info("只读模式结束；如需领取请追加 -execute")
		return
	}

	// TreasureRoadType.awardList 中 51..56 为元宝档，分别要求等级 1/5/10/15/20/25。
	ingotTiers := []struct {
		id    int32
		level int32
	}{
		{51, 1}, {52, 5}, {53, 10}, {54, 15}, {55, 20}, {56, 25},
	}
	claimed := 0
	for _, tier := range ingotTiers {
		if curLevel < tier.level || got[tier.id] {
			continue
		}
		payload := make([]byte, 4)
		binary.BigEndian.PutUint32(payload, uint32(tier.id))
		gResp := call(payload, actionTreasureRoadAward, fmt.Sprintf("get_award(id=%d,level=%d)", tier.id, tier.level))
		if gResp == nil || len(gResp.Payload) == 0 {
			continue
		}
		slog.Info("元宝档领取结果", "id", tier.id, "level", tier.level, "result", gResp.Payload[0])
		if gResp.Payload[0] == 2 { // SUCCESS
			claimed++
		}
	}
	after := sess.GetPlayerState().Ingots
	slog.Info("仙途秘宝领取完成", "claimed", claimed, "ingots_before", before, "ingots_after", after, "delta", after-before)
}

func readIntList(r *protocol.Reader) []int32 {
	count, err := r.ReadUint16()
	if err != nil {
		return nil
	}
	out := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return out
		}
		out = append(out, v)
	}
	return out
}

func treasureRoadStatusName(status uint8) string {
	switch status {
	case 0:
		return "开放"
	case 1:
		return "关闭"
	default:
		return "未知"
	}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
