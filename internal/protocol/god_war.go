// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方神魔大战协议 (Mod_GodWar_Base, Module 118 / 0x76)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_GodWar_Base：
// 先查询大战阶段，仅报名期 (SIGN_UP) 自动报名；其余阶段不发送业务请求。
// 自动化不调用 clear_cd_time / buy_challenge_times 等消耗元宝接口。
package protocol

import "fmt"

const (
	// ActionIDGodWarStatus 查询神魔大战阶段 (Module 118, Action 0 -> 0x00760000)
	ActionIDGodWarStatus uint32 = 0x00760000
	// ActionIDGodWarSignUp 报名神魔大战 (Module 118, Action 2 -> 0x00760002)
	ActionIDGodWarSignUp uint32 = 0x00760002
)

// Mod_GodWar_Base 阶段码 (部分关键值)。
const (
	// GodWarStatusClose 未开启/关闭
	GodWarStatusClose uint8 = 0
	// GodWarStatusSignUp 报名期
	GodWarStatusSignUp uint8 = 1
)

// GodWarResultSuccess 通用成功码。
const GodWarResultSuccess uint8 = 63

// GodWarStatus 表示神魔大战阶段信息。
//
// 线格式: UByte status, [UByte, Int, Int] race_list, UByte flag。
type GodWarStatus struct {
	// Status 当前阶段码
	Status uint8
	// RaceCount 赛程条目数 (内容不消费)
	RaceCount int32
	// Flag 附加标记
	Flag uint8
}

// BuildGodWarStatusPacket 构造查询神魔大战阶段封包 (0x00760000)。
func BuildGodWarStatusPacket() *Packet {
	return NewPacket(ActionIDGodWarStatus, nil)
}

// BuildGodWarSignUpPacket 构造报名封包 (0x00760002)。
func BuildGodWarSignUpPacket() *Packet {
	return NewPacket(ActionIDGodWarSignUp, nil)
}

// ParseGodWarStatusResult 解析神魔大战阶段回包。
func ParseGodWarStatusResult(payload []byte) (*GodWarStatus, error) {
	r := NewReader(payload)
	res := &GodWarStatus{}
	var err error
	if res.Status, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god war status: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read god war race count: %v", ErrInvalidPayload, err)
	}
	res.RaceCount = int32(count)
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read god war race[%d].a: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read god war race[%d].b: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read god war race[%d].c: %v", ErrInvalidPayload, i, err)
		}
	}
	if res.Flag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god war flag: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseGodWarSignUpResult 解析报名结果 (单字节结果码)。
func ParseGodWarSignUpResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty god war sign-up result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildGodWarStatusResultPacket 构造神魔大战阶段回包，供离线 mock 与单测使用。
func BuildGodWarStatusResultPacket(status uint8, flag uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(status)
	w.WriteUint16(0)
	w.WriteUint8(flag)
	return NewPacket(ActionIDGodWarStatus, w.Bytes())
}

// BuildGodWarSignUpResultPacket 构造报名回包，供离线 mock 与单测使用。
func BuildGodWarSignUpResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDGodWarSignUp, []byte{result})
}
