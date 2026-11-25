// Package dungeon 提供副本推图、主线任务循环、精英扫荡、六道轮回、伏魔塔等关卡挑战自动化。
//
// 本文件实现副本扫荡、精英扫荡、背包卷轴/材料自动清理与登录英雄副本扫荡。
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// eliteSweepBlocked 记录连续/反复开始失败的精英副本，避免每轮固定丢包。
// key = roleID:dungeonID，value = 解禁时间。
var eliteSweepBlocked sync.Map

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
		// 背包列表请求同样可能被服务端静默丢弃；显式记录，避免「没卖扫荡物」
		// 看起来像逻辑问题而无从定位。
		slog.Warn("背包列表拉取失败，本轮跳过扫荡物出售",
			"role_id", session.RoleID(), "err", err)
		return 0
	}

	sold := 0
	newSlots, grownSlots, skipNotSweepType, skipNotSellable := 0, 0, 0, 0
	var pending []protocol.ItemRecord
	for _, it := range pack.PackItems {
		qty := packItemQuantity(it)
		prevQty, existed := base.byPlayerItem[it.PlayerItemID]
		if existed && qty <= prevQty {
			continue
		}
		if existed {
			grownSlots++
		} else {
			newSlots++
		}
		// 新格位必然是扫荡掉落；已有格位数量增加时，仅对精英副本主要掉落类型
		// (1002=制作卷) 整格出售，避免把玩家原有消耗品/体力道具整组卖掉。
		if existed && !isEliteSweepStackType(dictRepo, it) {
			skipNotSweepType++
			continue
		}
		if !isPackItemSellable(dictRepo, it) {
			skipNotSellable++
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
		kit.Sleep(ctx, 500*time.Millisecond)
		for _, it := range pending {
			ok, retryable := sellPackItem(ctx, session, it)
			if ok {
				sold++
			} else if retryable {
				unresolved = append(unresolved, it)
			}
		}
	}

	if newSlots > 0 || grownSlots > 0 || sold > 0 || len(unresolved) > 0 {
		slog.Info("扫荡物出售明细", "role_id", session.RoleID(),
			"pack_items", len(pack.PackItems), "base_items", len(base.byPlayerItem),
			"new_slots", newSlots, "grown_slots", grownSlots,
			"skip_not_sweep_type", skipNotSweepType, "skip_not_sellable", skipNotSellable,
			"sold", sold, "unresolved", len(unresolved))
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

// sellAgedSweepStacks 在背包已满、且差量出售没能腾出空间时，直接出售背包里
// 已有的制作卷 (TypeID=1002) 堆叠。这些就是精英副本扫荡掉落；如果只卖「本轮新增」，
// 背包里沉积的旧扫荡物永远不会被出售，扫荡会一直卡在「背包已满」。
// 每次最多出售 maxStacks 叠，交由调用方重试扫荡。
func sellAgedSweepStacks(ctx context.Context, session *client.RoleSession, dictRepo dictionary.Repository, maxStacks int) int {
	if dictRepo == nil || maxStacks <= 0 {
		return 0
	}
	pack, err := fetchPackItemList(ctx, session)
	if err != nil || pack == nil {
		slog.Warn("背包列表拉取失败，无法出售沉积扫荡物", "role_id", session.RoleID(), "err", err)
		return 0
	}
	sold := 0
	for _, it := range pack.PackItems {
		if sold >= maxStacks || ctx.Err() != nil {
			break
		}
		if !isEliteSweepStackType(dictRepo, it) || !isPackItemSellable(dictRepo, it) {
			continue
		}
		if ok, _ := sellPackItem(ctx, session, it); ok {
			sold++
		}
	}
	if sold > 0 {
		slog.Info("背包已满，出售沉积的制作卷", "role_id", session.RoleID(),
			"sold", sold, "max_stacks", maxStacks)
	}
	return sold
}

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
	slog.Info("开始全局精英副本扫荡", "role_id", session.RoleID(), "dungeons", len(ids),
		"last_action", fmt.Sprintf("0x%08X", session.LastActionID()))

	// 角色剧情进度到达的最高地图：精英副本若位于更后面的地图，服务端会
	// 静默丢弃 25/1（2026-11-14：任务全在 map 21，map 22 的副本 168 必丢）。
	maxQuestMap := 0
	for qid := range session.GetQuests() {
		q, qErr := dictRepo.GetQuest(int(qid))
		if qErr != nil || q == nil {
			continue
		}
		m, mErr := dictRepo.GetMission(q.MissionID)
		if mErr != nil || m == nil {
			continue
		}
		if m.MapID > maxQuestMap {
			maxQuestMap = m.MapID
		}
	}

	swept := 0
	failStreak := 0
	startDropStreak := 0
	for _, dungeonID := range ids {
		if ctx.Err() != nil {
			break
		}
		blockKey := fmt.Sprintf("%s:%d", session.RoleID(), dungeonID)
		if until, ok := eliteSweepBlocked.Load(blockKey); ok {
			if t, _ := until.(time.Time); time.Now().Before(t) {
				continue
			}
		}
		// 网关偶发 EOF 重连；等待会话恢复，并用重连后的新动作号刷新锚点。
		if session.State() != client.StateActive {
			for i := 0; i < 30 && session.State() != client.StateActive; i++ {
				kit.Sleep(ctx, 200*time.Millisecond)
			}
			if session.State() != client.StateActive {
				slog.Info("会话未恢复，结束全局精英扫荡", "role_id", session.RoleID(), "swept", swept)
				break
			}
			// 重连后服务端场景/动作链需要短暂恢复时间。
			failStreak = 0
			kit.Sleep(ctx, 1500*time.Millisecond)
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

		// Module 25 状态查询在现行服务端只接受裸 4 字节 missionID；
		// 带 PrevAct 会被静默丢弃（2026-11-14 s0/s1 实测）。
		statusPkt := protocol.BuildPracticeGetStatusBarePacket(int32(dungeonID))
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
		// 只在「字典中存在且地图不超过角色剧情进度」的子关卡中挑选：
		// 等级最高优先，同等级取最大关卡 ID。这样同一副本有低阶可扫关卡时
		// 仍能扫荡（如副本 168 的 614 在 map 22 不可扫，451 在 map 13 可扫）。
		var best *protocol.PracticeStatusMissionItem
		for i := range status.Missions {
			cand := status.Missions[i]
			mi, mErr := dictRepo.GetMission(int(cand.MissionID))
			if mErr != nil || mi == nil {
				continue
			}
			if maxQuestMap > 0 && mi.MapID > maxQuestMap {
				continue
			}
			if best == nil || cand.Level > best.Level ||
				(cand.Level == best.Level && cand.MissionID > best.MissionID) {
				best = &cand
			}
		}
		if best == nil {
			slog.Debug("精英副本无可扫荡子关卡（关卡缺失或地图未解锁），跳过",
				"role_id", session.RoleID(), "dungeon_id", dungeonID,
				"missions", status.Missions, "max_quest_map", maxQuestMap)
			continue
		}

		// 25/1 仅接受裸 7 字节业务字段；带 PrevAct 会被静默丢弃（2026-11-14 实测）。
		startPkt, bErr := protocol.BuildStartPracticeBarePacket(protocol.StartPracticeRequest{
			MissionID: best.MissionID,
			Count:     1,
			AutoSale:  1,
		})
		if bErr != nil {
			continue
		}
		pCtx, pCancel := context.WithTimeout(ctx, 10*time.Second)
		startResp, stErr := session.Call(pCtx, startPkt, protocol.ActionIDPracticeStart)
		pCancel()
		if stErr != nil || startResp == nil {
			startDropStreak++
			eliteSweepBlocked.Store(blockKey, time.Now().Add(6*time.Hour))
			slog.Info("精英副本扫荡开始无回包，暂停该副本 6 小时",
				"role_id", session.RoleID(), "dungeon_id", dungeonID,
				"mission_id", best.MissionID, "streak", startDropStreak, "err", stErr)
			if startDropStreak >= 5 {
				slog.Info("精英副本扫荡连续开始失败，进入冷却", "role_id", session.RoleID(), "swept", swept)
				return swept, scheduler.NewCooldown(15*time.Minute, "精英副本扫荡连续无回包")
			}
			continue
		}
		startDropStreak = 0
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
		// Module 25 的 auto_sale 只覆盖白绿蓝装；抓包中官方客户端没有额外出售请求，
		// 但本服 auto_sale 可能漏掉部分掉落。这里在全局扫荡前后做背包差量，
		// 把新增的可出售物品补卖掉，避免背包被扫荡物堆满。
		var globalPackBase *packItemSnapshot
		if cfg.Sweep.SellSweptItems {
			globalPackBase = capturePackSnapshot(ctx, session)
		}
		globalSwept, globalErr := sweepEliteMissionsWithoutTown(ctx, session, dictRepo)
		if globalPackBase != nil {
			if sold := sellNewPackItems(ctx, session, dictRepo, globalPackBase); sold > 0 {
				slog.Info("全局精英扫荡物出售完成", "role_id", session.RoleID(), "sold", sold)
			}
		}
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
	// 官方顺序是从高等级城镇向低等级；低等级/未解锁城镇的列表请求会被服务端
	// 静默丢弃并污染 PrevAct 链，因此这里按 MissionLock 降序（倒序）扫荡，
	// 且首个列表失败即终止本轮，避免后续请求全部超时。
	for i, j := 0, len(towns)-1; i < j; i, j = i+1, j-1 {
		towns[i], towns[j] = towns[j], towns[i]
	}
	// 官方抓包 (saodang.pcapng) 中 35/0 的 PrevAct 为 0/23 (get_buy_power_data)。
	// 实测活动面板 (0/42) 等查询后直接发 35/0 会被服务端静默丢弃；
	// 先补发一次 0/75 心跳，把 PrevAct 链锚点恢复为心跳动作再开始扫荡。
	if hb := protocol.BuildPlayerHeartbeatPacket(int32(session.LastActionID())); hb != nil {
		if err := session.Send(hb); err == nil {
			kit.Sleep(ctx, 80*time.Millisecond)
		}
	}
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
		// 城镇 5 (MissionLock=1101600) 与 55 (71031) 在当前账号上未解锁/无英雄副本，
		// 官方抓包同样从 54 开始扫荡。请求这两城会被静默丢弃并污染 PrevAct 链，
		// 因此跳过，保证首个请求 (54) 成功、后续城镇链保持有效。
		if townID == 5 || townID == 55 {
			continue
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
				kit.Sleep(ctx, 200*time.Millisecond)
			}
			if session.State() != client.StateActive {
				break
			}
		}

		const maxTownAttempts = 2
		for attempt := 0; attempt < maxTownAttempts; attempt++ {
			if attempt > 0 {
				kit.Sleep(ctx, 500*time.Millisecond)
			}

			list, listErr := fetchHeroMissionList(ctx, session, townID)
			if listErr != nil {
				// 服务端静默丢弃时 lastActionID 已被推进到未送达的动作，
				// 会让后续请求的 PrevAct 全部失配。回退到最近一次真实回包的动作，
				// 再尝试下一个城镇，避免一次失败拖垮整轮。
				if ack := session.LastAckActionID(); ack != 0 {
					session.SetLastActionID(ack)
				}
				slog.Info("查询英雄副本列表失败，跳过该城镇",
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
				// 服务端返回体力不足时不要清零本地体力：实测本地 115 点仍可能
				// 收到该结果码（英雄副本可能使用独立次数/条件）。清零会让后续
				// 任务推图误判为体力耗尽，白白浪费一整个 30 分钟周期。
				slog.Info("英雄副本扫荡返回体力不足，结束扫荡",
					"role_id", session.RoleID(), "town_id", townID,
					"local_stamina", session.GetStamina())
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
					if sold == 0 {
						// 只卖新增无法腾出空间时，出售背包里沉积的制作卷（扫荡掉落）。
						sold = sellAgedSweepStacks(ctx, session, dictRepo, 3)
					}
				}
				if sold > 0 && attempt < maxTownAttempts-1 && sweepHasStamina() {
					slog.Info("背包已满，已卖出扫荡物并重试当前城镇",
						"role_id", session.RoleID(),
						"town_id", townID,
						"sold", sold,
						"retry", attempt+1,
					)
					kit.Sleep(ctx, 100*time.Millisecond)
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
		// 高于所有 Cron 玩法 (含天帝宝库 200)：魔王试炼 Mod 57、宝库 594 面板等
		// 都会切换服务端会话上下文，导致随后的 enter_town/get_sections 被静默丢弃。
		// 实测 2026-11-15：宝库先跑后，任务 564 的 enter_town/map 21 章节列表连续超时。
		scheduler.PhasePriority(scheduler.PhaseCoreLoop, 0),
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			if ctx.Err() != nil {
				return nil
			}
			// 重连窗口内 State 为 Connecting/Reconnecting 而非 Closed；此时发包
			// 必然失败，整个扫荡批次会空跑并跳过扫荡物出售。返回 ErrNotConnected
			// 让调度器在会话恢复后 5 分钟重试，而不是等下一个 30 分钟周期。
			if session.State() != client.StateActive {
				slog.Info("会话未就绪，副本 Routine 稍后重试",
					"role_id", session.RoleID(), "state", session.State().String())
				return client.ErrNotConnected
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
				// 副本进入/战斗只消耗当前体力池；存储/额外体力 (ExtraStamina)
				// 不适用于 enter_mission (2026-11-14 实测 201 存储体力仍返回 LESS_POWER)。
				questEnsureStamina := func() bool {
					if session.GetBaseStamina() >= powerPerSweep {
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
					for session.GetBaseStamina() < powerPerSweep && replenishedStamina < maxStamina {
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
					return session.GetBaseStamina() >= powerPerSweep
				}

				// 推图前置检查体力自愈
				_ = questEnsureStamina()

				if dictRepo != nil {
					if session.State() != client.StateActive {
						for i := 0; i < 15 && session.State() != client.StateActive; i++ {
							kit.Sleep(ctx, 200*time.Millisecond)
						}
					}

					questBad := false
					if _, bad := questContextBad.Load(session); bad {
						if session.GetBaseStamina() < 5 {
							// 体力不足时重连没有收益，保留熔断标记等体力恢复。
							slog.Info("任务上下文不可用且体力不足，暂不重连", "role_id", session.RoleID())
							questBad = true
						} else if allowQuestReconnect(session) {
							// 服务端上下文被污染后同一连接内无法恢复：主动重连获取
							// 全新会话（登录钩子已停用，重连后会话保持干净），
							// 并**在当前调度内**等待会话恢复后继续推图，避免新一轮
							// 30 分钟内又被其他玩法污染。
							questContextBad.Delete(session)
							questSectionsAnchorCache.Delete(session)
							questTownKnownGood.Delete(session)
							// 新会话的 Mod 4 状态与旧会话无关，必须清除地图静默期，
							// 否则重连后首个 4/0 会被静默期直接跳过，4/1 缺少 Mod 4 锚点。
							questSectionsDown.Delete(session)
							slog.Info("任务上下文不可用，换取新凭据主动重连后继续本轮推图",
								"role_id", session.RoleID())
							if !reconnectQuestSession(ctx, session) {
								slog.Info("主动重连未在超时内恢复，跳过本轮任务推图", "role_id", session.RoleID())
								questBad = true
							}
						} else {
							questBad = true
						}
					}
					if questBad {
						slog.Info("本会话任务上下文已不可用，跳过任务推图", "role_id", session.RoleID())
					} else {
						clearedMissions := make(map[int]bool)
						executeQuestCycle(
							ctx, session, dictRepo, cfg, questEnsureStamina,
							&totalCostStamina, &totalGainExp, &totalGainCoins,
							clearedMissions,
						)
						// 每轮调度推进一关；跨轮继续由 enter_town 的 Mod 3/4 锚点复位
						// 恢复任务上下文（2026-11-15 实测）。若上下文再次不可用，
						// fetchMissionSections 连续失败会置位 questContextBad 熔断本会话，
						// 下一轮调度将主动重连后继续。
					}
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
				// 任务推图可能停留在剧情副本场景；扫荡前复位城镇，避免 25/1 被静默丢弃。
				if _, ok := enterQuestTownScene(ctx, session); ok {
					kit.Sleep(ctx, 200*time.Millisecond)
				}
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
							kit.Sleep(ctx, 200*time.Millisecond)
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
						// 大循环兜底：精英副本扫荡结束后在 s0 出售所有装备制作卷。
						sellEquipmentScrollsAfterEliteSweep(ctx, session, dictRepo)
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

// RunHeroMissionSweepOnLogin 登录后立即执行英雄副本多城镇扫荡 (Module 35)。
//
// 35/0 对上下文非常敏感：线上实测一旦先查询过活动面板 (0/42) 等模块，
// 服务端会把后续 35/0 静默丢弃。官方抓包中 35/0 紧跟心跳，且从城镇 54 开始。
// 因此该扫荡必须在调度器启动、其它例程发请求之前执行。
//
// 只使用免费扫荡 (auto_num=0)，不等待每城结算推送，避免阻塞登录流程。
func RunHeroMissionSweepOnLogin(ctx context.Context, session *client.RoleSession, dictRepo dictionary.Repository) {
	if session == nil || session.State() != client.StateActive {
		return
	}
	towns := eliteMissionTowns(dictRepo)
	for i, j := 0, len(towns)-1; i < j; i, j = i+1, j-1 {
		towns[i], towns[j] = towns[j], towns[i]
	}
	swept := 0
	for _, townID := range towns {
		if ctx.Err() != nil {
			break
		}
		// 5/55 在当前账号未解锁或无英雄副本，请求会被静默丢弃并污染链。
		if townID == 5 || townID == 55 {
			continue
		}
		list, err := fetchHeroMissionListWithPrevAct(ctx, session, townID)
		if err != nil {
			if ack := session.LastAckActionID(); ack != 0 {
				session.SetLastActionID(ack)
			}
			slog.Info("登录后英雄副本列表失败，跳过该城镇",
				"role_id", session.RoleID(), "town_id", townID, "err", err)
			continue
		}
		if list == nil || len(list.Missions) == 0 {
			continue
		}
		startRes, sErr := startHeroMissionPractice(ctx, session, townID, 0, 0)
		if sErr != nil {
			slog.Info("登录后英雄副本开始扫荡无回包",
				"role_id", session.RoleID(), "town_id", townID, "err", sErr)
			continue
		}
		switch startRes.Result {
		case protocol.HeroMissionResultSuccess:
			if _, qErr := quicklyHeroMissionPractice(ctx, session); qErr != nil {
				slog.Info("登录后英雄副本快速完成无回包",
					"role_id", session.RoleID(), "town_id", townID, "err", qErr)
			}
			swept++
			// 官方抓包中 35/7 之后紧接 35/4 结算推送，下一城镇的 35/0 在其后发出。
			// 不等待结算直接查下一城会被服务端静默丢弃，因此保留约 1.2s 的结算窗口。
			select {
			case <-ctx.Done():
				return
			case <-time.After(1200 * time.Millisecond):
			}
		case protocol.HeroMissionResultNoMission:
			// 该城镇暂无可扫荡副本。
		case protocol.HeroMissionResultBagFull:
			slog.Info("登录后英雄副本扫荡背包已满，终止", "role_id", session.RoleID())
			return
		case protocol.HeroMissionResultNotEnoughPower, protocol.HeroMissionResultNotEnoughIngot:
			slog.Info("登录后英雄副本扫荡资源不足，终止",
				"role_id", session.RoleID(), "result", startRes.Result)
			return
		default:
			slog.Info("登录后英雄副本扫荡未成功",
				"role_id", session.RoleID(), "town_id", townID, "result", startRes.Result)
		}
	}
	slog.Info("登录后英雄副本多城镇扫荡完成", "role_id", session.RoleID(), "swept", swept)
}

// fetchHeroMissionListWithPrevAct 查询英雄副本列表（登录钩子专用）。
//
// 2026-11-14 实机：裸 [Uint16 town] 在 s0/s1 均稳定回包；带 PrevAct 的旧形态
// 在多次运行中整轮静默丢弃。因此优先裸载荷，失败后再回退旧形态，
// 兼顾早期 s0 抓包（旧形态可建立服务端链）与当前服务端行为。
func fetchHeroMissionListWithPrevAct(ctx context.Context, session *client.RoleSession, townID int) (*protocol.HeroMissionList, error) {
	callList := func(pkt *protocol.Packet, timeout time.Duration) (*protocol.HeroMissionList, error) {
		c, cancel := context.WithTimeout(ctx, timeout)
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

	if list, err := callList(protocol.BuildHeroMissionListPacket(uint16(townID)), 8*time.Second); err == nil {
		return list, nil
	}
	return callList(protocol.BuildHeroMissionListWithPrevActPacket(uint16(townID), session.LastActionID()), 4*time.Second)
}
