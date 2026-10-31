// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-08
package protocol

const (
	// 核心业务模块编号 (Module)
	ModulePlayer          uint16 = 0  // 角色主模块 (Mod_Player_Base)
	ModuleTown            uint16 = 1  // 城镇场景模块 (Mod_Town_Base)
	ModuleItem            uint16 = 2  // 物品背包模块 (Mod_Item_Base)
	ModuleMission         uint16 = 4  // 关卡任务模块 (Mod_Mission_Base)
	ModuleFarm            uint16 = 13 // 药园农场模块 (Mod_Farm_Base)
	ModuleFate            uint16 = 21 // 仙履奇缘模块 (Mod_Fate_Base)
	ModuleMissionPractice uint16 = 25 // 副本挂机扫荡模块 (Mod_MissionPractice_Base)
	ModuleArena           uint16 = 28 // 竞技场模块 (Mod_Arena_Base)
	ModuleTower           uint16 = 32 // 六道轮回塔模块 (Mod_Tower_Base)
	ModuleSealSoul        uint16 = 34 // 封灵与灵件模块 (Mod_SealSoul_Base)
	ModuleHeroMission     uint16 = 35  // 英雄副本模块 (Mod_HeroMission_Base)
	ModuleStLogin         uint16 = 94  // 跨服/网页登录模块 (Mod_StLogin_Base)
	ModuleNewPractice     uint16 = 584 // 新版城镇练功打坐模块 (Mod_NewPractice_Base)
)

const (
	// ActionIDNewPracticeGetInfo 获取现代离线打坐练功进度与累积经验 (Module 584, Action 1 -> 0x02480001, Mod_NewPractice_Base.get_practice_info)
	ActionIDNewPracticeGetInfo uint32 = 0x02480001
	// ActionIDNewPracticeCollectAir 单独领取新打坐灵气 (Module 584, Action 2 -> 0x02480002, Mod_NewPractice_Base.collect_air)
	ActionIDNewPracticeCollectAir uint32 = 0x02480002
	// ActionIDNewPracticeCollectAll 一键领取新打坐全部经验与灵气 (Module 584, Action 3 -> 0x02480003, Mod_NewPractice_Base.collect_all)
	ActionIDNewPracticeCollectAll uint32 = 0x02480003

	// 新版练功打坐业务结果码
	NewPracticeSuccess uint8 = 0
	NewPracticeFailed  uint8 = 1
)

const (
	// ActionIDItemGetPackList 获取玩家主背包与仓库物品列表 (Module 2, Action 41 -> 0x00020029)
	ActionIDItemGetPackList uint32 = 0x00020029
	// ActionIDItemUseGrid 使用背包指定格位物品 (Module 2, Action 53 -> 0x00020035)
	ActionIDItemUseGrid uint32 = 0x00020035
	// ActionIDItemSell 出售背包指定格位物品 (Module 2, Action 73 -> 0x00020049, Mod_Item_Base.player_sell_item)
	ActionIDItemSell uint32 = 0x00020049

	// 体力道具 ID (来源于数据字典 Pieb.db item 表)
	ItemIDSteamedBun    int32 = 1488 // 包子 (可补充 5 点体力)
	ItemIDRiceDumpling  int32 = 1444 // 粽子 (可补充 10 点体力)
	ItemIDTeaEgg        int32 = 2016 // 茶叶蛋 (可补充 50 点体力)

	// 物品使用业务结果码
	ItemActionSuccess uint8 = 20
	ItemSuccess       uint8 = 0
)

const (
	// ActionIDStLogin 跨服登录握手协议号 (Module 94, Action 0 -> 0x005E0000)
	ActionIDStLogin uint32 = 0x005E0000

	// ActionIDPlayerLogin 角色主服登录协议号 (Module 0, Action 0 -> 0x00000000, Mod_Player_Base.login)
	ActionIDPlayerLogin uint32 = 0x00000000
	// ActionIDPlayerGetInfo 获取角色主属性协议号 (Module 0, Action 2 -> 0x00000002, Mod_Player_Base.get_player_info)
	ActionIDPlayerGetInfo uint32 = 0x00000002
	// ActionIDPlayerUpdateData 服务器主动下发属性变更协议号 (Module 0, Action 3 -> 0x00000003, Mod_Player_Base.update_player_data)
	ActionIDPlayerUpdateData uint32 = 0x00000003
	// ActionIDPlayerHeartbeat 角色与网关长连接心跳保活协议号 (Module 0, Action 23 -> 0x00000017, Mod_Player_Base.heartbeat)
	ActionIDPlayerHeartbeat uint32 = 0x00000017
	// ActionIDPlayerServerTime 同步服务器系统时间协议号 (Module 0, Action 22 -> 0x00000016, Mod_Player_Base.get_server_time)
	ActionIDPlayerServerTime uint32 = 0x00000016
	// ActionIDPlayerStartPractice 角色开始城镇打坐练功 (Module 0, Action 36 -> 0x00000024, Mod_Player_Base.start_practice)
	ActionIDPlayerStartPractice uint32 = 0x00000024
	// ActionIDPlayerGetPracticeData 获取角色城镇打坐练功数据 (Module 0, Action 37 -> 0x00000025, Mod_Player_Base.get_practice_data)
	ActionIDPlayerGetPracticeData uint32 = 0x00000025
	// ActionIDPlayerSignFinishedPractice 确认结束练功并结算累积经验 (Module 0, Action 38 -> 0x00000026, Mod_Player_Base.sign_finished_practice)
	ActionIDPlayerSignFinishedPractice uint32 = 0x00000026
	// ActionIDPlayerCancelPractice 取消角色城镇打坐练功 (Module 0, Action 39 -> 0x00000027, Mod_Player_Base.cancel_practice)
	ActionIDPlayerCancelPractice uint32 = 0x00000027
	// ActionIDPlayerPracticeNotify 城镇打坐练功经验跳动与状态变更服务端通知 (Module 0, Action 40 -> 0x00000028, Mod_Player_Base.practice_notify)
	ActionIDPlayerPracticeNotify uint32 = 0x00000028
	// ActionIDPlayerInitStep1 角色初始化握手步 1 (Module 0, Action 72 -> 0x00000048, Mod_Player_Base.enter_town)
	ActionIDPlayerInitStep1 uint32 = 0x00000048
	// ActionIDPlayerInitStep2 角色初始化握手步 2 (Module 0, Action 99 -> 0x00000063)
	ActionIDPlayerInitStep2 uint32 = 0x00000063
	// ActionIDPlayerInitStep3 角色初始化握手步 3 (Module 165, Action 0 -> 0x00A50000)
	ActionIDPlayerInitStep3 uint32 = 0x00A50000

	// ActionIDTownEnter 进入城镇协议号 (Module 1, Action 0 -> 0x00010000)
	ActionIDTownEnter uint32 = 0x00010000
	// ActionIDTownLeave 离开城镇协议号 (Module 1, Action 1 -> 0x00010001)
	ActionIDTownLeave uint32 = 0x00010001

	// ActionIDFarmGetInfo 查询药园土地基础开垦状态 (Module 13, Action 0 -> 0x000D0000)
	ActionIDFarmGetInfo uint32 = 0x000D0000
	// ActionIDFarmGetRoles 查询药园伙伴列表 (Module 13, Action 1 -> 0x000D0001)
	ActionIDFarmGetRoles uint32 = 0x000D0001
	// ActionIDFarmGetPlantState 查询药园作物种植状态与倒计时 (Module 13, Action 21 -> 0x000D0015)
	ActionIDFarmGetPlantState uint32 = 0x000D0015
	// ActionIDFarmGetSeeds 查询药园种子库存与商城列表 (Module 13, Action 22 -> 0x000D0016)
	ActionIDFarmGetSeeds uint32 = 0x000D0016
	// ActionIDFarmBuySeed 购买药园种子 (Module 13, Action 23 -> 0x000D0017)
	ActionIDFarmBuySeed uint32 = 0x000D0017
	// ActionIDFarmPlant 药园种植作物 (Module 13, Action 24 -> 0x000D0018)
	ActionIDFarmPlant uint32 = 0x000D0018
	// ActionIDFarmHarvest 药园收获作物 (Module 13, Action 25 -> 0x000D0019)
	ActionIDFarmHarvest uint32 = 0x000D0019

	// ActionIDFateGetInfo 查询仙履奇缘状态 (Module 21, Action 0 -> 0x00150000)
	ActionIDFateGetInfo uint32 = 0x00150000
	// ActionIDFateGetQuestion 获取仙履奇缘题目与选项 (Module 21, Action 1 -> 0x00150001)
	ActionIDFateGetQuestion uint32 = 0x00150001
	// ActionIDFateAnswer 仙履奇缘回答提交选项 (Module 21, Action 2 -> 0x00150002)
	ActionIDFateAnswer uint32 = 0x00150002

	// ActionIDArenaGetTimes 查询竞技场剩余挑战次数 (Module 28, Action 13 -> 0x001C000D)
	ActionIDArenaGetTimes uint32 = 0x001C000D
	// ActionIDArenaGetRanking 查询玩家竞技场排名 (Module 28, Action 12 -> 0x001C000C)
	ActionIDArenaGetRanking uint32 = 0x001C000C
	// ActionIDArenaGetOpponents 查询竞技场可挑战对手列表 (Module 28, Action 1 -> 0x001C0001)
	ActionIDArenaGetOpponents uint32 = 0x001C0001
	// ActionIDArenaChallenge 发起竞技场挑战 (Module 28, Action 2 -> 0x001C0002)
	ActionIDArenaChallenge uint32 = 0x001C0002
	// ActionIDArenaNotify 竞技场排名变更服务端通知 (Module 28, Action 11 -> 0x001C000B)
	ActionIDArenaNotify uint32 = 0x001C000B

	// ActionIDPracticeGetStatus 查询副本扫荡挂机队列与状态 (Module 25, Action 0 -> 0x00190000)
	ActionIDPracticeGetStatus uint32 = 0x00190000
	// ActionIDPracticeGetInfo 查询副本扫荡信息 (Module 25, Action 7 -> 0x00190007)
	ActionIDPracticeGetInfo uint32 = 0x00190007
	// ActionIDPracticeStart 开始副本扫荡 (Module 25, Action 1 -> 0x00190001)
	ActionIDPracticeStart uint32 = 0x00190001
	// ActionIDPracticeCancel 取消副本扫荡 (Module 25, Action 2 -> 0x00190002)
	ActionIDPracticeCancel uint32 = 0x00190002
	// ActionIDPracticeQuickly 立即加速秒完成扫荡 (Module 25, Action 3 -> 0x00190003)
	ActionIDPracticeQuickly uint32 = 0x00190003
	// ActionIDPracticeNotify 扫荡轮次战斗结算服务端主动通知 (Module 25, Action 4 -> 0x00190004)
	ActionIDPracticeNotify uint32 = 0x00190004

	// ActionIDHeroGetList 查询章节英雄关卡列表与状态 (Module 35, Action 0 -> 0x00230000)
	ActionIDHeroGetList uint32 = 0x00230000
	// ActionIDHeroPracticeStart 开始章节英雄关卡扫荡 (Module 35, Action 2 -> 0x00230002)
	ActionIDHeroPracticeStart uint32 = 0x00230002
	// ActionIDHeroPracticeCancel 取消章节英雄关卡扫荡 (Module 35, Action 3 -> 0x00230003)
	ActionIDHeroPracticeCancel uint32 = 0x00230003
	// ActionIDHeroPracticeQuickly 极速秒完成英雄关卡扫荡 (Module 35, Action 7 -> 0x00230007)
	ActionIDHeroPracticeQuickly uint32 = 0x00230007

	// ActionIDTowerGetInfo 查询六道轮回塔层数与进度 (Module 32, Action 0 -> 0x00200000, Mod_Tower_Base.get_tower_info)
	ActionIDTowerGetInfo uint32 = 0x00200000
	// ActionIDTowerStartChallenge 挑战六道轮回塔指定层 (Module 32, Action 2 -> 0x00200002, Mod_Tower_Base.start_challenge)
	ActionIDTowerStartChallenge uint32 = 0x00200002
	// ActionIDTowerResetLayer 重置六道轮回塔层数 (Module 32, Action 3 -> 0x00200003, Mod_Tower_Base.reset_layer)
	ActionIDTowerResetLayer uint32 = 0x00200003
	// ActionIDTowerStartAutoChallenge 开始六道轮回塔自动挑战扫荡 (Module 32, Action 4 -> 0x00200004, Mod_Tower_Base.start_auto_challenge)
	ActionIDTowerStartAutoChallenge uint32 = 0x00200004
	// ActionIDTowerStopAutoChallenge 停止六道轮回塔自动挑战 (Module 32, Action 5 -> 0x00200005, Mod_Tower_Base.stop_auto_challenge)
	ActionIDTowerStopAutoChallenge uint32 = 0x00200005
	// ActionIDTowerNotifyAutoReport 六道轮回塔自动挑战战报推送 (Module 32, Action 6 -> 0x00200006, Mod_Tower_Base.notify_auto_challenge_report)
	ActionIDTowerNotifyAutoReport uint32 = 0x00200006
	// ActionIDTowerQuicklyAutoChallenge 六道轮回塔立即完成自动挑战加速 (Module 32, Action 7 -> 0x00200007, Mod_Tower_Base.quickly_auto_challenge)
	ActionIDTowerQuicklyAutoChallenge uint32 = 0x00200007
	// ActionIDTowerComplete 六道轮回塔完成通知 (Module 32, Action 8 -> 0x00200008, Mod_Tower_Base.complete)
	ActionIDTowerComplete uint32 = 0x00200008

	// ActionIDSealSoulInfoByLocation 获取角色封灵/灵件列表 (Module 34, Action 1 -> 0x00220001, Mod_SealSoul_Base.soul_info_by_location)
	ActionIDSealSoulInfoByLocation uint32 = 0x00220001
	// ActionIDSealSoulOpenKey 开启角色封灵灵件孔位 (Module 34, Action 5 -> 0x00220005, Mod_SealSoul_Base.open_player_soul_key)
	ActionIDSealSoulOpenKey uint32 = 0x00220005
	// ActionIDSealSoulGetTowerKey 获取爬塔封灵钥匙状态 (Module 34, Action 7 -> 0x00220007, Mod_SealSoul_Base.get_tower_key)
	ActionIDSealSoulGetTowerKey uint32 = 0x00220007
	// ActionIDSealSoulSell 出售/熔炼分解灵件 (Module 34, Action 8 -> 0x00220008, Mod_SealSoul_Base.sell_player_soul)
	ActionIDSealSoulSell uint32 = 0x00220008

	// 魔王试炼协议号 (Module 57 -> 0x0039, Mod_BeelzebubTrials_Base)
	// ActionIDBeelzebubTrialsGetOpenTime 查询魔王试炼开启时间 (Module 57, Action 0 -> 0x00390000)
	ActionIDBeelzebubTrialsGetOpenTime uint32 = 0x00390000
	// ActionIDBeelzebubTrialsOpen 打开魔王试炼面板 (Module 57, Action 1 -> 0x00390001)
	ActionIDBeelzebubTrialsOpen uint32 = 0x00390001
	// ActionIDBeelzebubTrialsGetTeamList 获取魔王试炼队伍列表 (Module 57, Action 2 -> 0x00390002)
	ActionIDBeelzebubTrialsGetTeamList uint32 = 0x00390002
	// ActionIDBeelzebubTrialsGetPlayerTeamInfo 获取当前队伍成员信息 (Module 57, Action 3 -> 0x00390003)
	ActionIDBeelzebubTrialsGetPlayerTeamInfo uint32 = 0x00390003
	// ActionIDBeelzebubTrialsCreateTeam 创建魔王试炼队伍 (Module 57, Action 4 -> 0x00390004)
	ActionIDBeelzebubTrialsCreateTeam uint32 = 0x00390004
	// ActionIDBeelzebubTrialsDissolveTeam 解散魔王试炼队伍 (Module 57, Action 5 -> 0x00390005)
	ActionIDBeelzebubTrialsDissolveTeam uint32 = 0x00390005
	// ActionIDBeelzebubTrialsJoinTeam 加入魔王试炼队伍 (Module 57, Action 6 -> 0x00390006)
	ActionIDBeelzebubTrialsJoinTeam uint32 = 0x00390006
	// ActionIDBeelzebubTrialsExitTeam 退出魔王试炼队伍 (Module 57, Action 7 -> 0x00390007)
	ActionIDBeelzebubTrialsExitTeam uint32 = 0x00390007
	// ActionIDBeelzebubTrialsGetHelperRoleList 获取助战伙伴列表 (Module 57, Action 8 -> 0x00390008)
	ActionIDBeelzebubTrialsGetHelperRoleList uint32 = 0x00390008
	// ActionIDBeelzebubTrialsChooseHelperRole 选择助战伙伴 (Module 57, Action 9 -> 0x00390009)
	ActionIDBeelzebubTrialsChooseHelperRole uint32 = 0x00390009
	// ActionIDBeelzebubTrialsSortTeam 队伍成员排序调整 (Module 57, Action 10 -> 0x0039000A)
	ActionIDBeelzebubTrialsSortTeam uint32 = 0x0039000A
	// ActionIDBeelzebubTrialsKickedOutMember 踢出队伍成员 (Module 57, Action 11 -> 0x0039000B)
	ActionIDBeelzebubTrialsKickedOutMember uint32 = 0x0039000B
	// ActionIDBeelzebubTrialsClose 关闭魔王试炼 (Module 57, Action 12 -> 0x0039000C)
	ActionIDBeelzebubTrialsClose uint32 = 0x0039000C
	// ActionIDBeelzebubTrialsFight 发起魔王试炼战斗 (Module 57, Action 13 -> 0x0039000D)
	ActionIDBeelzebubTrialsFight uint32 = 0x0039000D
	// ActionIDBeelzebubTrialsGetWarResult 获取魔王试炼战报结果 (Module 57, Action 14 -> 0x0039000E)
	ActionIDBeelzebubTrialsGetWarResult uint32 = 0x0039000E
	// ActionIDBeelzebubTrialsNotify 服务端魔王试炼状态通知 (Module 57, Action 15 -> 0x0039000F)
	ActionIDBeelzebubTrialsNotify uint32 = 0x0039000F
	// ActionIDBeelzebubTrialsLeaveWar 退出魔王试炼战场 (Module 57, Action 16 -> 0x00390010)
	ActionIDBeelzebubTrialsLeaveWar uint32 = 0x00390010
	// ActionIDBeelzebubTrialsNotifyNewTeam 服务端新队伍通知 (Module 57, Action 17 -> 0x00390011)
	ActionIDBeelzebubTrialsNotifyNewTeam uint32 = 0x00390011
	// ActionIDBeelzebubTrialsNotifyDropTeam 服务端队伍解散通知 (Module 57, Action 18 -> 0x00390012)
	ActionIDBeelzebubTrialsNotifyDropTeam uint32 = 0x00390012
	// ActionIDBeelzebubTrialsNotifyUpdateMemberNum 服务端更新人数通知 (Module 57, Action 19 -> 0x00390013)
	ActionIDBeelzebubTrialsNotifyUpdateMemberNum uint32 = 0x00390013
	// ActionIDBeelzebubTrialsCheckCanChallenge 检查今日是否可挑战魔王 (Module 57, Action 20 -> 0x00390014)
	ActionIDBeelzebubTrialsCheckCanChallenge uint32 = 0x00390014
	// ActionIDBeelzebubTrialsGetPlayerCDTime 获取挑战冷却CD (Module 57, Action 21 -> 0x00390015)
	ActionIDBeelzebubTrialsGetPlayerCDTime uint32 = 0x00390015
	// ActionIDBeelzebubTrialsGetCanChallengeList 获取可挑战魔王列表 (Module 57, Action 22 -> 0x00390016)
	ActionIDBeelzebubTrialsGetCanChallengeList uint32 = 0x00390016
	// ActionIDBeelzebubTrialsOpenMoral 查询道行面板信息 (Module 57, Action 23 -> 0x00390017)
	ActionIDBeelzebubTrialsOpenMoral uint32 = 0x00390017
	// ActionIDBeelzebubTrialsGetMoralAward 领取道行等级奖励 (Module 57, Action 24 -> 0x00390018)
	ActionIDBeelzebubTrialsGetMoralAward uint32 = 0x00390018
	// ActionIDBeelzebubTrialsGetDeploy 获取阵型布阵 (Module 57, Action 25 -> 0x00390019)
	ActionIDBeelzebubTrialsGetDeploy uint32 = 0x00390019
	// ActionIDBeelzebubTrialsChangeDeploy 修改阵型布阵 (Module 57, Action 26 -> 0x0039001A)
	ActionIDBeelzebubTrialsChangeDeploy uint32 = 0x0039001A
	// ActionIDBeelzebubTrialsInvite 邀请帮派/全服好友 (Module 57, Action 27 -> 0x0039001B)
	ActionIDBeelzebubTrialsInvite uint32 = 0x0039001B
)

// 副本扫荡状态结果码
const (
	PracticeResultSuccess        uint8 = 0 // 扫荡成功
	PracticeResultNotEnoughPower uint8 = 1 // 体力不足
	PracticeResultBagFull        uint8 = 2 // 背包已满
	PracticeResultInPractice     uint8 = 3 // 正在扫荡中
	PracticeResultNotEnoughIngot uint8 = 4 // 元宝不足
)

// 六道轮回塔挑战与重置结果码 (Mod_Tower_Base 状态标识)
const (
	TowerResultSuccess        uint8 = 0 // SUCCESS: 成功
	TowerResultInvalidLayer    uint8 = 1 // INVALID_LAYER: 无效的层/道
	TowerResultNoLv           uint8 = 2 // NO_LV: 等级不足
	TowerResultWarCD          uint8 = 3 // WAR_CD: 战斗冷却中
	TowerResultNotEnoughIngot uint8 = 4 // NOT_ENOUGH_INGOT: 元宝不足
	TowerResultFullTimes      uint8 = 5 // FULL_TIMES: 次数已满
	TowerResultFullBag        uint8 = 6 // FULL_BAG: 灵件背包已满
)

// 角色属性更新键 (Mod_Player_Base 属性枚举)
const (
	PlayerPropLevel         uint8 = 10 // 等级
	PlayerPropIngot         uint8 = 11 // 元宝
	PlayerPropCoins         uint8 = 12 // 铜钱
	PlayerPropHealth        uint8 = 13 // 当前生命
	PlayerPropMaxHealth     uint8 = 14 // 最大生命
	PlayerPropPower         uint8 = 15 // 当前基础体力
	PlayerPropMaxPower      uint8 = 16 // 最大基础体力
	PlayerPropExperience    uint8 = 17 // 当前经验
	PlayerPropMaxExperience uint8 = 18 // 最大经验
	PlayerPropPackEmptyNum  uint8 = 25 // 背包剩余空闲格子数
	PlayerPropVIPLevel      uint8 = 31 // VIP等级
	PlayerPropExtraPower    uint8 = 77 // 存储/额外/赠送体力池 (Offset 116)
)

// 城镇打坐练功通知类型 (Mod_Player_Base 动作 40 通知子类型)
const (
	PlayerPracticeNotifyUpdate   uint8 = 89 // UPDATE_PRACTICE: 累积经验增长刷新
	PlayerPracticeNotifyStart    uint8 = 90 // START_PRACTICE: 成功进入打坐练功状态
	PlayerPracticeNotifyFinished uint8 = 91 // FINISHED_PRACTICE: 正常结束打坐练功并结算经验
	PlayerPracticeNotifyCancel   uint8 = 92 // CANCEL_PRACTICE: 取消/中断打坐练功
)

// 城镇打坐场景状态枚举 (Mod_Town_Base 状态标识)
const (
	TownPracticeStateOn       uint8 = 35 // ON_PRACTICE: 城镇中正在打坐练功
	TownPracticeStateOff      uint8 = 36 // OFF_PRACTICE: 未在打坐状态
	TownPracticeStateFinished uint8 = 37 // FINISHED_PRACTICE: 练功已完成结算
)
