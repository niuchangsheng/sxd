// Package protocol 提供奇门遁甲一/二 (Mod_MiracleFighters_Base / Mod_MiracleFightersTwo_Base)
// 的协议编解码。
//
// 官方动作（两模块一致）：
//
//	get_info        0   request=[]  response=[UByte result, Int value, Byte flag, [Int×6 entry_id]]
//	practice        1   request=[Int entry_id, UByte practice_type]
//	break_through   2   request=[Int entry_id]
//	equip           3   request=[Int entry_id, Int slot]
//	unlock          4   request=[]
//	detail_info     5   request=[]
//	one_key_practice 6  request=[Int entry_id]
package protocol

import "fmt"

const (
	// ModuleMiracleFighters 奇门遁甲一 (0x00FB)
	ModuleMiracleFighters uint16 = 251
	// ModuleMiracleFightersTwo 奇门遁甲二 (0x0188)
	ModuleMiracleFightersTwo uint16 = 392
)

// 奇门遁甲动作编号 (两模块一致)。
const (
	ActionIDMiracleFightersInfo       uint32 = 0x00FB0000
	ActionIDMiracleFightersPractice   uint32 = 0x00FB0001
	ActionIDMiracleFightersBreakThru  uint32 = 0x00FB0002
	ActionIDMiracleFightersEquip      uint32 = 0x00FB0003
	ActionIDMiracleFightersUnlock     uint32 = 0x00FB0004
	ActionIDMiracleFightersDetailInfo uint32 = 0x00FB0005
	ActionIDMiracleFightersOneKey     uint32 = 0x00FB0006

	ActionIDMiracleFightersTwoInfo       uint32 = 0x01880000
	ActionIDMiracleFightersTwoPractice   uint32 = 0x01880001
	ActionIDMiracleFightersTwoBreakThru  uint32 = 0x01880002
	ActionIDMiracleFightersTwoEquip      uint32 = 0x01880003
	ActionIDMiracleFightersTwoUnlock     uint32 = 0x01880004
	ActionIDMiracleFightersTwoDetailInfo uint32 = 0x01880005
	ActionIDMiracleFightersTwoOneKey     uint32 = 0x01880006
)

// 奇门遁甲修炼/突破结果码 (两模块基本一致，货币差异由服务端返回码体现)。
const (
	MiracleFightersResultSuccess      uint8 = 0
	MiracleFightersResultFuncUnopen   uint8 = 1
	MiracleFightersResultFailed       uint8 = 2
	MiracleFightersPracticeNormal     uint8 = 3
	MiracleFightersPracticeIngot      uint8 = 4
	MiracleFightersResultLackCoin     uint8 = 5
	MiracleFightersResultLackIngot    uint8 = 6
	MiracleFightersResultTimesLimit   uint8 = 7
	MiracleFightersResultLevelUnopen  uint8 = 8
	MiracleFightersResultLackResource uint8 = 9
	MiracleFightersResultIdError      uint8 = 10
	MiracleFightersResultRoleError    uint8 = 11
	MiracleFightersResultMainLimit    uint8 = 12
	MiracleFightersResultLimit        uint8 = 13
)

// MiracleFightersInfo 表示奇门遁甲面板。
type MiracleFightersInfo struct {
	Result uint8
	Value  int32
	Flag   uint8
	// Entries 面板给出的 6 个条目编号；修炼/突破均以该编号为参数。
	Entries []int32
}

// BuildMiracleFightersInfoPacket 构造面板查询封包。
func BuildMiracleFightersInfoPacket(actionID uint32) *Packet {
	pkt := NewPacket(actionID, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildMiracleFightersPracticePacket 构造普通修炼封包 (type=NORMAL_PRACTICE)。
func BuildMiracleFightersPracticePacket(actionID uint32, entryID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(entryID)
	w.WriteUint8(MiracleFightersPracticeNormal)
	return NewPacket(actionID, w.Bytes())
}

// BuildMiracleFightersOneKeyPacket 构造一键修炼封包。
func BuildMiracleFightersOneKeyPacket(actionID uint32, entryID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(entryID)
	return NewPacket(actionID, w.Bytes())
}

// BuildMiracleFightersBreakThroughPacket 构造突破试炼封包。
func BuildMiracleFightersBreakThroughPacket(actionID uint32, entryID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(entryID)
	return NewPacket(actionID, w.Bytes())
}

// ParseMiracleFightersInfo 解析面板回包。
func ParseMiracleFightersInfo(payload []byte) (*MiracleFightersInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	info := &MiracleFightersInfo{}
	var err error
	if info.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("奇门遁甲面板 result 读取失败: %w", err)
	}
	if info.Value, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("奇门遁甲面板 value 读取失败: %w", err)
	}
	if info.Flag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("奇门遁甲面板 flag 读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("奇门遁甲面板条目数量读取失败: %w", err)
	}
	info.Entries = make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, rErr := r.ReadInt32()
		if rErr != nil {
			return nil, fmt.Errorf("奇门遁甲面板条目[%d]读取失败: %w", i, rErr)
		}
		info.Entries = append(info.Entries, id)
	}
	return info, nil
}

// ParseMiracleFightersActionCode 解析修炼/突破回包首字节结果码。
func ParseMiracleFightersActionCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("奇门遁甲结果码为空")
	}
	return payload[0], nil
}

// MiracleFightersResultText 返回结果码文本。
func MiracleFightersResultText(code uint8) string {
	switch code {
	case MiracleFightersResultSuccess:
		return "成功"
	case MiracleFightersResultFuncUnopen:
		return "功能未开放"
	case MiracleFightersResultFailed:
		return "失败"
	case MiracleFightersResultLackCoin:
		return "铜钱不足"
	case MiracleFightersResultLackIngot:
		return "元宝不足"
	case MiracleFightersResultTimesLimit:
		return "次数不足"
	case MiracleFightersResultLevelUnopen:
		return "等级未开放"
	case MiracleFightersResultLackResource:
		return "资源不足"
	case MiracleFightersResultIdError:
		return "条目编号错误"
	case MiracleFightersResultRoleError:
		return "角色错误"
	case MiracleFightersResultMainLimit:
		return "主角限制"
	case MiracleFightersResultLimit:
		return "限制"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 奇门遁甲 PvE 试炼 (Mod_MiracleFightersPve_Base / MiracleFightersPveTwo_Base)。
const (
	// ActionIDMiracleFightersPveInfo 奇门遁甲一试炼信息 (253/0)
	ActionIDMiracleFightersPveInfo uint32 = 0x00FD0000
	// ActionIDMiracleFightersPveFight 奇门遁甲一挑战 (253/1)
	ActionIDMiracleFightersPveFight uint32 = 0x00FD0001
	// ActionIDMiracleFightersPveTwoInfo 奇门遁甲二试炼信息 (394/0)
	ActionIDMiracleFightersPveTwoInfo uint32 = 0x018A0000
	// ActionIDMiracleFightersPveTwoFight 奇门遁甲二挑战 (394/1)
	ActionIDMiracleFightersPveTwoFight uint32 = 0x018A0001
)

// 奇门遁甲 PvE 挑战结果码。
const (
	MiraclePveSuccess     uint8 = 0
	MiraclePveFuncNoOpen  uint8 = 1
	MiraclePveLevelLimit  uint8 = 2
	MiraclePveCompleteAll uint8 = 3
	MiraclePveCDTime      uint8 = 4
)
