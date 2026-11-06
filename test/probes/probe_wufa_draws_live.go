//go:build probe

// 目标协议: 悟法领域冥想 (Mod_WuFaField_Base, Module 658 / 0x0292)
//
// 每日活跃度任务 69「每日悟法领域冥想」每次冥想(感悟) +10，上限 20 次。
//
//	panel_info (Act 0, 0x02920000) response=[秘馆数组, 资源数组, 公共字段...]
//	draw       (Act 1, 0x02920001) request=[Int field_type, UByte draw_type(0=单抽,1=十连)]
//	  response=[UByte result, [奖励数组], Int rest_times]
//
// 本探针只消耗面板中 LeftFreeOneCount / LeftFreeTenCount 的免费次数，
// 免费次数归零立即停止，绝不用元宝或道具补次数。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_wufa_draws_live.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const targetActivity = 1200

func readActivity(sess *client.RoleSession, ctx context.Context) (int, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	if err != nil || resp == nil {
		return 0, false
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
	total := 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		total += int(row[1] * row[3])
	}
	return total, true
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	state := sess.GetPlayerState()
	before, _ := readActivity(sess, ctx)
	slog.Info("悟法前", "daily_activity", before, "ingots", state.Ingots, "coins", state.Coins)

	infoResp := call(protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDTownEnter), protocol.ActionIDWuFaFieldInfo, 8*time.Second)
	if infoResp == nil {
		slog.Error("查询悟法面板失败")
		return
	}
	panel, err := protocol.ParseWuFaPanel(infoResp.Payload)
	if err != nil {
		slog.Error("解析悟法面板失败", "err", err, "hex", fmt.Sprintf("%x", infoResp.Payload))
		return
	}
	slog.Info("悟法面板公共字段", "rest_times", panel.RestTimes,
		"ru_men_active", panel.RuMenFieldActive, "psychic_res", panel.PsychicResNum,
		"lucky_day", panel.IsLuckyDay)
	for _, f := range panel.Fields {
		slog.Info("悟法秘馆",
			"field_type", f.FieldType,
			"today_count", f.TodayCount,
			"total_count", f.TotalCount,
			"free_one", f.LeftFreeOneCount,
			"free_ten", f.LeftFreeTenCount,
			"left_total_ten", f.LeftTotalTenCount,
		)
	}
	for _, it := range panel.Items {
		slog.Info("悟法资源", "item_id", it.ItemID, "num", it.ItemNum, "exception_num", it.ExceptionNum)
	}

	draws := 0
	for ctx.Err() == nil {
		if cur, ok := readActivity(sess, ctx); ok && cur >= targetActivity {
			slog.Info("活跃度已达标，停止抽卡", "daily_activity", cur)
			break
		}
		fieldType, drawType, ok := selectFreeDraw(panel)
		if !ok {
			slog.Info("悟法免费次数已用尽，停止 (不使用元宝/道具)")
			break
		}
		resp := call(protocol.BuildWuFaDrawCardPacket(fieldType, drawType), protocol.ActionIDWuFaDrawCard, 10*time.Second)
		if resp == nil {
			break
		}
		result, err := protocol.ParseWuFaDrawResult(resp.Payload)
		if err != nil {
			slog.Warn("解析悟法抽卡回包失败", "err", err)
			break
		}
		draws++
		slog.Info("悟法抽卡", "draw", draws, "field_type", fieldType, "draw_type", drawType,
			"result", result.Result, "result_text", protocol.WuFaResultText(result.Result),
			"awards", len(result.Awards), "rest_times", result.RestTimes)
		if result.Result != protocol.WuFaResultSuccess {
			break
		}
		time.Sleep(200 * time.Millisecond)
		// 重新拉面板，确保只使用真实剩余的免费次数。
		refresh := call(protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDWuFaFieldInfo), protocol.ActionIDWuFaFieldInfo, 8*time.Second)
		if refresh == nil {
			break
		}
		panel, err = protocol.ParseWuFaPanel(refresh.Payload)
		if err != nil {
			slog.Warn("刷新悟法面板失败", "err", err)
			break
		}
	}

	after := sess.GetPlayerState()
	finalAct, _ := readActivity(sess, ctx)
	slog.Info("悟法完成",
		"draws", draws,
		"daily_before", before,
		"daily_after", finalAct,
		"activity_delta", finalAct-before,
		"coin_delta", after.Coins-state.Coins,
		"ingot_delta", after.Ingots-state.Ingots,
	)
}

func selectFreeDraw(panel *protocol.WuFaPanel) (int32, uint8, bool) {
	if panel == nil {
		return 0, 0, false
	}
	bestTenField, bestTenCount := int32(0), int32(0)
	bestOneField, bestOneCount := int32(0), int32(0)
	for _, f := range panel.Fields {
		if f.LeftFreeTenCount > bestTenCount {
			bestTenField, bestTenCount = f.FieldType, f.LeftFreeTenCount
		}
		if f.LeftFreeOneCount > bestOneCount {
			bestOneField, bestOneCount = f.FieldType, f.LeftFreeOneCount
		}
	}
	if bestTenCount > 0 {
		return bestTenField, protocol.WuFaDrawTen, true
	}
	if bestOneCount > 0 {
		return bestOneField, protocol.WuFaDrawOne, true
	}
	return 0, 0, false
}
