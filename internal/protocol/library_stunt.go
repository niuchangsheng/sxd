// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方技能库/绝技协议 (Mod_Library_Base, Module 85 / 0x0055) 中
// 与宗师修行职业切换相关的部分：
//   - get_artifact_stunt_info        Act 5 -> 0x00550005  response [[Byte job_id, Int stunt_id, Int next_stunt_id, Byte is_active, Byte is_use]]
//     (职业 -> 已激活神器绝技；切换主角职业应使用此列表)
//   - get_master_practice_stunt_info Act 7 -> 0x00550007  同上布局，但未激活时 is_active=0，服务端会静默丢弃切换请求
//   - use_stunt_skill                Act 3 -> 0x00550003  request [Short stunt_id], response [UByte result, Short cur_role_id, Short cur_stunt_id]
//
// 官方客户端依据: com.protocols.Mod_Library_Base、com.datas.LibraryData、
// com.views.RoleMsgView.selectStunt / LibraryView.onTecSelected。
// use_stunt_skill 成功后服务端会改写主角当前 role_id (LibraryData.on_use_stunt_skill
// 同步更新 _data.player.mainRoleId)，这正是宗师修行切换主角职业的官方途径。
package protocol

import (
	"fmt"
	"math"
)

const (
	// ActionIDLibraryUseStuntSkill 使用/切换主角绝技 (Action 3 -> 0x00550003)
	ActionIDLibraryUseStuntSkill uint32 = 0x00550003
	// ActionIDLibraryArtifactStuntInfo 查询神器绝技映射 (Action 5 -> 0x00550005)
	// 该列表中的绝技为已激活的职业技能，是切换主角职业的正确来源。
	ActionIDLibraryArtifactStuntInfo uint32 = 0x00550005
	// ActionIDLibraryMasterPracticeStuntInfo 查询宗师修行绝技映射 (Action 7 -> 0x00550007)
	// 注意: 线上未激活的宗师绝技 role_stunt_id=0/is_active=0，不能直接用于 use_stunt_skill。
	ActionIDLibraryMasterPracticeStuntInfo uint32 = 0x00550007
)

// 技能库结果码 (Mod_Library_Base)。
const (
	// LibraryResultYes 是
	LibraryResultYes uint8 = 0
	// LibraryResultNo 否
	LibraryResultNo uint8 = 1
	// LibraryResultSuccess 成功
	LibraryResultSuccess uint8 = 2
	// LibraryResultPlayerLevelLimit 玩家等级不足
	LibraryResultPlayerLevelLimit uint8 = 3
	// LibraryResultNotEnoughXianLing 仙灵不足
	LibraryResultNotEnoughXianLing uint8 = 4
	// LibraryResultNotEnoughStarSoul 星魂不足
	LibraryResultNotEnoughStarSoul uint8 = 5
	// LibraryResultOtherRoleJobLimit 其它角色职业限制
	LibraryResultOtherRoleJobLimit uint8 = 6
	// LibraryResultMainRoleJobLimit 主角职业限制
	LibraryResultMainRoleJobLimit uint8 = 7
	// LibraryResultFullLevel 已满级
	LibraryResultFullLevel uint8 = 8
	// LibraryResultInvalidRoleJobID 职业编号无效
	LibraryResultInvalidRoleJobID uint8 = 9
	// LibraryResultNoHave 未拥有/未学习
	LibraryResultNoHave uint8 = 10
	// LibraryResultNotEnoughCoin 铜钱不足
	LibraryResultNotEnoughCoin uint8 = 11
)

// LibraryStuntInfo 描述某个职业的宗师修行绝技状态。
type LibraryStuntInfo struct {
	// RoleJobID 职业 JobId
	RoleJobID int32
	// RoleStuntID 当前绝技 ID (0 表示尚未学习，可参考 NextRoleStuntID)
	RoleStuntID int32
	// NextRoleStuntID 下一阶绝技 ID
	NextRoleStuntID int32
	// IsActive 是否已激活/可切换
	IsActive uint8
	// IsUse 是否当前使用中
	IsUse uint8
}

// BuildLibraryArtifactStuntInfoPacket 构造查询神器绝技映射封包 (0x00550005)。
func BuildLibraryArtifactStuntInfoPacket() *Packet {
	return NewPacket(ActionIDLibraryArtifactStuntInfo, nil)
}

// BuildLibraryMasterPracticeStuntInfoPacket 构造查询宗师修行绝技映射封包 (0x00550007)。
func BuildLibraryMasterPracticeStuntInfoPacket() *Packet {
	return NewPacket(ActionIDLibraryMasterPracticeStuntInfo, nil)
}

// ParseLibraryStuntInfoResult 解析职业 -> 绝技映射 (85/5 与 85/7 布局相同)。
// 响应格式: Uint16 count + N * (Uint8 job_id, Int32 stunt_id, Int32 next_stunt_id, Uint8 is_active, Uint8 is_use)。
func ParseLibraryStuntInfoResult(payload []byte) ([]LibraryStuntInfo, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read library stunt count: %v", ErrInvalidPayload, err)
	}
	out := make([]LibraryStuntInfo, 0, count)
	for i := 0; i < int(count); i++ {
		var info LibraryStuntInfo
		jobID, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read library stunt[%d].job_id: %v", ErrInvalidPayload, i, err)
		}
		info.RoleJobID = int32(jobID)
		if info.RoleStuntID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read library stunt[%d].stunt_id: %v", ErrInvalidPayload, i, err)
		}
		if info.NextRoleStuntID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read library stunt[%d].next_stunt_id: %v", ErrInvalidPayload, i, err)
		}
		if info.IsActive, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read library stunt[%d].is_active: %v", ErrInvalidPayload, i, err)
		}
		if info.IsUse, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read library stunt[%d].is_use: %v", ErrInvalidPayload, i, err)
		}
		out = append(out, info)
	}
	return out, nil
}

// ParseLibraryMasterPracticeStuntInfoResult 兼容旧命名；与 ParseLibraryStuntInfoResult 等价。
func ParseLibraryMasterPracticeStuntInfoResult(payload []byte) ([]LibraryStuntInfo, error) {
	return ParseLibraryStuntInfoResult(payload)
}

// BuildLibraryMasterPracticeStuntInfoResultPacket 构造绝技映射回包，供离线 mock 与单测使用。
func BuildLibraryMasterPracticeStuntInfoResultPacket(infos []LibraryStuntInfo) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(infos)))
	for _, info := range infos {
		w.WriteUint8(uint8(info.RoleJobID))
		w.WriteInt32(info.RoleStuntID)
		w.WriteInt32(info.NextRoleStuntID)
		w.WriteUint8(info.IsActive)
		w.WriteUint8(info.IsUse)
	}
	return NewPacket(ActionIDLibraryMasterPracticeStuntInfo, w.Bytes())
}

// BuildLibraryUseStuntSkillPacket 构造切换主角绝技封包 (0x00550003)。
// 官方请求为 Short stunt_id。
func BuildLibraryUseStuntSkillPacket(stuntID int32) (*Packet, error) {
	if stuntID <= 0 || stuntID > math.MaxInt16 {
		return nil, fmt.Errorf("protocol: invalid stunt id %d (must be 1..%d)", stuntID, math.MaxInt16)
	}
	w := NewWriter()
	w.WriteInt16(int16(stuntID))
	return NewPacket(ActionIDLibraryUseStuntSkill, w.Bytes()), nil
}

// ParseLibraryUseStuntSkillResult 解析切换绝技回包。
// 响应格式: Ubyte result (+ 成功时 Short cur_role_id, Short cur_stunt_id)。
func ParseLibraryUseStuntSkillResult(payload []byte) (result uint8, curRoleID int16, curStuntID int16, err error) {
	r := NewReader(payload)
	if result, err = r.ReadUint8(); err != nil {
		return 0, 0, 0, fmt.Errorf("%w: failed to read library stunt result: %v", ErrInvalidPayload, err)
	}
	if result != LibraryResultSuccess {
		return result, 0, 0, nil
	}
	if curRoleID, err = r.ReadInt16(); err != nil {
		return result, 0, 0, fmt.Errorf("%w: failed to read cur_role_id: %v", ErrInvalidPayload, err)
	}
	if curStuntID, err = r.ReadInt16(); err != nil {
		return result, 0, 0, fmt.Errorf("%w: failed to read cur_stunt_id: %v", ErrInvalidPayload, err)
	}
	return result, curRoleID, curStuntID, nil
}

// BuildLibraryUseStuntSkillResultPacket 构造切换绝技回包，供离线 mock 与单测使用。
func BuildLibraryUseStuntSkillResultPacket(result uint8, curRoleID, curStuntID int16) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	if result == LibraryResultSuccess {
		w.WriteInt16(curRoleID)
		w.WriteInt16(curStuntID)
	}
	return NewPacket(ActionIDLibraryUseStuntSkill, w.Bytes())
}

// LibraryUseStuntResultText 返回切换绝技结果码的中文语义，便于日志定位。
func LibraryUseStuntResultText(code uint8) string {
	switch code {
	case LibraryResultSuccess:
		return "成功"
	case LibraryResultPlayerLevelLimit:
		return "玩家等级不足"
	case LibraryResultNotEnoughXianLing:
		return "仙灵不足"
	case LibraryResultNotEnoughStarSoul:
		return "星魂不足"
	case LibraryResultOtherRoleJobLimit:
		return "其它角色职业限制"
	case LibraryResultMainRoleJobLimit:
		return "主角职业限制"
	case LibraryResultFullLevel:
		return "已满级"
	case LibraryResultInvalidRoleJobID:
		return "职业编号无效"
	case LibraryResultNoHave:
		return "未学习该绝技"
	case LibraryResultNotEnoughCoin:
		return "铜钱不足"
	default:
		return fmt.Sprintf("未知结果码 %d", code)
	}
}
