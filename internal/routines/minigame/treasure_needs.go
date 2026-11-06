package minigame

import (
	"context"
	"log/slog"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// 天帝宝库动态选奖涉及的道具编号。
const (
	itemHongMengZiQi       int32 = 2397 // 鸿蒙紫气 (法相/通天塔)
	itemDaFaLiShi          int32 = 6909 // 大法力石 (法阵)
	itemYongHengLingBao    int32 = 2010 // 永恒灵宝 (混沌虚空)
	itemChaoJiHuoLongZhu   int32 = 2407 // 超级火龙珠 (龙珠)
	itemXueHunShi          int32 = 4954 // 血魂石 (血脉)
	itemJingJieDian        int32 = 1740 // 境界点
	itemDaoYuan            int32 = 2403 // 道缘
	itemLingZhu            int32 = 3783 // 灵珠 (失却之阵)
	itemHuangYuPai         int32 = 347  // 黄玉牌
	itemHuoBanSuiPian      int32 = 6651 // 伙伴碎片自选包
	itemChengZhuangSuiPian int32 = 2343 // 橙装碎片
	itemShouHunSuiPian     int32 = 5117 // 兽魂碎片
	itemHuangJinShengShui  int32 = 4975 // 黄金圣水 (兵法)
	itemYiJieBaoShi        int32 = 9212 // 1阶宝石随机包 (幻兽)
	itemPuTongJinJieDan    int32 = 5397 // 普通进阶丹 (丹魂)
)

// NeedSnapshot 账号养成进度快照，用于天帝宝库动态选奖。
type NeedSnapshot struct {
	Items      map[int32]int32
	MagicArray *protocol.MagicArrayPanel
	DragonBall *protocol.DragonBallPanel
	Bloodline  *protocol.BloodlinePanel
	Library    *protocol.LibraryPanel
	ElixirSoul *protocol.ElixirSoulPanel
	SantWeapon *protocol.SantWeaponPanel
	Tactics    *protocol.TacticsPanel
	Chaos      *protocol.ChaosPanel
	Eudemons   *protocol.EudemonsPanel
}

// AwardNeed 单个候选奖励的需求评估结果。
type AwardNeed struct {
	Score  float64
	Reason string
}

// CollectNeedSnapshot 采集账号当前养成进度。所有子查询失败均容忍：对应字段留空，
// 评估规则会使用保守的默认缺口，避免因单个面板不可用而放弃整轮自动选奖。
func CollectNeedSnapshot(ctx context.Context, session *client.RoleSession, offered *protocol.TreasureHousePool) *NeedSnapshot {
	snap := &NeedSnapshot{Items: make(map[int32]int32)}

	// 1. 采集当前候选涉及的道具数量，以及动态规则依赖的关键道具。
	itemIDs := make(map[int32]bool)
	for _, id := range append(append(append([]int32{}, offered.Tier1Items...), offered.Tier2Items...), offered.Tier3Items...) {
		if award, ok := protocol.TreasureAwardByID(id); ok {
			itemIDs[award.ItemID] = true
		}
	}
	for _, id := range []int32{
		itemHongMengZiQi, itemDaFaLiShi, itemYongHengLingBao, itemChaoJiHuoLongZhu,
		itemXueHunShi, itemJingJieDian, itemDaoYuan, itemLingZhu, itemHuangYuPai,
		itemHuoBanSuiPian, itemChengZhuangSuiPian, itemShouHunSuiPian,
		itemHuangJinShengShui, itemYiJieBaoShi, itemPuTongJinJieDan,
	} {
		itemIDs[id] = true
	}
	for id := range itemIDs {
		count, err := session.GetItemNum(ctx, id)
		if err != nil {
			slog.Debug("天帝宝库动态选奖查询道具数量失败", "item_id", id, "err", err)
			continue
		}
		snap.Items[id] = count
	}

	// 2. 采集养成面板 (容错)。
	collect := func(name string, fn func() error) {
		if err := fn(); err != nil {
			slog.Debug("天帝宝库动态选奖查询面板失败", "panel", name, "err", err)
		}
	}
	collect("magic_array", func() (err error) {
		snap.MagicArray, err = session.GetMagicArrayPanel(ctx)
		return err
	})
	collect("dragon_ball", func() (err error) {
		snap.DragonBall, err = session.GetDragonBallPanel(ctx)
		return err
	})
	collect("bloodline", func() (err error) {
		snap.Bloodline, err = session.GetBloodlinePanel(ctx)
		return err
	})
	collect("library", func() (err error) {
		snap.Library, err = session.GetLibraryPanel(ctx)
		return err
	})
	collect("sant_weapon", func() (err error) {
		snap.SantWeapon, err = session.GetSantWeaponPanel(ctx)
		return err
	})
	collect("tactics", func() (err error) {
		snap.Tactics, err = session.GetTacticsPanel(ctx)
		return err
	})
	collect("chaos", func() (err error) {
		snap.Chaos, err = session.GetChaosPack(ctx)
		return err
	})
	collect("eudemons", func() (err error) {
		snap.Eudemons, err = session.GetEudemonsPanel(ctx)
		return err
	})
	// 丹魂需要角色 player_role_id；以圣器面板首个角色作为主角色。
	if snap.SantWeapon != nil && len(snap.SantWeapon.Roles) > 0 {
		mainRoleID := snap.SantWeapon.Roles[0].PlayerRoleID
		collect("elixir_soul", func() (err error) {
			snap.ElixirSoul, err = session.GetElixirSoulPanel(ctx, mainRoleID)
			return err
		})
	}
	return snap
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (s *NeedSnapshot) itemCount(itemID int32) (int32, bool) {
	if s == nil || s.Items == nil {
		return 0, false
	}
	v, ok := s.Items[itemID]
	return v, ok
}

// hasProgressData 判断快照是否成功采集到任意养成数据。
func (s *NeedSnapshot) hasProgressData() bool {
	if s == nil {
		return false
	}
	if len(s.Items) > 0 {
		return true
	}
	return s.MagicArray != nil || s.DragonBall != nil || s.Bloodline != nil ||
		s.Library != nil || s.ElixirSoul != nil || s.SantWeapon != nil ||
		s.Tactics != nil || s.Chaos != nil || s.Eudemons != nil
}

// EvaluateAwardNeed 依据账号快照评估单个候选奖励的需求分。
// 分数 = 系统权重 × 缺口比例；分数越高越应优先选择。
func EvaluateAwardNeed(awardID int32, snap *NeedSnapshot) AwardNeed {
	switch awardID {
	case 26: // 鸿蒙紫气 x15：法相/通天塔，全法相需求约 610
		stock, ok := snap.itemCount(itemHongMengZiQi)
		if !ok {
			return AwardNeed{Score: 90, Reason: "鸿蒙紫气数量未知，按高优先级"}
		}
		gap := clamp01((610 - float64(stock)) / 610)
		return AwardNeed{Score: 100 * gap, Reason: "鸿蒙紫气缺口"}
	case 23: // 大法力石 x2：法阵每颗 +500 经验
		if snap == nil || snap.MagicArray == nil {
			return AwardNeed{Score: 45, Reason: "法阵面板不可用，按中高优先级"}
		}
		remaining := float64(snap.MagicArray.MaxExp - snap.MagicArray.Exp)
		if remaining <= 0 {
			return AwardNeed{Score: 0, Reason: "法阵已满经验"}
		}
		needStones := remaining / 500
		stock := float64(0)
		if v, ok := snap.itemCount(itemDaFaLiShi); ok {
			stock = float64(v)
		}
		gap := clamp01((needStones - stock) / needStones)
		return AwardNeed{Score: 90 * gap, Reason: "法阵升级缺口"}
	case 9: // 永恒灵宝 x1
		stock, ok := snap.itemCount(itemYongHengLingBao)
		if !ok {
			return AwardNeed{Score: 40, Reason: "永恒灵宝数量未知"}
		}
		gap := clamp01((5 - float64(stock)) / 5)
		return AwardNeed{Score: 80 * gap, Reason: "永恒灵宝储备"}
	case 18: // 超级火龙珠 x3：每颗 +1000 经验
		if snap == nil || snap.DragonBall == nil || len(snap.DragonBall.Balls) == 0 {
			return AwardNeed{Score: 35, Reason: "龙珠面板不可用，按中优先级"}
		}
		deficit := int64(0)
		for _, b := range snap.DragonBall.Balls {
			if b.Level >= 10 {
				continue
			}
			if b.Exp < 245760 {
				deficit += 245760 - int64(b.Exp)
			}
		}
		if deficit <= 0 {
			return AwardNeed{Score: 0, Reason: "龙珠已满级"}
		}
		return AwardNeed{Score: 70, Reason: "龙珠经验缺口"}
	case 51: // 1阶宝石随机包 x3
		if snap == nil || snap.Eudemons == nil {
			return AwardNeed{Score: 32, Reason: "幻兽面板不可用，按中优先级"}
		}
		if snap.Eudemons.StoneMaxLevel == 0 && snap.Eudemons.TotalStoneStep == 0 {
			return AwardNeed{Score: 65, Reason: "幻兽宝石阶数为 0"}
		}
		gap := clamp01((10 - float64(snap.Eudemons.StoneMaxLevel)) / 10)
		return AwardNeed{Score: 65 * gap, Reason: "幻兽宝石阶数缺口"}
	case 34: // 普通进阶丹 x4：丹魂
		if snap == nil || snap.ElixirSoul == nil || len(snap.ElixirSoul.Slots) == 0 {
			return AwardNeed{Score: 30, Reason: "丹魂面板不可用，按中优先级"}
		}
		below := 0
		for _, slot := range snap.ElixirSoul.Slots {
			if slot.Level < 10 {
				below++
			}
		}
		gap := clamp01(float64(below) / float64(len(snap.ElixirSoul.Slots)))
		return AwardNeed{Score: 60 * gap, Reason: "丹魂魂位升级缺口"}
	case 12: // 血魂石 x1：血脉
		if snap == nil || snap.Bloodline == nil || len(snap.Bloodline.Bloodlines) == 0 {
			return AwardNeed{Score: 25, Reason: "血脉面板不可用"}
		}
		missingLevels := int64(0)
		for _, b := range snap.Bloodline.Bloodlines {
			if b.Level < 80 {
				missingLevels += int64(80 - b.Level)
			}
		}
		needStones := float64(missingLevels) / 10 // 保守按 10 级/颗估算
		stock := float64(0)
		if v, ok := snap.itemCount(itemXueHunShi); ok {
			stock = float64(v)
		}
		if needStones <= 0 {
			return AwardNeed{Score: 0, Reason: "血脉已满级"}
		}
		gap := clamp01((needStones - stock) / needStones)
		return AwardNeed{Score: 50 * gap, Reason: "血脉升级缺口"}
	case 4: // 伙伴碎片自选包
		stock, ok := snap.itemCount(itemHuoBanSuiPian)
		if !ok {
			return AwardNeed{Score: 20, Reason: "伙伴碎片数量未知"}
		}
		gap := clamp01((5 - float64(stock)) / 5)
		return AwardNeed{Score: 40 * gap, Reason: "伙伴碎片储备"}
	case 21: // 黄玉牌
		stock, ok := snap.itemCount(itemHuangYuPai)
		if !ok {
			return AwardNeed{Score: 15, Reason: "黄玉牌数量未知"}
		}
		gap := clamp01((2000 - float64(stock)) / 2000)
		return AwardNeed{Score: 30 * gap, Reason: "黄玉牌储备"}
	case 43: // 黄金圣水：兵法
		if snap == nil || snap.Tactics == nil || snap.Tactics.NextLevelExp <= 0 {
			return AwardNeed{Score: 10, Reason: "兵法面板不可用"}
		}
		remaining := float64(snap.Tactics.NextLevelExp - snap.Tactics.Exp)
		if remaining <= 0 {
			return AwardNeed{Score: 0, Reason: "兵法已满级"}
		}
		draws := remaining / 600 // 按最低 600 经验/次估算
		stock := float64(0)
		if v, ok := snap.itemCount(itemHuangJinShengShui); ok {
			stock = float64(v)
		}
		gap := clamp01((draws - stock) / draws)
		return AwardNeed{Score: 20 * gap, Reason: "兵法经验缺口"}
	case 42: // 境界点
		stock, ok := snap.itemCount(itemJingJieDian)
		if !ok {
			return AwardNeed{Score: 5, Reason: "境界点数量未知"}
		}
		gap := clamp01((50000 - float64(stock)) / 50000)
		return AwardNeed{Score: 10 * gap, Reason: "境界点储备"}
	case 40: // 道缘
		stock, ok := snap.itemCount(itemDaoYuan)
		if !ok {
			return AwardNeed{Score: 5, Reason: "道缘数量未知"}
		}
		gap := clamp01((10000 - float64(stock)) / 10000)
		return AwardNeed{Score: 10 * gap, Reason: "道缘储备"}
	case 32: // 灵珠
		stock, ok := snap.itemCount(itemLingZhu)
		if !ok {
			return AwardNeed{Score: 5, Reason: "灵珠数量未知"}
		}
		gap := clamp01((2000 - float64(stock)) / 2000)
		return AwardNeed{Score: 10 * gap, Reason: "灵珠储备"}
	case 36, 35: // 星魂 / 仙令：藏经阁
		if snap == nil || snap.Library == nil || len(snap.Library.Jobs) == 0 {
			return AwardNeed{Score: 0, Reason: "藏经阁面板不可用"}
		}
		maxLevel := int16(0)
		for _, j := range snap.Library.Jobs {
			if j.Level > maxLevel {
				maxLevel = j.Level
			}
		}
		gap := clamp01((270 - float64(maxLevel)) / 270)
		return AwardNeed{Score: 15 * gap, Reason: "藏经阁等级缺口"}
	case 15: // 兽魂碎片
		stock, ok := snap.itemCount(itemShouHunSuiPian)
		if !ok {
			return AwardNeed{Score: 15, Reason: "兽魂碎片数量未知"}
		}
		remainder := stock % 100
		gap := clamp01((100 - float64(remainder)) / 100)
		return AwardNeed{Score: 35 * gap, Reason: "兽魂碎片合成缺口"}
	case 3: // 一品神器包
		return AwardNeed{Score: 30, Reason: "神器自选包"}
	case 27: // 炼器宝箱
		return AwardNeed{Score: 25, Reason: "神器炼器材料"}
	case 20: // 橙装碎片
		stock, ok := snap.itemCount(itemChengZhuangSuiPian)
		if !ok {
			return AwardNeed{Score: 10, Reason: "橙装碎片数量未知"}
		}
		gap := clamp01((100 - float64(stock)) / 100)
		return AwardNeed{Score: 20 * gap, Reason: "橙装碎片储备"}
	case 1: // 元气丹小礼包
		return AwardNeed{Score: 15, Reason: "聚元材料"}
	default:
		return AwardNeed{Score: 0, Reason: "无特定养成规则"}
	}
}

// EvaluateTreasureNeeds 为当前候选奖励生成动态需求分。
func EvaluateTreasureNeeds(snap *NeedSnapshot, offered *protocol.TreasureHousePool) map[int32]AwardNeed {
	needs := make(map[int32]AwardNeed)
	for _, id := range append(append(append([]int32{}, offered.Tier1Items...), offered.Tier2Items...), offered.Tier3Items...) {
		needs[id] = EvaluateAwardNeed(id, snap)
	}
	return needs
}
