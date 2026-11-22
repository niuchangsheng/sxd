// Package protocol 提供扩充玩法协议：仙界远征、圣域世界塔、奇缘宝藏、仙境派遣与三界修炼。
package protocol

import (
	"fmt"
)

// ParseActionFirstByte 解析以 UByte 结果码开头的回包。
func ParseActionFirstByte(payload []byte, name string) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%s结果码为空", name)
	}
	return payload[0], nil
}

// 仙界远征任务动作 ID (Mod_ExpeditionTask_Base, Module 430)。
const (
	ActionIDExpeditionInfo  uint32 = 0x01AE0000
	ActionIDExpeditionStart uint32 = 0x01AE0001
)

// 远征任务结果码。
const (
	ExpeditionSuccess      uint8 = 0
	ExpeditionAlreadyStart uint8 = 1
	ExpeditionOverGet      uint8 = 2
	ExpeditionFailed       uint8 = 3
)

// ExpeditionTask 表示远征任务条目。
type ExpeditionTask struct {
	MissionID int32
	State     int32
	Field2    int32
	Field3    int32
	Rewards   []EraRebirthReward
}

// BuildExpeditionInfoPacket 构造远征任务面板查询封包。
func BuildExpeditionInfoPacket() *Packet {
	return NewPacket(ActionIDExpeditionInfo, nil)
}

// BuildExpeditionStartPacket 构造开始远征任务封包。
func BuildExpeditionStartPacket(missionID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(missionID)
	return NewPacket(ActionIDExpeditionStart, w.Bytes())
}

// ParseExpeditionInfo 解析远征任务面板 [Long, Int, [Int,Int,Int,Int,[Int,Int]], Int, ...]。
func ParseExpeditionInfo(payload []byte) ([]ExpeditionTask, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	if _, err := r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("远征任务时间读取失败: %w", err)
	}
	if _, err := r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("远征任务积分读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("远征任务数量读取失败: %w", err)
	}
	tasks := make([]ExpeditionTask, 0, count)
	for i := 0; i < int(count); i++ {
		var t ExpeditionTask
		if t.MissionID, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] id 读取失败: %w", i, err)
		}
		if t.State, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 状态读取失败: %w", i, err)
		}
		if t.Field2, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 字段读取失败: %w", i, err)
		}
		if t.Field3, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 字段读取失败: %w", i, err)
		}
		rewardCount, err := r.ReadUint16()
		if err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 奖励数量读取失败: %w", i, err)
		}
		for j := 0; j < int(rewardCount); j++ {
			var reward EraRebirthReward
			if reward.ItemID, err = r.ReadInt32(); err != nil {
				return tasks, fmt.Errorf("远征任务[%d] 奖励[%d]读取失败: %w", i, j, err)
			}
			num, nErr := r.ReadInt32()
			if nErr != nil {
				return tasks, fmt.Errorf("远征任务[%d] 奖励[%d]数量读取失败: %w", i, j, nErr)
			}
			reward.Num = int64(num)
			t.Rewards = append(t.Rewards, reward)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// ParseExpeditionResult 解析远征任务动作回包首字节。
func ParseExpeditionResult(payload []byte) (uint8, error) {
	return ParseActionFirstByte(payload, "远征任务")
}

// ExpeditionResultText 返回远征任务结果码文本。
func ExpeditionResultText(code uint8) string {
	switch code {
	case ExpeditionSuccess:
		return "成功"
	case ExpeditionAlreadyStart:
		return "已开始"
	case ExpeditionOverGet:
		return "已领取"
	case ExpeditionFailed:
		return "失败"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 全网幻魔塔动作 ID (Mod_SaWorldTower_Base, Module 521)。
const (
	ActionIDSaWorldTowerOpen   uint32 = 0x02090000
	ActionIDSaWorldTowerPanel  uint32 = 0x02090001
	ActionIDSaWorldTowerDeploy uint32 = 0x02090002
	ActionIDSaWorldTowerFight  uint32 = 0x02090003
)

// 全网幻魔塔结果码。
const (
	SaWorldTowerSuccess    uint8 = 0
	SaWorldTowerFuncNoOpen uint8 = 1
	SaWorldTowerNoTime     uint8 = 2
	SaWorldTowerCDTime     uint8 = 3
	SaWorldTowerErrorID    uint8 = 4
	SaWorldTowerFailure    uint8 = 5
	SaWorldTowerNoIngot    uint8 = 6
)

// BuildSaWorldTowerOpenPacket 构造开放状态查询封包。
func BuildSaWorldTowerOpenPacket() *Packet {
	return NewPacket(ActionIDSaWorldTowerOpen, nil)
}

// BuildSaWorldTowerPanelPacket 构造幻魔塔面板查询封包。
func BuildSaWorldTowerPanelPacket() *Packet {
	return NewPacket(ActionIDSaWorldTowerPanel, nil)
}

// BuildSaWorldTowerDeployPacket 构造幻魔塔阵容查询封包。
func BuildSaWorldTowerDeployPacket(towerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(towerID)
	return NewPacket(ActionIDSaWorldTowerDeploy, w.Bytes())
}

// BuildSaWorldTowerFightPacket 构造幻魔塔挑战封包。
func BuildSaWorldTowerFightPacket(towerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(towerID)
	return NewPacket(ActionIDSaWorldTowerFight, w.Bytes())
}

// ParseSaWorldTowerTowerIDs 解析幻魔塔面板 [Int total, [Int towerID, Int, Int, Int, Int]]。
func ParseSaWorldTowerTowerIDs(payload []byte) ([]int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	if _, err := r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("幻魔塔总数读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("幻魔塔数量读取失败: %w", err)
	}
	ids := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		var id int32
		for j := 0; j < 5; j++ {
			v, err := r.ReadInt32()
			if err != nil {
				return ids, fmt.Errorf("幻魔塔[%d]字段读取失败: %w", i, err)
			}
			if j == 0 {
				id = v
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// SaWorldTowerResultText 返回全网幻魔塔结果码文本。
func SaWorldTowerResultText(code uint8) string {
	switch code {
	case SaWorldTowerSuccess:
		return "成功"
	case SaWorldTowerFuncNoOpen:
		return "功能未开放"
	case SaWorldTowerNoTime:
		return "次数不足"
	case SaWorldTowerCDTime:
		return "冷却中"
	case SaWorldTowerErrorID:
		return "编号错误"
	case SaWorldTowerFailure:
		return "失败"
	case SaWorldTowerNoIngot:
		return "元宝不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 奇缘宝藏动作 ID (Mod_FortuneBox_Base, Module 808)。
const (
	ActionIDFortuneBoxOpen    uint32 = 0x03280000
	ActionIDFortuneBoxPanel   uint32 = 0x03280001
	ActionIDFortuneBoxOpenBox uint32 = 0x03280003
	ActionIDFortuneBoxChoose  uint32 = 0x03280004
)

// 奇缘宝藏结果码。
const (
	FortuneBoxSuccess       uint8 = 0
	FortuneBoxFuncNoOpen    uint8 = 1
	FortuneBoxAlreadyBuy    uint8 = 2
	FortuneBoxNoChargeIngot uint8 = 3
	FortuneBoxNoIngot       uint8 = 4
	FortuneBoxPackageFull   uint8 = 5
	FortuneBoxFailed        uint8 = 6
	FortuneBoxMaxChooseNum  uint8 = 7
	FortuneBoxNoScore       uint8 = 8
	FortuneBoxCanNotChoose  uint8 = 9
	FortuneBoxWrongID       uint8 = 10
)

// BuildFortuneBoxOpenPacket 构造奇缘宝藏开放状态查询封包。
func BuildFortuneBoxOpenPacket() *Packet {
	return NewPacket(ActionIDFortuneBoxOpen, nil)
}

// BuildFortuneBoxPanelPacket 构造奇缘宝藏面板查询封包。
func BuildFortuneBoxPanelPacket() *Packet {
	return NewPacket(ActionIDFortuneBoxPanel, nil)
}

// BuildFortuneBoxOpenBoxPacket 构造开启宝箱封包。
func BuildFortuneBoxOpenBoxPacket(boxID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(boxID)
	return NewPacket(ActionIDFortuneBoxOpenBox, w.Bytes())
}

// BuildFortuneBoxChoosePacket 构造选择宝箱奖励封包。
func BuildFortuneBoxChoosePacket(boxID, itemID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(boxID)
	w.WriteInt32(itemID)
	return NewPacket(ActionIDFortuneBoxChoose, w.Bytes())
}

// FortuneBoxResultText 返回奇缘宝藏结果码文本。
func FortuneBoxResultText(code uint8) string {
	switch code {
	case FortuneBoxSuccess:
		return "成功"
	case FortuneBoxFuncNoOpen:
		return "功能未开放"
	case FortuneBoxAlreadyBuy:
		return "已购买"
	case FortuneBoxNoChargeIngot:
		return "充值元宝不足"
	case FortuneBoxNoIngot:
		return "元宝不足"
	case FortuneBoxPackageFull:
		return "背包已满"
	case FortuneBoxFailed:
		return "失败"
	case FortuneBoxMaxChooseNum:
		return "选择已达上限"
	case FortuneBoxNoScore:
		return "积分不足"
	case FortuneBoxCanNotChoose:
		return "不可选择"
	case FortuneBoxWrongID:
		return "编号错误"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 宠物仙域游历动作 ID (Mod_FairylandDispatch_Base, Module 714)。
const (
	ActionIDFairylandMainPanel uint32 = 0x02CA0001
	ActionIDFairylandTaskPanel uint32 = 0x02CA0002
	ActionIDFairylandDispatch  uint32 = 0x02CA0006
	ActionIDFairylandTaskAward uint32 = 0x02CA000A
)

// 宠物仙域结果码。
const (
	FairylandSuccess        uint8 = 0
	FairylandFailed         uint8 = 1
	FairylandNotSame        uint8 = 2
	FairylandIsLocked       uint8 = 3
	FairylandTopLevel       uint8 = 4
	FairylandLessResource   uint8 = 5
	FairylandEmptyGrid      uint8 = 6
	FairylandLessEnergy     uint8 = 7
	FairylandLessGrid       uint8 = 8
	FairylandLessScore      uint8 = 9
	FairylandExchangeLimit  uint8 = 10
	FairylandItemPackFull   uint8 = 11
	FairylandIsGet          uint8 = 12
	FairylandLessCharge     uint8 = 13
	FairylandNoIngot        uint8 = 14
	FairylandBuyLimit       uint8 = 15
	FairylandLessUnlockNum  uint8 = 16
	FairylandIsActived      uint8 = 17
	FairylandTimesLimited   uint8 = 18
	FairylandOrderCompleted uint8 = 19
)

// BuildFairylandMainPanelPacket 构造宠物仙域主面板查询封包。
func BuildFairylandMainPanelPacket() *Packet {
	return NewPacket(ActionIDFairylandMainPanel, nil)
}

// BuildFairylandMainPanelWithPrevActPacket 构造携带显式 PrevAct 锚点的主面板查询封包。
//
// 线上实测裸载荷 714/1 被网关静默丢弃；部分模块要求尾部携带服务端最后受理的动作号。
// 这里写入显式锚点并保持 HasPrevAct=false，避免会话发送层用被丢弃的 lastActionID 覆盖。
func BuildFairylandMainPanelWithPrevActPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFairylandMainPanel, w.Bytes())
}

// BuildFairylandTaskPanelPacket 构造宠物仙域任务面板查询封包。
func BuildFairylandTaskPanelPacket() *Packet {
	return NewPacket(ActionIDFairylandTaskPanel, nil)
}

// BuildFairylandDispatchPacket 构造宠物游历派遣封包。
func BuildFairylandDispatchPacket(orderID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(orderID)
	return NewPacket(ActionIDFairylandDispatch, w.Bytes())
}

// BuildFairylandTaskAwardPacket 构造宠物仙域任务奖励领取封包。
func BuildFairylandTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDFairylandTaskAward, w.Bytes())
}

// FairylandResultText 返回宠物仙域结果码文本。
func FairylandResultText(code uint8) string {
	switch code {
	case FairylandSuccess:
		return "成功"
	case FairylandFailed:
		return "失败"
	case FairylandNotSame:
		return "不匹配"
	case FairylandIsLocked:
		return "未解锁"
	case FairylandTopLevel:
		return "已满级"
	case FairylandLessResource:
		return "资源不足"
	case FairylandEmptyGrid:
		return "空位不足"
	case FairylandLessEnergy:
		return "体力不足"
	case FairylandLessGrid:
		return "格子不足"
	case FairylandLessScore:
		return "积分不足"
	case FairylandExchangeLimit:
		return "兑换上限"
	case FairylandItemPackFull:
		return "背包已满"
	case FairylandIsGet:
		return "已领取"
	case FairylandNoIngot:
		return "元宝不足"
	case FairylandTimesLimited:
		return "次数上限"
	case FairylandOrderCompleted:
		return "订单已完成"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 三界修炼动作 ID (Mod_ThreeWorld_Base 529 / Mod_HumanWorld_Base 531)。
const (
	ActionIDThreeWorldStatus     uint32 = 0x02110001
	ActionIDThreeWorldResetTime  uint32 = 0x02110006
	ActionIDHumanWorldPractice   uint32 = 0x0213000C
	ActionIDHumanWorldStart      uint32 = 0x0213000D
	ActionIDHumanWorldStop       uint32 = 0x0213000E
	ActionIDHumanWorldAccelerate uint32 = 0x0213000F
)

// 三界修炼状态。
const (
	ThreeWorldSuccess  uint8 = 0
	ThreeWorldFailed   uint8 = 1
	ThreeWorldClose    uint8 = 2
	ThreeWorldReset    uint8 = 3
	ThreeWorldIdle     uint8 = 4
	ThreeWorldPractice uint8 = 5
	ThreeWorldComplete uint8 = 6
	ThreeWorldWait     uint8 = 7
	ThreeWorldOpen     uint8 = 8
)

// ThreeWorldStatus 表示三界修炼状态回包。
type ThreeWorldStatus struct {
	State uint8
	Sub   uint8
	V1    int32
	V2    int32
	V3    int32
}

// BuildThreeWorldStatusPacket 构造三界状态查询封包。
func BuildThreeWorldStatusPacket() *Packet {
	return NewPacket(ActionIDThreeWorldStatus, nil)
}

// ParseThreeWorldStatus 解析 [UByte, UByte, Int, Int, Int]。
func ParseThreeWorldStatus(payload []byte) (*ThreeWorldStatus, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	st := &ThreeWorldStatus{}
	var err error
	if st.State, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("三界状态读取失败: %w", err)
	}
	if st.Sub, err = r.ReadUint8(); err != nil {
		return st, nil
	}
	st.V1, _ = r.ReadInt32()
	st.V2, _ = r.ReadInt32()
	st.V3, _ = r.ReadInt32()
	return st, nil
}

// BuildHumanWorldPracticePacket 构造人界修炼信息查询封包。
func BuildHumanWorldPracticePacket() *Packet {
	return NewPacket(ActionIDHumanWorldPractice, nil)
}

// BuildHumanWorldStartPacket 构造人界开始修炼封包。
func BuildHumanWorldStartPacket() *Packet {
	return NewPacket(ActionIDHumanWorldStart, nil)
}

// BuildHumanWorldStopPacket 构造人界停止修炼封包。
func BuildHumanWorldStopPacket() *Packet {
	return NewPacket(ActionIDHumanWorldStop, nil)
}

// ParseHumanWorldPractice 解析 [UByte result, Int x7]，返回结果码与字段。
func ParseHumanWorldPractice(payload []byte) (uint8, []int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("人界修炼结果码读取失败: %w", err)
	}
	vals := make([]int32, 0, 7)
	for i := 0; i < 7 && r.Remaining() >= 4; i++ {
		v, rErr := r.ReadInt32()
		if rErr != nil {
			break
		}
		vals = append(vals, v)
	}
	return res, vals, nil
}

// ThreeWorldResultText 返回三界/人界结果码文本。
func ThreeWorldResultText(code uint8) string {
	switch code {
	case 0:
		return "成功"
	case 1:
		return "失败"
	case 2:
		return "已关闭"
	case 5:
		return "修炼中"
	case 6:
		return "已完成"
	case 7:
		return "等待中"
	default:
		return fmt.Sprintf("状态(%d)", code)
	}
}
