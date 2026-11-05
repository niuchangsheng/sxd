// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现仙盟/圣盟/住宅的状态探测协议：
//   - Mod_Sect_Base.get_open_status (Module 571, Action 6) 仙盟功能开放状态
//   - Mod_SectAlliance_Base.get_now_season (Module 884, Action 0) 圣盟当前赛季
//   - Mod_MarryHome_Base.get_self_home_info (Module 210, Action 0) 自身住宅状态
//
// 这些玩法依赖账号是否加入仙盟/圣盟或处于婚姻状态，自动化必须先探测再决定是否执行。
package protocol

import "fmt"

const (
	// ActionIDSectOpenStatus 查询仙盟功能开放状态 (Module 571, Action 6 -> 0x023B0006)
	ActionIDSectOpenStatus uint32 = 0x023B0006
	// ActionIDSectPanelInfo 查询仙盟面板信息 (Module 571, Action 0 -> 0x023B0000)
	ActionIDSectPanelInfo uint32 = 0x023B0000
	// ActionIDSectAllianceSeason 查询圣盟当前赛季 (Module 884, Action 0 -> 0x03740000)
	ActionIDSectAllianceSeason uint32 = 0x03740000
	// ActionIDMarryHomeSelfInfo 查询自身住宅状态 (Module 210, Action 0 -> 0x00D20000)
	ActionIDMarryHomeSelfInfo uint32 = 0x00D20000
)

// MarryHomeResultNonMarry 未处于婚姻状态。
const MarryHomeResultNonMarry uint8 = 3

// BuildSectOpenStatusPacket 构造查询仙盟开放状态封包 (0x023B0006)。
func BuildSectOpenStatusPacket() *Packet {
	return NewPacket(ActionIDSectOpenStatus, nil)
}

// BuildSectPanelInfoPacket 构造查询仙盟面板封包 (0x023B0000)。
func BuildSectPanelInfoPacket() *Packet {
	return NewPacket(ActionIDSectPanelInfo, nil)
}

// BuildSectAllianceSeasonPacket 构造查询圣盟赛季封包 (0x03740000)。
func BuildSectAllianceSeasonPacket() *Packet {
	return NewPacket(ActionIDSectAllianceSeason, nil)
}

// BuildMarryHomeSelfInfoPacket 构造查询自身住宅状态封包 (0x00D20000)。
func BuildMarryHomeSelfInfoPacket(playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	return NewPacket(ActionIDMarryHomeSelfInfo, w.Bytes())
}

// ParseSectOpenStatusResult 解析仙盟开放状态 (UByte status)。
func ParseSectOpenStatusResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty sect open status", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseSectAllianceSeasonResult 解析圣盟赛季 (Int season, Int ?)。
func ParseSectAllianceSeasonResult(payload []byte) (season int32, endTime int32, err error) {
	r := NewReader(payload)
	if season, err = r.ReadInt32(); err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read alliance season: %v", ErrInvalidPayload, err)
	}
	if endTime, err = r.ReadInt32(); err != nil {
		return season, 0, fmt.Errorf("%w: failed to read alliance season end: %v", ErrInvalidPayload, err)
	}
	return season, endTime, nil
}

// ParseMarryHomeSelfInfoResult 解析住宅状态 (Byte result)。
func ParseMarryHomeSelfInfoResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty marry home result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildSectOpenStatusResultPacket 构造仙盟开放状态回包，供离线 mock 与单测使用。
func BuildSectOpenStatusResultPacket(status uint8) *Packet {
	return NewPacket(ActionIDSectOpenStatus, []byte{status})
}

// BuildSectAllianceSeasonResultPacket 构造圣盟赛季回包，供离线 mock 与单测使用。
func BuildSectAllianceSeasonResultPacket(season, endTime int32) *Packet {
	w := NewWriter()
	w.WriteInt32(season)
	w.WriteInt32(endTime)
	return NewPacket(ActionIDSectAllianceSeason, w.Bytes())
}

// BuildMarryHomeSelfInfoResultPacket 构造住宅状态回包，供离线 mock 与单测使用。
func BuildMarryHomeSelfInfoResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDMarryHomeSelfInfo, []byte{result})
}
