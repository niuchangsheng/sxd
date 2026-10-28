// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件归档基于真实网络抓包 haha.pcapng 逆向还原的高级业务模块常量字典。
package protocol

const (
	// 扩展业务模块编号 (Module)
	ModuleVipAward           uint16 = 42  // VIP与在线礼包模块 (0x002A)
	ModuleStGodWelfare       uint16 = 52  // 跨服天神赐福与全服福利模块 (0x0034)
	ModuleWorshipMarsExt     uint16 = 24  // 拜祭先祖/关公模块 (0x0018)
	ModuleActivityWelfare    uint16 = 57  // 运营活动福利模块 (0x0039)
	ModuleJieYuanBox         uint16 = 232 // 结缘宝箱与缘分模块 (0x00E8)
	ModuleRefinePoolExec     uint16 = 282 // 粗铁淬炼执行模块 (0x011A)
	ModuleDailyAssistant     uint16 = 293 // 小助手活跃度领奖模块 (0x0125)
	ModuleCaravanRob         uint32 = 314 // 阵营商队劫镖模块 (0x013A)
	ModuleTakeBible          uint16 = 30  // 西天取经与护送取经模块 (0x001E)
	ModuleStTakeBible        uint16 = 114 // 圣域/跨服取经模块 (0x0072)
	ModuleSeekImmortal       uint16 = 466 // 寻仙之路副本模块 (0x01D2)
	ModuleQiMenPuppet        uint16 = 485 // 奇门遁甲阵眼傀儡模块 (0x01E5)
	ModuleQiMenArray         uint16 = 486 // 奇门遁甲法阵星图模块 (0x01E6)
	ModuleJieYuanContext     uint16 = 528 // 结缘开箱前置上下文握手模块 (0x0210)
	ModuleRefinePoolPanel    uint16 = 534 // 淬炼池面板与资源模块 (0x0216)
	ModuleQiMenFormation    uint16 = 584 // 奇门遁甲布阵与属性模块 (0x0248)
	ModuleTreasureHouse      uint16 = 594 // 天帝宝库抽奖与自选奖池模块 (0x0252)
	ModuleEraEvent           uint16 = 635 // 山河纪元游历奇遇事件模块 (0x027B)
	ModuleWuFaLingDao        uint16 = 658 // 悟法领域与灵道初解模块 (0x0292)
	ModuleMasterPractice     uint16 = 665 // 宗师修行与突破模块 (0x0299)
	ModuleEudemonsLottery    uint16 = 693 // 幻兽宝珠抽奖模块 (0x02B5)
	ModuleEudemonsFeed       uint16 = 694 // 幻兽灵石喂养模块 (0x02B6)
	ModuleEudemonsDeploy     uint16 = 695 // 幻兽阵位出战模块 (0x02B7)
	ModuleEudemonsInvade     uint16 = 696 // 山海幻兽入侵副本战斗模块 (0x02B8)
	ModuleDemonTower         uint16 = 701 // 锁妖塔/镇妖塔与幻兽主面板模块 (0x02BD)
	ModuleDemonBuff          uint16 = 705 // 锁妖塔战斗加成与状态模块 (0x02C1)
	ModuleLingDaoComprehend  uint16 = 711 // 灵道初解领悟与领域技能装配模块 (0x02C7)
	ModuleEraTravel          uint16 = 850 // 山河游历骰子与寻宝模块 (0x0352)
)

const (
	// 结缘宝箱道具全局 ID (来源于 Pieb.db 静态字典)
	ItemJieYuanBoxCommon    int32 = 3937 // 普通结缘宝箱 (绿)
	ItemJieYuanBoxAdvanced  int32 = 3938 // 高级结缘宝箱 (蓝)
	ItemJieYuanBoxFree      int32 = 3939 // 免费普通结缘宝箱
	ItemJieYuanBoxRare      int32 = 3940 // 稀有结缘宝箱 (紫)
	ItemJieYuanBoxSupreme   int32 = 3941 // 至尊结缘宝箱 (金)
	ItemJieYuanBoxLegendary int32 = 3942 // 传说结缘宝箱 (红 - 规则声明严格跳过，不可开启)
)

const (
	// ActionIDItemClaimAward 领取在线/礼包奖励 (Module 2, Action 29 -> 0x0002001D)
	ActionIDItemClaimAward uint32 = 0x0002001D
	// ActionIDVipAwardQuery 查询VIP/在线奖励面板 (Module 42, Action 17 -> 0x002A0011)
	ActionIDVipAwardQuery uint32 = 0x002A0011

	// ActionIDJieYuanBoxList 查询结缘宝箱库存列表 (Module 232, Action 30 -> 0x00E8001E)
	ActionIDJieYuanBoxList uint32 = 0x00E8001E
	// ActionIDJieYuanBoxOpenContext 开启结缘宝箱前置握手 (Module 528, Action 1 -> 0x02100001)
	ActionIDJieYuanBoxOpenContext uint32 = 0x02100001
	// ActionIDJieYuanBoxOpen 批量开启结缘宝箱 (Module 232, Action 34 -> 0x00E80022)
	ActionIDJieYuanBoxOpen uint32 = 0x00E80022

	// ActionIDAssistantClaimReward 领取日活跃度小助手宝箱 (Module 293, Action 2 -> 0x01250002)
	ActionIDAssistantClaimReward uint32 = 0x01250002

	// ActionIDQiMenArrayInfo 查询奇门遁甲法阵信息 (Module 486, Action 7 -> 0x01E60007)
	ActionIDQiMenArrayInfo uint32 = 0x01E60007
	// ActionIDQiMenPuppetInfo 查询阵眼傀儡属性 (Module 485, Action 0 -> 0x01E50000)
	ActionIDQiMenPuppetInfo uint32 = 0x01E50000
	// ActionIDQiMenUpgradeStar 奇门星图升级注灵 (Module 486, Action 1 -> 0x01E60001)
	ActionIDQiMenUpgradeStar uint32 = 0x01E60001
	// ActionIDQiMenFormationDeploy 奇门遁甲布阵生效 (Module 584, Action 1 -> 0x02480001)
	ActionIDQiMenFormationDeploy uint32 = 0x02480001
	// ActionIDQiMenFormationConfirm 奇门遁甲阵法确认 (Module 584, Action 3 -> 0x02480003)
	ActionIDQiMenFormationConfirm uint32 = 0x02480003

	// ActionIDRefinePoolPanel 查询粗铁淬炼池面板 (Module 534, Action 5 -> 0x02160005)
	ActionIDRefinePoolPanel uint32 = 0x02160005
	// ActionIDRefineExecute 执行粗铁淬炼提纯 (Module 282, Action 1 -> 0x011A0001)
	ActionIDRefineExecute uint32 = 0x011A0001

	// ActionIDGodBlessClaim 领取跨服天神赐福点数 (Module 52, Action 0 -> 0x00340000)
	ActionIDGodBlessClaim uint32 = 0x00340000
	// ActionIDGodBlessNotify 天神赐福状态推送 (Module 558, Action 2 -> 0x022E0002)
	ActionIDGodBlessNotify uint32 = 0x022E0002

	// ActionIDCaravanGetList 查询劫镖队伍列表 (Module 314, Action 1 -> 0x013A0001)
	ActionIDCaravanGetList uint32 = 0x013A0001
	// ActionIDCaravanGetDetail 查询劫镖目标详情 (Module 314, Action 0 -> 0x013A0000)
	ActionIDCaravanGetDetail uint32 = 0x013A0000
	// ActionIDCaravanHijack 发起劫镖拦截攻击 (Module 314, Action 12 -> 0x013A000C)
	ActionIDCaravanHijack uint32 = 0x013A000C
	// ActionIDCaravanSettle 劫镖战斗结果结算 (Module 314, Action 2 -> 0x013A0002)
	ActionIDCaravanSettle uint32 = 0x013A0002

	// ActionIDTakeBibleOpen 打开西天取经主面板 (Module 30, Action 0 -> 0x001E0000)
	ActionIDTakeBibleOpen uint32 = 0x001E0000
	// ActionIDTakeBibleClose 关闭取经面板 (Module 30, Action 1 -> 0x001E0001)
	ActionIDTakeBibleClose uint32 = 0x001E0001
	// ActionIDTakeBibleGetInfo 查询当前取经准备状态、使者类型与保护列表 (Module 30, Action 2 -> 0x001E0002)
	ActionIDTakeBibleGetInfo uint32 = 0x001E0002
	// ActionIDTakeBibleApplyFriend 申请指定好友护送取经 (Module 30, Action 3 -> 0x001E0003)
	ActionIDTakeBibleApplyFriend uint32 = 0x001E0003
	// ActionIDTakeBibleCancelApply 取消好友护送申请 (Module 30, Action 4 -> 0x001E0004)
	ActionIDTakeBibleCancelApply uint32 = 0x001E0004
	// ActionIDTakeBibleApproveApply 同意好友护送申请 (Module 30, Action 5 -> 0x001E0005)
	ActionIDTakeBibleApproveApply uint32 = 0x001E0005
	// ActionIDTakeBibleRejectApply 拒绝好友护送申请 (Module 30, Action 6 -> 0x001E0006)
	ActionIDTakeBibleRejectApply uint32 = 0x001E0006
	// ActionIDTakeBibleBuyProtection 购买元宝护送保护 (Module 30, Action 7 -> 0x001E0007)
	ActionIDTakeBibleBuyProtection uint32 = 0x001E0007
	// ActionIDTakeBibleRefresh 刷新取经使者等级 (Module 30, Action 8 -> 0x001E0008)
	ActionIDTakeBibleRefresh uint32 = 0x001E0008
	// ActionIDTakeBibleGetPlayer 查询取经路线上指定玩家队伍详情 (Module 30, Action 9 -> 0x001E0009)
	ActionIDTakeBibleGetPlayer uint32 = 0x001E0009
	// ActionIDTakeBibleStart 正式启程开始护送取经 (Module 30, Action 10 -> 0x001E000A)
	ActionIDTakeBibleStart uint32 = 0x001E000A
	// ActionIDTakeBibleRob 发起拦截/打劫他人取经队伍 (Module 30, Action 11 -> 0x001E000B)
	ActionIDTakeBibleRob uint32 = 0x001E000B
	// ActionIDTakeBibleNotify 取经事件推送 (Module 30, Action 12 -> 0x001E000C)
	ActionIDTakeBibleNotify uint32 = 0x001E000C
	// ActionIDTakeBibleQuickly 极速到达/加速取经完成 (Module 30, Action 14 -> 0x001E000E)
	ActionIDTakeBibleQuickly uint32 = 0x001E000E
	// ActionIDTakeBibleGetRobCD 查询拦截打劫冷却时间与次数 (Module 30, Action 16 -> 0x001E0010)
	ActionIDTakeBibleGetRobCD uint32 = 0x001E0010
	// ActionIDTakeBibleClearRobCD 清除拦截冷却倒计时 (Module 30, Action 17 -> 0x001E0011)
	ActionIDTakeBibleClearRobCD uint32 = 0x001E0011
	// ActionIDTakeBibleBuyBless 购买取经鼓舞祝福 (Module 30, Action 18 -> 0x001E0012)
	ActionIDTakeBibleBuyBless uint32 = 0x001E0012

	// ActionIDXunXianGetProgress 查询寻仙之路关卡进度 (Module 466, Action 1 -> 0x01D20001)
	ActionIDXunXianGetProgress uint32 = 0x01D20001
	// ActionIDXunXianChallenge 挑战寻仙之路关卡节点 (Module 466, Action 3 -> 0x01D20003)
	ActionIDXunXianChallenge uint32 = 0x01D20003
	// ActionIDXunXianClaimChest 领取寻仙之路通关宝箱 (Module 466, Action 13 -> 0x01D2000D)
	ActionIDXunXianClaimChest uint32 = 0x01D2000D

	// ActionIDDemonTowerEnter 进入挑战锁妖塔指定层 (Module 701, Action 1 -> 0x02BD0001)
	ActionIDDemonTowerEnter uint32 = 0x02BD0001
	// ActionIDDemonTowerBuff 锁妖塔战斗Buff选择 (Module 705, Action 6 -> 0x02C10006)
	ActionIDDemonTowerBuff uint32 = 0x02C10006
	// ActionIDDemonTowerBattle 锁妖塔战斗挑战/结算 (Module 705, Action 1 -> 0x02C10001)
	ActionIDDemonTowerBattle uint32 = 0x02C10001
	// ActionIDDemonTowerDeploy 锁妖塔阵型校验 (Module 312, Action 38 -> 0x01380026)
	ActionIDDemonTowerDeploy uint32 = 0x01380026

	// ActionIDEraTravelGetState 查询山河纪元游历状态 (Module 850, Action 4 -> 0x03520004)
	ActionIDEraTravelGetState uint32 = 0x03520004
	// ActionIDEraTravelRollDice 山河游历掷骰子移动 (Module 850, Action 0 -> 0x03520000)
	ActionIDEraTravelRollDice uint32 = 0x03520000
	// ActionIDEraEventSettle 触发山河画卷奇遇事件 (Module 635, Action 0 -> 0x027B0000)
	ActionIDEraEventSettle uint32 = 0x027B0000

	// ActionIDLingDaoGetStatus 查询灵道初解任务状态 (Module 658, Action 0 -> 0x02920000)
	ActionIDLingDaoGetStatus uint32 = 0x02920000
	// ActionIDLingDaoComprehend 提交灵道初解任务领悟 (Module 711, Action 0 -> 0x02C70000)
	ActionIDLingDaoComprehend uint32 = 0x02C70000

	// ActionIDHuanshouPanel 查询山海幻兽主面板 (Module 701, Action 0 -> 0x02BD0000)
	ActionIDHuanshouPanel uint32 = 0x02BD0000
	// ActionIDHuanshouInvadeInfo 查询幻兽入侵关卡信息 (Module 696, Action 0 -> 0x02B80000)
	ActionIDHuanshouInvadeInfo uint32 = 0x02B80000
	// ActionIDHuanshouInvadeChapter 选定幻兽入侵章节 (Module 696, Action 1 -> 0x02B80001)
	ActionIDHuanshouInvadeChapter uint32 = 0x02B80001
	// ActionIDHuanshouInvadeChallenge 挑战幻兽入侵副本 (Module 696, Action 2 -> 0x02B80002)
	ActionIDHuanshouInvadeChallenge uint32 = 0x02B80002
	// ActionIDHuanshouInvadeSweep 极速秒通关幻兽入侵 (Module 696, Action 3 -> 0x02B80003)
	ActionIDHuanshouInvadeSweep uint32 = 0x02B80003
	// ActionIDHuanshouInvadeSettle 幻兽入侵战斗结算 (Module 696, Action 4 -> 0x02B80004)
	ActionIDHuanshouInvadeSettle uint32 = 0x02B80004

	// ActionIDWuFaFieldInfo 查询悟法领域属性 (Module 658, Action 1 -> 0x02920001)
	ActionIDWuFaFieldInfo uint32 = 0x02920001
	// ActionIDWuFaDrawCard 悟法领域感悟抽卡 (Module 658, Action 0 -> 0x02920000)
	ActionIDWuFaDrawCard uint32 = 0x02920000

	// ActionIDMasterPracticePanel 查询宗师修行面板与境界 (Module 665, Action 1 -> 0x02990001)
	ActionIDMasterPracticePanel uint32 = 0x02990001
	// ActionIDMasterPracticeSelectSkill 选定宗师修行心法 (Module 665, Action 3 -> 0x02990003)
	ActionIDMasterPracticeSelectSkill uint32 = 0x02990003
	// ActionIDMasterPracticeChallenge 发起宗师修行试炼挑战 (Module 665, Action 2 -> 0x02990002)
	ActionIDMasterPracticeChallenge uint32 = 0x02990002
	// ActionIDMasterPracticeSettle 上报宗师修行试炼结果 (Module 665, Action 4 -> 0x02990004)
	ActionIDMasterPracticeSettle uint32 = 0x02990004
	// ActionIDMasterPracticeBreakthrough 突破宗师境界 (Module 665, Action 5 -> 0x02990005)
	ActionIDMasterPracticeBreakthrough uint32 = 0x02990005

	// ActionIDTreasureHouseInfo 查询天帝宝库钥匙与信息 (Module 594, Action 2 -> 0x02520002)
	ActionIDTreasureHouseInfo uint32 = 0x02520002
	// ActionIDTreasureHousePool 拉取天帝宝库自选奖池 (Module 594, Action 9 -> 0x02520009)
	ActionIDTreasureHousePool uint32 = 0x02520009
	// ActionIDTreasureHouseSelect 设定自选大奖 (Module 594, Action 3 -> 0x02520003)
	ActionIDTreasureHouseSelect uint32 = 0x02520003
	// ActionIDTreasureHouseConfirm 锁定确认自选大奖 (Module 594, Action 4 -> 0x02520004)
	ActionIDTreasureHouseConfirm uint32 = 0x02520004
	// ActionIDTreasureHouseRefresh 刷新天帝宝库状态 (Module 594, Action 1 -> 0x02520001)
	ActionIDTreasureHouseRefresh uint32 = 0x02520001
	// ActionIDTreasureHouseDraw 开启天帝宝库抽奖 (Module 594, Action 6 -> 0x02520006)
	ActionIDTreasureHouseDraw uint32 = 0x02520006
)
