// Package routines 提供对齐原版辅助 30+ 自动化玩法的六大业务子域实现矩阵与统一注册工厂。
//
// @author Ateng
// @since 2026-10-08
package routines

import (
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/routines/pvp"
	"sxd-pie-ng/internal/routines/social"
	"sxd-pie-ng/internal/scheduler"
)

// RoutineDefinition 暴露给控制台和配置中心的玩法元数据。
type RoutineDefinition struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Domain      string                 `json:"domain"`
	Schedule    scheduler.ScheduleType `json:"schedule"`
	Description string                 `json:"description"`
	DefaultOn   bool                   `json:"default_on"`
}

// GetAllDefinitions 返回系统支持的全部 30+ 自动化玩法的元数据清单。
func GetAllDefinitions() []RoutineDefinition {
	return []RoutineDefinition{
		// 1. Daily 领域 (5)
		{ID: "daily_sign_in", Name: "每日签到", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动完成每日签到与各种更新福利礼包领取", DefaultOn: false},
		{ID: "daily_quest", Name: "每日日常小助手", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动完成小助手日常活跃度与常规跑环", DefaultOn: false},
		{ID: "mail_collect", Name: "邮件一键收取", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动一键领取系统邮件与好友信件附件", DefaultOn: false},
		{ID: "online_gift", Name: "在线成长礼包", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动领取各时段在线时长福利与成长礼包", DefaultOn: false},
		{ID: "vip_welfare", Name: "VIP特权与俸禄", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动领取VIP每日礼包与官职俸禄", DefaultOn: false},

		// 2. Farming 领域 (6)
		{ID: "herb_garden", Name: "药园种植巡检", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "全自动药园播种、施肥、除草与成熟采摘", DefaultOn: true},
		{ID: "pilgrimage", Name: "西天取经护送", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "西天取经召唤唐僧/白龙马并协助好友护送拦截", DefaultOn: false},
		{ID: "crystal_mine", Name: "仙界矿山开采", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "全自动仙界矿石开采与防抢夺守护", DefaultOn: false},
		{ID: "spirit_pool", Name: "异兽与淬炼池", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "异兽自动喂养与灵宝淬炼池提炼", DefaultOn: false},
		{ID: "gourd_realm", Name: "壶中界炼丹合成", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "壶中界灵气转化、丹药炼制与材料融合", DefaultOn: false},
		{ID: "rune_refine", Name: "符文炼化八卦炉", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "符文自动淬炼与八卦炉灵气提纯", DefaultOn: false},

		// 3. Social 领域 (5)
		{ID: "guild_activities", Name: "仙盟日常活动", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "仙盟铜钱捐献、神兽召唤喂养与魔神挑战", DefaultOn: false},
		{ID: "sacred_alliance", Name: "圣盟日常活动", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "圣盟祭祀灵兽、幻魔塔挑战与商店兑换", DefaultOn: false},
		{ID: "homestead", Name: "住宅与夫妻宝箱", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "住宅家具祝福领取与夫妻宝箱日常互动", DefaultOn: false},
		{ID: "friendship", Name: "好友结义与送花", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "好友自动送花互动与结义羁绊维护", DefaultOn: false},
		{ID: "fame_blessing", Name: "仙界尊像膜拜", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "仙界膜拜前三名霸主大神与声望祝福获取", DefaultOn: false},

		// 4. Dungeon 领域 (6)
		{ID: "dungeon_sweep", Name: "关卡体力扫荡", Domain: "dungeon", Schedule: scheduler.ScheduleLoop, Description: "主线/精英关卡体力自动消耗与装备材料扫荡", DefaultOn: true},
		{ID: "six_realms", Name: "六道轮回挑战", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "六道轮回塔爬塔与灵件自动熔炼分解", DefaultOn: false},
		{ID: "demon_tower", Name: "伏魔塔与试炼", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "伏魔塔日常挑战与怪物试炼通关", DefaultOn: false},
		{ID: "training_room", Name: "仙界练功房", Domain: "dungeon", Schedule: scheduler.ScheduleLoop, Description: "仙界练功房普通/高级场挂机收益维护", DefaultOn: false},
		{ID: "zodiac_challenge", Name: "生肖挑战与金油", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "十二生肖关卡挑战与金油炼化突破", DefaultOn: false},
		{ID: "spirit_trial", Name: "精灵试炼扫荡", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "精灵副本星级通关挑战与材料扫荡", DefaultOn: false},

		// 5. PVP 领域 (6)
		{ID: "arena", Name: "本服竞技场", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "本服竞技场自动寻找适宜对手挑战与排名领奖", DefaultOn: false},
		{ID: "celestial_arena", Name: "仙界竞技场", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "仙界竞技场自动连胜挑战与赛事竞猜下注", DefaultOn: false},
		{ID: "gods_and_demons", Name: "神魔大战阵营", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "神魔大战阵营对抗定时参战", DefaultOn: false},
		{ID: "caravan_hijack", Name: "阵营劫镖拦截", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "阵营商队巡查截击与声望抢夺", DefaultOn: false},
		{ID: "world_boss", Name: "世界Boss争夺", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "擎天木/赤炎兽Boss定时准点参战与伤害鼓舞", DefaultOn: false},
		{ID: "cross_server_ladder", Name: "跨服天梯排位", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "跨服天梯自动匹配竞技与段位宝箱领用", DefaultOn: false},

		// 6. Minigame 领域 (7)
		{ID: "lucky_star", Name: "帮派吉星高照", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "帮派吉星高照扔骰子与高分奖励获取", DefaultOn: false},
		{ID: "immortal_fantasy", Name: "仙履奇缘智能答题", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "连接万条题库自动毫秒级精准答题", DefaultOn: false},
		{ID: "imperial_exam", Name: "金榜题名会试", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "金榜题名乡试会试殿试自动化智能答题", DefaultOn: false},
		{ID: "partner_guess", Name: "伙伴猜猜看", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "伙伴特征自动比对竞猜", DefaultOn: false},
		{ID: "fishing", Name: "自动钓鱼命格", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "自动水域钓鱼与猎命命格吞噬融合", DefaultOn: false},
		{ID: "scratch_card", Name: "幸运刮刮卡", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "日常免费刮刮卡福利抽取", DefaultOn: false},
		{ID: "ice_cave", Name: "一键冰窟探险", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "一键五锤最优路径通关与金币领取", DefaultOn: false},
	}
}

// RegistryFactory 构建并注册所有玩法的统一工厂。
type RegistryFactory struct {
	qaEngine   qa.Engine
	dictRepo   dictionary.Repository
	dungeonCfg dungeon.SweepConfig
}

// NewRegistryFactory 创建注册工厂实例。
func NewRegistryFactory(qaEngine qa.Engine, dictRepo dictionary.Repository) *RegistryFactory {
	cfg := dungeon.DefaultSweepConfig()
	cfg.PreferPracticeQuickly = true
	return &RegistryFactory{
		qaEngine:   qaEngine,
		dictRepo:   dictRepo,
		dungeonCfg: cfg,
	}
}

// SetDungeonConfig 设置副本自动化策略配置。
func (f *RegistryFactory) SetDungeonConfig(cfg dungeon.SweepConfig) {
	f.dungeonCfg = cfg
}

// RegisterAll 根据配置开关，向角色调度器装配全量活动玩法。
func (f *RegistryFactory) RegisterAll(sched *scheduler.RoleScheduler, enabledMap map[string]bool) int {
	defs := GetAllDefinitions()
	count := 0

	for _, d := range defs {
		// 若显式配置了则遵从配置；若未配置则按 DefaultOn 预设
		enabled, ok := enabledMap[d.ID]
		if !ok {
			enabled = d.DefaultOn
		}

		if !enabled {
			continue
		}

		rt := f.createRoutine(d.ID)
		if rt != nil {
			if err := sched.Register(rt); err == nil {
				count++
			}
		}
	}
	return count
}

func (f *RegistryFactory) createRoutine(id string) scheduler.ActivityRoutine {
	// 动态路由实例化具体领域 Routine
	switch id {
	// 1. Daily
	case "daily_sign_in":
		return daily.NewSignInRoutine()
	case "daily_quest":
		return daily.NewDailyQuestRoutine()
	case "mail_collect":
		return daily.NewMailCollectRoutine()
	case "online_gift":
		return daily.NewOnlineGiftRoutine()
	case "vip_welfare":
		return daily.NewVIPWelfareRoutine()

	// 2. Farming
	case "herb_garden":
		return farming.NewHerbGardenRoutine()
	case "pilgrimage":
		return farming.NewPilgrimageRoutine()
	case "crystal_mine":
		return farming.NewCrystalMineRoutine()
	case "spirit_pool":
		return farming.NewSpiritPoolRoutine()
	case "gourd_realm":
		return farming.NewGourdRealmRoutine(f.dictRepo)
	case "rune_refine":
		return farming.NewRuneRefineRoutine()

	// 3. Social
	case "guild_activities":
		return social.NewGuildActivitiesRoutine()
	case "sacred_alliance":
		return social.NewSacredAllianceRoutine()
	case "homestead":
		return social.NewHomesteadRoutine()
	case "friendship":
		return social.NewFriendshipRoutine()
	case "fame_blessing":
		return social.NewFameBlessingRoutine()

	// 4. Dungeon
	case "dungeon_sweep":
		return dungeon.NewDungeonSweepRoutine(f.dictRepo, f.dungeonCfg)
	case "six_realms":
		return dungeon.NewSixRealmsRoutine()
	case "demon_tower":
		return dungeon.NewDemonTowerRoutine()
	case "training_room":
		return dungeon.NewTrainingRoomRoutine()
	case "zodiac_challenge":
		return dungeon.NewZodiacRoutine()
	case "spirit_trial":
		return dungeon.NewSpiritTrialRoutine()

	// 5. PVP
	case "arena":
		return pvp.NewArenaRoutine()
	case "celestial_arena":
		return pvp.NewCelestialArenaRoutine()
	case "gods_and_demons":
		return pvp.NewGodsAndDemonsRoutine()
	case "caravan_hijack":
		return pvp.NewCaravanHijackRoutine()
	case "world_boss":
		return pvp.NewWorldBossRoutine()
	case "cross_server_ladder":
		return pvp.NewCrossServerLadderRoutine()

	// 6. Minigame
	case "lucky_star":
		return minigame.NewLuckyStarRoutine()
	case "immortal_fantasy":
		return minigame.NewImmortalFantasyRoutine(f.qaEngine)
	case "imperial_exam":
		return minigame.NewImperialExamRoutine(f.qaEngine)
	case "partner_guess":
		return minigame.NewPartnerGuessRoutine()
	case "fishing":
		return minigame.NewFishingRoutine()
	case "scratch_card":
		return minigame.NewScratchCardRoutine()
	case "ice_cave":
		return minigame.NewIceCaveRoutine()

	default:
		return nil
	}
}
