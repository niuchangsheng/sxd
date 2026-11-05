// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方仙界矿山协议 (Mod_StMine_Base, Module 264 / 0x108)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_StMine_Base 与
// com.datas.StMineData：查询矿山信息后进入当前矿脉、领取收益并离开。
// 自动化不调用 buy_rob_count / clean_rob_cd_time / increase_protect_time。
package protocol

import "fmt"

const (
	// ActionIDStMineInfo 查询矿山信息 (Module 264, Action 0 -> 0x01080000)
	ActionIDStMineInfo uint32 = 0x01080000
	// ActionIDStMineEnter 进入矿山 (Module 264, Action 1 -> 0x01080001)
	ActionIDStMineEnter uint32 = 0x01080001
	// ActionIDStMineLeave 离开矿山 (Module 264, Action 2 -> 0x01080002)
	ActionIDStMineLeave uint32 = 0x01080002
	// ActionIDStMineReceiveAward 领取矿山收益 (Module 264, Action 7 -> 0x01080007)
	ActionIDStMineReceiveAward uint32 = 0x01080007
)

// Mod_StMine_Base 结果码 (关键值)。
const (
	// StMineResultSuccess 成功
	StMineResultSuccess uint8 = 9
	// StMineResultFunctionUnopen 功能未开放
	StMineResultFunctionUnopen uint8 = 10
	// StMineResultSuperTownClosed 超级城镇关闭
	StMineResultSuperTownClosed uint8 = 11
	// StMineResultResting 休息中
	StMineResultResting uint8 = 12
	// StMineResultFailed 失败
	StMineResultFailed uint8 = 13
)

// StMineInfo 表示矿山概览。
type StMineInfo struct {
	// Result 结果码
	Result uint8
	// Level 矿脉等级
	Level uint8
	// CurMountain 当前矿脉编号
	CurMountain uint8
}

// BuildStMineInfoPacket 构造查询矿山信息封包 (0x01080000)。
func BuildStMineInfoPacket() *Packet {
	return NewPacket(ActionIDStMineInfo, nil)
}

// BuildStMineEnterPacket 构造进入矿山封包 (0x01080001)。
func BuildStMineEnterPacket(mountain uint8) *Packet {
	return NewPacket(ActionIDStMineEnter, []byte{mountain})
}

// BuildStMineReceiveAwardPacket 构造领取收益封包 (0x01080007)。
func BuildStMineReceiveAwardPacket() *Packet {
	return NewPacket(ActionIDStMineReceiveAward, nil)
}

// BuildStMineLeavePacket 构造离开矿山封包 (0x01080002)。
func BuildStMineLeavePacket() *Packet {
	return NewPacket(ActionIDStMineLeave, nil)
}

// ParseStMineInfoResult 解析矿山概览。
func ParseStMineInfoResult(payload []byte) (*StMineInfo, error) {
	r := NewReader(payload)
	res := &StMineInfo{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine result: %v", ErrInvalidPayload, err)
	}
	if res.Level, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine level: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // total_skill
		return nil, fmt.Errorf("%w: failed to read st mine total_skill: %v", ErrInvalidPayload, err)
	}
	if res.CurMountain, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine cur_mountain: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine mountain list count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		for j := 0; j < 3; j++ {
			if _, err := r.ReadUint8(); err != nil {
				return nil, fmt.Errorf("%w: failed to read st mine mountain[%d][%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
	}
	// lucky_open_flag / lucky_flag 不消费。
	return res, nil
}

// ParseStMineActionResult 解析进入/领取/离开的结果码 (首字节)。
func ParseStMineActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty st mine result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildStMineInfoResultPacket 构造矿山概览回包，供离线 mock 与单测使用。
func BuildStMineInfoResultPacket(info StMineInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Result)
	w.WriteUint8(info.Level)
	w.WriteInt32(0)
	w.WriteUint8(info.CurMountain)
	w.WriteUint16(0)
	w.WriteUint8(0)
	w.WriteUint8(0)
	return NewPacket(ActionIDStMineInfo, w.Bytes())
}

// BuildStMineActionResultPacket 构造矿山动作回包，供离线 mock 与单测使用。
func BuildStMineActionResultPacket(actionID uint32, result uint8) *Packet {
	return NewPacket(actionID, []byte{result})
}
