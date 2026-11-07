// Package protocol 提供法力星图 (Mod_StarPicture_Base, Module 485) 协议编解码。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"fmt"
)

// 法力星图业务结果码 (对齐 2026 官方微端 Mod_StarPicture_Base)。
const (
	// StarPictureResultSuccess 占星成功
	StarPictureResultSuccess uint8 = 0
)

// StarPictureInfo 代表法力星图面板状态。
//
// 线格式 (Mod_StarPicture_Base.get_info):
// Int32 has_divination_count, Int32 free_divination_count, Int32 ingot_divination_count,
// Int32 chip_count, Int32 reset_count, Int32 cur_deploy_level,
// [Short, Int32, Int32, Short] award_list, Byte × 4。
//
// 本结构仅解析前 6 个 Int32；尾部奖池与锁定标记由客户端展示使用，
// 自动化只需免费占星次数。
type StarPictureInfo struct {
	// HasDivination 已占星次数
	HasDivination int32
	// FreeDivination 剩余免费占星次数
	FreeDivination int32
	// IngotDivination 剩余元宝占星次数 (自动化永不使用)
	IngotDivination int32
	// ChipCount 星图碎片数量
	ChipCount int32
	// ResetCount 剩余重置次数
	ResetCount int32
	// CurDeployLevel 当前阵位等级
	CurDeployLevel int32
}

// BuildStarPictureInfoPacket 构造查询法力星图面板封包 (Module 485, Action 0)。
func BuildStarPictureInfoPacket(prevAct uint32) (*Packet, error) {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDStarPictureInfo, w.Bytes())
	pkt.HasPrevAct = true
	return pkt, nil
}

// ParseStarPictureInfoResult 解析法力星图面板回包。
func ParseStarPictureInfoResult(payload []byte) (*StarPictureInfo, error) {
	if len(payload) < 24 {
		return nil, fmt.Errorf("%w: payload too short for star picture info (%dB)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	fields := make([]int32, 6)
	for i := range fields {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read star picture field #%d: %v", ErrInvalidPayload, i, err)
		}
		fields[i] = v
	}
	return &StarPictureInfo{
		HasDivination:   fields[0],
		FreeDivination:  fields[1],
		IngotDivination: fields[2],
		ChipCount:       fields[3],
		ResetCount:      fields[4],
		CurDeployLevel:  fields[5],
	}, nil
}

// BuildStarPictureInfoResultPacket 构造法力星图面板回包 (测试与 Mock 服务端使用)。
func BuildStarPictureInfoResultPacket(info StarPictureInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.HasDivination)
	w.WriteInt32(info.FreeDivination)
	w.WriteInt32(info.IngotDivination)
	w.WriteInt32(info.ChipCount)
	w.WriteInt32(info.ResetCount)
	w.WriteInt32(info.CurDeployLevel)
	w.WriteUint16(0)          // award_list 条目数
	w.WriteBytes([]byte{0, 0, 0, 0}) // 尾部锁定标记
	return NewPacket(ActionIDStarPictureInfo, w.Bytes())
}

// BuildStarPictureDivinationPacket 构造免费占星请求封包 (Module 485, Action 1)。
func BuildStarPictureDivinationPacket(prevAct uint32) (*Packet, error) {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDStarPictureInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDStarPictureDivination, w.Bytes())
	pkt.HasPrevAct = true
	return pkt, nil
}

// ParseStarPictureDivinationResult 解析占星结果码。
func ParseStarPictureDivinationResult(payload []byte) (uint8, error) {
	r := NewReader(payload)
	code, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read divination result: %v", ErrInvalidPayload, err)
	}
	return code, nil
}