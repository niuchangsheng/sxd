//go:build probe

// 目标协议: [Module 594] 天帝宝库 main_panel(1) / select_panel(2)
// 基准对齐: 官方 com.assist.server.source.TreasureHouseTypeData.objHouseAward 与方案 A 选奖规则
//
// 运行命令: go run -tags=probe test/probes/probe_treasure_house_select.go -config configs/s1.yaml
// 预期表现: 只读拉取服务端当前三档候选，按「道具品质 > 自选包/材料/货币/宝石包/令牌 > 数量 > sort」
// 规则打印每档推荐奖励与候选明细，不设置大奖、不抽奖、不消耗任何资源。
package main

import (
	"context"
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
	cfgPath := flag.String("config", "configs/s1.yaml", "配置文件路径")
	noFilter := flag.Bool("no-filter", false, "关闭所属功能开放过滤")
	autoMode := flag.Bool("auto", false, "使用账号养成进度动态需求分选奖 (与生产 auto 模式一致)")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("挂载数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ticket, err := xdAuth.Login(ctx, acc.Username, acc.Password, role.ServerID)
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

	call := func(pkt *protocol.Packet, action uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, action)
		if cErr != nil || resp == nil {
			slog.Warn("宝库请求无回包", "action", fmt.Sprintf("0x%08X", action), "err", cErr)
			return nil
		}
		return resp
	}

	mainResp := call(protocol.BuildTreasureHouseMainPanelPacket(sess.LastActionID()),
		protocol.ActionIDTreasureHouseMainPanel, 3*time.Second)
	if mainResp == nil {
		return
	}
	mainInfo, err := protocol.ParseTreasureHouseMainPanelResult(mainResp.Payload)
	if err != nil || mainInfo.Result != protocol.TreasureHouseResultSuccess {
		slog.Error("主面板解析失败", "err", err)
		return
	}
	slog.Info("宝库主面板",
		"free_item_num", mainInfo.FreeItemNum,
		"use_free_flag", mainInfo.UseFreeFlag,
		"left_can_get_free", mainInfo.LeftCanGetFree,
		"wish_list", mainInfo.WishList)

	panelResp := call(protocol.BuildTreasureHouseInfoPacket(sess.LastActionID()),
		protocol.ActionIDTreasureHouseSelectPanel, 3*time.Second)
	if panelResp == nil {
		return
	}
	panel, err := protocol.ParseTreasureHouseSelectPanelResult(panelResp.Payload)
	if err != nil || panel.Result != protocol.TreasureHouseResultSuccess {
		slog.Error("自选面板解析失败", "err", err)
		return
	}

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
	slog.Info("当前开放候选",
		"tier1", pool.Tier1Items,
		"tier2", pool.Tier2Items,
		"tier3", pool.Tier3Items)

	lookup := func(itemID int32) (*dictionary.Item, error) {
		return dictRepo.GetItem(int(itemID))
	}

	var isFuncOpen func(int32) bool
	if !*noFilter {
		openSet, fErr := sess.RefreshPlayerFunctions(ctx)
		if fErr != nil {
			slog.Warn("获取功能开放列表失败，本次不做功能过滤", "err", fErr)
		} else {
			isFuncOpen = func(functionID int32) bool { return openSet[functionID] }
			slog.Info("已加载功能开放列表", "count", len(openSet))
		}
	}

	// 候选明细
	for _, entry := range []struct {
		tier int32
		ids  []int32
	}{{1, pool.Tier1Items}, {2, pool.Tier2Items}, {3, pool.Tier3Items}} {
		for _, id := range entry.ids {
			award, ok := protocol.TreasureAwardByID(id)
			if !ok {
				fmt.Printf("  tier=%d award=%d 未收录于官方表\n", entry.tier, id)
				continue
			}
			item, _ := lookup(award.ItemID)
			name := ""
			if item != nil {
				name = item.Name
			}
			fmt.Printf("  tier=%d award=%-3d item=%-5d %-16s x%-4d item_quality=%d type=%d sort=%d\n",
				entry.tier, award.ID, award.ItemID, name, award.ItemNum,
				func() int {
					if item != nil {
						return item.Quality
					}
					return 0
				}(),
				func() int {
					if item != nil {
						return item.TypeID
					}
					return 0
				}(),
				award.Sort)
		}
	}

	var selected []protocol.TreasureAwardSelect
	if *autoMode {
		snap := minigame.CollectNeedSnapshot(ctx, sess, pool)
		needs := minigame.EvaluateTreasureNeeds(snap, pool)
		fmt.Println("========== 动态需求分 ==========")
		for _, entry := range []struct {
			tier int32
			ids  []int32
		}{{1, pool.Tier1Items}, {2, pool.Tier2Items}, {3, pool.Tier3Items}} {
			for _, id := range entry.ids {
				award, ok := protocol.TreasureAwardByID(id)
				if !ok {
					continue
				}
				need := needs[id]
				fmt.Printf("  tier=%d award=%-3d item=%-5d need_score=%-8.2f reason=%s\n",
					entry.tier, award.ID, award.ItemID, need.Score, need.Reason)
			}
		}
		selected = minigame.SelectTreasureAwardsByNeed(pool, lookup, isFuncOpen, needs)
		fmt.Println("========== 动态需求推荐选择 ==========")
	} else {
		selected = minigame.SelectTreasureAwardsByRarity(pool, lookup, isFuncOpen, cfg.Scheduler.TreasureHouse.PreferAwards)
		fmt.Println("========== 方案 A 推荐选择 ==========")
	}
	for _, s := range selected {
		award, _ := protocol.TreasureAwardByID(s.ItemID)
		item, _ := lookup(award.ItemID)
		name := ""
		if item != nil {
			name = item.Name
		}
		fmt.Printf("  tier=%d %s award=%-3d item=%-5d %-16s x%d\n",
			s.TierID, protocol.TreasureAwardQualityName(award.Quality), award.ID, award.ItemID, name, award.ItemNum)
	}
	fmt.Println("(只读演练，未写入自选大奖，未抽奖)")
}
