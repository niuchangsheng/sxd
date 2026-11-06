//go:build probe

// 目标: 线上审计每日活跃度构成与缺口 (Mod_Player_Base.get_game_assistant_info, 0x0000002A)
//
// 回包尾部 active_degree_list 每行为 [task_id, cur_times, max_times, per_active_add]，
// 每日任务 task_id <= 83，周常任务 >= 84。
//
// 本探针只读，不发送任何写请求；用于回答「还差多少活跃度、哪些玩法没做」。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_activity_audit_live.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

// dailyTaskNames 来自官方 AssistantTypeData.objFunc["1"]["1"]，仅每日任务 (id <= 83)。
var dailyTaskNames = map[int32]string{
	1: "购买体力", 2: "招财神符", 3: "每日登录", 7: "关公", 8: "帮派俸禄",
	9: "领取石头", 10: "免费洗练", 11: "送花", 12: "取经", 13: "护送取经",
	14: "使用仙露", 15: "购买仙露", 16: "神龙培养", 17: "摘仙桃", 18: "吉星高照",
	29: "喜从天降", 32: "每日神秘宝箱", 33: "每日药园种植仙令", 34: "每日灵脉",
	35: "每日猎妖", 46: "五福临门", 65: "每日问灵", 66: "点金石", 67: "每日回忆之境",
	68: "每日多宝山寻宝", 69: "每日悟法领域冥想", 70: "每日轮回净地领取挂机奖励",
	71: "每日纪元湮劫中完成战斗", 72: "每日阶位赛胜利", 73: "每日进入仙幽猎境",
	74: "每日琳琅阁购买商品", 75: "每日消耗50点体力", 76: "每日收取练功奖励",
	77: "每日宗门灵兽喂养", 78: "每日探索禁地之书", 79: "每日服用修为丹",
	80: "每日诸法器冢寻器/招灵", 81: "每日法力星图占星", 82: "每日领取好礼馈赠",
	83: "每日劫镖",
}

type activityRow struct {
	id       int32
	cur      int32
	max      int32
	per      int32
	earned   int32
	remain   int32
	remainPt int32
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	cCtx, cCancel := context.WithTimeout(ctx, 10*time.Second)
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	cCancel()
	if err != nil || resp == nil {
		slog.Error("查询活跃度失败", "err", err)
		return
	}
	r := protocol.NewReader(resp.Payload)
	for i := 0; i < 33; i++ {
		if i == 1 || i == 3 {
			_, _ = r.ReadInt64()
		} else {
			_, _ = r.ReadInt32()
		}
	}
	readList := func() [][4]int32 {
		n, _ := r.ReadUint16()
		out := make([][4]int32, 0, n)
		for i := 0; i < int(n); i++ {
			var row [4]int32
			for j := 0; j < 4; j++ {
				row[j], _ = r.ReadInt32()
			}
			out = append(out, row)
		}
		return out
	}
	_ = readList()
	active := readList()

	var rows []activityRow
	total, weekly := 0, 0
	for _, x := range active {
		row := activityRow{id: x[0], cur: x[1], max: x[2], per: x[3]}
		row.earned = row.cur * row.per
		row.remain = row.max - row.cur
		if row.per > 0 {
			row.remainPt = row.remain * row.per
		}
		if row.id > 83 {
			weekly += int(row.earned)
			continue
		}
		total += int(row.earned)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].remainPt > rows[j].remainPt })

	state := sess.GetPlayerState()
	slog.Info("每日活跃度审计", "daily_total", total, "weekly_total", weekly,
		"target", 1200, "gap", max(0, 1200-total),
		"ingots", state.Ingots, "coins", state.Coins)

	for _, row := range rows {
		name := dailyTaskNames[row.id]
		if name == "" {
			name = "(未知)"
		}
		status := "已完成"
		if row.remain > 0 {
			status = "未完成"
		}
		slog.Info(fmt.Sprintf("活跃度任务 %02d", row.id),
			"name", name,
			"cur", row.cur, "max", row.max, "per", row.per,
			"earned", row.earned, "remain_pt", row.remainPt,
			"status", status,
		)
	}
}
