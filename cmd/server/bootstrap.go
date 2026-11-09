// Package main 提供神仙道助手服务端主入口与运行时生命周期管理。
//
// 本文件负责把区服生效的调度配置装配为独立的玩法注册工厂与开关矩阵。
//
// @author Ateng
// @since 2026-11-06
package main

import (
	"strings"

	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/routines"
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/routines/social"
)

// buildRoutineEnabledMap 将预设与显式开关合并为玩法启用矩阵。
// dungeon 与 dungeon_sweep 互为兼容别名。
func buildRoutineEnabledMap(sc config.SchedulerConfig) map[string]bool {
	enabled := make(map[string]bool)
	if strings.EqualFold(sc.Preset, "all") {
		for _, d := range routines.GetAllDefinitions() {
			enabled[d.ID] = true
		}
	}
	for k, v := range sc.Routines {
		enabled[k] = v
		switch k {
		case "dungeon":
			enabled["dungeon_sweep"] = v
		case "dungeon_sweep":
			enabled["dungeon"] = v
		}
	}
	return enabled
}

// newRegistryFactory 根据区服生效配置构建独立玩法工厂。
// 同一区服的多个角色可以共享同一个工厂 (工厂在装配完成后只读)。
func newRegistryFactory(
	sc config.SchedulerConfig,
	qaEngine qa.Engine,
	dictRepo dictionary.Repository,
) *routines.RegistryFactory {
	factory := routines.NewRegistryFactory(qaEngine, dictRepo)

	// 1. 副本推图与扫荡配置
	dCfg := dungeon.DefaultSweepConfig()
	hasSubDungeon := sc.Dungeon.QuestCycle.Enabled ||
		sc.Dungeon.QuestCycle.AutoReplenishStamina ||
		sc.Dungeon.QuestCycle.MaxReplenishStamina > 0 ||
		sc.Dungeon.QuestCycle.MaxReplenishTimes > 0 ||
		sc.Dungeon.Sweep.Enabled ||
		sc.Dungeon.Sweep.SellSweptItems ||
		sc.Dungeon.Sweep.AutoSellSweptItems ||
		sc.Dungeon.SellSweptItems ||
		sc.Dungeon.AutoSellSweptItems ||
		len(sc.Dungeon.Sweep.Pipeline) > 0
	if hasSubDungeon {
		dCfg.QuestCycle.Enabled = sc.Dungeon.QuestCycle.Enabled
		dCfg.QuestCycle.AutoReplenishStamina = sc.Dungeon.QuestCycle.AutoReplenishStamina
		dCfg.QuestCycle.MaxReplenishStamina = sc.Dungeon.QuestCycle.MaxReplenishStamina
		dCfg.QuestCycle.MaxReplenishTimes = sc.Dungeon.QuestCycle.MaxReplenishTimes
	} else {
		dCfg.QuestCycle.AutoReplenishStamina = sc.Dungeon.AutoReplenishStamina
		dCfg.QuestCycle.MaxReplenishStamina = sc.Dungeon.MaxReplenishStamina
		dCfg.QuestCycle.MaxReplenishTimes = sc.Dungeon.MaxReplenishTimes
	}
	if hasSubDungeon {
		dCfg.Sweep.Enabled = sc.Dungeon.Sweep.Enabled
		dCfg.Sweep.Pipeline = sc.Dungeon.Sweep.Pipeline
		dCfg.Sweep.PreferPracticeQuickly = sc.Dungeon.Sweep.PreferPracticeQuickly
		dCfg.Sweep.SellSweptItems = sc.Dungeon.Sweep.SellSweptItems || sc.Dungeon.Sweep.AutoSellSweptItems || sc.Dungeon.SellSweptItems || sc.Dungeon.AutoSellSweptItems
		dCfg.Sweep.GlobalElite = sc.Dungeon.Sweep.GlobalElite
	} else {
		if len(sc.Dungeon.Pipeline) > 0 {
			dCfg.Sweep.Pipeline = sc.Dungeon.Pipeline
		}
		dCfg.Sweep.PreferPracticeQuickly = sc.Dungeon.PreferPracticeQuickly
		dCfg.Sweep.SellSweptItems = sc.Dungeon.SellSweptItems || sc.Dungeon.AutoSellSweptItems
	}
	dCfg.Pipeline = sc.Dungeon.Pipeline
	dCfg.PreferPracticeQuickly = sc.Dungeon.PreferPracticeQuickly
	dCfg.AutoReplenishStamina = sc.Dungeon.AutoReplenishStamina
	dCfg.MaxReplenishStamina = sc.Dungeon.MaxReplenishStamina
	dCfg.MaxReplenishTimes = sc.Dungeon.MaxReplenishTimes
	dCfg.SellSweptItems = dCfg.Sweep.SellSweptItems
	factory.SetDungeonConfig(dCfg)

	// 2. 结缘宝箱配置 (每批最多 10 个)
	jBoxCfg := social.DefaultJieYuanBoxConfig()
	if sc.JieYuanBox.MaxBatchSize > 0 {
		jBoxCfg.MaxBatchSize = sc.JieYuanBox.MaxBatchSize
	}
	factory.SetJieYuanBoxConfig(jBoxCfg)

	// 3. 帮派祭神上香配置 (默认免费白檀香，不主动覆盖更高祝福)
	fbCfg := social.DefaultFactionBlessingConfig()
	if sc.FactionBlessing.OfferingID > 0 {
		fbCfg.OfferingID = sc.FactionBlessing.OfferingID
	}
	fbCfg.AllowCover = sc.FactionBlessing.AllowCover
	factory.SetFactionBlessingConfig(fbCfg)

	// 4. 天帝宝库自选三档大奖配置
	thCfg := minigame.TreasureHouseConfig{
		DictRepo:      dictRepo,
		SelectionMode: sc.TreasureHouse.SelectionMode,
		PreferAwards:  sc.TreasureHouse.PreferAwards,
		Interactive:   sc.TreasureHouse.Interactive,
		Awards:        sc.TreasureHouse.ResolveSelections(),
		MaxDrawTimes:  sc.TreasureHouse.MaxDrawTimes,
	}
	factory.SetTreasureHouseConfig(thCfg)

	// 5. 个人吉星高照策略配置 (仅使用免费逆天改运次数，永不消耗元宝)
	lsCfg := minigame.DefaultLuckyStarConfig()
	if sc.LuckyStar.MaxRerollsPerRoll > 0 {
		lsCfg.MaxRerollsPerRoll = sc.LuckyStar.MaxRerollsPerRoll
	}
	factory.SetLuckyStarConfig(lsCfg)

	// 5.1 宗师修行方向顺序与阵容整理策略配置
	mpCfg := minigame.DefaultMasterPracticeConfig()
	mpCfg.DictRepo = dictRepo
	mpCfg.PracticeOrder = sc.MasterPractice.PracticeOrder
	if sc.MasterPractice.MaxStarsPerJob > 0 {
		mpCfg.MaxStarsPerJob = sc.MasterPractice.MaxStarsPerJob
	}
	if sc.MasterPractice.MaxChallengesPerDay > 0 {
		mpCfg.MaxChallengesPerDay = sc.MasterPractice.MaxChallengesPerDay
	}
	if sc.MasterPractice.AutoNormalizeDeploy != nil {
		mpCfg.AutoNormalizeDeploy = *sc.MasterPractice.AutoNormalizeDeploy
	}
	if sc.MasterPractice.AllowSwitchStunt != nil {
		mpCfg.AllowSwitchStunt = *sc.MasterPractice.AllowSwitchStunt
	}
	if sc.MasterPractice.AllowStepUp != nil {
		mpCfg.AllowStepUp = *sc.MasterPractice.AllowStepUp
	}
	if sc.MasterPractice.MaxRetriesPerStar > 0 {
		mpCfg.MaxRetriesPerStar = sc.MasterPractice.MaxRetriesPerStar
	}
	if sc.MasterPractice.SwitchJobOnFailure != nil {
		mpCfg.SwitchJobOnFailure = *sc.MasterPractice.SwitchJobOnFailure
	}
	factory.SetMasterPracticeConfig(mpCfg)

	// 6. 在线礼包与功能终结礼包策略配置
	ogCfg := daily.DefaultOnlineGiftConfig()
	if sc.OnlineGift.ClaimFunctionEndGifts != nil {
		ogCfg.ClaimFunctionEndGifts = *sc.OnlineGift.ClaimFunctionEndGifts
	}
	if sc.OnlineGift.MaxClaimsPerGift > 0 {
		ogCfg.MaxClaimsPerGift = sc.OnlineGift.MaxClaimsPerGift
	}
	factory.SetOnlineGiftConfig(ogCfg)

	// 7. 魔王试炼自动化策略配置
	btCfg := dungeon.DefaultBeelzebubTrialsConfig()
	if sc.BeelzebubTrials.PreferBossID > 0 {
		btCfg.PreferBossID = sc.BeelzebubTrials.PreferBossID
	}
	if sc.BeelzebubTrials.MinLevel > 0 {
		btCfg.MinLevel = sc.BeelzebubTrials.MinLevel
	}
	btCfg.AutoMoralAward = sc.BeelzebubTrials.AutoMoralAward
	factory.SetBeelzebubTrialsConfig(btCfg)

	// 8. 仙界练功房挂机收益维护策略配置 (默认只占空闲席位，不主动 PVP 争夺)
	trCfg := dungeon.DefaultTrainingRoomConfig()
	if sc.TrainingRoom.AllowRobSeat || sc.TrainingRoom.MaxRobAttempts > 0 {
		trCfg.AllowRobSeat = sc.TrainingRoom.AllowRobSeat
		if sc.TrainingRoom.MaxRobAttempts > 0 {
			trCfg.MaxRobAttempts = sc.TrainingRoom.MaxRobAttempts
		}
	}
	factory.SetTrainingRoomConfig(trCfg)

	// 9. 六道轮回挑战与灵件策略配置
	srCfg := dungeon.DefaultSixRealmsConfig()
	if sc.SixRealms.AutoReset || sc.SixRealms.AutoSellBlue || sc.SixRealms.AutoSellPurple || sc.SixRealms.MaxFloorRetries > 0 {
		srCfg.AutoReset = sc.SixRealms.AutoReset
		srCfg.AutoSellBlue = sc.SixRealms.AutoSellBlue
		srCfg.AutoSellPurple = sc.SixRealms.AutoSellPurple
		if sc.SixRealms.MaxFloorRetries > 0 {
			srCfg.MaxFloorRetries = sc.SixRealms.MaxFloorRetries
		}
	}
	factory.SetSixRealmsConfig(srCfg)

	// 10. 西天取经护送与打劫策略配置 (默认自动打劫，仅使用免费次数)
	pCfg := farming.DefaultPilgrimageConfig()
	if sc.Pilgrimage.AutoRob != nil {
		pCfg.AutoRob = *sc.Pilgrimage.AutoRob
	}
	if sc.Pilgrimage.MaxRobAttempts > 0 {
		pCfg.MaxRobAttempts = sc.Pilgrimage.MaxRobAttempts
	}
	if sc.Pilgrimage.MaxProbeTargets > 0 {
		pCfg.MaxProbeTargets = sc.Pilgrimage.MaxProbeTargets
	}
	// 未配置 force_saint_area 时保持 SaintAreaAuto (按圣域功能开放自动判定)。
	if sc.Pilgrimage.ForceSaintArea != nil {
		if *sc.Pilgrimage.ForceSaintArea {
			pCfg.SaintAreaFallback = farming.SaintAreaForceOnly
		} else {
			pCfg.SaintAreaFallback = farming.SaintAreaAllowFallback
		}
	}
	factory.SetPilgrimageConfig(pCfg)

	// 11. 粗铁淬炼池策略配置 (默认淬炼玄铁; 可切换提炼灵材, 两者均只消耗铜钱)
	spCfg := farming.DefaultSpiritPoolConfig()
	if target, ok := farming.ParseSpiritPoolRefineTarget(sc.SpiritPool.RefineTarget); ok {
		spCfg.Target = target
	}
	factory.SetSpiritPoolConfig(spCfg)

	return factory
}

// resolveServerAddr 解析区服网关地址；sandbox/空值走已知静态映射。
func resolveServerAddr(role config.RoleConfig) string {
	serverAddr := role.ServerAddr
	if serverAddr != "" && serverAddr != "sandbox" {
		return serverAddr
	}
	if strings.Contains(role.ServerID, "813") {
		return "49.232.196.100:8381"
	}
	if strings.EqualFold(role.ServerID, "s0") || strings.EqualFold(role.ServerID, "0") {
		return "9x050.sxdweb.xd.com:8200"
	}
	return serverAddr
}
