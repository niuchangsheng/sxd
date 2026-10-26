// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现基于 haha.pcapng 逆向报文对齐的 16 项高频玩法的二进制请求封包构造与回包解析。
package protocol

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// --------------------------------------------------------------------------------
// 1. 领取奖励 (在线/系统礼包奖励)
// --------------------------------------------------------------------------------

// BuildClaimAwardPacket 构造领取指定序列号礼包封包 (Module 2, Action 29)。
func BuildClaimAwardPacket(awardSeqID uint32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(awardSeqID)
	if prevAct == 0 {
		prevAct = ActionIDVipAwardQuery
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDItemClaimAward, w.Bytes())
}

// ParseClaimAwardResult 解析礼包领取结果。
func ParseClaimAwardResult(payload []byte) (bool, error) {
	if len(payload) == 0 {
		return true, nil
	}
	r := NewReader(payload)
	code, err := r.ReadUint8()
	if err != nil {
		return false, fmt.Errorf("%w: failed to read claim code: %v", ErrInvalidPayload, err)
	}
	// 结果码 0x14 (20) 或 0x00 表示成功
	return code == ItemActionSuccess || code == ItemSuccess, nil
}

// --------------------------------------------------------------------------------
// 2. 结缘宝箱：除了传说，其他都开
// --------------------------------------------------------------------------------

// JieYuanBoxItem 表示玩家背包持有的结缘宝箱条目。
type JieYuanBoxItem struct {
	ItemID int32 // 宝箱道具 ID (3937..3942)
	Count  int32 // 持有数量
}

// BuildJieYuanBoxListPacket 构造查询结缘宝箱列表封包 (Module 232, Action 30)。
func BuildJieYuanBoxListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDJieYuanBoxList, w.Bytes())
}

// ParseJieYuanBoxListResult 解析结缘宝箱列表回包。
func ParseJieYuanBoxListResult(payload []byte) ([]JieYuanBoxItem, error) {
	if len(payload) < 2 {
		return nil, nil
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read box count: %v", ErrInvalidPayload, err)
	}
	var items []JieYuanBoxItem
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			break
		}
		itemCount, err := r.ReadInt32()
		if err != nil {
			break
		}
		items = append(items, JieYuanBoxItem{ItemID: itemID, Count: itemCount})
	}
	return items, nil
}

// FilterOpenableJieYuanBoxes 按照规则“除了传说，其他都开”过滤出允许开启的宝箱。
// 传说结缘宝箱 (ItemJieYuanBoxLegendary = 3942) 被严格剔除。
func FilterOpenableJieYuanBoxes(boxes []JieYuanBoxItem) []JieYuanBoxItem {
	var openable []JieYuanBoxItem
	for _, b := range boxes {
		if b.Count <= 0 {
			continue
		}
		// 严正过滤：跳过传说结缘宝箱
		if b.ItemID == ItemJieYuanBoxLegendary {
			continue
		}
		openable = append(openable, b)
	}
	return openable
}

// BuildJieYuanBoxOpenContextPacket 构造结缘宝箱开箱前置上下文协议包 (Module 528, Action 1)。
func BuildJieYuanBoxOpenContextPacket(prevAct uint32) *Packet {
	w := NewWriter()
	// 依据 haha.pcapng 帧实采: 07 04 + PrevAct
	w.WriteUint16(0x0704)
	if prevAct == 0 {
		prevAct = ActionIDJieYuanBoxList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDJieYuanBoxOpenContext, w.Bytes())
}

// BuildJieYuanBoxOpenPacket 构造批量开启结缘宝箱请求封包 (Module 232, Action 34)。
func BuildJieYuanBoxOpenPacket(itemID int32, openCount int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(openCount)
	if prevAct == 0 {
		prevAct = ActionIDJieYuanBoxOpenContext
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDJieYuanBoxOpen, w.Bytes())
}

// --------------------------------------------------------------------------------
// 3. 领取日活跃奖励
// --------------------------------------------------------------------------------

// BuildAssistantClaimRewardPacket 构造小助手日活跃奖励领取封包 (Module 293, Action 2)。
func BuildAssistantClaimRewardPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDAssistantClaimReward, w.Bytes())
}

// --------------------------------------------------------------------------------
// 4. 奇门遁甲
// --------------------------------------------------------------------------------

// BuildQiMenArrayInfoPacket 构造查询奇门法阵封包 (Module 486, Action 7)。
func BuildQiMenArrayInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0) // Type
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenArrayInfo, w.Bytes())
}

// BuildQiMenPuppetInfoPacket 构造查询阵眼傀儡属性封包 (Module 485, Action 0)。
func BuildQiMenPuppetInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenArrayInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenPuppetInfo, w.Bytes())
}

// BuildQiMenUpgradeStarPacket 构造奇门星图升级注灵封包 (Module 486, Action 1)。
func BuildQiMenUpgradeStarPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0)
	if prevAct == 0 {
		prevAct = ActionIDQiMenPuppetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenUpgradeStar, w.Bytes())
}

// BuildQiMenFormationDeployPacket 构造奇门遁甲布阵生效封包 (Module 584, Action 1)。
func BuildQiMenFormationDeployPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenUpgradeStar
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenFormationDeploy, w.Bytes())
}

// BuildQiMenFormationConfirmPacket 构造奇门遁甲阵法确认封包 (Module 584, Action 3)。
func BuildQiMenFormationConfirmPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenFormationDeploy
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenFormationConfirm, w.Bytes())
}

// --------------------------------------------------------------------------------
// 5. 淬炼粗铁
// --------------------------------------------------------------------------------

// BuildRefinePoolPanelPacket 构造查询淬炼池面板封包 (Module 534, Action 5)。
func BuildRefinePoolPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(4) // 默认面板类型
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDRefinePoolPanel, w.Bytes())
}

// BuildRefineExecutePacket 构造提交粗铁淬炼提纯封包 (Module 282, Action 1)。
func BuildRefineExecutePacket(prevAct uint32) *Packet {
	w := NewWriter()
	// 依据抓包实采: 06 07 0a 紧随 PrevAct
	w.WriteUint8(0x06)
	w.WriteUint8(0x07)
	w.WriteUint8(0x0a)
	if prevAct == 0 {
		prevAct = ActionIDRefinePoolPanel
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDRefineExecute, w.Bytes())
}

// --------------------------------------------------------------------------------
// 6. 天神赐福
// --------------------------------------------------------------------------------

// BuildGodBlessClaimPacket 构造领取跨服天神赐福封包 (Module 52, Action 0)。
func BuildGodBlessClaimPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDGodBlessClaim, w.Bytes())
}

// --------------------------------------------------------------------------------
// 7. 劫镖
// --------------------------------------------------------------------------------

// BuildCaravanGetListPacket 构造查询劫镖运送队伍列表封包 (Module 314, Action 1)。
func BuildCaravanGetListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanGetList, w.Bytes())
}

// BuildCaravanGetDetailPacket 构造查询指定劫镖目标详情封包 (Module 314, Action 0)。
func BuildCaravanGetDetailPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDCaravanGetList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanGetDetail, w.Bytes())
}

// BuildCaravanHijackPacket 构造发起打劫拦截封包 (Module 314, Action 12)。
// 依据 haha.pcapng 抓包事实: 载荷仅包含 PrevAct (0x013A0000)。
func BuildCaravanHijackPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDCaravanGetDetail
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanHijack, w.Bytes())
}

// BuildCaravanSettlePacket 构造劫镖战斗结算封包 (Module 314, Action 2)。
func BuildCaravanSettlePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDCaravanHijack
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanSettle, w.Bytes())
}

// --------------------------------------------------------------------------------
// 8. 寻仙之路
// --------------------------------------------------------------------------------

// BuildXunXianGetProgressPacket 构造查询寻仙之路进度封包 (Module 466, Action 1)。
func BuildXunXianGetProgressPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianGetProgress, w.Bytes())
}

// BuildXunXianChallengePacket 构造挑战寻仙之路关卡节点封包 (Module 466, Action 3)。
func BuildXunXianChallengePacket(nodeID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(nodeID)
	if prevAct == 0 {
		prevAct = ActionIDXunXianGetProgress
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianChallenge, w.Bytes())
}

// BuildXunXianClaimChestPacket 构造领取寻仙之路通关宝箱封包 (Module 466, Action 13)。
func BuildXunXianClaimChestPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDXunXianChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianClaimChest, w.Bytes())
}

// --------------------------------------------------------------------------------
// 9. 锁妖塔
// --------------------------------------------------------------------------------

// BuildDemonTowerEnterPacket 构造进入锁妖塔指定层封包 (Module 701, Action 1)。
func BuildDemonTowerEnterPacket(layer int32, trialID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(layer)
	w.WriteInt32(trialID)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDDemonTowerEnter, w.Bytes())
}

// BuildDemonTowerBuffPacket 构造锁妖塔战斗 Buff 选择封包 (Module 705, Action 6)。
func BuildDemonTowerBuffPacket(buffID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(buffID)
	if prevAct == 0 {
		prevAct = ActionIDDemonTowerEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDDemonTowerBuff, w.Bytes())
}

// BuildDemonTowerBattlePacket 构造锁妖塔战斗触发与结算封包 (Module 705, Action 1)。
func BuildDemonTowerBattlePacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(1)
	if prevAct == 0 {
		prevAct = ActionIDDemonTowerDeploy
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDDemonTowerBattle, w.Bytes())
}

// ParseDemonTowerPanelResult 解析锁妖塔主面板回包 (Module 701, Action 0)，提取当前所在层与试炼怪编号。
func ParseDemonTowerPanelResult(payload []byte) (layer int32, trialID int32, err error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 8 {
		return 3, 31, nil // 默认兜底层
	}
	r := NewReader(decomp)
	layer, err = r.ReadInt32()
	if err != nil {
		return 3, 31, nil
	}
	trialID, err = r.ReadInt32()
	if err != nil {
		return layer, 31, nil
	}
	return layer, trialID, nil
}

// --------------------------------------------------------------------------------
// 10. 山河游历
// --------------------------------------------------------------------------------

// BuildEraTravelGetStatePacket 构造查询山河游历状态封包 (Module 850, Action 4)。
func BuildEraTravelGetStatePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraTravelGetState, w.Bytes())
}

// BuildEraTravelRollDicePacket 构造山河游历掷骰移动封包 (Module 850, Action 0)。
func BuildEraTravelRollDicePacket(step int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(step)
	if prevAct == 0 {
		prevAct = ActionIDEraTravelGetState
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraTravelRollDice, w.Bytes())
}

// --------------------------------------------------------------------------------
// 11. 灵道初解
// --------------------------------------------------------------------------------

// BuildLingDaoGetStatusPacket 构造查询灵道初解进度封包 (Module 658, Action 0)。
func BuildLingDaoGetStatusPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDLingDaoGetStatus, w.Bytes())
}

// BuildLingDaoComprehendPacket 构造提交灵道领悟封包 (Module 711, Action 0)。
func BuildLingDaoComprehendPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDLingDaoGetStatus
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDLingDaoComprehend, w.Bytes())
}

// --------------------------------------------------------------------------------
// 12. 山海幻兽
// --------------------------------------------------------------------------------

// BuildHuanshouPanelPacket 构造查询山海幻兽主面板封包 (Module 701, Action 0)。
func BuildHuanshouPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouPanel, w.Bytes())
}

// BuildHuanshouInvadeChallengePacket 构造挑战幻兽入侵副本封包 (Module 696, Action 2)。
// 依据 haha.pcapng 抓包事实: 载荷仅包含 PrevAct。
func BuildHuanshouInvadeChallengePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDHuanshouPanel
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeChallenge, w.Bytes())
}

// BuildHuanshouInvadeNodePacket 构造山海幻兽入侵逐关挑战扫荡封包 (Module 696, Action 4)。
func BuildHuanshouInvadeNodePacket(nodeID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(nodeID)
	if prevAct == 0 {
		prevAct = ActionIDHuanshouInvadeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeSettle, w.Bytes())
}

// BuildHuanshouInvadeSweepPacket 构造秒完成幻兽入侵扫荡封包 (Module 696, Action 3)。
func BuildHuanshouInvadeSweepPacket(prevAct uint32) *Packet {
	w := NewWriter()
	// 依据抓包实采: 00 0f cc af 06 + PrevAct
	w.WriteUint32(0x000FCCAF)
	w.WriteUint8(0x06)
	if prevAct == 0 {
		prevAct = ActionIDHuanshouInvadeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeSweep, w.Bytes())
}

// --------------------------------------------------------------------------------
// 14. 悟法领域
// --------------------------------------------------------------------------------

// BuildWuFaFieldInfoPacket 构造查询悟法领域属性封包 (Module 658, Action 1)。
func BuildWuFaFieldInfoPacket(fieldID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(fieldID)
	w.WriteUint8(0)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDWuFaFieldInfo, w.Bytes())
}

// BuildWuFaDrawCardPacket 构造悟法抽卡感悟封包 (Module 658, Action 0)。
func BuildWuFaDrawCardPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDWuFaFieldInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDWuFaDrawCard, w.Bytes())
}

// --------------------------------------------------------------------------------
// 15. 宗师修行
// --------------------------------------------------------------------------------

// BuildMasterPracticePanelPacket 构造查询宗师修行主面板封包 (Module 665, Action 1)。
func BuildMasterPracticePanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMasterPracticePanel, w.Bytes())
}

// BuildMasterPracticeSelectSkillPacket 构造选定宗师修行心法封包 (Module 665, Action 3)。
func BuildMasterPracticeSelectSkillPacket(realmID int32, skillID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(realmID)
	w.WriteInt32(skillID)
	if prevAct == 0 {
		prevAct = ActionIDMasterPracticePanel
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMasterPracticeSelectSkill, w.Bytes())
}

// BuildMasterPracticeChallengePacket 构造发起宗师修行试炼挑战封包 (Module 665, Action 2)。
func BuildMasterPracticeChallengePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDMasterPracticeSelectSkill
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMasterPracticeChallenge, w.Bytes())
}

// BuildMasterPracticeFastFinishPacket 构造宗师试炼极速通关封包 (Module 665, Action 4)。
// 依据抓包实采: 00 0f cc af 09 + PrevAct
func BuildMasterPracticeFastFinishPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0x000FCCAF)
	w.WriteUint8(0x09)
	if prevAct == 0 {
		prevAct = ActionIDMasterPracticeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMasterPracticeSettle, w.Bytes())
}

// BuildMasterPracticeBreakthroughPacket 构造突破宗师境界封包 (Module 665, Action 5)。
func BuildMasterPracticeBreakthroughPacket(realmID int32, skillID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(realmID)
	w.WriteInt32(skillID)
	if prevAct == 0 {
		prevAct = ActionIDMasterPracticeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMasterPracticeBreakthrough, w.Bytes())
}

// --------------------------------------------------------------------------------
// 16. 天帝宝库
// --------------------------------------------------------------------------------

// TreasureAwardSelect 自选奖品设定条目。
type TreasureAwardSelect struct {
	TierID int32
	ItemID int32
}

// BuildTreasureHouseInfoPacket 构造查询天帝宝库钥匙与信息封包 (Module 594, Action 2)。
func BuildTreasureHouseInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTreasureHouseInfo, w.Bytes())
}

// BuildTreasureHousePoolPacket 构造拉取天帝宝库自选奖池封包 (Module 594, Action 9)。
func BuildTreasureHousePoolPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTreasureHousePool, w.Bytes())
}

// BuildTreasureHouseSelectPacket 构造设定自选大奖封包 (Module 594, Action 3)。
func BuildTreasureHouseSelectPacket(selections []TreasureAwardSelect, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(selections)))
	for _, s := range selections {
		w.WriteInt32(s.TierID)
		w.WriteInt32(s.ItemID)
	}
	if prevAct == 0 {
		prevAct = ActionIDTreasureHousePool
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTreasureHouseSelect, w.Bytes())
}

// BuildTreasureHouseConfirmPacket 构造锁定确认自选大奖封包 (Module 594, Action 4)。
func BuildTreasureHouseConfirmPacket(confirmID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(confirmID)
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseSelect
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTreasureHouseConfirm, w.Bytes())
}

// BuildTreasureHouseRefreshPacket 构造刷新天帝宝库状态封包 (Module 594, Action 1)。
func BuildTreasureHouseRefreshPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseConfirm
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTreasureHouseRefresh, w.Bytes())
}

// BuildTreasureHouseDrawPacket 构造开启天帝宝库抽奖封包 (Module 594, Action 6)。
func BuildTreasureHouseDrawPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseRefresh
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTreasureHouseDraw, w.Bytes())
}

// Helper: DecompressPayloadIfNeeded 透明尝试解压 zlib 载荷
func DecompressPayloadIfNeeded(payload []byte) []byte {
	if len(payload) >= 2 && payload[0] == 0x78 {
		zr, err := zlib.NewReader(bytes.NewReader(payload))
		if err == nil {
			defer zr.Close()
			decomp, err := io.ReadAll(zr)
			if err == nil {
				return decomp
			}
		}
	}
	return payload
}
