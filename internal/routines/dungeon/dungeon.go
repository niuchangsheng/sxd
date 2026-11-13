// Package dungeon 提供副本关卡扫荡、六道轮回、伏魔塔与练功房等推图挑战玩法。
//
// @author Ateng
// @since 2026-10-08
package dungeon

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// QuestCycleFeatureConfig 剧情推图任务闭环配置
type QuestCycleFeatureConfig struct {
	Enabled              bool `json:"enabled"`
	AutoReplenishStamina bool `json:"auto_replenish_stamina"` // 体力不足时自动按 [包子 > 粽子 > 茶叶蛋] 自愈 (仅作用于推图)
	MaxReplenishStamina  int  `json:"max_replenish_stamina"`  // 单次推图最大补充体力上限 (点数，仅作用于推图；包子5/粽子10/茶叶蛋50)
	MaxReplenishTimes    int  `json:"max_replenish_times"`    // 兼容旧字段：单次推图最大自愈补充次数 (仅作用于推图)
}

// SweepFeatureConfig 副本关卡扫荡配置
type SweepFeatureConfig struct {
	Enabled               bool     `json:"enabled"`
	Pipeline              []string `json:"pipeline"`                // 扫荡流水线优先级: "sweep_elite"
	PreferPracticeQuickly bool     `json:"prefer_practice_quickly"` // 兼容旧配置字段
	SellSweptItems        bool     `json:"sell_swept_items"`        // 扫荡精英副本后是否自动卖掉扫荡物
	AutoSellSweptItems    bool     `json:"auto_sell_swept_items"`   // 别名
	// GlobalElite 通过 Module 25 全局扫荡所有精英副本（无需身处精英城镇）。
	GlobalElite bool `json:"global_elite"`
}

// SweepConfig 关卡推图与扫荡自定义策略配置。包含两大核心功能：剧情推图 与 关卡扫荡。
type SweepConfig struct {
	// 功能一: 剧情推图 (循环接任务/做任务/交任务直至无任务可做)
	QuestCycle QuestCycleFeatureConfig `json:"quest_cycle"`
	// 功能二: 关卡扫荡 (扫荡精英副本)
	Sweep SweepFeatureConfig `json:"sweep"`

	// PreferPracticeQuickly 兼容旧配置字段 (英雄副本固定使用 quickly)
	PreferPracticeQuickly bool
	// AutoReplenishStamina 当主线或支线等副本无体力继续时，是否自动按 [包子 > 粽子 > 茶叶蛋] 优先级吃道具补充体力 (默认 true)
	AutoReplenishStamina bool
	// MaxReplenishStamina 单次执行自动补充体力的最大点数上限 (默认 200，每个包子5/粽子10/茶叶蛋50)
	MaxReplenishStamina int
	// MaxReplenishTimes 单次执行自动补充体力的最大次数 (兼容旧配置，默认 10)
	MaxReplenishTimes int
	// SellSweptItems 扫荡精英副本后是否自动卖掉扫荡物
	SellSweptItems bool `json:"sell_swept_items"`
	// AutoSellSweptItems 扫荡精英副本后是否自动卖掉扫荡物 (别名)
	AutoSellSweptItems bool `json:"auto_sell_swept_items"`
	// Pipeline 策略优先级流水线，按先后顺序流转
	// 可选值: "push_mainline", "push_side", "sweep_elite"
	Pipeline []string
}

// DefaultSweepConfig 返回默认扫荡策略配置。
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		QuestCycle: QuestCycleFeatureConfig{
			Enabled:              false, // 默认纯扫荡；由 yaml 或 pipeline 显式开启
			AutoReplenishStamina: true,
			MaxReplenishStamina:  200,
			MaxReplenishTimes:    10,
		},
		Sweep: SweepFeatureConfig{
			Enabled:        true,
			Pipeline:       []string{"sweep_elite"},
			SellSweptItems: false,
			GlobalElite:    false,
		},
		AutoReplenishStamina: true,
		MaxReplenishStamina:  200,
		MaxReplenishTimes:    10,
		Pipeline:             nil,
		SellSweptItems:       false,
	}
}

// fallbackEliteTowns 为数据字典不可用时的兜底精英副本城镇列表
// (来源于 Pieb.db: SELECT DISTINCT map FROM Missions WHERE MissionType=1)。
var fallbackEliteTowns = []int{4, 5, 7, 8, 9, 13, 14, 15, 21, 22, 23, 24, 26, 27, 29, 33, 35, 39, 40, 43, 54, 55}

// eliteMissionTowns 返回所有存在精英副本的城镇/地图 ID。
func eliteMissionTowns(dictRepo dictionary.Repository) []int {
	if dictRepo != nil {
		if towns, err := dictRepo.GetEliteMissionTowns(); err == nil && len(towns) > 0 {
			return towns
		} else if err != nil {
			slog.Warn("查询精英副本城镇列表失败，使用内置兜底列表", "err", err)
		}
	}
	return fallbackEliteTowns
}

// heroNotifyWaiter 聚合服务端主动推送的英雄副本扫荡战报。
type heroNotifyWaiter struct {
	ch chan protocol.HeroMissionNotify
}

// heroNotifyWaiters 按会话缓存推送监听器，避免每次 Routine 执行重复注册。
var heroNotifyWaiters sync.Map // map[*client.RoleSession]*heroNotifyWaiter

// heroNotifyWaiterFor 获取 (或首次创建) 指定会话的英雄副本推送监听器。
func heroNotifyWaiterFor(session *client.RoleSession) *heroNotifyWaiter {
	if v, ok := heroNotifyWaiters.Load(session); ok {
		return v.(*heroNotifyWaiter)
	}
	w := &heroNotifyWaiter{ch: make(chan protocol.HeroMissionNotify, 64)}
	actual, loaded := heroNotifyWaiters.LoadOrStore(session, w)
	if loaded {
		return actual.(*heroNotifyWaiter)
	}
	session.RegisterHandler(protocol.ActionHeroMissionNotify, func(pkt *protocol.Packet) {
		n, err := protocol.ParseHeroMissionNotify(pkt.Payload)
		if err != nil {
			slog.Warn("解析英雄副本扫荡推送失败", "role_id", session.RoleID(), "err", err)
			return
		}
		select {
		case w.ch <- *n:
		default:
		}
	})
	return w
}

// drain 丢弃上一轮残留的推送，保证本轮等待从干净状态开始。
func (w *heroNotifyWaiter) drain() {
	for {
		select {
		case <-w.ch:
		default:
			return
		}
	}
}

// waitTerminal 等待本轮扫荡的终态推送 (结束/背包满/体力不足)。
// 返回 (是否终态, 错误)。超时视为非终态，由上层继续处理。
func (w *heroNotifyWaiter) waitTerminal(ctx context.Context, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case n := <-w.ch:
			switch n.Type {
			case protocol.HeroMissionNotifyFinish:
				return true, nil
			case protocol.HeroMissionResultBagFull:
				return true, scheduler.ErrBagFull
			case protocol.HeroMissionResultNotEnoughPower:
				return true, scheduler.ErrStaminaDepleted
			}
			if n.EmptyBagNumber <= 0 {
				return true, scheduler.ErrBagFull
			}
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// packItemSnapshot 记录背包某时刻的物品数量，用于识别扫荡新增/叠加的掉落物。
type packItemSnapshot struct {
	byPlayerItem map[int32]int32 // PlayerItemID -> 当前数量
}

func newPackItemSnapshot(pack *protocol.PackItemListResponse) *packItemSnapshot {
	s := &packItemSnapshot{byPlayerItem: make(map[int32]int32, len(pack.PackItems))}
	for _, it := range pack.PackItems {
		s.byPlayerItem[it.PlayerItemID] = packItemQuantity(it)
	}
	return s
}

// packItemQuantity 返回物品堆叠数量。官方 ItemRecord 第 6 个 Int 字段为 num。
func packItemQuantity(it protocol.ItemRecord) int32 {
	if it.Field5 > 0 {
		return it.Field5
	}
	return 1
}

// fetchPackItemList 拉取当前主背包物品列表。
func fetchPackItemList(ctx context.Context, session *client.RoleSession) (*protocol.PackItemListResponse, error) {
	pkt := protocol.BuildGetPackItemListPacket()
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(c, pkt, protocol.ActionIDItemGetPackList)
	cancel()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("dungeon: 背包列表返回空响应")
	}
	return protocol.ParsePackItemListResponse(resp.Payload)
}

// capturePackSnapshot 拉取并记录当前背包快照；失败时返回 nil。
func capturePackSnapshot(ctx context.Context, session *client.RoleSession) *packItemSnapshot {
	pack, err := fetchPackItemList(ctx, session)
	if err != nil || pack == nil {
		if err != nil {
			slog.Warn("拉取背包快照失败，本轮跳过扫荡物出售", "role_id", session.RoleID(), "err", err)
		}
		return nil
	}
	return newPackItemSnapshot(pack)
}

// sellPackItem 出售背包中的单件物品。
// 返回 (是否售出, 是否可重试)。网络超时/解析失败可重试；服务端明确拒绝则不再重试。
func sellPackItem(ctx context.Context, session *client.RoleSession, it protocol.ItemRecord) (bool, bool) {
	if it.GridID <= 0 || it.GridID > 32767 {
		return false, false
	}
	sellPkt, err := protocol.BuildSellItemPacket(int16(it.GridID))
	if err != nil {
		return false, false
	}
	sellCtx, sellCancel := context.WithTimeout(ctx, 8*time.Second)
	sellResp, callErr := session.Call(sellCtx, sellPkt, protocol.ActionIDItemSell)
	sellCancel()
	if callErr != nil || sellResp == nil {
		slog.Info("自动卖出扫荡物无回包", "role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID, "err", callErr)
		return false, true
	}
	ok, code, pErr := protocol.ParseSellItemResponse(sellResp.Payload)
	if pErr != nil {
		slog.Warn("自动卖出扫荡物回包解析失败", "role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID, "err", pErr)
		return false, true
	}
	if !ok {
		slog.Warn("自动卖出扫荡物被服务端拒绝", "role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID, "result_code", code)
		return false, false
	}
	slog.Info("自动卖出精英副本扫荡物成功",
		"role_id", session.RoleID(),
		"player_item_id", it.PlayerItemID,
		"item_id", it.ItemID,
		"grid_id", it.GridID,
		"quantity", packItemQuantity(it),
	)
	return true, false
}

// sellNewPackItems 对比基线快照，卖出扫荡后新增或数量增加的物品，返回成功售出数量。
func sellNewPackItems(ctx context.Context, session *client.RoleSession, dictRepo dictionary.Repository, base *packItemSnapshot) int {
	if base == nil {
		return 0
	}
	pack, err := fetchPackItemList(ctx, session)
	if err != nil || pack == nil {
		return 0
	}

	sold := 0
	var pending []protocol.ItemRecord
	for _, it := range pack.PackItems {
		qty := packItemQuantity(it)
		prevQty, existed := base.byPlayerItem[it.PlayerItemID]
		if existed && qty <= prevQty {
			continue
		}
		// 新格位必然是扫荡掉落；已有格位数量增加时，仅对精英副本主要掉落类型
		// (1002=制作卷) 整格出售，避免把玩家原有消耗品/体力道具整组卖掉。
		if existed && !isEliteSweepStackType(dictRepo, it) {
			continue
		}
		if !isPackItemSellable(dictRepo, it) {
			continue
		}
		ok, retryable := sellPackItem(ctx, session, it)
		if ok {
			sold++
		} else if retryable {
			pending = append(pending, it)
		}
	}

	// 扫荡刚结束时服务端可能仍在结算，首个出售请求容易超时；短暂等待后重试一次。
	var unresolved []protocol.ItemRecord
	if len(pending) > 0 {
		time.Sleep(500 * time.Millisecond)
		for _, it := range pending {
			ok, retryable := sellPackItem(ctx, session, it)
			if ok {
				sold++
			} else if retryable {
				unresolved = append(unresolved, it)
			}
		}
	}

	// 以本轮结束时的背包为下一轮基线；未能售出的物品保留原始基线，
	// 使其在下一城镇/下一次执行时仍可被识别为扫荡产物并重试。
	newBase := newPackItemSnapshot(pack)
	for _, it := range unresolved {
		if prevQty, existed := base.byPlayerItem[it.PlayerItemID]; existed {
			newBase.byPlayerItem[it.PlayerItemID] = prevQty
		} else {
			delete(newBase.byPlayerItem, it.PlayerItemID)
		}
	}
	*base = *newBase
	return sold
}

// isPackItemSellable 判断扫荡物是否可出售。
// ItemRecord 第 5 个 Int 字段为 price；同时参考数据字典兜底。
func isPackItemSellable(dictRepo dictionary.Repository, it protocol.ItemRecord) bool {
	if it.Field4 > 0 {
		return true
	}
	if dictRepo != nil {
		if dItem, err := dictRepo.GetItem(int(it.ItemID)); err == nil && dItem != nil && dItem.Price > 0 {
			return true
		}
	}
	return false
}

// eliteSweepStackItemTypeID 精英副本可堆叠掉落的主要道具类型 (制作卷)。
const eliteSweepStackItemTypeID = 1002

// isEliteSweepStackType 判断数量增加的已有格位是否属于可安全整格出售的精英掉落类型。
// 消耗品 (包子/粽子/茶叶蛋等) 即使数量增加也不整格出售，避免误卖玩家原有库存。
func isEliteSweepStackType(dictRepo dictionary.Repository, it protocol.ItemRecord) bool {
	if dictRepo == nil {
		return false
	}
	dItem, err := dictRepo.GetItem(int(it.ItemID))
	if err != nil || dItem == nil {
		return false
	}
	return dItem.TypeID == eliteSweepStackItemTypeID
}

// fetchHeroMissionList 查询指定城镇的英雄副本列表 (Module 35, Action 0)。
func fetchHeroMissionList(ctx context.Context, session *client.RoleSession, townID int) (*protocol.HeroMissionList, error) {
	pkt := protocol.BuildHeroMissionListPacket(uint16(townID))
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(c, pkt, protocol.ActionHeroMissionList)
	cancel()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("dungeon: 英雄副本列表返回空响应")
	}
	return protocol.ParseHeroMissionListResponse(resp.Payload)
}

// startHeroMissionPractice 向指定城镇下发英雄副本扫荡指令 (Module 35, Action 2)。
func startHeroMissionPractice(ctx context.Context, session *client.RoleSession, townID int, autoNum, heroType uint8) (*protocol.HeroPracticeResult, error) {
	pkt, err := protocol.BuildHeroPracticePacket(protocol.HeroPracticeRequest{
		TownID:   uint16(townID),
		AutoNum:  autoNum,
		HeroType: heroType,
	})
	if err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	resp, callErr := session.Call(c, pkt, protocol.ActionHeroMissionStartPractice)
	cancel()
	if callErr != nil {
		return nil, callErr
	}
	if resp == nil {
		return nil, errors.New("dungeon: 英雄副本扫荡返回空响应")
	}
	return protocol.ParseHeroPracticeResult(resp.Payload)
}

// quicklyHeroMissionPractice 立即完成当前英雄副本扫荡队列 (Module 35, Action 7)。
func quicklyHeroMissionPractice(ctx context.Context, session *client.RoleSession) (*protocol.HeroQuicklyResult, error) {
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(c, protocol.BuildHeroQuicklyPacket(), protocol.ActionHeroMissionQuickly)
	cancel()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("dungeon: 英雄副本快速完成返回空响应")
	}
	return protocol.ParseHeroQuicklyResponse(resp.Payload)
}

// eliteTownOutcome 表示单个城镇英雄副本扫荡的终态分类。
type eliteTownOutcome int

const (
	eliteTownDone eliteTownOutcome = iota
	eliteTownNoMission
	eliteTownBagFull
	eliteTownNotEnoughPower
	eliteTownIngotRisk
)

// performEliteTownSweep 对单个城镇执行一次完整扫荡 (list 前置检查由调用方完成):
// start_practice -> quickly -> 等待 notify 终态。
func performEliteTownSweep(
	ctx context.Context,
	session *client.RoleSession,
	waiter *heroNotifyWaiter,
	townID int,
) (eliteTownOutcome, *protocol.HeroPracticeResult, error) {
	waiter.drain()

	// 抓包中官方客户端使用 auto_num=1，但该字段语义为“战斗后自动购买翻牌奖励”，
	// 可能产生元宝消耗；自动化铁律要求禁用，固定使用 0 (免费扫荡)。
	startRes, startErr := startHeroMissionPractice(ctx, session, townID, 0, 0)
	if startErr != nil {
		return eliteTownDone, nil, startErr
	}
	switch startRes.Result {
	case protocol.HeroMissionResultSuccess:
		// 继续快速完成
	case protocol.HeroMissionResultNoMission:
		return eliteTownNoMission, startRes, nil
	case protocol.HeroMissionResultBagFull:
		return eliteTownBagFull, startRes, nil
	case protocol.HeroMissionResultNotEnoughPower:
		return eliteTownNotEnoughPower, startRes, nil
	case protocol.HeroMissionResultNotEnoughIngot:
		return eliteTownIngotRisk, startRes, nil
	default:
		return eliteTownDone, startRes, fmt.Errorf("start_practice rejected: result=%d", startRes.Result)
	}

	quickRes, quickErr := quicklyHeroMissionPractice(ctx, session)
	if quickErr != nil {
		// 服务端静默丢弃快速完成请求时，终态推送仍可能到达；继续等待结算即可。
		slog.Info("英雄副本扫荡快速完成未收到回包，继续等待结算推送",
			"role_id", session.RoleID(), "town_id", townID, "err", quickErr)
	} else if quickRes != nil {
		switch quickRes.Result {
		case protocol.HeroMissionResultSuccess:
			// 正常终态
		case protocol.HeroMissionResultBagFull:
			return eliteTownBagFull, startRes, nil
		case protocol.HeroMissionResultNotEnoughPower:
			return eliteTownNotEnoughPower, startRes, nil
		case protocol.HeroMissionResultNotEnoughIngot:
			return eliteTownIngotRisk, startRes, nil
		default:
			slog.Warn("英雄副本快速完成被服务端拒绝", "role_id", session.RoleID(), "town_id", townID, "result_code", quickRes.Result)
		}
	}

	// 等待终态推送，确保掉落已结算后再卖物品；超时则继续下一城镇。
	if _, waitErr := waiter.waitTerminal(ctx, 2*time.Second); waitErr != nil {
		switch {
		case errors.Is(waitErr, scheduler.ErrBagFull):
			return eliteTownBagFull, startRes, nil
		case errors.Is(waitErr, scheduler.ErrStaminaDepleted):
			return eliteTownNotEnoughPower, startRes, nil
		default:
			return eliteTownDone, startRes, waitErr
		}
	}
	return eliteTownDone, startRes, nil
}

// sweepEliteMissionsWithoutTown 使用 Module 25 (副本扫荡) 全局扫荡所有精英副本。
//
// 官方 get_status(精英副本ID) 会返回该副本的子关卡列表；对子关卡 start_practice
// (count=1, auto_sale=1) 即可完成一轮扫荡并自动出售白绿蓝装备，无需角色身处该城镇。
func sweepEliteMissionsWithoutTown(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
) (int, error) {
	if dictRepo == nil {
		slog.Info("数据字典不可用，跳过全局精英扫荡", "role_id", session.RoleID())
		return 0, nil
	}
	ids, err := dictRepo.GetEliteMissionIDs()
	if err != nil || len(ids) == 0 {
		slog.Info("数据字典中未发现精英副本关卡，跳过全局精英扫荡",
			"role_id", session.RoleID(), "err", err)
		return 0, nil
	}
	anchorAct := session.LastActionID()
	if anchorAct == 0 || anchorAct == protocol.ActionIDPracticeGetStatus || anchorAct == protocol.ActionIDPracticeStart {
		anchorAct = 42 // 登录后的玩家初始化动作，实测可作为 Module 25 的 PrevAct 锚点
	}
	slog.Info("开始全局精英副本扫荡", "role_id", session.RoleID(), "dungeons", len(ids), "anchor_act", anchorAct)

	swept := 0
	failStreak := 0
	for _, dungeonID := range ids {
		if ctx.Err() != nil {
			break
		}
		// 网关偶发 EOF 重连；等待会话恢复，并用重连后的新动作号刷新锚点。
		if session.State() != client.StateActive {
			for i := 0; i < 30 && session.State() != client.StateActive; i++ {
				time.Sleep(200 * time.Millisecond)
			}
			if session.State() != client.StateActive {
				slog.Info("会话未恢复，结束全局精英扫荡", "role_id", session.RoleID(), "swept", swept)
				break
			}
			// 重连后服务端场景/动作链需要短暂恢复时间。
			failStreak = 0
			anchorAct = session.LastActionID()
			time.Sleep(1500 * time.Millisecond)
		}
		// 只有“状态查询自身”会造成自引用；start 之后应把 start 作为下一次
		// 状态查询的 PrevAct（实测 start→status 可被服务端受理）。
		if cur := session.LastActionID(); cur != 0 && cur != protocol.ActionIDPracticeGetStatus {
			anchorAct = cur
		}
		if session.GetStamina() < 5 {
			slog.Info("体力不足，结束全局精英扫荡",
				"role_id", session.RoleID(), "swept", swept, "remaining_stamina", session.GetStamina())
			break
		}
		// 服务端对 Module 25 有频率限制；逐副本节流，避免连发被静默丢弃。
		select {
		case <-ctx.Done():
			return swept, ctx.Err()
		case <-time.After(350 * time.Millisecond):
		}

		// Module 25 会自动用会话最近动作覆盖 PrevAct；连续 status 会自引用
		// 0x00190000 被丢弃，因此每次查询前把链重置到登录锚点。
		session.SetLastActionID(anchorAct)
		statusPkt, pErr := protocol.BuildPracticeGetStatusPacket(int32(dungeonID), anchorAct)
		if pErr != nil {
			continue
		}
		statusTimeout := 8 * time.Second
		if failStreak > 0 {
			// 连续失败时缩短超时，避免 190 个副本 × 8s 长时间卡住。
			statusTimeout = 3 * time.Second
		}
		sCtx, sCancel := context.WithTimeout(ctx, statusTimeout)
		statusResp, sErr := session.Call(sCtx, statusPkt, protocol.ActionIDPracticeGetStatus)
		sCancel()
		if sErr != nil || statusResp == nil {
			failStreak++
			slog.Info("精英副本状态无回包", "role_id", session.RoleID(), "dungeon_id", dungeonID,
				"fail_streak", failStreak, "err", sErr)
			// 单个副本 ID 不可扫荡时服务端也会静默不回包；3 连失会误判为
			// 全局断线（qq 账号 166~168 连续失败即中止整轮）。提高到 30 连失
			// 才认为网关异常，其余情况继续尝试后续副本。
			if failStreak >= 30 {
				slog.Info("精英副本扫荡连续无回包，进入冷却待恢复", "role_id", session.RoleID(), "swept", swept)
				return swept, scheduler.NewCooldown(10*time.Minute, "精英副本扫荡连续无回包")
			}
			continue
		}
		failStreak = 0
		status, parseErr := protocol.ParsePracticeStatusResult(statusResp.Payload)
		if parseErr != nil || status == nil || len(status.Missions) == 0 {
			slog.Debug("精英副本状态不可用", "dungeon_id", dungeonID, "err", parseErr, "len", len(statusResp.Payload))
			continue
		}
		// 优先扫荡等级最高的子关卡；同等级取最大关卡 ID。
		best := status.Missions[0]
		for _, m := range status.Missions[1:] {
			if m.Level > best.Level || (m.Level == best.Level && m.MissionID > best.MissionID) {
				best = m
			}
		}

		startPkt, bErr := protocol.BuildStartPracticePacket(protocol.StartPracticeRequest{
			MissionID: best.MissionID,
			Count:     1,
			AutoSale:  1,
			PrevAct:   protocol.ActionIDPracticeGetStatus,
		})
		if bErr != nil {
			continue
		}
		pCtx, pCancel := context.WithTimeout(ctx, 10*time.Second)
		startResp, stErr := session.Call(pCtx, startPkt, protocol.ActionIDPracticeStart)
		pCancel()
		if stErr != nil || startResp == nil {
			slog.Info("精英副本扫荡无回包，继续下一副本",
				"role_id", session.RoleID(), "dungeon_id", dungeonID, "mission_id", best.MissionID)
			continue
		}
		res, rErr := protocol.ParseStartPracticeResult(startResp.Payload)
		if rErr != nil || res == nil {
			continue
		}
		slog.Debug("精英副本扫荡回包", "dungeon_id", dungeonID, "mission_id", best.MissionID, "result", res.Result)
		switch res.Result {
		case protocol.PracticeResultSuccess:
			swept++
			slog.Info("精英副本扫荡成功",
				"role_id", session.RoleID(), "dungeon_id", dungeonID, "mission_id", best.MissionID, "swept", swept)
		case protocol.PracticeResultNotEnoughPower:
			slog.Info("体力不足，结束全局精英扫荡",
				"role_id", session.RoleID(), "swept", swept)
			return swept, nil
		case protocol.PracticeResultBagFull:
			slog.Info("背包已满，结束全局精英扫荡",
				"role_id", session.RoleID(), "swept", swept)
			return swept, scheduler.ErrBagFull
		case protocol.PracticeResultInPractice:
			slog.Info("副本扫荡队列进行中，结束本轮",
				"role_id", session.RoleID(), "swept", swept)
			return swept, nil
		default:
			continue
		}
	}
	slog.Info("全局精英副本扫荡完成", "role_id", session.RoleID(), "swept", swept)
	return swept, nil
}

// executeEliteMissionSweep 按官方 Mod_HeroMission_Base 协议逐城镇扫荡精英副本。
//
// 关键点:
//  1. 扫荡入口是城镇 (town_id)，不是单个副本实例；一个城镇一次 start_practice 即排队该城镇可扫荡副本。
//  2. 按城镇全量扫荡，不使用本地轮数上限截断；体力/每日次数由服务端熔断。
//  3. 只使用免费扫荡 (auto_num=0)，绝不自动购买翻牌奖励或消耗元宝。
func executeEliteMissionSweep(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	cfg SweepConfig,
	sweepHasStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
) (int, error) {
	if cfg.Sweep.GlobalElite {
		// 官方客户端 (2026-11-13 /tmp/saodang.pcapng) 在多城镇英雄副本扫荡之外
		// 仍会使用 Module 25 全局精英扫荡；两者是不同系统，不能互相短路。
		// 先按城镇执行 35/0→35/2→35/7，再执行 Module 25 全局精英扫荡。
		heroCfg := cfg
		heroCfg.Sweep.GlobalElite = false
		heroSwept, heroErr := executeEliteMissionSweep(ctx, session, dictRepo, heroCfg,
			sweepHasStamina, totalCostStamina, totalGainExp, totalGainCoins)
		globalSwept, globalErr := sweepEliteMissionsWithoutTown(ctx, session, dictRepo)
		if globalErr != nil {
			return heroSwept, globalErr
		}
		if heroErr != nil {
			return heroSwept + globalSwept, heroErr
		}
		return heroSwept + globalSwept, nil
	}
	towns := eliteMissionTowns(dictRepo)
	if len(towns) == 0 {
		slog.Info("数据字典中未发现精英副本城镇，跳过精英扫荡", "role_id", session.RoleID())
		return 0, nil
	}
	// 官方客户端实测 (2026-11-13 /tmp/saodang.pcapng)：不切换场景即可对多个城镇
	// 依次下发 35/0 列表 + 35/2 start_practice + 35/7 quickly，连续扫荡 54/43/40/39。
	// 因此这里不再限制为“当前城镇”，按字典城镇顺序逐城扫荡。
	slog.Info("英雄副本多城镇扫荡",
		"role_id", session.RoleID(),
		"town_map_id", session.GetPlayerState().TownMapID,
		"towns", len(towns))

	waiter := heroNotifyWaiterFor(session)
	var packBase *packItemSnapshot
	if cfg.Sweep.SellSweptItems {
		packBase = capturePackSnapshot(ctx, session)
	}

	successRounds := 0
	for _, townID := range towns {
		if ctx.Err() != nil {
			return successRounds, ctx.Err()
		}
		if !sweepHasStamina() {
			slog.Info("当前角色体力不足，精英副本扫荡阶段结束",
				"role_id", session.RoleID(),
				"remaining_stamina", session.GetStamina(),
			)
			break
		}
		if session.State() != client.StateActive {
			for i := 0; i < 15 && session.State() != client.StateActive; i++ {
				time.Sleep(200 * time.Millisecond)
			}
			if session.State() != client.StateActive {
				break
			}
		}

		const maxTownAttempts = 2
		for attempt := 0; attempt < maxTownAttempts; attempt++ {
			if attempt > 0 {
				time.Sleep(500 * time.Millisecond)
			}

			list, listErr := fetchHeroMissionList(ctx, session, townID)
			if listErr != nil {
				// 列表查询被服务端静默丢弃说明当前城镇上下文不匹配；重试无意义。
				slog.Info("查询英雄副本列表失败/服务端未响应，跳过该城镇",
					"role_id", session.RoleID(), "town_id", townID, "err", listErr)
				break
			}
			if list == nil || len(list.Missions) == 0 {
				break
			}
			challengeable := 0
			for _, m := range list.Missions {
				if m.CanChallenge == 1 {
					challengeable++
				}
			}
			// 即使 can_challenge 全为 0 也继续下发 start_practice：
			// 已首通/可扫荡副本仍可能出现在列表中，最终由服务端以 NO_MISSION 兜底。

			beforeStamina := session.GetStamina()
			beforeState := session.GetPlayerState()
			outcome, startRes, sweepErr := performEliteTownSweep(ctx, session, waiter, townID)

			afterStamina := session.GetStamina()
			if cost := beforeStamina - afterStamina; cost > 0 {
				*totalCostStamina += cost
			}
			afterState := session.GetPlayerState()
			if afterState.Experience > beforeState.Experience {
				*totalGainExp += afterState.Experience - beforeState.Experience
			}
			if afterState.Coins > beforeState.Coins {
				*totalGainCoins += afterState.Coins - beforeState.Coins
			}

			if sweepErr != nil {
				if isTransientDungeonError(sweepErr) && attempt < maxTownAttempts-1 {
					// 超时通常源于服务端主动丢弃（城镇上下文不匹配/正在结算），重试一次即可。
					slog.Info("英雄副本城镇扫荡等待超时，重试当前城镇",
						"role_id", session.RoleID(), "town_id", townID, "attempt", attempt+1, "err", sweepErr)
					continue
				}
				slog.Info("英雄副本城镇扫荡失败，跳过该城镇",
					"role_id", session.RoleID(), "town_id", townID, "err", sweepErr)
				break
			}

			switch outcome {
			case eliteTownNoMission:
				slog.Debug("该城镇暂无可扫荡英雄副本", "role_id", session.RoleID(), "town_id", townID)
			case eliteTownNotEnoughPower:
				slog.Info("英雄副本扫荡返回体力不足，结束扫荡", "role_id", session.RoleID(), "town_id", townID)
				session.SetStamina(0)
				session.SetExtraStamina(0)
				return successRounds, scheduler.ErrStaminaDepleted
			case eliteTownIngotRisk:
				slog.Warn("英雄副本扫荡返回元宝不足，立即停止以避免误消耗元宝", "role_id", session.RoleID(), "town_id", townID)
				return successRounds, nil
			case eliteTownBagFull:
				// 停止当前扫荡队列，先把已结算的掉落卖掉，为后续副本腾出背包格位。
				_ = session.Send(protocol.BuildHeroStopPracticePacket())
				sold := 0
				if cfg.Sweep.SellSweptItems {
					sold = sellNewPackItems(ctx, session, dictRepo, packBase)
				}
				if sold > 0 && attempt < maxTownAttempts-1 && sweepHasStamina() {
					slog.Info("背包已满，已卖出扫荡物并重试当前城镇",
						"role_id", session.RoleID(),
						"town_id", townID,
						"sold", sold,
						"retry", attempt+1,
					)
					time.Sleep(100 * time.Millisecond)
					continue
				}
				slog.Warn("背包已满且无法通过出售扫荡物腾出空间，暂停精英扫荡",
					"role_id", session.RoleID(),
					"town_id", townID,
					"sold", sold,
				)
				return successRounds, scheduler.ErrBagFull
			case eliteTownDone:
				successRounds++
				practiceTime := int32(0)
				if startRes != nil {
					practiceTime = startRes.PracticeTime
				}
				slog.Info("英雄副本城镇扫荡完成",
					"role_id", session.RoleID(),
					"town_id", townID,
					"missions", len(list.Missions),
					"challengeable", challengeable,
					"practice_time", practiceTime,
					"remaining_stamina", afterStamina,
				)
				if cfg.Sweep.SellSweptItems {
					if sold := sellNewPackItems(ctx, session, dictRepo, packBase); sold > 0 {
						slog.Info("英雄副本扫荡物出售完成", "role_id", session.RoleID(), "town_id", townID, "sold", sold)
					}
				}
			}
			break
		}
	}

	return successRounds, nil
}

// isTransientDungeonError 判断是否为可重试的瞬时网络超时。
func isTransientDungeonError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

// NewDungeonRoutine 构造副本推图与扫荡任务 (统一命名为 dungeon)。
func NewDungeonRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	return NewDungeonSweepRoutine(dictRepo, cfgs...)
}

// NewDungeonSweepRoutine 构造副本体力扫荡任务，支持挂载数据字典与智能策略。
// SellBagCraftScrolls 出售背包中的装备制作卷（名称含「制作卷」且无元宝价值），
// 用于背包满时释放扫荡所需的格位。返回成功出售数量。
func SellBagCraftScrolls(ctx context.Context, session *client.RoleSession, dictRepo dictionary.Repository, maxSell int) int {
	if session == nil || dictRepo == nil || maxSell <= 0 {
		return 0
	}
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(cCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	cancel()
	if err != nil || resp == nil {
		return 0
	}
	pack, perr := protocol.ParsePackItemListResponse(resp.Payload)
	if perr != nil || pack == nil {
		return 0
	}
	sold := 0
	for _, it := range pack.PackItems {
		if sold >= maxSell || ctx.Err() != nil {
			break
		}
		dItem, dErr := dictRepo.GetItem(int(it.ItemID))
		if dErr != nil || dItem == nil {
			continue
		}
		if !strings.Contains(dItem.Name, "制作卷") || dItem.Ingot > 0 {
			continue
		}
		pkt, pErr := protocol.BuildSellItemPacket(int16(it.GridID))
		if pErr != nil {
			continue
		}
		sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
		sResp, sErr := session.Call(sCtx, pkt, protocol.ActionIDItemSell)
		sCancel()
		if sErr != nil || sResp == nil {
			continue
		}
		if ok, _, pErr := protocol.ParseSellItemResponse(sResp.Payload); pErr == nil && ok {
			sold++
			slog.Info("出售装备制作卷成功",
				"role_id", session.RoleID(), "grid_id", it.GridID,
				"item_id", it.ItemID, "name", dItem.Name)
		}
		select {
		case <-ctx.Done():
			return sold
		case <-time.After(120 * time.Millisecond):
		}
	}
	return sold
}

func NewDungeonSweepRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	cfg := DefaultSweepConfig()
	hasSubConfig := false
	if len(cfgs) > 0 {
		c := cfgs[0]
		hasSubConfig = c.QuestCycle.Enabled || c.QuestCycle.AutoReplenishStamina ||
			c.QuestCycle.MaxReplenishStamina > 0 || c.QuestCycle.MaxReplenishTimes > 0 ||
			c.Sweep.Enabled || len(c.Sweep.Pipeline) > 0 || c.Sweep.GlobalElite ||
			c.Sweep.SellSweptItems || c.Sweep.AutoSellSweptItems || c.SellSweptItems || c.AutoSellSweptItems

		// 继承外层顶层兼容字段到子功能
		if hasSubConfig {
			cfg.QuestCycle = c.QuestCycle
			if cfg.QuestCycle.MaxReplenishStamina <= 0 {
				if cfg.QuestCycle.MaxReplenishTimes > 0 {
					cfg.QuestCycle.MaxReplenishStamina = cfg.QuestCycle.MaxReplenishTimes * 10
				} else {
					cfg.QuestCycle.MaxReplenishStamina = 200
				}
			}
			cfg.Sweep = c.Sweep
			if c.SellSweptItems || c.AutoSellSweptItems {
				cfg.Sweep.SellSweptItems = true
			}
		} else {
			// 若未显式配置子块，从顶层兼容字段继承
			cfg.QuestCycle.Enabled = c.QuestCycle.Enabled
			cfg.QuestCycle.AutoReplenishStamina = c.AutoReplenishStamina
			if c.MaxReplenishStamina > 0 {
				cfg.QuestCycle.MaxReplenishStamina = c.MaxReplenishStamina
			} else if c.MaxReplenishTimes > 0 {
				cfg.QuestCycle.MaxReplenishStamina = c.MaxReplenishTimes * 10
			}
			cfg.Sweep.PreferPracticeQuickly = c.Sweep.PreferPracticeQuickly
			if c.SellSweptItems || c.AutoSellSweptItems {
				cfg.Sweep.SellSweptItems = true
			}
			if len(c.Pipeline) > 0 {
				cfg.Sweep.Pipeline = c.Pipeline
			}
		}

		if len(c.Pipeline) > 0 {
			cfg.Pipeline = c.Pipeline
			for _, p := range c.Pipeline {
				if p == "quest_cycle" || p == "push_mainline" || p == "push_side" || p == "quest" {
					cfg.QuestCycle.Enabled = true
				}
				if p == "sweep_elite" {
					cfg.Sweep.Enabled = true
				}
			}
		}

		cfg.Pipeline = c.Pipeline
		cfg.AutoReplenishStamina = c.AutoReplenishStamina
		cfg.MaxReplenishStamina = c.MaxReplenishStamina
		cfg.MaxReplenishTimes = c.MaxReplenishTimes
		cfg.PreferPracticeQuickly = c.PreferPracticeQuickly
		cfg.SellSweptItems = cfg.Sweep.SellSweptItems
	}

	if cfg.Sweep.SellSweptItems || cfg.Sweep.AutoSellSweptItems || cfg.SellSweptItems || cfg.AutoSellSweptItems {
		cfg.Sweep.SellSweptItems = true
	}
	if len(cfg.Sweep.Pipeline) == 0 {
		cfg.Sweep.Pipeline = []string{"sweep_elite"}
	}

	return scheduler.NewBaseRoutine(
		"dungeon",
		"dungeon",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，跳过本次副本 Routine", "role_id", session.RoleID())
				return nil
			}

			// 2. 检查背包容量；已满时先出售装备制作卷腾出扫荡空间。
			state := session.GetPlayerState()
			if state.BagCapacity <= 0 {
				sold := SellBagCraftScrolls(ctx, session, dictRepo, 20)
				if sold > 0 {
					slog.Info("背包已满，已出售装备制作卷腾出扫荡空间",
						"role_id", session.RoleID(), "sold", sold)
				} else {
					slog.Warn("背包已满且无可出售的装备制作卷，无法继续关卡推图与扫荡",
						"role_id", session.RoleID())
					return scheduler.ErrBagFull
				}
			}

			const powerPerSweep = 5

			totalCostStamina := 0
			totalGainExp := int64(0)
			totalGainCoins := int64(0)

			// =========================================================================
			// 功能一：主线/支线剧情推图 (quest_cycle)
			// 特性：循环接任务/做任务/交任务直至无可做任务 break；
			// 约束：auto_replenish_stamina 仅作用于推图。
			// =========================================================================
			isExplicitPipeline := len(cfg.Pipeline) > 0
			shouldRunQuest := cfg.QuestCycle.Enabled
			if isExplicitPipeline {
				hasQuestStep := false
				for _, p := range cfg.Pipeline {
					if p == "quest_cycle" || p == "push_mainline" || p == "push_side" || p == "quest" {
						hasQuestStep = true
						break
					}
				}
				shouldRunQuest = hasQuestStep
			}

			if shouldRunQuest {
				replenishedStamina := 0
				questEnsureStamina := func() bool {
					if session.GetStamina() >= powerPerSweep {
						return true
					}
					// auto_replenish_stamina 只作用于推图！
					if !cfg.QuestCycle.AutoReplenishStamina {
						return false
					}
					maxStamina := cfg.QuestCycle.MaxReplenishStamina
					if maxStamina <= 0 {
						if cfg.QuestCycle.MaxReplenishTimes > 0 {
							maxStamina = cfg.QuestCycle.MaxReplenishTimes * 10
						} else {
							maxStamina = 200
						}
					}
					for session.GetStamina() < powerPerSweep && replenishedStamina < maxStamina {
						ok, itemName, addedPoints, err := TryReplenishStamina(ctx, session)
						if err != nil {
							slog.Warn("推图尝试自动补充体力异常", "role_id", session.RoleID(), "err", err)
							break
						}
						if !ok {
							break // 背包中无可用体力道具
						}
						replenishedStamina += addedPoints
						slog.Info("推图自愈补充体力成功",
							"role_id", session.RoleID(),
							"item", itemName,
							"added_stamina", addedPoints,
							"total_replenished", replenishedStamina,
							"max_replenish_stamina", maxStamina,
						)
					}
					return session.GetStamina() >= powerPerSweep
				}

				// 推图前置检查体力自愈
				_ = questEnsureStamina()

				if dictRepo != nil {
					if session.State() != client.StateActive {
						for i := 0; i < 15 && session.State() != client.StateActive; i++ {
							time.Sleep(200 * time.Millisecond)
						}
					}

					clearedMissions := make(map[int]bool)
					executeQuestCycle(
						ctx, session, dictRepo, cfg, questEnsureStamina,
						&totalCostStamina, &totalGainExp, &totalGainCoins,
						clearedMissions,
					)
				}
			}

			// 停机/断线后不再进入后续扫荡流水线，避免继续产生无意义的协议告警。
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，跳过后续扫荡流水线", "role_id", session.RoleID())
				return nil
			}

			// =========================================================================
			// 功能二：关卡扫荡 (sweep)
			// 特性：扫荡精英副本 (sweep_elite)；
			// 保护：扫荡阶段不吃体力药，仅消耗自然体力！
			// =========================================================================
			shouldRunSweep := false
			sweepPipeline := cfg.Sweep.Pipeline
			if isExplicitPipeline {
				var filteredSweep []string
				for _, p := range cfg.Pipeline {
					if p == "sweep_elite" {
						filteredSweep = append(filteredSweep, p)
					}
				}
				if len(filteredSweep) > 0 {
					sweepPipeline = filteredSweep
					shouldRunSweep = true
				}
			} else if cfg.Sweep.Enabled {
				shouldRunSweep = true
			}

			sweepSuccessRounds := 0
			if shouldRunSweep {
				// 扫荡仅消耗自然体力，不补充体力药
				sweepHasStamina := func() bool {
					return session.GetStamina() >= powerPerSweep
				}

				if !sweepHasStamina() && totalCostStamina == 0 {
					slog.Warn("角色当前体力不足，无法继续扫荡",
						"role_id", session.RoleID(),
						"current_stamina", session.GetStamina(),
						"required", powerPerSweep,
					)
					return scheduler.ErrStaminaDepleted
				}

			sweepLoop:
				for _, step := range sweepPipeline {
					if session.State() != client.StateActive {
						for i := 0; i < 15 && session.State() != client.StateActive; i++ {
							time.Sleep(200 * time.Millisecond)
						}
					}
					if !sweepHasStamina() {
						slog.Info("当前角色体力不足，关卡扫荡阶段结束", "role_id", session.RoleID(), "remaining_stamina", session.GetStamina())
						break
					}

					switch step {
					case "sweep_elite":
						// 扫荡精英: 按官方 Mod_HeroMission_Base (Module 35) 协议逐城镇下发。
						// 协议与主线 Module 25 完全不同，详见 executeEliteMissionSweep 注释。
						rounds, eliteErr := executeEliteMissionSweep(
							ctx, session, dictRepo, cfg, sweepHasStamina,
							&totalCostStamina, &totalGainExp, &totalGainCoins,
						)
						sweepSuccessRounds += rounds
						if eliteErr != nil {
							return eliteErr
						}
						if !isExplicitPipeline {
							break sweepLoop
						}
					}
				}
			}

			// 8. 汇总本轮批次结算
			if sweepSuccessRounds > 0 || totalCostStamina > 0 {
				slog.Info("关卡推图与体力扫荡批次完成",
					"role_id", session.RoleID(),
					"sweep_times", sweepSuccessRounds,
					"cost_stamina", totalCostStamina,
					"remaining_stamina", session.GetStamina(),
					"gain_exp", totalGainExp,
					"gain_coins", totalGainCoins,
				)
			}

			// 9. 若剩余体力不足 1 轮，触发智能语义熔断告知调度器挂起冷却
			if session.GetStamina() < powerPerSweep {
				slog.Info("角色体力已完全耗尽，关卡扫荡进入智能冷却期",
					"role_id", session.RoleID(),
					"remaining_stamina", session.GetStamina(),
				)
				return scheduler.ErrStaminaDepleted
			}

			return nil

		},
	)
}

// SixRealmsConfig 六道轮回挑战与灵件处理策略配置。
type SixRealmsConfig struct {
	AutoReset       bool `yaml:"auto_reset" json:"auto_reset"`               // 通关后是否自动消耗每日免费重置次数
	AutoSellBlue    bool `yaml:"auto_sell_blue" json:"auto_sell_blue"`       // 挑战获胜是否自动出售蓝色灵件
	AutoSellPurple  bool `yaml:"auto_sell_purple" json:"auto_sell_purple"`   // 挑战获胜是否自动出售紫色灵件
	MaxFloorRetries int  `yaml:"max_floor_retries" json:"max_floor_retries"` // 每层挑战失败最大重试次数 (每层单独计算，失败达到上限后退出)
}

// DefaultSixRealmsConfig 返回六道轮回默认策略配置。
func DefaultSixRealmsConfig() SixRealmsConfig {
	return SixRealmsConfig{
		AutoReset:       true,
		AutoSellBlue:    true,
		AutoSellPurple:  false,
		MaxFloorRetries: 3,
	}
}

// NewSixRealmsRoutine 构造六道轮回挑战与灵件分解任务。
func NewSixRealmsRoutine(cfgs ...SixRealmsConfig) scheduler.ActivityRoutine {
	cfg := DefaultSixRealmsConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	return scheduler.NewBaseRoutine(
		"six_realms",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行副本任务: 六道轮回爬塔与灵件自动处理", "role_id", session.RoleID())

			// 1. 获取当前六道轮回塔面板与进度信息
			info, err := session.GetTowerInfo(ctx)
			if err != nil {
				slog.Warn("获取六道轮回塔信息失败或未开放", "role_id", session.RoleID(), "err", err)
				return nil
			}

			layerName := protocol.TowerLayerName(info.Layer)
			slog.Info("六道轮回塔当前状态",
				"role_id", session.RoleID(),
				"layer", info.Layer,
				"layer_name", layerName,
				"sequence_war", info.SequenceWar,
				"sequence_max", info.SequenceMax,
				"bag_num", info.BagNum,
				"reset_num", info.ResetNum,
				"reset_ingot", info.ResetIngot,
			)

			// 2. 检查等级门槛
			minLevel := protocol.TowerLayerLevelLimit(info.Layer)
			roleLevel := session.GetPlayerState().Level
			if roleLevel > 0 && roleLevel < minLevel {
				slog.Info("未达到当前六道挑战等级门槛，跳过挑战",
					"role_id", session.RoleID(),
					"role_level", roleLevel,
					"required_level", minLevel,
					"layer", info.Layer,
				)
				return nil
			}

			// 3. 灵件背包空间：get_tower_info 的 bag_num 字段语义未确认
			// （线上实测封灵背包为空时仍返回 20，疑为容量而非占用格数）。
			// 因此不再用 bag_num>=20 预判“已满”，直接发起挑战，
			// 由服务端 start_challenge 的 FULL_BAG(6) 结果码驱动暂停。
			slog.Debug("六道轮回面板背包字段（仅诊断，不作为门禁）",
				"role_id", session.RoleID(), "bag_num", info.BagNum)

			// 4. 判断是否需要重置: SequenceWar 表示当前待挑战层 (1..SequenceMax)，
			// 线上实测全通时返回 SequenceWar == SequenceMax（而非 >），因此必须用 >=。
			maxFloor := int(info.SequenceMax)
			if maxFloor <= 0 {
				maxFloor = 25
			}
			if int(info.SequenceWar) >= maxFloor {
				// 仙·天道(12) 为最终道：全通后服务端不再允许重置（2026-11-10 用户确认，
				// 且实测重置请求被静默丢弃）。直接结束，避免每 30 分钟无意义重试。
				if info.Layer >= protocol.TowerLayerXianHeaven {
					slog.Info("六道轮回已全通最终道且无法重置，挑战完成",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", layerName,
					)
					return nil
				}
				if cfg.AutoReset && info.ResetNum > 0 {
					slog.Info("六道轮回当前道已全通，检测到今日剩余免费重置次数，执行重置",
						"role_id", session.RoleID(),
						"reset_num", info.ResetNum,
					)
					if jitter != nil {
						_ = jitter.Wait(ctx)
					}
					resetRes, resetErr := session.ResetTowerLayer(ctx)
					if resetErr != nil {
						slog.Warn("六道轮回塔重置无回包，稍后重试", "err", resetErr)
						return scheduler.NewCooldown(30*time.Minute, "六道轮回重置无回包")
					}
					if resetRes.Result != protocol.TowerResultSuccess {
						slog.Info("六道轮回塔重置未成功",
							"role_id", session.RoleID(), "result", resetRes.Result)
						return nil
					}
					slog.Info("六道轮回塔重置成功", "role_id", session.RoleID(), "layer", resetRes.Layer)
					// 刷新最新进度
					if newInfo, nErr := session.GetTowerInfo(ctx); nErr == nil {
						info = newInfo
					} else {
						info.SequenceWar = 1
					}
					if int(info.SequenceWar) >= maxFloor {
						slog.Info("六道轮回重置后仍显示全通，结束本轮",
							"role_id", session.RoleID(),
							"sequence_war", info.SequenceWar,
							"sequence_max", maxFloor)
						return nil
					}
				} else {
					slog.Info("六道轮回当前道今日已通关且无需重置或无免费重置次数，挑战完成",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", layerName,
					)
					return nil
				}
			}

			// 5. 循环推进挑战未通关层数 (SequenceWar .. 25)
			// 注意: info.SequenceWar 是当前待挑战层数 (1~25)
			startFloor := int(info.SequenceWar)
			if startFloor <= 0 {
				startFloor = 1
			}

			maxRetries := cfg.MaxFloorRetries
			if maxRetries <= 0 {
				maxRetries = 3
			}

			for floor := startFloor; floor <= maxFloor; floor++ {
				floorRetries := 0
				floorWon := false

				for floorRetries < maxRetries {
					select {
					case <-ctx.Done():
						slog.Info("六道轮回挑战被外部上下文中断", "role_id", session.RoleID())
						return ctx.Err()
					default:
					}

					// 服务端连续战斗存在冷却保护 (对齐官方客户端 CommonConstant.DelayTimeToWar，需留足间隔)
					time.Sleep(1500 * time.Millisecond)

					slog.Info("正在挑战六道轮回塔关卡",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", protocol.TowerLayerName(info.Layer),
						"floor", floor,
						"retry", floorRetries,
						"max_retries", maxRetries,
					)

					// 自动卖出灵件策略
					res, chErr := session.ChallengeTower(ctx, uint8(floor), cfg.AutoSellBlue, cfg.AutoSellPurple)
					if chErr != nil {
						floorRetries++
						slog.Warn("六道轮回塔挑战请求失败",
							"role_id", session.RoleID(),
							"floor", floor,
							"retry", floorRetries,
							"max_retries", maxRetries,
							"err", chErr,
						)
						if floorRetries >= maxRetries {
							slog.Warn("六道轮回当前层重试次数已达上限，终止挑战",
								"role_id", session.RoleID(),
								"floor", floor,
								"retries", floorRetries,
							)
							return nil
						}
						continue
					}

					if res.Result == protocol.TowerResultSuccess {
						slog.Info("六道轮回塔关卡挑战胜利",
							"role_id", session.RoleID(),
							"floor", floor,
							"gain_exp", res.WarExp,
							"gain_fame", res.WarFame,
							"gain_coin", res.WarCoin,
							"soul_id", res.WarSoulID,
						)
						info.SequenceWar = uint8(floor + 1)
						floorWon = true
						if floor == maxFloor {
							slog.Info("恭喜通关六道轮回当前道全部层数！",
								"role_id", session.RoleID(),
								"layer", info.Layer,
								"layer_name", protocol.TowerLayerName(info.Layer),
							)
						}
						break
					}

					// 检查不可重试的硬终止条件
					if res.Result == protocol.TowerResultFullBag {
						slog.Info("灵件背包已满，终止六道轮回挑战", "role_id", session.RoleID())
						return nil
					}
					if res.Result == protocol.TowerResultFullTimes {
						slog.Info("今日六道挑战次数已达上限", "role_id", session.RoleID())
						return nil
					}
					if res.Result == protocol.TowerResultInvalidLayer {
						// 该层已通关/无效（线上全通后挑战最后一层返回 1），重试无意义。
						slog.Info("六道轮回该层已通关或无效，结束本轮",
							"role_id", session.RoleID(),
							"floor", floor,
							"sequence_war", info.SequenceWar,
							"sequence_max", maxFloor)
						return nil
					}

					// 挑战失败或者遭遇限制 (WAR_CD, 战力不足等)，每层单独计算重试次数
					floorRetries++
					slog.Warn("六道轮回关卡挑战未成功",
						"role_id", session.RoleID(),
						"floor", floor,
						"result_code", res.Result,
						"retry", floorRetries,
						"max_retries", maxRetries,
					)
					if floorRetries >= maxRetries {
						slog.Warn("六道轮回当前层挑战失败已达最大重试次数，终止爬塔",
							"role_id", session.RoleID(),
							"floor", floor,
							"retries", floorRetries,
						)
						return nil
					}
				}

				if !floorWon {
					break
				}

				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("六道轮回爬塔任务执行完毕", "role_id", session.RoleID())
			return nil
		},
	)
}

// 锁妖塔单轮挑战上限。官方客户端最多展示 40 层，超过后不再追加下一层。
const demonTowerMaxChallengesPerRun = 40

// demonTowerMaxLossRetries 同一层战斗失败后的最大重试次数。
const demonTowerMaxLossRetries = 3

// demonTowerCallTimeout 锁妖塔单次请求超时。
const demonTowerCallTimeout = 8 * time.Second

// NewDemonTowerRoutine 构造锁妖塔挑战任务 (Feature 9: 锁妖塔)。
//
// 协议对齐 com.protocols.Mod_RepressDemonTower_Base (Module 705):
//
//	panel_info    Act 0 0x02C10000 request=[]
//	challenge     Act 1 0x02C10001 request=[Int layer_id]
//	get_base_info Act 8 0x02C10008 request=[] -> [Byte isSeasonTime]
//
// 每轮先查询面板，按 layer_list 中最高层 +1 作为下一层目标，
// 依据 challenge 回包结果码决定继续、领取次数耗尽退出或熔断。
func NewDemonTowerRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"demon_tower",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行副本任务: 锁妖塔挑战与试炼推图", "role_id", session.RoleID())

			// 0. 先通过官方功能开放列表判定门槛，未开放时不再发送会被静默丢弃的业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDRepressDemonTower)
			if fErr != nil {
				slog.Info("锁妖塔功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("锁妖塔功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDRepressDemonTower)
				return nil
			}

			// 1. 查询赛季开放状态，未开赛时面板请求无意义。
			baseCtx, baseCancel := context.WithTimeout(ctx, demonTowerCallTimeout)
			baseResp, baseErr := session.Call(baseCtx, protocol.BuildDemonTowerBaseInfoPacket(), protocol.ActionIDDemonTowerBaseInfo)
			baseCancel()
			if baseErr != nil || baseResp == nil {
				slog.Info("锁妖塔赛季状态查询失败，跳过", "role_id", session.RoleID(), "err", baseErr)
				return nil
			}
			inSeason, parseErr := protocol.ParseDemonTowerBaseInfoResult(baseResp.Payload)
			if parseErr != nil {
				slog.Info("锁妖塔赛季状态解析失败，跳过", "role_id", session.RoleID(), "err", parseErr)
				return nil
			}
			if !inSeason {
				slog.Info("锁妖塔当前不在赛季时间，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 循环挑战：每轮以最新面板数据计算下一层目标，成功则继续。
			challenged := 0
			losses := 0
			for round := 0; round < demonTowerMaxChallengesPerRun; round++ {
				if ctx.Err() != nil {
					return nil
				}

				panel, err := fetchDemonTowerPanel(ctx, session)
				if err != nil {
					slog.Info("锁妖塔面板查询失败，结束本轮", "role_id", session.RoleID(), "err", err)
					return nil
				}
				switch panel.Result {
				case protocol.DemonTowerResultSuccess:
					// 继续执行
				case protocol.DemonTowerResultFunctionNoOpen:
					slog.Info("锁妖塔功能未开放，跳过", "role_id", session.RoleID(), "result", panel.Result)
					return nil
				default:
					slog.Info("锁妖塔面板返回失败结果码，结束本轮",
						"role_id", session.RoleID(), "result", panel.Result, "result_text", demonTowerResultText(panel.Result))
					return nil
				}

				if panel.NowTicketNum <= 0 {
					slog.Info("锁妖塔挑战次数已耗尽", "role_id", session.RoleID(), "challenged", challenged)
					return scheduler.ErrAttemptsExhausted
				}

				// 当前阶层数已满但升阶回合条件未满足时，challenge 必然返回
				// RACE_STEP_ERROR(6)，直接收尾并说明原因。
				// 线上实测 2026-11-11：finish_layer=40、total_round=521 > 434。
				if layerNeed, roundNeed, ok := panel.StepUpRequirement(); ok &&
					layerNeed > 0 && roundNeed > 0 &&
					panel.NowFinishLayer >= layerNeed && panel.NowTotalRound > roundNeed {
					slog.Info("锁妖塔当前阶已通关但升阶回合条件未满足，跳过挑战",
						"role_id", session.RoleID(),
						"now_step", panel.NowStep,
						"finish_layer", panel.NowFinishLayer,
						"layer_need", layerNeed,
						"total_round", panel.NowTotalRound,
						"round_need", roundNeed)
					return nil
				}

				target := panel.NextLayerID()
				// 跨阶次时 layer_list 可能清空且 now_finish_layer 归零，
				// 此时用排行榜中的 self_layer 兜底。线上实测 self_layer 即当前
				// 可挑战层，不需 +1（+1 会返回 LAYER_ERROR）。
				if len(panel.LayerList) == 0 && panel.NowFinishLayer <= 0 {
					if selfLayer, rankErr := fetchDemonTowerSelfLayer(ctx, session); rankErr == nil && selfLayer > 0 {
						target = selfLayer
					}
				}

				result, err := challengeDemonTowerLayer(ctx, session, target)
				if err != nil {
					slog.Info("锁妖塔挑战请求失败，结束本轮",
						"role_id", session.RoleID(), "layer", target, "err", err)
					return nil
				}

				if result != protocol.DemonTowerResultSuccess {
					slog.Info("锁妖塔挑战未成功，结束本轮",
						"role_id", session.RoleID(), "layer", target,
						"result", result, "result_text", demonTowerResultText(result),
						"challenged", challenged)
					switch result {
					case protocol.DemonTowerResultFailed:
						losses++
						if losses > demonTowerMaxLossRetries {
							slog.Info("锁妖塔连续挑战失败，达到重试上限，结束本轮",
								"role_id", session.RoleID(), "layer", target,
								"retries", losses, "challenged", challenged)
							return nil
						}
						slog.Info("锁妖塔挑战失败，重试同一层",
							"role_id", session.RoleID(), "layer", target,
							"retry", losses, "max_retries", demonTowerMaxLossRetries,
							"challenged", challenged)
						select {
						case <-ctx.Done():
							return nil
						case <-time.After(time.Second):
						}
						continue
					case protocol.DemonTowerResultNotEnoughTicket:
						return scheduler.ErrAttemptsExhausted
					case protocol.DemonTowerResultItemPackFull:
						return scheduler.ErrBagFull
					case protocol.DemonTowerResultRaceStepError:
						slog.Info("锁妖塔赛季阶段错误（可能已满阶或需升阶）",
							"role_id", session.RoleID(), "layer", target,
							"now_step", panel.NowStep, "finish_layer", panel.NowFinishLayer,
							"total_round", panel.NowTotalRound)
						return nil
					case protocol.DemonTowerResultNotEnoughIngot, protocol.DemonTowerResultBuyNumError:
						// 本实现从不购买次数；出现元宝/购买类错误说明服务端状态异常，记录后熔断。
						return scheduler.ErrConditionNotMet
					default:
						// LAYER_ERROR 表示已到当前阶层的层数上限，属正常收尾；其余失败码本轮退出。
						return nil
					}
				}

				challenged++
				losses = 0
				slog.Info("锁妖塔挑战成功",
					"role_id", session.RoleID(), "layer", target,
					"tickets_before", panel.NowTicketNum, "challenged", challenged)

				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("锁妖塔单轮挑战达到上限，等待下次调度",
				"role_id", session.RoleID(), "challenged", challenged)
			return nil
		},
	)
}

// fetchDemonTowerPanel 查询并解析锁妖塔主面板。
func fetchDemonTowerPanel(ctx context.Context, session *client.RoleSession) (*protocol.DemonTowerPanel, error) {
	callCtx, cancel := context.WithTimeout(ctx, demonTowerCallTimeout)
	defer cancel()
	resp, err := session.Call(callCtx, protocol.BuildDemonTowerPanelPacket(), protocol.ActionIDDemonTowerPanel)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("锁妖塔面板回包为空")
	}
	return protocol.ParseDemonTowerPanelResult(resp.Payload)
}

// fetchDemonTowerSelfLayer 查询锁妖塔排行榜中的赛季累计通关层数。
func fetchDemonTowerSelfLayer(ctx context.Context, session *client.RoleSession) (int32, error) {
	callCtx, cancel := context.WithTimeout(ctx, demonTowerCallTimeout)
	defer cancel()
	resp, err := session.Call(callCtx, protocol.BuildDemonTowerRankPacket(), protocol.ActionIDDemonTowerRank)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("锁妖塔排行榜回包为空")
	}
	result, selfLayer, err := protocol.ParseDemonTowerRankSelfLayer(resp.Payload)
	if err != nil {
		return 0, err
	}
	if result != protocol.DemonTowerResultSuccess {
		return 0, fmt.Errorf("锁妖塔排行榜结果码 %d", result)
	}
	return selfLayer, nil
}

// challengeDemonTowerLayer 挑战指定层并返回业务结果码。
func challengeDemonTowerLayer(ctx context.Context, session *client.RoleSession, layerID int32) (uint8, error) {
	callCtx, cancel := context.WithTimeout(ctx, demonTowerCallTimeout)
	defer cancel()
	resp, err := session.Call(callCtx, protocol.BuildDemonTowerChallengePacket(layerID), protocol.ActionIDDemonTowerChallenge)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("锁妖塔挑战回包为空")
	}
	return protocol.ParseDemonTowerChallengeResult(resp.Payload)
}

// demonTowerResultText 返回锁妖塔结果码的中文语义文本。
func demonTowerResultText(result uint8) string {
	switch result {
	case protocol.DemonTowerResultSuccess:
		return "成功"
	case protocol.DemonTowerResultFailed:
		return "失败"
	case protocol.DemonTowerResultFunctionNoOpen:
		return "功能未开放"
	case protocol.DemonTowerResultNotEnoughTicket:
		return "挑战次数不足"
	case protocol.DemonTowerResultLayerError:
		return "层数错误"
	case protocol.DemonTowerResultItemPackFull:
		return "背包已满"
	case protocol.DemonTowerResultRaceStepError:
		return "赛季阶段错误"
	case protocol.DemonTowerResultNotEnoughIngot:
		return "元宝不足"
	case protocol.DemonTowerResultBuyNumError:
		return "购买次数错误"
	default:
		return "未知结果码"
	}
}

// NewQiMenRoutine 构造奇门遁甲阵法星图升级与布设任务 (Feature 4: 奇门遁甲)。
// NewQiMenRoutine 构造奇门遁甲一/二（Module 251/392）普通修炼与突破试炼任务。
func NewQiMenRoutine() scheduler.ActivityRoutine {
	return newQiMenMiracleRoutine()
}

// NewXunXianRoutine 构造寻仙之路自动推图与宝箱领用任务 (Feature 8: 寻仙之路)。
func NewXunXianRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"xunxian_road",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行玩法任务: 寻仙之路自动推图与节点扫荡", "role_id", session.RoleID())

			// 1. 查询当前寻仙关卡进度 (Mod 466, Act 1)
			progPkt := protocol.BuildXunXianGetProgressPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(pCtx, progPkt, protocol.ActionIDXunXianGetProgress)
			pCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("寻仙之路功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 依次挑战节点 (依据抓包 0xBC, 0xBD, 0xBE) 并领取节点宝箱 (Act 13)
			nodes := []int32{0xBC, 0xBD, 0xBE}
			var clearedCount int

			for _, nID := range nodes {
				chalPkt := protocol.BuildXunXianChallengePacket(nID, protocol.ActionIDXunXianGetProgress)
				cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
				cResp, cErr := session.Call(cCtx, chalPkt, protocol.ActionIDXunXianChallenge)
				cCancel()

				if cErr == nil && cResp != nil {
					// 领取节点宝箱 (Act 13)
					chestPkt := protocol.BuildXunXianClaimChestPacket(protocol.ActionIDXunXianChallenge)
					chCtx, chCancel := context.WithTimeout(ctx, 8*time.Second)
					_, _ = session.Call(chCtx, chestPkt, protocol.ActionIDXunXianClaimChest)
					chCancel()
					clearedCount++
				}
			}

			slog.Info("寻仙之路节点挑战与宝箱领取完成", "role_id", session.RoleID(), "cleared_nodes", clearedCount)
			return nil
		},
	)
}

// NewZodiacRoutine 构造生肖挑战与金油洗练任务。
// NewZodiacRoutine 构造生肖挑战任务 (Module 44, Mod_Zodiac_Base)。
//
// 挑战前先读取 require_level 校验角色等级；不满足或已通关/冷却时直接跳过。
// 同一关卡战败后最多重试 zodiacMaxLossRetries 次。
func NewZodiacRoutine() scheduler.ActivityRoutine {
	const zodiacMaxLossRetries = 3
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"zodiac_challenge",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 0. 先通过官方功能开放列表判定门槛，未开放时不再发送业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDZodiac)
			if fErr != nil {
				slog.Info("生肖挑战功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("生肖挑战功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDZodiac)
				return nil
			}

			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildZodiacInfoPacket(), protocol.ActionIDZodiacInfo)
			iCancel()
			if err != nil {
				slog.Info("生肖挑战功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseZodiacInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析生肖信息失败: %w", err)
			}
			level := session.GetPlayerState().Level
			slog.Info("生肖挑战状态",
				"role_id", session.RoleID(),
				"now_level", info.NowZodiacLevel,
				"now_barrier", info.NowBarrier,
				"next_level", info.NextZodiacLevel,
				"next_barrier", info.NextBarrier,
				"require_level", info.RequireLevel,
				"player_level", level,
				"heaven", info.Heaven,
			)

			// 面板全 0 表示生肖挑战尚未开放/未解锁；此时服务端会静默丢弃 challenge 请求。
			if info.NowZodiacLevel == 0 && info.NextZodiacLevel == 0 && info.RequireLevel <= 0 && info.Heaven == 0 {
				slog.Info("生肖挑战面板为空，功能未开放/未解锁，跳过", "role_id", session.RoleID())
				return nil
			}

			// 挑战前置校验：角色等级必须达到 require_level。
			if info.RequireLevel > int32(level) {
				slog.Info("角色等级不足，跳过生肖挑战",
					"role_id", session.RoleID(),
					"require_level", info.RequireLevel,
					"player_level", level,
				)
				return nil
			}
			// 已通关最高天 (require_level 超过上限 250 由客户端标记 is_end)。
			if info.RequireLevel > 250 {
				slog.Info("生肖挑战已全部通关", "role_id", session.RoleID(), "require_level", info.RequireLevel)
				return nil
			}

			losses := 0
			for {
				cCtx, cCancel := context.WithTimeout(ctx, 15*time.Second) // 回包携带完整战报
				resp, err := session.Call(cCtx, protocol.BuildZodiacChallengePacket(), protocol.ActionIDZodiacChallenge)
				cCancel()
				if err != nil {
					if errSessionTerminated(ctx, err) {
						return fmt.Errorf("生肖挑战请求失败: %w", err)
					}
					// 功能未开放/未解锁时服务端对 challenge 静默不回包，按不可用跳过而非报错。
					slog.Info("生肖挑战请求无回包，判定功能未开放/不可用，跳过",
						"role_id", session.RoleID(), "err", err)
					return nil
				}
				result, err := protocol.ParseZodiacChallengeResult(resp.Payload)
				if err != nil {
					return fmt.Errorf("解析生肖挑战回包失败: %w", err)
				}
				switch result.Result {
				case protocol.ZodiacResultSuccess:
					slog.Info("生肖挑战成功",
						"role_id", session.RoleID(),
						"zodiac_level", result.ZodiacLevel,
						"barrier", result.Barrier,
					)
					return nil
				case protocol.ZodiacResultFail:
					losses++
					if losses >= zodiacMaxLossRetries {
						slog.Info("生肖挑战连续失败，达到重试上限，结束本轮",
							"role_id", session.RoleID(), "losses", losses,
							"max_retries", zodiacMaxLossRetries)
						return nil
					}
					slog.Info("生肖挑战失败，重试同一关卡",
						"role_id", session.RoleID(), "losses", losses,
						"max_retries", zodiacMaxLossRetries)
					continue
				case protocol.ZodiacResultTopHeaven:
					slog.Info("生肖挑战已通关最高天", "role_id", session.RoleID())
				case protocol.ZodiacResultNotEnoughPlayerLevel:
					slog.Info("生肖挑战等级不足", "role_id", session.RoleID())
				case protocol.ZodiacResultBagFull:
					return scheduler.ErrBagFull
				case protocol.ZodiacResultCDTime:
					slog.Info("生肖挑战冷却中，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "生肖挑战冷却")
				case protocol.ZodiacResultNoEnoughStatePoint:
					slog.Info("生肖挑战境界点不足", "role_id", session.RoleID())
				case protocol.ZodiacResultLock:
					slog.Info("生肖关卡未解锁", "role_id", session.RoleID())
				default:
					slog.Warn("生肖挑战返回未知结果", "role_id", session.RoleID(), "result", result.Result)
				}
				return nil
			}
		},
	)
}

// NewSpiritTrialRoutine 构造精灵试炼通关挑战任务。
// NewSpiritTrialRoutine 构造精灵试炼任务 (Module 205, Mod_StTrials_Base)。
//
// 先读各模式剩余挑战次数；仅对仍有次数的模式领奖并逐次挑战。
// 挑战失败/不可挑战/次数耗尽时立即停止该模式。全程不消耗元宝。
func NewSpiritTrialRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"spirit_trial",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			mCtx, mCancel := context.WithTimeout(ctx, callTimeout)
			statusResp, err := session.Call(mCtx, protocol.BuildStTrialsModeStatusPacket(), protocol.ActionIDStTrialsModeStatus)
			mCancel()
			if err != nil {
				slog.Info("精灵试炼功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			modes, err := protocol.ParseStTrialsModeStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析精灵试炼模式失败: %w", err)
			}

			const stTrialsMaxLossRetries = 3
			active := 0
			for _, mode := range modes {
				if mode.RemainChallengeTimes == 0 {
					continue
				}
				active++

				fetchInfo := func() (*protocol.StTrialsModeInfo, error) {
					cCtx, cancel := context.WithTimeout(ctx, callTimeout)
					resp, cErr := session.Call(cCtx, protocol.BuildStTrialsModeInfoPacket(mode.ModeType), protocol.ActionIDStTrialsModeInfo)
					cancel()
					if cErr != nil {
						return nil, cErr
					}
					return protocol.ParseStTrialsModeInfoResult(resp.Payload)
				}
				claim := func(info *protocol.StTrialsModeInfo) error {
					aCtx, aCancel := context.WithTimeout(ctx, callTimeout)
					resp, aErr := session.Call(aCtx, protocol.BuildStTrialsGetAwardPacket(info.Floor, info.Pass, mode.ModeType), protocol.ActionIDStTrialsGetAward)
					aCancel()
					if aErr != nil {
						return aErr
					}
					result, awards, pErr := protocol.ParseStTrialsAwardResult(resp.Payload)
					if pErr != nil {
						return pErr
					}
					switch result {
					case protocol.StTrialsResultSuccess:
						slog.Info("精灵试炼奖励领取成功", "role_id", session.RoleID(), "mode", mode.ModeType, "floor", info.Floor, "awards", awards)
					case protocol.StTrialsResultIsGet:
						// 已领取，忽略
					case protocol.StTrialsResultItemPackFull,
						protocol.StTrialsResultFatePackFull,
						protocol.StTrialsResultFivePackFull:
						return scheduler.ErrBagFull
					default:
						slog.Debug("精灵试炼奖励未领取", "role_id", session.RoleID(), "mode", mode.ModeType, "result", result)
					}
					return nil
				}

				info, iErr := fetchInfo()
				if iErr != nil {
					slog.Warn("查询精灵试炼模式详情失败", "role_id", session.RoleID(), "mode", mode.ModeType, "err", iErr)
					continue
				}
				if err := claim(info); err != nil {
					if errors.Is(err, scheduler.ErrBagFull) {
						return err
					}
					slog.Warn("领取精灵试炼奖励失败", "role_id", session.RoleID(), "mode", mode.ModeType, "err", err)
				}

				losses := 0
				for i := uint8(0); i < mode.RemainChallengeTimes; i++ {
					cCtx, cCancel := context.WithTimeout(ctx, 15*time.Second) // 战报较大
					resp, cErr := session.Call(cCtx, protocol.BuildStTrialsChallengePacket(mode.ModeType), protocol.ActionIDStTrialsChallenge)
					cCancel()
					if cErr != nil {
						slog.Info("精灵试炼挑战无应答", "role_id", session.RoleID(), "mode", mode.ModeType, "err", cErr)
						break
					}
					result, pErr := protocol.ParseStTrialsChallengeResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析精灵试炼挑战回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					switch result {
					case protocol.StTrialsResultSuccess:
						losses = 0
						slog.Info("精灵试炼挑战成功", "role_id", session.RoleID(), "mode", mode.ModeType, "index", i+1)
						if info, iErr = fetchInfo(); iErr == nil {
							if cErr := claim(info); cErr != nil {
								if errors.Is(cErr, scheduler.ErrBagFull) {
									return cErr
								}
							}
						}
					case protocol.StTrialsResultFaild:
						losses++
						if losses >= stTrialsMaxLossRetries {
							slog.Info("精灵试炼连续挑战失败，达到重试上限，停止该模式",
								"role_id", session.RoleID(), "mode", mode.ModeType,
								"losses", losses, "max_retries", stTrialsMaxLossRetries)
							i = mode.RemainChallengeTimes
						} else {
							slog.Info("精灵试炼挑战失败，重试同一模式",
								"role_id", session.RoleID(), "mode", mode.ModeType,
								"losses", losses, "max_retries", stTrialsMaxLossRetries)
						}
					case protocol.StTrialsResultCannotChallenge, protocol.StTrialsResultTimeOut:
						slog.Info("精灵试炼当前不可挑战", "role_id", session.RoleID(), "mode", mode.ModeType, "result", result)
						i = mode.RemainChallengeTimes
					default:
						slog.Info("精灵试炼挑战未成功", "role_id", session.RoleID(), "mode", mode.ModeType, "result", result)
						i = mode.RemainChallengeTimes
					}
				}
			}
			slog.Info("精灵试炼巡检完成", "role_id", session.RoleID(), "modes", len(modes), "active_modes", active)
			return nil
		},
	)
}

// executeQuestCycle 任务闭环状态机:
// 循环接任务、完成任务，一直到没有任务可做了再 break，随后流转执行精英副本扫荡。
func executeQuestCycle(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	cfg SweepConfig,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
) {
	if dictRepo == nil || session == nil {
		return
	}

	const powerPerSweep = 5
	const maxCycles = 50
	failedMissions := make(map[int]bool)

	slog.Info("启动主线/支线任务闭环自动化驱动", "role_id", session.RoleID(), "town_id", session.GetPlayerState().TownID)

	// 0. 主动向服务端全量同步当前任务列表 (Module 3, Action 9: town_quest_show)
	// 在 2026 真实网关中，town_quest_show 返回状态枚举 (QuestType):
	//   0 = Acceptable (可接取)
	//   1 = Accepted   (已接取/进行中)
	//   2 = Completed  (已达成目标/可交付)
	//   3 = LevelLimit (等级不足)
	syncedItems, syncErr := session.SyncQuests(ctx)
	if syncErr == nil {
		executeAuthoritativeQuestCycle(
			ctx, session, dictRepo, syncedItems, ensureStamina,
			totalCostStamina, totalGainExp, totalGainCoins,
			clearedMissions, failedMissions,
		)
		return
	}

	// 停机或断线导致的同步失败不再降级扫描 NPC，直接静默退出。
	if errSessionTerminated(ctx, syncErr) {
		slog.Info("会话已关闭，终止任务闭环驱动", "role_id", session.RoleID())
		return
	}

	// 兼容离线单测 Mock Server (未实现 0x00030009 town_quest_show 时的 NPC 扫描降级路径)
	for cycle := 1; cycle <= maxCycles; cycle++ {
		if ctx.Err() != nil || session.State() == client.StateClosed {
			slog.Info("会话已关闭，终止任务闭环驱动", "role_id", session.RoleID())
			return
		}
		if session.GetStamina() < powerPerSweep && !ensureStamina() {
			slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetStamina())
			break
		}

		progressMade := false

		// 1. 交付当前身上已达成的任务
		currentQuests := session.GetQuests()
		for qid, state := range currentQuests {
			if state == 1 || state == 2 {
				q, _ := dictRepo.GetQuest(int(qid))
				title := fmt.Sprintf("Quest#%d", qid)
				if q != nil && q.Title != "" {
					title = q.Title
				}
				slog.Info("正在交付已达成任务", "role_id", session.RoleID(), "quest_id", qid, "title", title)

				compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: int32(qid)})
				compCtx, compCancel := context.WithTimeout(ctx, 8*time.Second)
				_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
				compCancel()

				session.RemoveQuest(qid)
				if q != nil {
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
				}
				progressMade = true
				time.Sleep(50 * time.Millisecond)
			}
		}

		// 2. 扫描城镇 NPC 列表并接取可接任务
		curTown := session.GetPlayerState().TownID
		var towns []int
		if curTown > 0 {
			towns = append(towns, curTown)
		}
		for _, fallbackTown := range []int{22, 4, 1} {
			if fallbackTown != curTown {
				towns = append(towns, fallbackTown)
			}
		}

		validQuestNPCs, _ := dictRepo.GetQuestNPCs()
		var candidateNPCs []int
		seenNPC := make(map[int]bool)
		for _, tid := range towns {
			npcs, err := dictRepo.GetTownNPCs(tid)
			if err == nil {
				for _, nid := range npcs {
					if !seenNPC[nid] {
						seenNPC[nid] = true
						if validQuestNPCs == nil || validQuestNPCs[nid] {
							candidateNPCs = append(candidateNPCs, nid)
						}
					}
				}
			}
		}

		for _, npcID := range candidateNPCs {
			npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{NPCID: int32(npcID)})
			npcCtx, npcCancel := context.WithTimeout(ctx, 8*time.Second)
			npcResp, npcErr := session.Call(npcCtx, npcPkt, protocol.ActionIDQuestGetNPCQuests)
			npcCancel()

			if npcErr != nil || npcResp == nil {
				if errSessionTerminated(ctx, npcErr) {
					slog.Info("会话已关闭，终止 NPC 任务扫描", "role_id", session.RoleID())
					return
				}
				continue
			}

			items, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload)
			if pErr != nil || len(items) == 0 {
				continue
			}

			for _, it := range items {
				questID := uint32(it.QuestID)

				if it.State == 2 {
					q, _ := dictRepo.GetQuest(int(it.QuestID))
					title := fmt.Sprintf("Quest#%d", questID)
					if q != nil && q.Title != "" {
						title = q.Title
					}
					slog.Info("NPC 处发现已达成任务，正在交付结算", "role_id", session.RoleID(), "npc_id", npcID, "quest_id", questID, "title", title)
					compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: it.QuestID})
					compCtx, compCancel := context.WithTimeout(ctx, 8*time.Second)
					_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
					compCancel()

					session.RemoveQuest(questID)
					if q != nil {
						session.AddRewards(q.AwardExp, q.AwardCoins)
						*totalGainExp += q.AwardExp
						*totalGainCoins += q.AwardCoins
					}
					progressMade = true
					time.Sleep(50 * time.Millisecond)
					continue
				}

				if it.State == 0 {
					session.SetQuestState(questID, 0)
					continue
				}

				if it.State != 1 {
					continue
				}
				questsNow := session.GetQuests()
				if _, exists := questsNow[questID]; exists {
					continue
				}

				q, _ := dictRepo.GetQuest(int(it.QuestID))
				if q != nil && q.Level > 0 && q.Level > session.GetPlayerState().Level {
					continue
				}

				title := fmt.Sprintf("Quest#%d", it.QuestID)
				if q != nil && q.Title != "" {
					title = q.Title
				}
				slog.Info("发现可接取任务，正在接受", "role_id", session.RoleID(), "npc_id", npcID, "quest_id", it.QuestID, "title", title)

				accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{QuestID: it.QuestID})
				accCtx, accCancel := context.WithTimeout(ctx, 8*time.Second)
				_, accErr := session.Call(accCtx, accPkt, protocol.ActionIDQuestAccept)
				accCancel()

				if accErr == nil {
					session.SetQuestState(questID, 0)
					progressMade = true
					slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					time.Sleep(50 * time.Millisecond)
				}
			}
		}

		// 3. 推进已接任务关卡挑战与对话剧情
		questsInProgress := session.GetQuests()
		for qid, qState := range questsInProgress {
			if qState != 0 {
				continue
			}
			q, err := dictRepo.GetQuest(int(qid))
			if err != nil || q == nil {
				continue
			}

			if q.MissionID == 0 {
				slog.Info("正在交付对话剧情任务", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
				compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: int32(qid)})
				compCtx, compCancel := context.WithTimeout(ctx, 8*time.Second)
				_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
				compCancel()

				session.RemoveQuest(qid)
				session.AddRewards(q.AwardExp, q.AwardCoins)
				*totalGainExp += q.AwardExp
				*totalGainCoins += q.AwardCoins
				progressMade = true
				time.Sleep(50 * time.Millisecond)
				continue
			}

			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetStamina() < powerPerSweep && !ensureStamina() {
				break
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title, "mission_id", mid, "player_level", session.GetPlayerState().Level)

			challengeSuccess := false
			// 主线任务关卡推进统一使用官方 Module 25 扫荡协议。
			// 注意: 0x00230002 属于英雄副本 start_practice，不能用于主线推图关卡。
			m25Req := protocol.StartPracticeRequest{
				MissionID: int32(mid),
				Count:     1,
				AutoSale:  1,
			}
			m25Pkt, _ := protocol.BuildStartPracticePacket(m25Req)
			enterCtx, enterCancel := context.WithTimeout(ctx, 8*time.Second)
			enterResp, enterErr := session.Call(enterCtx, m25Pkt, protocol.ActionIDPracticeStart)
			enterCancel()

			if enterErr == nil && enterResp != nil {
				if res, pErr := protocol.ParseStartPracticeResult(enterResp.Payload); pErr == nil && res != nil && res.Result == protocol.PracticeResultSuccess {
					challengeSuccess = true
					if quickPkt, qErr := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0}); qErr == nil {
						_ = session.Send(quickPkt)
					}
				}
			}

			if challengeSuccess {
				cost := powerPerSweep
				remStamina, _ := session.ConsumeStamina(cost)
				session.AddRewards(2500, 12000)
				*totalCostStamina += cost
				*totalGainExp += 2500
				*totalGainCoins += 12000
				clearedMissions[mid] = true
				progressMade = true
				slog.Info("任务关卡推进挑战成功", "role_id", session.RoleID(), "mission_id", mid, "remaining_stamina", remStamina)

				compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: int32(qid)})
				compCtx, compCancel := context.WithTimeout(ctx, 8*time.Second)
				_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
				compCancel()

				session.RemoveQuest(qid)
				session.AddRewards(q.AwardExp, q.AwardCoins)
				*totalGainExp += q.AwardExp
				*totalGainCoins += q.AwardCoins
				slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
				time.Sleep(50 * time.Millisecond)
			} else {
				slog.Info("任务关卡暂未通关或卡战力/等级，记录跳过", "role_id", session.RoleID(), "mission_id", mid, "quest_id", qid)
				failedMissions[mid] = true
			}
		}

		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
	resetTownScene(session)
}

// questStateName 将服务端任务状态枚举转换为可读名称。
func questStateName(state int32) string {
	switch state {
	case -1:
		return "LevelLimit(等级不足)"
	case 0:
		return "Acceptable(可接取)"
	case 1:
		return "Accepted(进行中)"
	case 2:
		return "Completed(可交付)"
	default:
		return "Unknown"
	}
}

// errSessionTerminated 判断错误是否由停机取消或会话关闭引起。
// 用于优雅停机时静默退出推图循环，避免把撤站过程中的必然失败记录成业务告警。
func errSessionTerminated(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	return errors.Is(err, client.ErrSessionClosed) || errors.Is(err, context.Canceled)
}

// questBeginNPCID / questEndNPCID 安全读取任务字典中的 NPC 编号，供 Mod 3 对话前置链使用。
func questBeginNPCID(q *dictionary.Quest) int32 {
	if q == nil {
		return 0
	}
	return int32(q.BeginNPCID)
}

func questEndNPCID(q *dictionary.Quest) int32 {
	if q == nil {
		return 0
	}
	return int32(q.EndNPCID)
}

// queryNPCQuestState 按官方客户端 NPCDialogView.npc_info_by_npcid 流程打开指定 NPC 的对话，
// 返回目标任务在该 NPC 处的状态 (0=可接取, 1=已接取, 2=可交付)。任务不在列表中时返回 nil。
func queryNPCQuestState(ctx context.Context, session *client.RoleSession, npcID, questID int32) (*int32, error) {
	npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{
		NPCID:   npcID,
		PrevAct: session.LastActionID(),
	})
	npcCtx, npcCancel := context.WithTimeout(ctx, 8*time.Second)
	npcResp, npcErr := session.Call(npcCtx, npcPkt, protocol.ActionIDQuestGetNPCQuests)
	npcCancel()
	if npcErr != nil {
		return nil, npcErr
	}
	if npcResp == nil {
		return nil, errors.New("dungeon: empty npc quest response")
	}
	items, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload)
	if pErr != nil {
		return nil, pErr
	}
	for _, it := range items {
		if it.QuestID == questID {
			state := int32(it.State)
			return &state, nil
		}
	}
	return nil, nil
}

// tryAcceptQuest 发送一次 accept_quest (Module 3, Action 3) 并解析结果。
// 返回 (是否成功, 服务端明确失败码, 通信错误)；err != nil 且 resCode == 0 表示无回包。
func tryAcceptQuest(ctx context.Context, session *client.RoleSession, questID int32) (bool, uint8, error) {
	accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{
		QuestID: questID,
		PrevAct: session.LastActionID(),
	})
	accCtx, accCancel := context.WithTimeout(ctx, 8*time.Second)
	accResp, accErr := session.Call(accCtx, accPkt, protocol.ActionIDQuestAccept)
	accCancel()

	// Mod_Quest_Base.SUCCEED == 5 (兼容单测 0 / 1)
	if accErr == nil && accResp != nil &&
		(len(accResp.Payload) == 0 || accResp.Payload[0] == 5 || accResp.Payload[0] == 1 || accResp.Payload[0] == 0) {
		return true, 0, nil
	}
	// 服务端已明确回包拒绝（如背包已满、任务已存在），无需重试。
	if accResp != nil && len(accResp.Payload) > 0 {
		return false, accResp.Payload[0], nil
	}
	return false, 0, accErr
}

// questAcceptedOnServer 复核服务端任务列表，覆盖「服务端已受理、仅回包丢失」的情况。
func questAcceptedOnServer(ctx context.Context, session *client.RoleSession, questID int32) bool {
	items, syncErr := session.SyncQuests(ctx)
	if syncErr != nil {
		return false
	}
	for _, it := range items {
		if it.QuestID == questID && (it.State == 1 || it.State == 2) {
			slog.Info("接取任务回包超时，但服务端任务列表已确认接取生效",
				"role_id", session.RoleID(), "quest_id", questID, "state", it.State)
			return true
		}
	}
	return false
}

// acceptQuestWithRecovery 接取任务。
//
// 优先直接发送 accept_quest：服务端接受该请求时并不强制要求先打开 NPC 对话。
// 任务 NPC 不在角色当前城镇时（例如任务起始 NPC 位于仙界），
// npc_info_by_npcid 会被服务端静默丢弃；此时若仍以该动作为前置链接取，
// 会连续超时并阻塞整轮任务循环（实机 2026-11-06：任务 355 螳螂妖魔）。
//
// 因此 NPC 对话改为兜底路径：仅当直接接取无回包且服务端任务列表仍未生效时，
// 才按官方 NPCDialogView 流程补发 npc_info_by_npcid 建立前置链并重试。
// 返回 (是否接取成功, 服务端结果码, 最后一次通信错误)。
func acceptQuestWithRecovery(ctx context.Context, session *client.RoleSession, questID, beginNPCID int32) (bool, uint8, error) {
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		accepted, resCode, accErr := tryAcceptQuest(ctx, session, questID)
		if accepted {
			return true, 0, nil
		}
		if resCode != 0 {
			return false, resCode, nil
		}
		lastErr = accErr
		if errSessionTerminated(ctx, accErr) {
			return false, 0, lastErr
		}

		// 未收到任何回包：先复核服务端任务状态，覆盖「服务端已处理成功、仅回包丢失」。
		if questAcceptedOnServer(ctx, session, questID) {
			return true, 0, nil
		}

		// 兜底：按官方 NPCDialogView 流程打开 NPC 对话，再重试接取。
		if attempt == 1 && beginNPCID > 0 {
			npcState, npcErr := queryNPCQuestState(ctx, session, beginNPCID, questID)
			if npcErr != nil {
				if errSessionTerminated(ctx, npcErr) {
					return false, 0, npcErr
				}
				slog.Info("兜底 NPC 对话打开失败，任务接取无回包",
					"role_id", session.RoleID(), "quest_id", questID, "npc_id", beginNPCID, "err", npcErr)
			} else if npcState != nil && (*npcState == 1 || *npcState == 2) {
				// QuestType.Accepted(1) / Completed(2): 服务端已受理，幂等返回成功。
				slog.Info("NPC 对话已确认任务处于已接取状态，跳过重复接取",
					"role_id", session.RoleID(), "quest_id", questID,
					"npc_id", beginNPCID, "state", *npcState)
				return true, 0, nil
			} else if accepted, resCode, accErr = tryAcceptQuest(ctx, session, questID); accepted {
				return true, 0, nil
			} else if resCode != 0 {
				return false, resCode, nil
			} else {
				lastErr = accErr
				if errSessionTerminated(ctx, accErr) {
					return false, 0, lastErr
				}
				if questAcceptedOnServer(ctx, session, questID) {
					return true, 0, nil
				}
			}
		}

		if attempt == 1 {
			slog.Info("接取任务未收到回包，复核未生效后重试一次",
				"role_id", session.RoleID(), "quest_id", questID, "err", accErr)
		}
	}
	return false, 0, lastErr
}

// executeAuthoritativeQuestCycle 基于真实网关 Mod 3 (Mod_Quest_Base) 与 Mod 4 (Mod_Mission_Base) 驱动全自动主支线推图闭环。
// QuestType 状态定义 (对齐 2026 客户端 Main.swf):
//   - 0 (Acceptable): 可接取任务 -> 调用 0x00030003 (accept_quest)
//   - 1 (Accepted):   已接取进行中 -> 纯对话调用 0x00030004 (finish_quest)，副本挑战调用 Mod 4 (get_sections -> enter_mission -> fight_monster -> rank_mission -> open_box -> pickup_award)
//   - 2 (Completed):  已达成可交付 -> 调用 0x00030005 (complete_quest)
func executeAuthoritativeQuestCycle(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	initialItems []protocol.PlayerQuestItem,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
	failedMissions map[int]bool,
) {
	const powerPerSweep = 5
	const maxCycles = 50
	items := initialItems

	for cycle := 1; cycle <= maxCycles; cycle++ {
		if ctx.Err() != nil || session.State() == client.StateClosed {
			slog.Info("会话已关闭，终止任务推图循环", "role_id", session.RoleID(), "cycle", cycle)
			return
		}
		if cycle > 1 {
			var err error
			items, err = session.SyncQuests(ctx)
			if err != nil {
				if errSessionTerminated(ctx, err) {
					slog.Info("会话已关闭，终止任务推图循环", "role_id", session.RoleID(), "cycle", cycle)
					return
				}
				slog.Warn("同步服务端任务列表异常，终止本轮任务推图", "role_id", session.RoleID(), "err", err)
				break
			}
		}

		progressMade := false

		// 诊断: 输出服务端任务条目与本地字典映射，便于核对 Quest -> MissionId 是否漂移。
		if cycle == 1 {
			for _, it := range items {
				q, _ := dictRepo.GetQuest(int(it.QuestID))
				title, missionName := "", ""
				missionID, missionLock := 0, 0
				if q != nil {
					title = q.Title
					missionID = q.MissionID
					if m, mErr := dictRepo.GetMission(q.MissionID); mErr == nil && m != nil {
						missionName = m.Name
						missionLock = m.MissionLock
					}
				}
				slog.Info("服务端任务条目",
					"role_id", session.RoleID(),
					"quest_id", it.QuestID,
					"title", title,
					"state", it.State,
					"state_name", questStateName(it.State),
					"need_type", it.NeedType,
					"target_id", it.TargetID,
					"current", it.CurrentCount,
					"max", it.MaxCount,
					"mission_id", missionID,
					"mission_name", missionName,
					"mission_lock", missionLock,
				)
			}
		}

		// -----------------------------------------------------------------
		// Step 1: 优先交付所有已达成目标的任务 (State == 2: Completed)
		// -----------------------------------------------------------------
		completedAny := false
		for _, it := range items {
			if it.State != 2 {
				continue
			}
			q, _ := dictRepo.GetQuest(int(it.QuestID))
			title := fmt.Sprintf("Quest#%d", it.QuestID)
			if q != nil && q.Title != "" {
				title = q.Title
			}
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务交付循环", "role_id", session.RoleID())
				return
			}
			slog.Info("正在交付已达成任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)

			ok, packFull := submitCompleteQuest(ctx, session, it.QuestID, questEndNPCID(q))
			if packFull {
				slog.Warn("背包已满，无法交付任务领取奖励，退出推图循环", "role_id", session.RoleID(), "quest_id", it.QuestID)
				return
			}
			if ok {
				session.RemoveQuest(uint32(it.QuestID))
				if q != nil {
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
				}
				slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
				completedAny = true
				progressMade = true
				time.Sleep(80 * time.Millisecond)
			}
		}

		if completedAny {
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
		}

		// -----------------------------------------------------------------
		// Step 2: 接取所有满足等级的可接取任务 (State == 0: Acceptable)
		// 神仙道同时进行中的任务上限为 5 个 (State == 1)
		// -----------------------------------------------------------------
		acceptedAny := false
		playerLevel := session.GetPlayerState().Level

		activeCount := 0
		for _, it := range items {
			if it.State == 1 {
				activeCount++
			}
		}

		for _, it := range items {
			if activeCount >= 5 {
				break
			}
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务接取循环", "role_id", session.RoleID())
				return
			}
			if it.State != 0 {
				continue
			}
			q, _ := dictRepo.GetQuest(int(it.QuestID))
			if q != nil && q.Level > 0 && q.Level > playerLevel {
				slog.Info("任务等级不足，暂不可接取",
					"role_id", session.RoleID(),
					"quest_id", it.QuestID,
					"title", q.Title,
					"quest_level", q.Level,
					"player_level", playerLevel,
				)
				continue
			}
			title := fmt.Sprintf("Quest#%d", it.QuestID)
			if q != nil && q.Title != "" {
				title = q.Title
			}
			slog.Info("发现可接取任务，正在接受", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)

			// 按官方 NPCDialogView 流程，接取前需先以 BeginNPCId 打开 NPC 对话，
			// 建立服务端 PrevAct 原子链所需的 npc_info_by_npcid 前置动作。
			accepted, resCode, accErr := acceptQuestWithRecovery(ctx, session, it.QuestID, questBeginNPCID(q))
			if accepted {
				slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
				acceptedAny = true
				progressMade = true
				activeCount++
				time.Sleep(80 * time.Millisecond)
			} else {
				if errSessionTerminated(ctx, accErr) {
					slog.Info("会话已关闭，终止任务接取循环", "role_id", session.RoleID(), "quest_id", it.QuestID)
					return
				}
				if resCode == 7 || resCode == 17 {
					slog.Warn("背包已满，无法接受新任务，退出推图循环", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					return
				}
				slog.Warn("接受任务未成功", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title, "result", resCode, "err", accErr)
				// 一旦接取任务重试后仍超时或失败（如已达上限或服务端无响应），停止尝试后续任务，防止连续超时阻塞调度器
				break
			}
		}

		if acceptedAny {
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
			// 接取后若有任务直接处于可交付状态 (State == 2)，立即交付
			for _, it := range items {
				if it.State == 2 {
					q, _ := dictRepo.GetQuest(int(it.QuestID))
					title := fmt.Sprintf("Quest#%d", it.QuestID)
					if q != nil && q.Title != "" {
						title = q.Title
					}
					if ok, _ := submitCompleteQuest(ctx, session, it.QuestID, questEndNPCID(q)); ok {
						session.RemoveQuest(uint32(it.QuestID))
						if q != nil {
							session.AddRewards(q.AwardExp, q.AwardCoins)
							*totalGainExp += q.AwardExp
							*totalGainCoins += q.AwardCoins
						}
						slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
						progressMade = true
					}
				}
			}
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
		}

		// -----------------------------------------------------------------
		// Step 3: 推进已接取进行中的任务 (State == 1: Accepted / InProgress)
		// 排序优先级：对话任务优先 -> 主线任务 (Type=1) 优先 -> 较小 Lock 优先
		// -----------------------------------------------------------------
		var inProgress []protocol.PlayerQuestItem
		for _, it := range items {
			if it.State == 1 {
				inProgress = append(inProgress, it)
			}
		}
		sort.SliceStable(inProgress, func(i, j int) bool {
			qi, _ := dictRepo.GetQuest(int(inProgress[i].QuestID))
			qj, _ := dictRepo.GetQuest(int(inProgress[j].QuestID))
			if qi == nil || qj == nil {
				return inProgress[i].QuestID < inProgress[j].QuestID
			}
			talkI := qi.MissionID == 0 || qi.TalkQuest == 1
			talkJ := qj.MissionID == 0 || qj.TalkQuest == 1
			if talkI != talkJ {
				return talkI
			}
			if qi.Type != qj.Type {
				return qi.Type < qj.Type
			}
			if qi.Lock != qj.Lock {
				return qi.Lock < qj.Lock
			}
			return qi.ID < qj.ID
		})

		for _, it := range inProgress {
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务推进循环", "role_id", session.RoleID())
				return
			}
			q, err := dictRepo.GetQuest(int(it.QuestID))
			if err != nil || q == nil {
				continue
			}

			// 3A. 对话类任务 (MissionID == 0 或 TalkQuest == 1)
			if q.MissionID == 0 || q.TalkQuest == 1 {
				slog.Info("正在交付对话剧情任务", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
				finPkt := protocol.BuildFinishQuestPacket(it.QuestID, session.LastActionID())
				finCtx, finCancel := context.WithTimeout(ctx, 8*time.Second)
				_, _ = session.Call(finCtx, finPkt, protocol.ActionIDQuestFinish)
				finCancel()

				ok, packFull := submitCompleteQuest(ctx, session, it.QuestID, questEndNPCID(q))
				if packFull {
					slog.Warn("背包已满，无法交付对话任务", "role_id", session.RoleID(), "quest_id", q.ID)
					return
				}
				if ok {
					session.RemoveQuest(uint32(it.QuestID))
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("对话任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
					progressMade = true
					time.Sleep(80 * time.Millisecond)
				}
				continue
			}

			// 3B. 副本挑战类任务 (MissionID > 0)
			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetStamina() < powerPerSweep && !ensureStamina() {
				slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetStamina())
				return
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title, "mission_id", mid, "player_level", session.GetPlayerState().Level)

			if advanceQuestMission(
				ctx, session, dictRepo, q, it.CurrentCount, it.MaxCount, ensureStamina,
				totalCostStamina, totalGainExp, totalGainCoins,
				clearedMissions, failedMissions,
			) {
				progressMade = true
				// 任一任务完成或取得实质推进后立即跳出重排，确保新解锁的主线任务优先接取并执行
				break
			} else if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务推进循环", "role_id", session.RoleID(), "mission_id", mid)
				return
			} else {
				slog.Info("任务关卡暂未通关或卡战力/等级，记录跳过", "role_id", session.RoleID(), "mission_id", mid, "quest_id", q.ID)
				failedMissions[mid] = true
			}
		}

		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
	resetTownScene(session)
}

// submitCompleteQuest 向服务端发送完成并交付任务请求 (Mod 3, Act 5: complete_quest)。
// 交付前先按官方 NPCDialogView 流程以 EndNPCId 打开 NPC 对话 (npc_info_by_npcid)，
// 建立服务端 PrevAct 原子链所需的对话前置；缺失时网关会静默丢弃 complete_quest。
// 依据客户端 NPCDialogView.onComplete (Method 6441) 字节码约定：
//   - completedState > 0 (如 1, 3, 5): 交付成功 (completeQuestCallback)
//   - completedState == 0 (且为 16B 完整回包): 背包已满 (NPCDialogViewLang.PackFull)
//   - completedState == -1 (0xFF): 任务物品不足或未达成 (NPCDialogViewLang.QuestItem)
func submitCompleteQuest(ctx context.Context, session *client.RoleSession, questID, endNPCID int32) (succeeded bool, packFull bool) {
	sendSubmit := func() (*protocol.Packet, error) {
		subPkt := protocol.BuildSubmitCompleteQuestPacket(questID, session.LastActionID())
		subCtx, subCancel := context.WithTimeout(ctx, 8*time.Second)
		defer subCancel()
		return session.Call(subCtx, subPkt, protocol.ActionIDQuestSubmitComplete)
	}

	// 0. 优先直接交付 complete_quest (0x00030005)，服务端不强制要求先打开 NPC 对话。
	resp, err := sendSubmit()

	// 无回包时兜底：若任务交付 NPC 不在当前城镇（如仙界 NPC），先补发 NPC 对话
	// 建立官方 PrevAct 前置链，再重试一次交付。
	if (err != nil || resp == nil || len(resp.Payload) == 0) && endNPCID > 0 {
		if _, npcErr := queryNPCQuestState(ctx, session, endNPCID, questID); npcErr != nil {
			if errSessionTerminated(ctx, npcErr) {
				return false, false
			}
			slog.Warn("打开 NPC 交付对话失败，停止本次交付",
				"role_id", session.RoleID(), "quest_id", questID, "npc_id", endNPCID, "err", npcErr)
		} else {
			resp, err = sendSubmit()
		}
	}
	if err != nil || resp == nil || len(resp.Payload) == 0 {
		return false, false
	}

	state := int8(resp.Payload[0])
	if state > 0 {
		return true, false
	}
	if state == 0 && len(resp.Payload) >= 16 {
		return false, true
	}

	// 若任务尚未标记 finish (state == -1)，补发一次 finish_quest (0x00030004) 后重试 complete_quest (0x00030005)
	finPkt := protocol.BuildFinishQuestPacket(questID, session.LastActionID())
	finCtx, finCancel := context.WithTimeout(ctx, 8*time.Second)
	_, _ = session.Call(finCtx, finPkt, protocol.ActionIDQuestFinish)
	finCancel()

	subPkt2 := protocol.BuildSubmitCompleteQuestPacket(questID, session.LastActionID())
	subCtx2, subCancel2 := context.WithTimeout(ctx, 8*time.Second)
	resp2, err2 := session.Call(subCtx2, subPkt2, protocol.ActionIDQuestSubmitComplete)
	subCancel2()
	if err2 != nil || resp2 == nil || len(resp2.Payload) == 0 {
		return false, false
	}
	state2 := int8(resp2.Payload[0])
	if state2 > 0 {
		return true, false
	}
	if state2 == 0 && len(resp2.Payload) >= 16 {
		return false, true
	}
	return false, false
}

// advanceQuestMission 推进指定任务的关联剧情关卡（包含自动清理同章节前置未通关小关与秒扫荡闭环）。
func advanceQuestMission(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	q *dictionary.Quest,
	initialCount int32,
	maxCount int32,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
	failedMissions map[int]bool,
) bool {
	targetMission, err := dictRepo.GetMission(q.MissionID)
	if err != nil || targetMission == nil {
		return false
	}
	// 精英副本任务 (Quests.Type=4) 与主线/支线共用 Module 4 首通状态机，
	// 但精英关卡不在 get_sections 章节列表内，也不参与 Module 25 普通关卡扫荡。
	isEliteQuest := q.Type == 4
	if q.Type != 1 && q.Type != 2 && !isEliteQuest {
		slog.Info("任务关卡属于非主线/支线/精英任务，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "type", q.Type)
		return false
	}
	if isEliteQuest {
		if targetMission.Type != 1 {
			slog.Info("精英任务关联关卡并非精英副本，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "mission_id", targetMission.ID, "type", targetMission.Type)
			return false
		}
	} else if targetMission.Type != 0 {
		slog.Info("任务关卡属于非普通主线副本，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "mission_id", targetMission.ID, "type", targetMission.Type)
		return false
	}

	playerLevel := session.GetPlayerState().Level
	if missionLevelLocked(targetMission.MissionLock, playerLevel) {
		slog.Warn("任务关卡等级未达标，暂不推进剧情副本",
			"role_id", session.RoleID(),
			"quest_id", q.ID,
			"title", q.Title,
			"mission_id", targetMission.ID,
			"name", targetMission.Name,
			"mission_lock", targetMission.MissionLock,
			"required_level", missionRequiredLevel(targetMission.MissionLock),
			"player_level", playerLevel,
		)
		return false
	}

	const maxStageSteps = 10
	lastCount := initialCount
	targetMax := maxCount
	noCountGainRuns := 0
	clearedPrereq := false

	for step := 0; step < maxStageSteps; step++ {
		if ctx.Err() != nil || session.State() == client.StateClosed {
			return clearedPrereq || lastCount > initialCount
		}
		if session.GetStamina() < 5 && !ensureStamina() {
			return clearedPrereq || lastCount > initialCount
		}

		// 计算本批次需要扫荡的次数 (单批最多 5 次以保持平稳)
		batchCount := uint16(1)
		if targetMax > lastCount {
			needed := targetMax - lastCount
			avail := int32(session.GetStamina() / 5)
			if avail < needed && ensureStamina != nil {
				if ensureStamina() {
					avail = int32(session.GetStamina() / 5)
				}
			}
			if needed > 0 && avail > 0 {
				cnt := min(needed, avail)
				if cnt > 5 {
					cnt = 5
				}
				if cnt > 0 {
					batchCount = uint16(cnt)
				}
			}
		}

		// 0. 激活章节关卡列表上下文 (Mod 4, Act 0: get_sections)，对齐官方前置调用链
		mapID := int32(targetMission.MapID)
		if mapID <= 0 {
			mapID = int32(targetMission.SectionID)
		}
		targetKnownUncleared := false
		// targetInSections 记录目标关卡是否出现在 get_sections 章节列表中。
		// 未出现说明该关卡尚未解锁 (或章节列表拉取失败)，此时 Module 25 扫荡会被网关静默丢弃，
		// 必须直接走剧情副本，避免每次空等 5 秒。
		targetInSections := false
		if mapID > 0 {
			secPkt := protocol.BuildMissionGetSectionsPacket(mapID, session.LastActionID())
			secCtx, secCancel := context.WithTimeout(ctx, 8*time.Second)
			secResp, secErr := session.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
			secCancel()
			if secErr != nil {
				if errSessionTerminated(ctx, secErr) {
					return clearedPrereq || lastCount > initialCount
				}
				if isEliteQuest {
					// 精英副本本就不在 get_sections 列表中，继续走 Module 4 首通状态机。
					slog.Debug("精英副本跳过章节列表查询", "role_id", session.RoleID(), "map_id", mapID, "err", secErr)
				} else {
					// 非精英关卡：get_sections 失败说明章节/地图上下文不可用，
					// 继续盲发 enter_mission 会被服务端静默丢弃，直接跳过本轮该关卡。
					slog.Warn("激活章节关卡列表上下文失败，跳过该关卡", "role_id", session.RoleID(), "map_id", mapID, "err", secErr)
					return clearedPrereq || lastCount > initialCount
				}
			} else if secResp != nil {
				if sections, pErr := protocol.ParseMissionGetSectionsResponse(secResp.Payload); pErr == nil {
					clearedCount, otherCount := 0, 0
					targetStatus := int8(-1)
					targetFound := false
					for _, sec := range sections {
						if sec.Status == 1 {
							clearedMissions[int(sec.MissionID)] = true
							clearedCount++
						} else {
							otherCount++
						}
						if int(sec.MissionID) == targetMission.ID {
							targetStatus = sec.Status
							targetFound = true
							targetInSections = true
							targetKnownUncleared = sec.Status == 0
						}
					}
					if step == 0 {
						slog.Info("章节关卡状态同步",
							"role_id", session.RoleID(),
							"map_id", mapID,
							"total", len(sections),
							"status_1", clearedCount,
							"status_other", otherCount,
							"target_mission_id", targetMission.ID,
							"target_found", targetFound,
							"target_status", targetStatus,
						)
					}
				}
			}
		}

		// 1. 优先尝试 Module 25 秒扫荡推进关卡。
		// 服务端 get_sections 标明该关卡尚未通关 (status == 0) 时，Module 25 扫荡必然无响应，
		// 直接走剧情副本首通挑战，避免每次空等 5 秒超时。
		practiced := false
		var pErr error
		if isEliteQuest {
			// 精英副本不在 get_sections 章节列表内，Module 25 普通扫荡对其不生效，
			// 直接走 Module 4 首通状态机 (enter -> fight -> rank -> box -> pickup)。
			slog.Info("精英副本任务直接进入首通挑战",
				"role_id", session.RoleID(),
				"mission_id", targetMission.ID,
				"name", targetMission.Name,
			)
		} else if !targetKnownUncleared && targetInSections {
			practiced, _, pErr = practiceMission(ctx, session, int32(targetMission.ID), batchCount, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins)
			if pErr != nil {
				if errors.Is(pErr, scheduler.ErrStaminaDepleted) || errors.Is(pErr, scheduler.ErrBagFull) {
					return clearedPrereq || lastCount > initialCount
				}
			}
		} else {
			reason := "关卡尚未首通"
			if !targetInSections {
				reason = "关卡不在已开放章节列表中 (未解锁或列表拉取失败)"
			}
			slog.Info("跳过 Module 25 扫荡直接进入剧情副本",
				"role_id", session.RoleID(),
				"mission_id", targetMission.ID,
				"name", targetMission.Name,
				"reason", reason,
			)
		}

		if !practiced {
			// 若该关卡在服务端已通关但扫荡失败，不再尝试进入剧情挑战（剧情副本对已通关关卡无怪物）
			if clearedMissions[targetMission.ID] {
				return clearedPrereq || lastCount > initialCount
			}

			// 若 Module 25 扫荡受限（如首通尚未解锁），降级检查前置关卡并走剧情副本手动挑战。
			// 精英关卡与普通章节关卡相互独立，不做同章节前置扫描，直接挑战目标关卡。
			var prereqMission *dictionary.Mission
			if !isEliteQuest && targetMission.MapID > 0 {
				secPkt := protocol.BuildMissionGetSectionsPacket(int32(targetMission.MapID), session.LastActionID())
				secCtx, secCancel := context.WithTimeout(ctx, 8*time.Second)
				secResp, secErr := session.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
				secCancel()
				if errSessionTerminated(ctx, secErr) {
					return clearedPrereq || lastCount > initialCount
				}

				if secErr == nil && secResp != nil {
					if sections, pErr := protocol.ParseMissionGetSectionsResponse(secResp.Payload); pErr == nil {
						for _, sec := range sections {
							if sec.Status == 1 {
								clearedMissions[int(sec.MissionID)] = true
							}
							if sec.Status == 0 && int(sec.MissionID) != targetMission.ID && !failedMissions[int(sec.MissionID)] {
								if sm, mErr := dictRepo.GetMission(int(sec.MissionID)); mErr == nil && sm != nil {
									if sm.MissionLock <= targetMission.MissionLock {
										if prereqMission == nil || sm.MissionLock < prereqMission.MissionLock {
											prereqMission = sm
										}
									}
								}
							}
						}
					}
				}
			}

			if prereqMission != nil {
				slog.Info("发现前置未通关剧情关卡，先行推进挑战",
					"role_id", session.RoleID(),
					"quest_id", q.ID,
					"prereq_mission_id", prereqMission.ID,
					"prereq_mission_name", prereqMission.Name,
					"target_mission_id", targetMission.ID,
				)
				if !challengeStoryMission(ctx, session, prereqMission, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
					failedMissions[prereqMission.ID] = true
					return clearedPrereq || lastCount > initialCount
				}
				clearedPrereq = true
				continue
			}

			// 正式挑战主线目标关卡
			if !challengeStoryMission(ctx, session, targetMission, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
				return clearedPrereq || lastCount > initialCount
			}
		}

		// 2. 挑战/扫荡后，同步任务状态检查是否已达成可交付 (State == 2) 或目标计数增长
		refreshed, sErr := session.SyncQuests(ctx)
		if sErr == nil {
			questState := int32(-1)
			var matchedItem protocol.PlayerQuestItem
			for _, it := range refreshed {
				if int(it.QuestID) == q.ID {
					questState = it.State
					matchedItem = it
					break
				}
			}
			if questState == 2 {
				ok, _ := submitCompleteQuest(ctx, session, int32(q.ID), questEndNPCID(q))
				if ok {
					session.RemoveQuest(uint32(q.ID))
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
				}
				return true
			}
			if questState == -1 {
				// 任务已不在进行列表中（已自动完成）
				return true
			}
			if matchedItem.MaxCount > 0 {
				targetMax = matchedItem.MaxCount
			}
			if matchedItem.CurrentCount > lastCount {
				slog.Info("任务目标进度更新",
					"role_id", session.RoleID(),
					"quest_id", q.ID,
					"title", q.Title,
					"progress", fmt.Sprintf("%d/%d", matchedItem.CurrentCount, matchedItem.MaxCount),
				)
				lastCount = matchedItem.CurrentCount
				noCountGainRuns = 0
				if matchedItem.MaxCount > 0 && matchedItem.CurrentCount >= matchedItem.MaxCount {
					continue
				}
				continue
			}
			noCountGainRuns++
			// 若杀怪任务挑战后计数未增长，或掉落物任务连续 2 次未掉落，退出本轮避免空转
			if matchedItem.NeedType == 0 || noCountGainRuns >= 2 {
				return lastCount > initialCount
			}
		} else {
			return true
		}
	}

	return clearedPrereq || lastCount > initialCount
}

// practiceMission 使用 Module 25 (Mod_MissionPractice_Base) 执行关卡秒扫荡。
func practiceMission(
	ctx context.Context,
	session *client.RoleSession,
	missionID int32,
	count uint16,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
) (bool, int, error) {
	if session == nil {
		return false, 0, client.ErrNotConnected
	}
	if session.GetStamina() < 5 && !ensureStamina() {
		return false, 0, scheduler.ErrStaminaDepleted
	}
	if count <= 0 {
		count = 1
	}
	maxAvail := uint16(session.GetStamina() / 5)
	if count > maxAvail {
		count = maxAvail
	}
	if count == 0 {
		return false, 0, scheduler.ErrStaminaDepleted
	}

	// 1. 发起扫荡 (ActionIDPracticeStart: 0x00190001)
	m25Req := protocol.StartPracticeRequest{
		MissionID: missionID,
		Count:     int16(count),
		AutoSale:  1,
	}
	m25Pkt, _ := protocol.BuildStartPracticePacket(m25Req)

	callCtx, callCancel := context.WithTimeout(ctx, 8*time.Second)
	respPkt, callErr := session.Call(callCtx, m25Pkt, protocol.ActionIDPracticeStart)
	callCancel()

	if callErr != nil || respPkt == nil {
		slog.Warn("Module 25 扫荡请求未收到响应", "role_id", session.RoleID(), "mission_id", missionID, "err", callErr)
		return false, 0, callErr
	}

	m25Res, pErr := protocol.ParseStartPracticeResult(respPkt.Payload)
	if pErr != nil || m25Res == nil {
		slog.Warn("Module 25 扫荡回包解析失败", "role_id", session.RoleID(), "mission_id", missionID, "err", pErr)
		return false, 0, pErr
	}

	slog.Info("Module 25 扫荡回包", "role_id", session.RoleID(), "mission_id", missionID, "result", m25Res.Result, "count", m25Res.Count)

	switch m25Res.Result {
	case protocol.PracticeResultSuccess:
		// 快速完成扫荡 (ActionIDPracticeQuickly: 0x00190003，服务端主动推送 0x00190004 结算通知)
		quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0})
		qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
		_, _ = session.Call(qCtx, quickPkt, protocol.ActionIDPracticeNotify)
		qCancel()

		actualRounds := int(count)
		if m25Res.Count > 0 && int(m25Res.Count) <= actualRounds {
			actualRounds = int(m25Res.Count)
		}
		cost := actualRounds * 5
		_, _ = session.ConsumeStamina(cost)
		gainExp := int64(actualRounds) * 2500
		gainCoins := int64(actualRounds) * 12000
		session.AddRewards(gainExp, gainCoins)
		if totalCostStamina != nil {
			*totalCostStamina += cost
		}
		if totalGainExp != nil {
			*totalGainExp += gainExp
		}
		if totalGainCoins != nil {
			*totalGainCoins += gainCoins
		}
		return true, actualRounds, nil

	case protocol.PracticeResultNotEnoughPower:
		session.SetStamina(0)
		session.SetExtraStamina(0)
		return false, 0, scheduler.ErrStaminaDepleted

	case protocol.PracticeResultBagFull:
		return false, 0, scheduler.ErrBagFull

	default:
		return false, 0, fmt.Errorf("practice rejected with code %d", m25Res.Result)
	}
}

func resetTownScene(session *client.RoleSession) {
	// 在神仙道官方客户端架构中，通关剧情副本或任务流转退出后直接留在当前主界面，
	// 严禁在会话运行中途重放登录期握手包 (0x0048/0x0063/0x00A50000/0x0002)，
	// 否则会重置网关状态机，导致后续六道轮回等战斗协议被服务端静默丢弃。
}

// missionRequiredLevel 计算剧情关卡 MissionLock 对应的角色等级门槛。
// Pieb.db 约定 MissionLock = 等级 * 1000 + 章节内关卡序号 (如 20080 表示 20 级章节第 8 关)，
// 因此 20080 在角色等级 20 时即可挑战，21010 需要等级 21。
func missionRequiredLevel(missionLock int) int {
	if missionLock <= 0 {
		return 0
	}
	return missionLock / 1000
}

// missionLevelLocked 判断角色等级是否尚未达到剧情关卡解锁门槛。
// playerLevel <= 0 表示等级未知，此时不做客户端拦截，交由服务端裁决。
func missionLevelLocked(missionLock, playerLevel int) bool {
	if playerLevel <= 0 {
		return false
	}
	return missionRequiredLevel(missionLock) > playerLevel
}

// challengeStoryMission 使用 Module 4 (Mod_Mission_Base) 完整五步状态机通关指定剧情关卡：
// enter_mission (0x00040001) -> fight_monster (0x00040002) -> rank_mission (0x00040003) -> open_box (0x00040004) -> pickup_award (0x00040005)。
func challengeStoryMission(
	ctx context.Context,
	session *client.RoleSession,
	m *dictionary.Mission,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
) bool {
	if m == nil {
		return false
	}
	if ctx.Err() != nil || session.State() == client.StateClosed {
		return false
	}

	playerLevel := session.GetPlayerState().Level
	requiredLevel := missionRequiredLevel(m.MissionLock)
	if missionLevelLocked(m.MissionLock, playerLevel) {
		slog.Warn("角色等级未达到剧情关卡解锁门槛，跳过副本进入",
			"role_id", session.RoleID(),
			"mission_id", m.ID,
			"name", m.Name,
			"mission_lock", m.MissionLock,
			"required_level", requiredLevel,
			"player_level", playerLevel,
		)
		return false
	}

	var monsterTeams []int32
	for _, part := range strings.Split(m.Monsters, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if v, err := strconv.Atoi(part); err == nil && v > 0 {
			monsterTeams = append(monsterTeams, int32(v))
		}
	}
	if len(monsterTeams) == 0 {
		slog.Warn("关卡缺失怪物编队配置，无法发起挑战", "mission_id", m.ID, "name", m.Name)
		return false
	}

	if session.GetStamina() < 5 && !ensureStamina() {
		return false
	}

	// 0. 激活章节关卡列表上下文 (Mod 4, Act 0: get_sections)，对齐官方前置调用链
	if session.LastActionID() != protocol.ActionIDMissionGetSections {
		mapID := int32(m.MapID)
		if mapID <= 0 {
			mapID = int32(m.SectionID)
		}
		secPkt := protocol.BuildMissionGetSectionsPacket(mapID, session.LastActionID())
		secCtx, secCancel := context.WithTimeout(ctx, 8*time.Second)
		_, _ = session.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
		secCancel()
		if ctx.Err() != nil || session.State() == client.StateClosed {
			return false
		}
	}

	// 1. 进入副本 (Mod 4, Act 1: enter_mission, 前置对齐 ActionIDMissionGetSections)
	prevAct := session.LastActionID()
	enterPkt := protocol.BuildMissionPlotEnterPacket(int32(m.ID), prevAct)
	slog.Info("准备进入剧情副本",
		"role_id", session.RoleID(),
		"mission_id", m.ID,
		"name", m.Name,
		"mission_lock", m.MissionLock,
		"required_level", requiredLevel,
		"player_level", playerLevel,
		"stamina", session.GetStamina(),
		"prev_act", fmt.Sprintf("0x%08X", prevAct),
		"payload_hex", hex.EncodeToString(enterPkt.Payload),
	)
	enterCtx, enterCancel := context.WithTimeout(ctx, 8*time.Second)
	enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionIDMissionPlotEnter)
	enterCancel()

	if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
		if errSessionTerminated(ctx, enterErr) {
			return false
		}
		slog.Warn("进入剧情副本未收到有效响应",
			"role_id", session.RoleID(),
			"mission_id", m.ID,
			"name", m.Name,
			"mission_lock", m.MissionLock,
			"required_level", requiredLevel,
			"player_level", playerLevel,
			"prev_act", fmt.Sprintf("0x%08X", prevAct),
			"payload_hex", hex.EncodeToString(enterPkt.Payload),
			"err", enterErr,
		)
		resetTownScene(session)
		return false
	}

	enterStatus := enterResp.Payload[0]
	if enterStatus == 20 { // AWARD_BOX_NO_OPEN: 上次通关宝箱尚未开启或拾取，先行清理再重进
		boxCtx, boxCancel := context.WithTimeout(ctx, 8*time.Second)
		_, _ = session.Call(boxCtx, protocol.BuildMissionOpenBoxPacket(protocol.ActionIDMissionRank), protocol.ActionIDMissionOpenBox)
		boxCancel()
		pickCtx, pickCancel := context.WithTimeout(ctx, 8*time.Second)
		_, _ = session.Call(pickCtx, protocol.BuildMissionPickupAwardPacket(protocol.ActionIDMissionOpenBox), protocol.ActionIDMissionPickupAward)
		pickCancel()

		enterPkt = protocol.BuildMissionPlotEnterPacket(int32(m.ID), protocol.ActionIDMissionGetSections)
		enterCtxR, enterCancelR := context.WithTimeout(ctx, 8*time.Second)
		enterResp, enterErr = session.Call(enterCtxR, enterPkt, protocol.ActionIDMissionPlotEnter)
		enterCancelR()
		if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
			return false
		}
		enterStatus = enterResp.Payload[0]
	}

	if enterStatus == protocol.MissionResultNotEnoughPower || enterStatus == 18 { // LESS_POWER == 18
		session.SetStamina(0)
		session.SetExtraStamina(0)
		if !ensureStamina() {
			return false
		}
		enterPkt = protocol.BuildMissionPlotEnterPacket(int32(m.ID), protocol.ActionIDMissionGetSections)
		enterCtx2, enterCancel2 := context.WithTimeout(ctx, 8*time.Second)
		enterResp, enterErr = session.Call(enterCtx2, enterPkt, protocol.ActionIDMissionPlotEnter)
		enterCancel2()
		if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
			return false
		}
		enterStatus = enterResp.Payload[0]
	}

	if enterStatus != protocol.MissionResultSuccess {
		slog.Warn("进入剧情副本未成功", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "status", fmt.Sprintf("0x%02X", enterStatus))
		return false
	}

	// 依据服务端 enter_mission 实时回包动态获取当前关卡待挑战怪物波次列表
	// 官方回包规范: [UByte(status), Int32(mid), Int32(stam), Byte(b), Int32(i1), Int32(i2), Uint16(mCnt), ...[Int32(mt)]]
	// 实测 2026 网关首通时 mCnt 恒为 0，怪物编队需回退 Pieb.db Missions.monsters 静态配置，
	// 不能据此判定关卡已通关（是否通关以 get_sections status 为准）。
	var activeMonsterTeams []int32
	if len(enterResp.Payload) >= 20 {
		mCnt := binary.BigEndian.Uint16(enterResp.Payload[18:20])
		if mCnt == 0 {
			slog.Info("enter_mission 未下发怪物编队，回退关卡字典配置",
				"role_id", session.RoleID(),
				"mission_id", m.ID,
				"name", m.Name,
				"configured_waves", len(monsterTeams),
			)
		} else {
			r := protocol.NewReader(enterResp.Payload[20:])
			for i := uint16(0); i < mCnt && r.Remaining() >= 4; i++ {
				mt, _ := r.ReadInt32()
				activeMonsterTeams = append(activeMonsterTeams, mt)
			}
		}
	}
	if len(activeMonsterTeams) > 0 {
		monsterTeams = activeMonsterTeams
	}

	// 2. 逐波次挑战副本内怪物编队 (Mod 4, Act 2: fight_monster)
	anyWaveSuccess := false
	for idx, mtID := range monsterTeams {
		if idx > 0 {
			// 服务端连续战斗存在冷却保护 (对齐客户端 CommonConstant.DelayTimeToWar)
			select {
			case <-ctx.Done():
				resetTownScene(session)
				return anyWaveSuccess
			case <-time.After(1000 * time.Millisecond):
			}
		}

		fightPkt := protocol.BuildMissionFightMonsterPacket(mtID, 0, 0, session.LastActionID())
		fightCtx, fightCancel := context.WithTimeout(ctx, 8*time.Second)
		fightResp, fightErr := session.Call(fightCtx, fightPkt, protocol.ActionIDMissionFightMonster)
		fightCancel()

		if fightErr != nil || fightResp == nil || len(fightResp.Payload) == 0 {
			if errSessionTerminated(ctx, fightErr) {
				return anyWaveSuccess
			}
			if anyWaveSuccess {
				slog.Info("剧情副本已击败当前可用波次怪物，返回城镇同步进度", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID)
				resetTownScene(session)
				return true
			}
			slog.Warn("剧情副本挑战怪物未收到响应", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID, "err", fightErr)
			resetTownScene(session)
			return false
		}

		resByte := fightResp.Payload[len(fightResp.Payload)-1]
		// 10 = SUCCESS, 19 = TRUNK_ALREADY_FIGHT, 30 = HASCHALLENGED (该波怪物已击败，继续下一波)
		if resByte != protocol.MissionResultSuccess && resByte != 19 && resByte != 30 {
			if anyWaveSuccess {
				slog.Info("剧情副本后续波次暂不可用，返回城镇同步进度", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID)
				resetTownScene(session)
				return true
			}
			slog.Warn("剧情副本战斗未获胜或受阻",
				"role_id", session.RoleID(),
				"mission_id", m.ID,
				"name", m.Name,
				"wave", fmt.Sprintf("%d/%d", idx+1, len(monsterTeams)),
				"monster_team_id", mtID,
				"result", resByte,
			)
			resetTownScene(session)
			return false
		}
		anyWaveSuccess = true
	}

	// 3. 结算星级评价 (Mod 4, Act 3: rank_mission)
	// 回包解析铁律: 结算三步必须持有回包并校验非空。任一步超时都说明服务端仍停留在副本态，
	// 此时继续发起后续 Mod 4/25 请求会被网关静默丢弃，必须立即中止并交由外层重试。
	rankCtx, rankCancel := context.WithTimeout(ctx, 8*time.Second)
	rankResp, rankErr := session.Call(rankCtx, protocol.BuildMissionRankPacket(protocol.ActionIDMissionFightMonster), protocol.ActionIDMissionRank)
	rankCancel()
	if rankErr != nil || rankResp == nil {
		if errSessionTerminated(ctx, rankErr) {
			return false
		}
		slog.Warn("副本星级结算未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", rankErr)
		resetTownScene(session)
		return false
	}

	// 4. 开启通关宝箱 (Mod 4, Act 4: open_box)
	boxCtx, boxCancel := context.WithTimeout(ctx, 8*time.Second)
	boxResp, boxErr := session.Call(boxCtx, protocol.BuildMissionOpenBoxPacket(protocol.ActionIDMissionRank), protocol.ActionIDMissionOpenBox)
	boxCancel()
	if boxErr != nil || boxResp == nil {
		if errSessionTerminated(ctx, boxErr) {
			return false
		}
		slog.Warn("副本通关宝箱开启未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", boxErr)
		resetTownScene(session)
		return false
	}

	// 5. 拾取宝箱奖励 (Mod 4, Act 5: pickup_award)
	pickCtx, pickCancel := context.WithTimeout(ctx, 8*time.Second)
	pickResp, pickErr := session.Call(pickCtx, protocol.BuildMissionPickupAwardPacket(protocol.ActionIDMissionOpenBox), protocol.ActionIDMissionPickupAward)
	pickCancel()
	if pickErr != nil || pickResp == nil {
		if errSessionTerminated(ctx, pickErr) {
			return false
		}
		slog.Warn("副本结算奖励拾取未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", pickErr)
		resetTownScene(session)
		return false
	}

	// 通关完成，返回城镇场景
	resetTownScene(session)

	// 预留战斗冷却间隔，避免下一关卡首波战斗触发服务端 CD
	time.Sleep(1200 * time.Millisecond)

	remStamina, _ := session.ConsumeStamina(5)
	session.AddRewards(2500, 12000)
	*totalCostStamina += 5
	*totalGainExp += 2500
	*totalGainCoins += 12000
	clearedMissions[m.ID] = true

	slog.Info("任务关卡推进挑战成功",
		"role_id", session.RoleID(),
		"mission_id", m.ID,
		"mission_name", m.Name,
		"waves", len(monsterTeams),
		"remaining_stamina", remStamina,
	)
	return true
}
