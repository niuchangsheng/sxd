//go:build probe

// 目标: 在不消耗元宝的前提下，用「冥想竹简/书卷/玄简」补充悟法领域冥想次数
//
// 每日活跃度任务 69「每日悟法领域冥想」每次 +10，上限 20 次。
// 当前免费次数已为 0，但角色持有冥想类道具：
//
//	8358 冥想竹简 x29（高级通玄秘馆）
//	8641 冥想书卷 x12（入门通玄秘馆）
//	8700 冥想玄简 x10（限时秘馆）
//	8359 冥想玉简 x0 （独门通玄秘馆，绝不触碰）
//
// 官方 WuFaFieldDrawCardView 仅在池类型 1（免费池）且道具不足时弹出元宝购买确认；
// 其余秘馆直接以道具抽取。本探针只对 field_type 3 与 7 抽卡（两种可能的道具
// 对齐顺序下均持有足量道具），每次抽卡后立即校验元宝变化，一旦元宝减少立即终止。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_wufa_item_draw_live.go
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

func logPanel(panel *protocol.WuFaPanel) {
	for _, f := range panel.Fields {
		slog.Info("悟法秘馆", "field_type", f.FieldType, "today_count", f.TodayCount,
			"free_one", f.LeftFreeOneCount, "free_ten", f.LeftFreeTenCount)
	}
	for _, it := range panel.Items {
		slog.Info("悟法资源", "item_id", it.ItemID, "num", it.ItemNum)
	}
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
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	// 先发一次只读活跃度请求，建立窗口内 PrevAct 链。
	before, _ := readActivity(sess, ctx)
	st := sess.GetPlayerState()
	slog.Info("起始", "daily_activity", before, "ingots", st.Ingots, "coins", st.Coins)

	infoResp := call(protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDTownEnter), protocol.ActionIDWuFaFieldInfo, 8*time.Second)
	if infoResp == nil {
		slog.Error("查询悟法面板失败")
		return
	}
	panel, err := protocol.ParseWuFaPanel(infoResp.Payload)
	if err != nil {
		slog.Error("解析悟法面板失败", "err", err)
		return
	}
	logPanel(panel)

	ingotBase := st.Ingots
	draws := 0
	for _, fieldType := range []int32{3, 7} {
		for attempt := 0; attempt < 4 && ctx.Err() == nil; attempt++ {
			if cur, ok := readActivity(sess, ctx); ok && cur >= targetActivity {
				slog.Info("活跃度已达标，停止", "daily_activity", cur)
				goto done
			}
			preState := sess.GetPlayerState()
			resp := call(protocol.BuildWuFaDrawCardPacket(fieldType, protocol.WuFaDrawOne), protocol.ActionIDWuFaDrawCard, 10*time.Second)
			if resp == nil {
				slog.Warn("抽卡无回包，停止", "field_type", fieldType)
				goto done
			}
			result, pErr := protocol.ParseWuFaDrawResult(resp.Payload)
			if pErr != nil {
				slog.Warn("解析抽卡回包失败", "err", pErr)
				goto done
			}
			draws++
			slog.Info("悟法抽卡", "draw", draws, "field_type", fieldType,
				"result", result.Result, "result_text", protocol.WuFaResultText(result.Result),
				"awards", len(result.Awards), "rest_times", result.RestTimes)
			time.Sleep(300 * time.Millisecond)
			postState := sess.GetPlayerState()
			if postState.Ingots != ingotBase {
				slog.Error("检测到元宝变化，立即终止", "before", preState.Ingots, "after", postState.Ingots)
				goto done
			}
			if result.Result != protocol.WuFaResultSuccess {
				slog.Info("抽卡未成功，换下一秘馆", "field_type", fieldType, "result", result.Result)
				break
			}
			refresh := call(protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDWuFaFieldInfo), protocol.ActionIDWuFaFieldInfo, 8*time.Second)
			if refresh == nil {
				goto done
			}
			panel, err = protocol.ParseWuFaPanel(refresh.Payload)
			if err != nil {
				slog.Warn("刷新面板失败", "err", err)
				goto done
			}
			logPanel(panel)
			time.Sleep(200 * time.Millisecond)
		}
	}

done:
	final := sess.GetPlayerState()
	finalAct, _ := readActivity(sess, ctx)
	slog.Info("悟法道具抽卡完成",
		"draws", draws,
		"daily_before", before,
		"daily_after", finalAct,
		"activity_delta", finalAct-before,
		"coin_delta", final.Coins-st.Coins,
		"ingot_delta", final.Ingots-ingotBase,
	)
}
