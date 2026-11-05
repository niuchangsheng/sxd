// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方设备/手机绑定奖励协议 (Mod_DeviceBindRewards_Base, Module 814 / 0x32E)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_DeviceBindRewards_Base 与
// com.assist.server.DeviceBindRewardsType：账号完成手机绑定/验证后可领取
// 首绑奖励 (200 元宝) 与月度奖励 (50 元宝)。自动化只负责领取，
// 不代用户发送验证码或提交手机号。
package protocol

import "fmt"

const (
	// ActionIDDeviceBindInfo 查询设备绑定状态 (Module 814, Action 0 -> 0x032E0000)
	ActionIDDeviceBindInfo uint32 = 0x032E0000
	// ActionIDDeviceBindFirstReward 领取首绑奖励 (Module 814, Action 5 -> 0x032E0005)
	ActionIDDeviceBindFirstReward uint32 = 0x032E0005
	// ActionIDDeviceBindMonthlyReward 领取月度奖励 (Module 814, Action 6 -> 0x032E0006)
	ActionIDDeviceBindMonthlyReward uint32 = 0x032E0006
)

// Mod_DeviceBindRewards_Base 结果码。
const (
	// DeviceBindResultSuccess 成功
	DeviceBindResultSuccess uint8 = 0
	// DeviceBindResultFailed 失败
	DeviceBindResultFailed uint8 = 1
	// DeviceBindResultVerifyCD 验证码冷却中
	DeviceBindResultVerifyCD uint8 = 2
	// DeviceBindResultNeedNotVerify 无需验证
	DeviceBindResultNeedNotVerify uint8 = 3
	// DeviceBindResultPhoneIllegality 手机号非法
	DeviceBindResultPhoneIllegality uint8 = 4
	// DeviceBindResultFuncNotOpen 功能未开放
	DeviceBindResultFuncNotOpen uint8 = 5
	// DeviceBindResultInvalidCode 验证码错误
	DeviceBindResultInvalidCode uint8 = 6
	// DeviceBindResultAlreadyBound 已绑定
	DeviceBindResultAlreadyBound uint8 = 7
	// DeviceBindResultPhoneBindLimit 手机号绑定次数上限
	DeviceBindResultPhoneBindLimit uint8 = 8
	// DeviceBindResultNotBound 未绑定
	DeviceBindResultNotBound uint8 = 9
	// DeviceBindResultPhoneVerified 手机号已验证
	DeviceBindResultPhoneVerified uint8 = 10
	// DeviceBindResultPhoneError 手机号错误
	DeviceBindResultPhoneError uint8 = 11
	// DeviceBindResultAlreadyGet 奖励已领取
	DeviceBindResultAlreadyGet uint8 = 12
	// DeviceBindResultPackFull 背包已满
	DeviceBindResultPackFull uint8 = 13
	// DeviceBindResultNotVerifyAndBind 未完成验证与绑定
	DeviceBindResultNotVerifyAndBind uint8 = 14
	// DeviceBindResultFirstBindMonthly 首绑当月限制
	DeviceBindResultFirstBindMonthly uint8 = 15
)

// DeviceBindInfo 表示设备绑定状态。
//
// 线格式: UByte result, Byte is_bind, String phone_num, Byte first_reward,
// Byte monthly_reward, Int verify_cd, Byte need_verify_bind, Int start_time。
type DeviceBindInfo struct {
	// Result 查询结果码
	Result uint8
	// IsBind 是否已绑定手机
	IsBind uint8
	// PhoneNum 已绑定手机号 (脱敏显示)
	PhoneNum string
	// FirstReward 首绑奖励领取状态 (0=未领, 非0=已领)
	FirstReward uint8
	// MonthlyReward 月度奖励领取状态 (0=未领, 非0=已领)
	MonthlyReward uint8
	// VerifyCD 验证码发送冷却
	VerifyCD int32
	// NeedVerifyBind 是否需要先验证再绑定
	NeedVerifyBind uint8
	// StartTime 活动开始时间
	StartTime int32
}

// BuildDeviceBindInfoPacket 构造查询设备绑定状态封包 (0x032E0000)。
func BuildDeviceBindInfoPacket() *Packet {
	return NewPacket(ActionIDDeviceBindInfo, nil)
}

// BuildDeviceBindFirstRewardPacket 构造领取首绑奖励封包 (0x032E0005)。
func BuildDeviceBindFirstRewardPacket() *Packet {
	return NewPacket(ActionIDDeviceBindFirstReward, nil)
}

// BuildDeviceBindMonthlyRewardPacket 构造领取月度奖励封包 (0x032E0006)。
func BuildDeviceBindMonthlyRewardPacket() *Packet {
	return NewPacket(ActionIDDeviceBindMonthlyReward, nil)
}

// ParseDeviceBindInfoResult 解析设备绑定状态回包。
func ParseDeviceBindInfoResult(payload []byte) (*DeviceBindInfo, error) {
	r := NewReader(payload)
	res := &DeviceBindInfo{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind result: %v", ErrInvalidPayload, err)
	}
	if res.IsBind, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind is_bind: %v", ErrInvalidPayload, err)
	}
	if res.PhoneNum, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind phone_num: %v", ErrInvalidPayload, err)
	}
	if res.FirstReward, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind first_reward: %v", ErrInvalidPayload, err)
	}
	if res.MonthlyReward, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind monthly_reward: %v", ErrInvalidPayload, err)
	}
	if res.VerifyCD, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind verify_cd: %v", ErrInvalidPayload, err)
	}
	if res.NeedVerifyBind, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind need_verify_bind: %v", ErrInvalidPayload, err)
	}
	if res.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read device bind start_time: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseDeviceBindRewardResult 解析奖励领取结果 (单字节结果码)。
func ParseDeviceBindRewardResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty device bind reward result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildDeviceBindInfoResultPacket 构造设备绑定状态回包，供离线 mock 与单测使用。
func BuildDeviceBindInfoResultPacket(info DeviceBindInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Result)
	w.WriteUint8(info.IsBind)
	w.WriteString(info.PhoneNum)
	w.WriteUint8(info.FirstReward)
	w.WriteUint8(info.MonthlyReward)
	w.WriteInt32(info.VerifyCD)
	w.WriteUint8(info.NeedVerifyBind)
	w.WriteInt32(info.StartTime)
	return NewPacket(ActionIDDeviceBindInfo, w.Bytes())
}

// BuildDeviceBindRewardResultPacket 构造奖励领取回包，供离线 mock 与单测使用。
func BuildDeviceBindRewardResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDDeviceBindFirstReward, []byte{result})
}
