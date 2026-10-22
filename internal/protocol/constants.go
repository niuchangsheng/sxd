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
	ModuleHeroMission     uint16 = 35 // 英雄副本模块 (Mod_HeroMission_Base)
	ModuleStLogin         uint16 = 94 // 跨服/网页登录模块 (Mod_StLogin_Base)
)

const (
	// ActionIDItemGetPackList 获取玩家主背包与仓库物品列表 (Module 2, Action 41 -> 0x00020029)
	ActionIDItemGetPackList uint32 = 0x00020029
	// ActionIDItemUseGrid 使用背包指定格位物品 (Module 2, Action 53 -> 0x00020035)
	ActionIDItemUseGrid uint32 = 0x00020035

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
)

// 副本扫荡状态结果码
const (
	PracticeResultSuccess        uint8 = 0 // 扫荡成功
	PracticeResultNotEnoughPower uint8 = 1 // 体力不足
	PracticeResultBagFull        uint8 = 2 // 背包已满
	PracticeResultInPractice     uint8 = 3 // 正在扫荡中
	PracticeResultNotEnoughIngot uint8 = 4 // 元宝不足
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
