//go:build probe

// 目标协议: [Module 594] 天帝宝库 (原版 com.protocols.Mod_TreasureHouse_Base)
//
//	Action 1  main_panel          查询主面板 (免费钥匙/当前自选/累计次数)
//	Action 2  select_panel        查询当前可选大奖候选 (open_flag)
//	Action 3  set_award           设定三档自选大奖 (回包单字节 result_code)
//	Action 4  set_use_free_flag   设置优先使用免费次数
//	Action 6  lottery             消耗钥匙抽奖 (回包 result_code + 中奖条目 ID)
//	Action 8  get_free_item       领取今日免费钥匙
//	Action 9  get_award_list      拉取全部候选大奖条目
//
// 基准对齐: 反编译 Main.swf 得到的 com.protocols.Mod_TreasureHouse_Base 与
// com.datas.TreasureHouseData 字段定义。
//
// 背景: 旧 Routine 对 Act 2/3/4/6 回包全部丢弃 (`_, _ = session.Call`)，无法识别
// NO_INGOT / ITEM_PACK_FULL / MAX_TIMES / NOT_SELECT_AWARD 等失败码，且配置的默认
// 三档大奖 (50/25/42) 未必在服务端当前开放候选中。
//
// 运行命令 (只读): go run -tags=probe test/probes/probe_treasure_house_live.go
// 预期表现: 打印 Act 1/2/9 的原始回包与解析结果，不写奖、不消耗钥匙。
//
// 运行命令 (执行): go run -tags=probe test/probes/probe_treasure_house_live.go -apply -draw 1
// 预期表现: 领取免费钥匙 -> 按当前开放候选设定三档大奖 -> 设置免费优先 -> 抽奖 N 次，
//
//	逐步打印 result_code 并在失败时立即停止。
//
// 运行命令 (复现 PrevAct 链漂移): go run -tags=probe test/probes/probe_treasure_house_live.go -routine -gap 20s -debug
// 预期表现: 选奖阶段空闲等待 20s，心跳持续推进服务端 PrevAct 链。若 594 请求未标记
// HasPrevAct，Act4 会因携带过期 PrevAct 被服务端静默丢弃 (context deadline exceeded)；
// 修复后所有 594 请求在发包瞬间用真实最近动作号覆盖末尾 4 字节，Act4 正常返回结果码。
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/minigame"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	serverID := flag.String("server-id", "", "目标区服 ID (留空取配置文件首个角色)")
	roleName := flag.String("role", "", "目标角色名 (留空取该区服首个角色)")
	apply := flag.Bool("apply", false, "领取免费钥匙、设定自选大奖并设置免费优先")
	draw := flag.Int("draw", 0, "Act 6 抽奖次数 (显式指定才消耗免费钥匙，默认 0)")
	routineMode := flag.Bool("routine", false, "直接执行生产 TreasureHouse Routine (只消耗免费钥匙)")
	gap := flag.Duration("gap", 0, "插入空闲等待以复现心跳推进 PrevAct 链：routine 模式位于 select_panel 与 set_award 之间，普通模式位于 Act4 之前")
	preIdle := flag.Duration("pre-idle", 0, "执行 Routine 前空转指定时长，使自动心跳成为最近发送动作 (复现 PrevAct=心跳 场景)")
	debug := flag.Bool("debug", false, "开启 DEBUG 日志，打印每一个出站/入站封包的 ActionID 与载荷")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelInfo)
	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	if len(cfg.Accounts) == 0 {
		slog.Error("配置文件缺少账号")
		return
	}

	acc := cfg.Accounts[0]
	var role *config.RoleConfig
	for i := range acc.Roles {
		r := &acc.Roles[i]
		if *serverID != "" && r.ServerID != *serverID {
			continue
		}
		if *roleName != "" && r.RoleName != *roleName {
			continue
		}
		role = r
		break
	}
	if role == nil {
		slog.Error("配置文件中未找到匹配角色", "server_id", *serverID, "role", *roleName)
		return
	}

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Warn("加载静态字典失败，将仅打印道具ID", "err", err)
		dictRepo = nil
	}

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活，开始天帝宝库协议测试",
		"role_id", sess.RoleID(),
		"server_id", role.ServerID,
	)

	var awardMap map[int]*dictionary.WishpoolAward
	if dictRepo != nil {
		awardMap, _ = dictRepo.GetWishpoolAwards()
	}

	if *routineMode {
		thCfg := minigame.TreasureHouseConfig{
			DictRepo:     dictRepo,
			Awards:       cfg.Scheduler.TreasureHouse.ResolveSelections(),
			MaxDrawTimes: cfg.Scheduler.TreasureHouse.MaxDrawTimes,
		}
		if *gap > 0 {
			// 返回 nil 让 Routine 保留已解析的合法大奖，仅插入一段空闲等待，
			// 复现交互选奖期间心跳持续发包导致的 PrevAct 链漂移。
			thCfg.Selector = func(selCtx context.Context, _ *protocol.TreasureHousePool, _ dictionary.Repository) ([]protocol.TreasureAwardSelect, error) {
				fmt.Printf("\n========== 模拟交互等待 %s (心跳持续运行) ==========\n", *gap)
				timer := time.NewTimer(*gap)
				defer timer.Stop()
				select {
				case <-timer.C:
					return nil, nil
				case <-selCtx.Done():
					return nil, selCtx.Err()
				}
			}
		}
		if *preIdle > 0 {
			fmt.Printf("\n========== Routine 前空转 %s (让自动心跳推进 PrevAct) ==========\n", *preIdle)
			time.Sleep(*preIdle)
		}
		fmt.Printf("\n========== 直接执行生产 TreasureHouse Routine ==========\n")
		if err := minigame.NewTreasureHouseRoutine(thCfg).Execute(ctx, sess, nil); err != nil {
			slog.Error("TreasureHouse Routine 执行失败", "err", err)
			return
		}
		fmt.Printf("========== Routine 执行结束 ==========\n")
		return
	}

	prevAct := sess.LastActionID()
	call := func(label string, action uint32, pkt *protocol.Packet) *protocol.Packet {
		cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
		defer ccancel()
		resp, cErr := sess.Call(cctx, pkt, action)
		if cErr != nil || resp == nil {
			fmt.Printf("\n[%s] action=0x%08X 无回包 err=%v\n", label, action, cErr)
			return nil
		}
		fmt.Printf("\n[%s] action=0x%08X req_prev=0x%08X resp_len=%d\n",
			label, action, prevAct, len(resp.Payload))
		prevAct = action
		printPayload(resp.Payload)
		return resp
	}

	// 1. Act 1: 主面板 (只读)
	mainResp := call("main_panel", protocol.ActionIDTreasureHouseMainPanel,
		protocol.BuildTreasureHouseMainPanelPacket(prevAct))
	var mainInfo *protocol.TreasureHouseMainPanel
	if mainResp != nil {
		mainInfo, err = protocol.ParseTreasureHouseMainPanelResult(mainResp.Payload)
		if err != nil {
			slog.Warn("解析主面板失败", "err", err)
			mainInfo = nil
		} else {
			printMainPanel(mainInfo)
			if mainInfo.Result != protocol.TreasureHouseResultSuccess {
				fmt.Printf("\n[熔断] 主面板失败码 %d (%s)\n",
					mainInfo.Result, protocol.TreasureHouseResultName(mainInfo.Result))
				return
			}
		}
	}

	// 2. Act 2: 自选面板 (只读，给出当前开放候选)
	panelResp := call("select_panel", protocol.ActionIDTreasureHouseSelectPanel,
		protocol.BuildTreasureHouseInfoPacket(prevAct))
	var offered *protocol.TreasureHousePool
	if panelResp != nil {
		panel, pErr := protocol.ParseTreasureHouseSelectPanelResult(panelResp.Payload)
		if pErr != nil {
			slog.Warn("解析自选面板失败", "err", pErr)
		} else if panel.Result != protocol.TreasureHouseResultSuccess {
			fmt.Printf("\n[熔断] 自选面板失败码 %d (%s)\n",
				panel.Result, protocol.TreasureHouseResultName(panel.Result))
			return
		} else {
			printSelectPanel(panel, awardMap)
			offered = poolFromPanel(panel)
		}
	}

	// 3. Act 9: 全量候选大奖 (只读)
	poolResp := call("award_list", protocol.ActionIDTreasureHouseGetAwardList,
		protocol.BuildTreasureHousePoolPacket(prevAct))
	if poolResp != nil {
		if pool, pErr := protocol.ParseTreasureHousePoolResult(poolResp.Payload); pErr != nil {
			slog.Warn("解析全量奖池失败", "err", pErr)
		} else {
			fmt.Printf("\n[全量奖池] total=%d\n", pool.TotalCount)
			printTierIDs("Tier3/凡品", pool.Tier3Items, awardMap)
			printTierIDs("Tier2/上品", pool.Tier2Items, awardMap)
			printTierIDs("Tier1/珍品", pool.Tier1Items, awardMap)
		}
	}

	if !*apply {
		fmt.Printf("\n[只读完成] 未执行 Act 3/4/8/6，未写奖、未消耗钥匙。\n")
		return
	}

	// 4. Act 8: 领取免费钥匙
	if mainInfo != nil && mainInfo.LeftCanGetFree > 0 {
		freeResp := call("get_free_item", protocol.ActionIDTreasureHouseGetFreeItem,
			protocol.BuildTreasureHouseGetFreeItemPacket(prevAct))
		if freeResp != nil {
			printResultCode("get_free_item", freeResp.Payload)
			if refresh := call("main_panel_refresh", protocol.ActionIDTreasureHouseMainPanel,
				protocol.BuildTreasureHouseMainPanelPacket(prevAct)); refresh != nil {
				if mi, e := protocol.ParseTreasureHouseMainPanelResult(refresh.Payload); e == nil && mi.Result == protocol.TreasureHouseResultSuccess {
					mainInfo = mi
					printMainPanel(mi)
				}
			}
		}
	}

	// 5. Act 3: 按当前开放候选设定三档大奖
	awards := resolveAwards(cfg.Scheduler.TreasureHouse.ResolveSelections(), mainInfo, offered)
	fmt.Printf("\n[自选大奖] %+v\n", awards)
	selResp := call("set_award", protocol.ActionIDTreasureHouseSetAward,
		protocol.BuildTreasureHouseSelectPacket(awards, prevAct))
	if selResp != nil {
		if code, e := protocol.ParseTreasureHouseResultCode(selResp.Payload); e != nil {
			slog.Warn("解析 set_award 回包失败", "err", e)
			return
		} else if code != protocol.TreasureHouseResultSuccess {
			fmt.Printf("\n[熔断] set_award 失败码 %d (%s)\n",
				code, protocol.TreasureHouseResultName(code))
			return
		} else {
			fmt.Printf("[set_award] 成功\n")
		}
	}

	// 6. Act 4: 优先使用免费次数
	// -gap 在 Act4 前插入空闲等待，用于观察心跳持续发包后 Act4 的回包延迟。
	if *gap > 0 {
		fmt.Printf("\n[gap] Act4 前空闲等待 %s (心跳持续运行)...\n", *gap)
		timer := time.NewTimer(*gap)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
	}
	act4Start := time.Now()
	flagResp := call("set_use_free_flag", protocol.ActionIDTreasureHouseSetUseFreeFlag,
		protocol.BuildTreasureHouseUseFreeFlagPacket(1, prevAct))
	fmt.Printf("[set_use_free_flag] elapsed=%s resp=%v\n", time.Since(act4Start), flagResp != nil)
	if flagResp != nil {
		printResultCode("set_use_free_flag", flagResp.Payload)
	}

	// 7. Act 6: 抽奖 (显式指定次数)
	for i := 0; i < *draw; i++ {
		drawResp := call(fmt.Sprintf("lottery#%d", i+1), protocol.ActionIDTreasureHouseLottery,
			protocol.BuildTreasureHouseDrawPacket(prevAct))
		if drawResp == nil {
			break
		}
		lottery, lErr := protocol.ParseTreasureHouseLotteryResult(drawResp.Payload)
		if lErr != nil {
			slog.Warn("解析抽奖回包失败", "err", lErr)
			break
		}
		fmt.Printf("[lottery] result=%d (%s) award_id=%d\n",
			lottery.Result,
			protocol.TreasureHouseResultName(lottery.Result),
			lottery.AwardID)
		if lottery.Result != protocol.TreasureHouseResultSuccess {
			fmt.Printf("[熔断] 抽奖失败，停止后续抽奖\n")
			break
		}
	}
	if *draw > 0 {
		call("main_panel_after_draw", protocol.ActionIDTreasureHouseMainPanel,
			protocol.BuildTreasureHouseMainPanelPacket(prevAct))
	}

	fmt.Printf("\n[测试完成] apply=%v draw=%d，实际发包顺序: main_panel -> select_panel -> award_list -> get_free_item -> set_award -> set_use_free_flag -> lottery\n",
		*apply, *draw)
}

// printPayload 打印原始回包与解压后的载荷十六进制，并给出常见整型前缀解释。
func printPayload(payload []byte) {
	fmt.Printf("  raw(%d)=%s\n", len(payload), hex.EncodeToString(payload))
	decomp := protocol.DecompressPayloadIfNeeded(payload)
	if len(decomp) != len(payload) {
		fmt.Printf("  decomp(%d)=%s\n", len(decomp), hex.EncodeToString(decomp))
	}
	if len(decomp) >= 4 {
		fmt.Printf("  head_u8=%d head_u16=%d head_u32=0x%08X\n",
			decomp[0], binary.BigEndian.Uint16(decomp[:2]), binary.BigEndian.Uint32(decomp[:4]))
	}
}

func printResultCode(label string, payload []byte) {
	if code, err := protocol.ParseTreasureHouseResultCode(payload); err != nil {
		slog.Warn("解析结果码失败", "label", label, "err", err)
	} else {
		fmt.Printf("[%s] result=%d (%s)\n", label, code, protocol.TreasureHouseResultName(code))
	}
}

func printMainPanel(p *protocol.TreasureHouseMainPanel) {
	if p == nil {
		return
	}
	fmt.Printf("\n[主面板] result=%d (%s)\n", p.Result, protocol.TreasureHouseResultName(p.Result))
	if p.Result != protocol.TreasureHouseResultSuccess {
		return
	}
	fmt.Printf("  当前自选: %+v\n", p.WishList)
	fmt.Printf("  free_item_num=%d use_free_flag=%d free_need_times=%d free_item_accu=%d use_ingot_times=%d\n",
		p.FreeItemNum, p.UseFreeFlag, p.FreeNeedTimes, p.FreeItemAccu, p.UseIngotTimes)
	fmt.Printf("  left_can_get_free=%d total_times=%d ingot_cost_list=%+v\n",
		p.LeftCanGetFree, p.TotalTimes, p.IngotCostList)
}

func printSelectPanel(p *protocol.TreasureHouseSelectPanel, awardMap map[int]*dictionary.WishpoolAward) {
	fmt.Printf("\n[自选面板] result=%d (%s) notify_flag=%d\n",
		p.Result, protocol.TreasureHouseResultName(p.Result), p.NotifyFlag)
	for _, q := range p.Qualities {
		fmt.Printf("  Quality%d refresh_times=%d refresh_ingot=%d refresh_limit=%d\n",
			q.Quality, q.RefreshTimes, q.RefreshIngot, q.RefreshLimit)
		for _, c := range q.Awards {
			status := "未开放"
			if c.OpenFlag != 0 {
				status = "可自选"
			}
			fmt.Printf("    award_id=%-3d %-28s [%s]\n", c.AwardID, awardName(c.AwardID, awardMap), status)
		}
	}
}

func printTierIDs(label string, items []int32, awardMap map[int]*dictionary.WishpoolAward) {
	fmt.Printf("  %s (%d):\n", label, len(items))
	for _, id := range items {
		fmt.Printf("    id=%-3d %s\n", id, awardName(id, awardMap))
	}
}

func awardName(id int32, awardMap map[int]*dictionary.WishpoolAward) string {
	if awardMap != nil {
		if a, ok := awardMap[int(id)]; ok && a != nil && a.ItemName != "" {
			return a.ItemName
		}
	}
	return ""
}

// poolFromPanel 将 open_flag=1 的候选转换为三档奖池。
func poolFromPanel(panel *protocol.TreasureHouseSelectPanel) *protocol.TreasureHousePool {
	pool := &protocol.TreasureHousePool{}
	for _, q := range panel.Qualities {
		for _, c := range q.Awards {
			if c.OpenFlag == 0 {
				continue
			}
			switch q.Quality {
			case 1:
				pool.Tier1Items = append(pool.Tier1Items, c.AwardID)
			case 2:
				pool.Tier2Items = append(pool.Tier2Items, c.AwardID)
			case 3:
				pool.Tier3Items = append(pool.Tier3Items, c.AwardID)
			}
			pool.TotalCount++
		}
	}
	return pool
}

// resolveAwards 与 Routine 相同策略: 配置值不在开放候选中时回退到当前自选/首个开放候选。
func resolveAwards(configured []protocol.TreasureAwardSelect, main *protocol.TreasureHouseMainPanel, offered *protocol.TreasureHousePool) []protocol.TreasureAwardSelect {
	current := []protocol.TreasureAwardSelect(nil)
	if main != nil {
		current = main.WishList
	}
	if offered == nil {
		return configured
	}
	contains := func(items []int32, id int32) bool {
		for _, it := range items {
			if it == id {
				return true
			}
		}
		return false
	}
	curMap := map[int32]int32{}
	for _, a := range current {
		curMap[a.TierID] = a.ItemID
	}
	cfgMap := map[int32]int32{}
	for _, a := range configured {
		cfgMap[a.TierID] = a.ItemID
	}
	tiers := []struct {
		tier    int32
		options []int32
	}{{1, offered.Tier1Items}, {2, offered.Tier2Items}, {3, offered.Tier3Items}}
	out := make([]protocol.TreasureAwardSelect, 0, 3)
	for _, t := range tiers {
		id := cfgMap[t.tier]
		if len(t.options) > 0 && !contains(t.options, id) {
			if cur := curMap[t.tier]; contains(t.options, cur) {
				id = cur
			} else {
				id = t.options[0]
			}
		}
		out = append(out, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: id})
	}
	return out
}
