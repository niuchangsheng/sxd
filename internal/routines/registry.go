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
		{ID: "online_gift", Name: "在线礼包与终结礼包", Domain: "daily", Schedule: scheduler.ScheduleLoop, Description: "领取在线时长福利，以及吉星高照/喜从天降/灵脉/拜关公/聚灵等功能终结礼包", DefaultOn: false},
		{ID: "vip_welfare", Name: "VIP特权与俸禄", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动领取VIP每日礼包与官职俸禄", DefaultOn: false},
		{ID: "god_bless", Name: "跨服天神赐福", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动领用跨服天神赐福增益点数", DefaultOn: false},
		{ID: "online_reward", Name: "在线奖励一键领取", Domain: "daily", Schedule: scheduler.ScheduleLoop, Description: "按当日在线时长一键领取在线奖励 (含元宝档位，不消耗元宝)", DefaultOn: false},
		{ID: "partner_practice", Name: "幻境修炼", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "开始/结算幻境修炼免费收益 (不加速、不雇佣)", DefaultOn: false},
		{ID: "fairyland_dispatch", Name: "宠物仙域游历", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "免费派遣宠物游历并领取任务奖励 (不买礼包)", DefaultOn: false},
		{ID: "three_world", Name: "三界修炼", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "人界修炼开始/结算 (不使用加速)", DefaultOn: false},
		{ID: "expedition", Name: "仙界远征任务", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "派遣 5 条远征任务 (免费, 不消耗元宝)", DefaultOn: false},
		{ID: "courtyard_pet", Name: "宠物灵符召唤", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "循环 call(NORMAL) 消耗全部灵符召唤宠物 (不消耗元宝)", DefaultOn: false},
		{ID: "weekend_event", Name: "周末狂欢礼与水果机", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "周末领取狂欢礼并用免费次数抽水果机 (不消耗元宝)", DefaultOn: false},
		{ID: "daoyuan_vip_shop", Name: "精品杂货 VIP 商店", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "VIP5/VIP8 购买神甲晶石/天魂自选包 (仅低价货币档)", DefaultOn: false},
		{ID: "device_bind_reward", Name: "设备绑定奖励", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "手机绑定后领取首绑200元宝与月度50元宝 (不代绑定)", DefaultOn: false},
		{ID: "town_practice", Name: "城镇打坐练功", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "上线自动结算并领取离线城镇打坐练功经验", DefaultOn: true},
		{ID: "destiny_sword", Name: "飞剑", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "自动一键淬炼飞剑剑意 (不消耗元宝)", DefaultOn: false},
		{ID: "daily_gifts", Name: "每日免费礼包", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "领取每日俸禄、每日灵石与 PC 微端礼包 (仅免费档)", DefaultOn: false},
		{ID: "saint_practice", Name: "圣域修炼替身", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "圣域开启时进入免费修炼房并领取累积经验 (不消耗元宝)", DefaultOn: false},
		{ID: "dragonball_offer", Name: "神龙普通上供", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "每日普通上供获取活跃度与龙珠经验 (仅消耗铜钱)", DefaultOn: false},
		{ID: "fate_hunt", Name: "免费猎命与命宫碎片", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "每日免费指定猎命师与铜钱兑换命宫碎片 (不消耗元宝)", DefaultOn: false},
		{ID: "children_daily", Name: "孩子学习与任务", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "孩子上学与已完成任务奖励领取 (消耗培养金，不消耗元宝)", DefaultOn: false},
		{ID: "rebirth_land", Name: "轮回净地挂机奖励", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "免费一键领取轮回净地挂机奖励 (不消耗元宝)", DefaultOn: false},
		{ID: "activity_report", Name: "每日活跃度汇总", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "只读汇总每日/周常活跃度并输出 1200 达标缺口", DefaultOn: false},
		{ID: "tactics_water", Name: "兵法圣水购买", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "铜钱购买湛蓝圣水完成兵法活跃度 (不消耗元宝)", DefaultOn: false},
		{ID: "partner_link_box", Name: "缘魂宝箱购买", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "道缘/铜钱购买缘魂宝箱完成活跃度 (不消耗元宝)", DefaultOn: false},
		{ID: "xiudan", Name: "服用修为丹", Domain: "daily", Schedule: scheduler.ScheduleCron, Description: "通天道场服用背包内修为丹完成活跃度 (不购买、不消耗元宝)", DefaultOn: false},

		// 2. Farming 领域 (6)
		{ID: "herb_garden", Name: "药园种植巡检", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "全自动药园播种、施肥、除草与成熟采摘", DefaultOn: true},
		{ID: "pilgrimage", Name: "西天取经护送", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "自动刷新使者启程护送，并打劫取经路上其他玩家队伍", DefaultOn: false},
		{ID: "crystal_mine", Name: "仙界矿山开采", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "全自动仙界矿石开采与防抢夺守护", DefaultOn: false},
		{ID: "spirit_pool", Name: "异兽与淬炼池", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "异兽自动喂养与灵宝淬炼池提炼", DefaultOn: false},
		{ID: "refine_pot", Name: "灵霄宝殿炼宝炉", Domain: "farming", Schedule: scheduler.ScheduleCron, Description: "按材料数量 >=500 选择最大数量炼制 (不消耗元宝)", DefaultOn: false},
		{ID: "gourd_realm", Name: "壶中界炼丹合成", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "壶中界灵气转化、丹药炼制与材料融合", DefaultOn: false},
		{ID: "rune_refine", Name: "符文炼化八卦炉", Domain: "farming", Schedule: scheduler.ScheduleLoop, Description: "符文自动淬炼与八卦炉灵气提纯", DefaultOn: false},

		// 3. Social 领域 (5)
		{ID: "guild_activities", Name: "仙盟日常活动", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "仙盟铜钱捐献、神兽召唤喂养与魔神挑战", DefaultOn: false},
		{ID: "sacred_alliance", Name: "圣盟日常活动", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "圣盟祭祀灵兽、幻魔塔挑战与商店兑换", DefaultOn: false},
		{ID: "homestead", Name: "住宅与夫妻宝箱", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "住宅家具祝福领取与夫妻宝箱日常互动", DefaultOn: false},
		{ID: "friendship", Name: "好友结义与送花", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "好友自动送花互动与结义羁绊维护", DefaultOn: false},
		{ID: "fame_blessing", Name: "仙界尊像膜拜", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "仙界膜拜前三名霸主大神与声望祝福获取", DefaultOn: false},
		{ID: "void_worship", Name: "仙界虚空膜拜", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "大神榜每日免费膜拜 (不消耗元宝)", DefaultOn: false},
		{ID: "sa_world_tower", Name: "全网幻魔塔", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "开放时按塔编号免费挑战 (不买次数)", DefaultOn: false},
		{ID: "sa_union_worship", Name: "万古神墟膜拜", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "圣盟膜拜面板铜钱/免费档 (不使用元宝档)", DefaultOn: false},
		{ID: "jieyuan_box", Name: "结缘宝箱开启", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "批量开启结缘宝箱 (除了传说，其他都开)", DefaultOn: false},
		{ID: "faction_blessing", Name: "帮派祭神", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "每日帮派上香祭神，获取祝福、声望与神像灵气", DefaultOn: false},
		{ID: "faction_lucky_wheel", Name: "帮派转盘", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "消耗青龙令自动转动帮派转盘直至次数耗尽", DefaultOn: false},
		{ID: "collection_booklets", Name: "神仙收藏册", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "领取声望档位奖励并拜访好友获得声望 (不消耗元宝)", DefaultOn: false},
		{ID: "npc_friendship", Name: "NPC结交", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "结交 NPC、领取任务奖励并赠送铜钱伴手礼 (不消耗元宝)", DefaultOn: false},
		{ID: "marry_home", Name: "家园奖励", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "领取自家小蛋糕家园奖励 (免费, 非活动时间自动跳过)", DefaultOn: false},
		{ID: "sect_monster", Name: "宗门灵兽喂养", Domain: "social", Schedule: scheduler.ScheduleCron, Description: "经仙盟城镇会话免费喂养宗门灵兽 (type=0, 不消耗元宝)", DefaultOn: false},

		// 4. Dungeon 领域 (8)
		{ID: "dungeon", Name: "副本推图与扫荡", Domain: "dungeon", Schedule: scheduler.ScheduleLoop, Description: "主线/支线任务闭环推图与关卡扫荡", DefaultOn: true},
		{ID: "six_realms", Name: "六道轮回挑战", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "六道轮回塔爬塔与灵件自动熔炼分解", DefaultOn: false},
		{ID: "demon_tower", Name: "伏魔锁妖塔挑战", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "锁妖塔日常挑战与怪物试炼通关", DefaultOn: false},
		{ID: "training_room", Name: "仙界练功房", Domain: "dungeon", Schedule: scheduler.ScheduleLoop, Description: "仙界练功房普通/高级场挂机收益维护", DefaultOn: false},
		{ID: "zodiac_challenge", Name: "生肖挑战与金油", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "十二生肖关卡挑战与金油炼化突破", DefaultOn: false},
		{ID: "spirit_trial", Name: "精灵试炼扫荡", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "精灵副本星级通关挑战与材料扫荡", DefaultOn: false},
		{ID: "qimen_dunjia", Name: "奇门遁甲法阵", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "奇门遁甲法阵星图注灵与布阵生效", DefaultOn: false},
		{ID: "xunxian_road", Name: "寻仙之路推图", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "寻仙之路关卡挑战与节点宝箱领用", DefaultOn: false},
		{ID: "beelzebub_trials", Name: "魔王试炼", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "自动创建队伍、招募助战镜像、挑战每日魔王试炼并领取道行奖励", DefaultOn: false},
		{ID: "tong_tian_tower", Name: "通天塔", Domain: "dungeon", Schedule: scheduler.ScheduleCron, Description: "自动逐层挑战通天塔并按需突破重天 (不消耗元宝)", DefaultOn: false},

		// 5. PVP 领域 (6)
		{ID: "arena", Name: "本服竞技场", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "本服竞技场自动寻找适宜对手挑战与排名领奖", DefaultOn: false},
		{ID: "celestial_arena", Name: "仙界竞技场", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "仙界竞技场积分赛自动挑战与最弱对手筛选", DefaultOn: false},
		{ID: "holy_arena", Name: "圣域竞技场", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "圣域竞技场抢占空席位与最弱对手 (不消耗元宝)", DefaultOn: false},
		{ID: "gods_and_demons", Name: "神魔大战阵营", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "神魔大战阵营对抗定时参战", DefaultOn: false},
		{ID: "caravan_hijack", Name: "阵营劫镖拦截", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "阵营商队巡查截击与声望抢夺", DefaultOn: false},
		{ID: "world_boss", Name: "世界Boss争夺", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "擎天木/赤炎兽Boss定时准点参战与伤害鼓舞", DefaultOn: false},
		{ID: "cross_server_ladder", Name: "跨服天梯排位", Domain: "pvp", Schedule: scheduler.ScheduleCron, Description: "跨服天梯自动匹配竞技与段位宝箱领用", DefaultOn: false},

		// 6. Minigame 领域 (7)
		{ID: "lucky_star", Name: "吉星高照", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "个人吉星高照摇骰子、免费逆天改运与奖励领取", DefaultOn: false},
		{ID: "faction_lucky_star", Name: "帮派吉星高照", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "帮派吉星高照扔骰子与高分奖励获取", DefaultOn: false},
		{ID: "travel_event", Name: "仙旅奇缘事件抉择", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "自动拉取仙旅奇缘事件并依据题库收益权重选择最优分支", DefaultOn: false},
		{ID: "imperial_exam", Name: "金榜题名会试", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "金榜题名乡试会试殿试自动化智能答题", DefaultOn: false},
		{ID: "partner_guess", Name: "伙伴猜猜看", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "伙伴特征自动比对竞猜", DefaultOn: false},
		{ID: "fishing", Name: "自动钓鱼命格", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "自动水域钓鱼与猎命命格吞噬融合", DefaultOn: false},
		{ID: "scratch_card", Name: "幸运刮刮卡", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "日常免费刮刮卡福利抽取", DefaultOn: false},
		{ID: "ask_fairy", Name: "每日问灵", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "每日免费问灵一次，随机奖励优先选元宝，不消耗元宝", DefaultOn: false},
		{ID: "golden_touch_stone", Name: "点金石", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "点化金石获取经验/幸运值 (不消耗元宝)", DefaultOn: false},
		{ID: "forbidden_book", Name: "禁地之书", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "自动探索禁地之书，免费次数用尽后仅用铜钱兑换", DefaultOn: false},
		{ID: "ice_cave", Name: "一键冰窟探险", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "一键五锤最优路径通关与金币领取", DefaultOn: false},
		{ID: "era_travel", Name: "山河纪元游历", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "山河纪元游历掷骰子与奇遇寻宝", DefaultOn: false},
		{ID: "lingdao_practice", Name: "灵道初解修炼", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "灵道初解任务领悟与阶段进阶", DefaultOn: false},
		{ID: "huanshou_invade", Name: "山海幻兽入侵", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "山海幻兽入侵副本推图与极速扫荡", DefaultOn: false},
		{ID: "wufa_field", Name: "悟法领域感悟", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "悟法领域属性加成与秘籍感悟抽卡", DefaultOn: false},
		{ID: "master_practice", Name: "宗师修行试炼", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "宗师修行心法挑战与境界突破", DefaultOn: false},
		{ID: "treasure_house", Name: "天帝宝库自选", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "天帝宝库自选大奖与钥匙抽奖开启", DefaultOn: false},
		{ID: "find_immortal", Name: "喜从天降画龙鱼", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "喜从天降免费龙鱼令召唤与奖励领取 (不消耗元宝)", DefaultOn: false},
		{ID: "five_blessings", Name: "五福临门", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "五福临门每日免费祈福与奖励领取 (不消耗元宝)", DefaultOn: false},
		{ID: "find_beast", Name: "寻找异兽", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "每日使用免费次数寻找异兽并领取奖励 (不消耗元宝)", DefaultOn: false},
		{ID: "duobao_mountain", Name: "多宝山", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "每日使用免费次数挖掘多宝山宝藏 (不消耗元宝)", DefaultOn: false},
		{ID: "eudemons", Name: "幻兽经脉", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "自动激活幻兽经脉并维护幻兽面板 (不消耗元宝)", DefaultOn: false},
		{ID: "partner_link", Name: "灵结", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "自动激活灵结 (仅消耗伙伴灵契，不消耗元宝)", DefaultOn: false},
		{ID: "star_picture", Name: "法力星图占星", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "每日消耗免费次数占星 (free 用尽即停止，不消耗元宝)", DefaultOn: false},
		{ID: "era_yanjie", Name: "纪元湮劫战斗", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "纪元湮劫每日推进一关战斗 (免费, 不加体力/元宝)", DefaultOn: false},
		{ID: "bailian", Name: "百炼乾坤一/二", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "百炼乾坤一 (272) 与二 (320) 一键挑战 (不消耗元宝)", DefaultOn: false},
		{ID: "soul_hunt", Name: "灵域猎妖", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "灵域猎妖关卡挑战 (仅免费次数, 不买次数)", DefaultOn: false},
		{ID: "era_creation", Name: "纪元造化秘境", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "先探索再挑战事件并领取战斗奖励 (不消耗元宝)", DefaultOn: false},
		{ID: "fortune_box", Name: "奇缘宝藏", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "开放时免费开启宝箱 (不买每日礼包)", DefaultOn: false},
		{ID: "cultivation_card", Name: "神仙卡牌收藏集", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "觅寻仙踪任务、铜钱卡包商城与叩寻仙踪开包 (不消耗元宝)", DefaultOn: false},
		{ID: "era_treasure", Name: "纪元秘藏与轮回图录", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "领取纪元秘藏/轮回图录免费奖励 (不消耗元宝)", DefaultOn: false},
		{ID: "mystic_hunt", Name: "仙幽猎境", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "仙幽猎境入口开放时免费进入一次 (按活动时段)", DefaultOn: false},
		{ID: "get_peach", Name: "摘仙桃", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "摘取已召唤仙桃并用免费/铜钱次数召唤 (不消耗元宝)", DefaultOn: false},
		{ID: "pet_animal", Name: "叶公好龙普通培养", Domain: "minigame", Schedule: scheduler.ScheduleCron, Description: "每日叶公好龙铜钱普通培养 (不使用元宝培养)", DefaultOn: false},
	}
}

// RegistryFactory 构建并注册所有玩法的统一工厂。
type RegistryFactory struct {
	qaEngine          qa.Engine
	dictRepo          dictionary.Repository
	dungeonCfg        dungeon.SweepConfig
	sixRealmsCfg      dungeon.SixRealmsConfig
	beelzebubCfg      dungeon.BeelzebubTrialsConfig
	trainingCfg       dungeon.TrainingRoomConfig
	jieyuanCfg        social.JieYuanBoxConfig
	blessingCfg       social.FactionBlessingConfig
	treasureCfg       minigame.TreasureHouseConfig
	luckyCfg          minigame.LuckyStarConfig
	masterPracticeCfg minigame.MasterPracticeConfig
	pilgrimageCfg     farming.PilgrimageConfig
	spiritPoolCfg     farming.SpiritPoolConfig
	onlineGiftCfg     daily.OnlineGiftConfig
}

// NewRegistryFactory 创建注册工厂实例。
func NewRegistryFactory(qaEngine qa.Engine, dictRepo dictionary.Repository) *RegistryFactory {
	cfg := dungeon.DefaultSweepConfig()
	cfg.PreferPracticeQuickly = true
	return &RegistryFactory{
		qaEngine:          qaEngine,
		dictRepo:          dictRepo,
		dungeonCfg:        cfg,
		sixRealmsCfg:      dungeon.DefaultSixRealmsConfig(),
		beelzebubCfg:      dungeon.DefaultBeelzebubTrialsConfig(),
		trainingCfg:       dungeon.DefaultTrainingRoomConfig(),
		jieyuanCfg:        social.DefaultJieYuanBoxConfig(),
		blessingCfg:       social.DefaultFactionBlessingConfig(),
		treasureCfg:       minigame.DefaultTreasureHouseConfig(),
		luckyCfg:          minigame.DefaultLuckyStarConfig(),
		masterPracticeCfg: minigame.DefaultMasterPracticeConfig(),
		pilgrimageCfg:     farming.DefaultPilgrimageConfig(),
		spiritPoolCfg:     farming.DefaultSpiritPoolConfig(),
		onlineGiftCfg:     daily.DefaultOnlineGiftConfig(),
	}
}

// SetDungeonConfig 设置副本自动化策略配置。
func (f *RegistryFactory) SetDungeonConfig(cfg dungeon.SweepConfig) {
	f.dungeonCfg = cfg
}

// SetSixRealmsConfig 设置六道轮回自动化策略配置。
func (f *RegistryFactory) SetSixRealmsConfig(cfg dungeon.SixRealmsConfig) {
	f.sixRealmsCfg = cfg
}

// SetBeelzebubTrialsConfig 设置魔王试炼策略配置。
func (f *RegistryFactory) SetBeelzebubTrialsConfig(cfg dungeon.BeelzebubTrialsConfig) {
	f.beelzebubCfg = cfg
}

// SetTrainingRoomConfig 设置仙界练功房挂机收益维护策略配置。
func (f *RegistryFactory) SetTrainingRoomConfig(cfg dungeon.TrainingRoomConfig) {
	f.trainingCfg = cfg
}

// SetJieYuanBoxConfig 设置结缘宝箱策略配置。
func (f *RegistryFactory) SetJieYuanBoxConfig(cfg social.JieYuanBoxConfig) {
	f.jieyuanCfg = cfg
}

// SetFactionBlessingConfig 设置帮派祭神上香策略配置。
func (f *RegistryFactory) SetFactionBlessingConfig(cfg social.FactionBlessingConfig) {
	f.blessingCfg = cfg
}

// SetTreasureHouseConfig 设置天帝宝库自选大奖与抽奖策略配置。
func (f *RegistryFactory) SetTreasureHouseConfig(cfg minigame.TreasureHouseConfig) {
	if cfg.DictRepo == nil {
		cfg.DictRepo = f.dictRepo
	}
	f.treasureCfg = cfg
}

// SetLuckyStarConfig 设置个人吉星高照自动化策略配置。
func (f *RegistryFactory) SetLuckyStarConfig(cfg minigame.LuckyStarConfig) {
	f.luckyCfg = cfg
}

// SetMasterPracticeConfig 设置宗师修行方向顺序与阵容整理策略。
func (f *RegistryFactory) SetMasterPracticeConfig(cfg minigame.MasterPracticeConfig) {
	if cfg.DictRepo == nil {
		cfg.DictRepo = f.dictRepo
	}
	f.masterPracticeCfg = cfg
}

// SetOnlineGiftConfig 设置在线礼包与功能终结礼包策略配置。
func (f *RegistryFactory) SetOnlineGiftConfig(cfg daily.OnlineGiftConfig) {
	f.onlineGiftCfg = cfg
}

// SetPilgrimageConfig 设置西天取经护送与打劫策略配置。
func (f *RegistryFactory) SetPilgrimageConfig(cfg farming.PilgrimageConfig) {
	f.pilgrimageCfg = cfg
}

// SetSpiritPoolConfig 设置粗铁淬炼池产出目标配置 (玄铁/灵材)。
func (f *RegistryFactory) SetSpiritPoolConfig(cfg farming.SpiritPoolConfig) {
	f.spiritPoolCfg = cfg
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

// CreateRoutine 按玩法 ID 实例化单个 Routine。
// 供控制台在运行时首次开启某个启动时未注册的玩法时补装配。
func (f *RegistryFactory) CreateRoutine(id string) scheduler.ActivityRoutine {
	return f.createRoutine(id)
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
		return daily.NewOnlineGiftRoutine(f.onlineGiftCfg)
	case "vip_welfare":
		return daily.NewVIPWelfareRoutine()
	case "god_bless":
		return daily.NewGodBlessRoutine()
	case "online_reward":
		return daily.NewOnlineRewardRoutine()
	case "courtyard_pet":
		return daily.NewCourtyardPetRoutine()
	case "weekend_event":
		return daily.NewWeekendEventRoutine()
	case "partner_practice":
		return daily.NewPartnerPracticeRoutine()
	case "expedition":
		return daily.NewExpeditionRoutine()
	case "fairyland_dispatch":
		return daily.NewFairylandDispatchRoutine()
	case "three_world":
		return daily.NewThreeWorldRoutine()
	case "daoyuan_vip_shop":
		return daily.NewDaoyuanVipShopRoutine()
	case "device_bind_reward":
		return daily.NewDeviceBindRewardRoutine()
	case "town_practice":
		return daily.NewTownPracticeRoutine()
	case "destiny_sword":
		return daily.NewDestinySwordRoutine()
	case "daily_gifts":
		return daily.NewDailyGiftsRoutine()
	case "saint_practice":
		return daily.NewSaintPracticeRoutine()
	case "dragonball_offer":
		return daily.NewDragonballOfferRoutine()
	case "fate_hunt":
		return daily.NewFateHuntRoutine()
	case "children_daily":
		return daily.NewChildrenDailyRoutine()
	case "rebirth_land":
		return daily.NewRebirthLandRoutine()
	case "activity_report":
		return daily.NewDailyActivityReportRoutine()
	case "tactics_water":
		return daily.NewTacticsWaterRoutine()
	case "partner_link_box":
		return daily.NewPartnerLinkBoxRoutine()
	case "xiudan":
		return daily.NewXiuDanRoutine()

	// 2. Farming
	case "herb_garden":
		return farming.NewHerbGardenRoutine()
	case "pilgrimage":
		return farming.NewPilgrimageRoutine(f.pilgrimageCfg)
	case "crystal_mine":
		return farming.NewCrystalMineRoutine()
	case "refine_pot":
		return farming.NewRefinePotRoutine()
	case "spirit_pool":
		return farming.NewSpiritPoolRoutine(f.spiritPoolCfg)
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
	case "sa_union_worship":
		return social.NewSaUnionWorshipRoutine()
	case "sa_world_tower":
		return social.NewSaWorldTowerRoutine()
	case "void_worship":
		return social.NewVoidWorshipRoutine()
	case "fame_blessing":
		return social.NewFameBlessingRoutine()
	case "jieyuan_box":
		return social.NewJieYuanBoxRoutine(f.jieyuanCfg)
	case "faction_blessing":
		return social.NewFactionBlessingRoutine(f.blessingCfg)
	case "faction_lucky_wheel":
		return social.NewFactionLuckyWheelRoutine()
	case "collection_booklets":
		return social.NewCollectionBookletsRoutine()
	case "npc_friendship":
		return social.NewNpcFriendshipRoutine()
	case "marry_home":
		return social.NewMarryHomeRoutine()
	case "sect_monster":
		return social.NewSectMonsterRoutine()

	// 4. Dungeon
	case "dungeon", "dungeon_sweep":
		return dungeon.NewDungeonSweepRoutine(f.dictRepo, f.dungeonCfg)
	case "six_realms":
		return dungeon.NewSixRealmsRoutine(f.sixRealmsCfg)
	case "demon_tower":
		return dungeon.NewDemonTowerRoutine()
	case "training_room":
		return dungeon.NewTrainingRoomRoutine(f.trainingCfg)
	case "zodiac_challenge":
		return dungeon.NewZodiacRoutine()
	case "spirit_trial":
		return dungeon.NewSpiritTrialRoutine()
	case "qimen_dunjia":
		return dungeon.NewQiMenRoutine()
	case "xunxian_road":
		return dungeon.NewXunXianRoutine()
	case "beelzebub_trials":
		return dungeon.NewBeelzebubTrialsRoutine(f.beelzebubCfg)
	case "tong_tian_tower":
		return dungeon.NewTongTianTowerRoutine()

	// 5. PVP
	case "arena":
		return pvp.NewArenaRoutine()
	case "celestial_arena":
		return pvp.NewCelestialArenaRoutine()
	case "holy_arena":
		return pvp.NewHolyArenaRoutine()
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
		return minigame.NewLuckyStarRoutine(f.luckyCfg)
	case "faction_lucky_star":
		return minigame.NewFactionLuckyStarRoutine()
	case "travel_event", "immortal_fantasy":
		return minigame.NewTravelEventRoutine(f.qaEngine)
	case "imperial_exam":
		return minigame.NewImperialExamRoutine(f.qaEngine)
	case "partner_guess":
		return minigame.NewPartnerGuessRoutine()
	case "fishing":
		return minigame.NewFishingRoutine()
	case "scratch_card":
		return minigame.NewScratchCardRoutine()
	case "ask_fairy":
		return minigame.NewAskFairyRoutine()
	case "golden_touch_stone":
		return minigame.NewGoldenTouchStoneRoutine()
	case "forbidden_book":
		return minigame.NewForbiddenBookRoutine()
	case "ice_cave":
		return minigame.NewIceCaveRoutine()
	case "era_travel":
		return minigame.NewEraTravelRoutine()
	case "lingdao_practice":
		return minigame.NewLingDaoRoutine()
	case "huanshou_invade":
		return minigame.NewHuanshouRoutine()
	case "wufa_field":
		return minigame.NewWuFaRoutine()
	case "master_practice":
		cfg := f.masterPracticeCfg
		if cfg.DictRepo == nil {
			cfg.DictRepo = f.dictRepo
		}
		return minigame.NewMasterPracticeRoutine(cfg)
	case "treasure_house":
		cfg := f.treasureCfg
		if cfg.DictRepo == nil {
			cfg.DictRepo = f.dictRepo
		}
		return minigame.NewTreasureHouseRoutine(cfg)
	case "find_immortal":
		return minigame.NewFindImmortalRoutine()
	case "five_blessings":
		return minigame.NewFiveBlessingsRoutine()
	case "find_beast":
		return minigame.NewFindBeastRoutine()
	case "duobao_mountain":
		return minigame.NewDuobaoMountainRoutine()
	case "eudemons":
		return minigame.NewEudemonsRoutine()
	case "partner_link":
		return minigame.NewPartnerLinkRoutine()
	case "star_picture":
		return minigame.NewStarPictureRoutine()
	case "era_yanjie":
		return minigame.NewEraYanjieRoutine()
	case "bailian":
		return minigame.NewBaiLianRoutine()
	case "soul_hunt":
		return minigame.NewSoulHuntRoutine()
	case "era_creation":
		return minigame.NewEraCreationRoutine()
	case "cultivation_card":
		return minigame.NewCultivationCardRoutine()
	case "fortune_box":
		return minigame.NewFortuneBoxRoutine()
	case "era_treasure":
		return minigame.NewEraTreasureRoutine()
	case "mystic_hunt":
		return minigame.NewMysticHuntRoutine()
	case "get_peach":
		return minigame.NewGetPeachRoutine()
	case "pet_animal":
		return minigame.NewPetAnimalRoutine()

	default:
		return nil
	}
}
