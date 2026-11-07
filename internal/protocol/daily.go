// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-11
package protocol

import (
	"fmt"
)

// 每日签到协议 (Module 89 / 0x59, Mod_SignIn_Base)。
//
// 依据官方 Main.swf 反编译的 com.protocols.Mod_SignIn_Base：
// 签到活动是独立的月历签到面板，**不是** Module 0。旧实现使用的
// 0x00000049 在实服无任何回包，已废弃。
const (
	// ActionIDSignInStatus 查询签到活动状态 (Module 89, Action 5 -> 0x00590005)
	ActionIDSignInStatus uint32 = 0x00590005
	// ActionIDSignInInfo 查询签到详情 (Module 89, Action 0 -> 0x00590000)
	ActionIDSignInInfo uint32 = 0x00590000
	// ActionIDSignInPlayer 执行每日签到 (Module 89, Action 2 -> 0x00590002)
	ActionIDSignInPlayer uint32 = 0x00590002
	// ActionIDSignInContinue 领取连签奖励 (Module 89, Action 1 -> 0x00590001)
	ActionIDSignInContinue uint32 = 0x00590001
	// ActionIDSignInTotalAward 领取累计签到档位奖励 (Module 89, Action 4 -> 0x00590004)
	ActionIDSignInTotalAward uint32 = 0x00590004
)

// Mod_SignIn_Base 结果码与状态码。
const (
	// SignInStateCanGet 可领取
	SignInStateCanGet uint8 = 0
	// SignInStateCannotGet 不可领取
	SignInStateCannotGet uint8 = 1
	// SignInStateAlreadyGet 已领取
	SignInStateAlreadyGet uint8 = 7
	// SignInStateNoSignIn 今日未签到
	SignInStateNoSignIn uint8 = 8
	// SignInStateAlreadySignIn 今日已签到
	SignInStateAlreadySignIn uint8 = 9
	// SignInResultSuccess 领取成功 (连签/累计档位)
	SignInResultSuccess uint8 = 10
	// SignInResultFailed 领取失败 (连签/累计档位)
	SignInResultFailed uint8 = 11
	// SignInResultOK 签到成功
	SignInResultOK uint8 = 12
	// SignInResultFail 签到失败
	SignInResultFail uint8 = 13
	// SignInResultPackFull 背包已满
	SignInResultPackFull uint8 = 14
	// SignInResultNoIngot 补签元宝不足
	SignInResultNoIngot uint8 = 15
)

// SignInStatus 表示签到活动对玩家展示的状态。
//
// 回包线格式: Byte activity_a, Byte has_award, Byte activity_b。
// 官方 SignInData.get_sign_in_status 判定活动开启为
// (a == 1 || has_award == 1) && activity_b == 1。
type SignInStatus struct {
	// ActivityOpen 签到活动是否开启
	ActivityOpen bool
	// HasAward 是否有可领取奖励
	HasAward bool
	// RawA 原始字段 a
	RawA uint8
	// RawB 原始字段 b (has_award)
	RawB uint8
	// RawC 原始字段 c
	RawC uint8
}

// SignInTier 表示累计签到档位的领取状态。
type SignInTier struct {
	// Tier 档位编号 (2..6 对应 5/15/25/35/45 天)
	Tier uint8
	// State 状态码 (0=可领取, 1=不可领取, 7=已领取)
	State uint8
}

// SignInInfo 表示 get_sign_in_info 回包。
//
// 线格式: UByte continue_state, Int continue_day, [Int total_sign_in],
// [UByte tier, UByte state] total_info, Int total_day, UByte sign_state,
// Int repair_sign, Int cur_day。
type SignInInfo struct {
	// ContinueState 连签奖励状态 (0=可领取, 7=已领取)
	ContinueState uint8
	// ContinueDay 当前连续签到天数
	ContinueDay int32
	// TotalSignIn 已签到日期列表 (月内日期)
	TotalSignIn []int32
	// TotalInfo 累计档位状态列表
	TotalInfo []SignInTier
	// TotalDay 本月累计签到天数
	TotalDay int32
	// SignState 今日签到状态 (8=未签到, 9=已签到)
	SignState uint8
	// RepairSign 剩余补签次数
	RepairSign int32
	// CurDay 当前月内日期
	CurDay int32
}

// SignInActionResult 表示签到/累计档位奖励回包。
//
// player_sign_in 线格式: UByte result, [Int total_sign_in],
// [UByte tier, UByte state] total_info, Int total_day。
// get_total_award 在此基础上追加 UByte sign_state, Int cur_day。
type SignInActionResult struct {
	// Result 结果码
	Result uint8
	// TotalSignIn 已签到日期列表
	TotalSignIn []int32
	// TotalInfo 累计档位状态列表
	TotalInfo []SignInTier
	// TotalDay 本月累计签到天数
	TotalDay int32
	// SignState 签到状态 (仅 get_total_award 下发)
	SignState uint8
	// CurDay 当前月内日期 (仅 get_total_award 下发)
	CurDay int32
}

// BuildSignInStatusPacket 构造查询签到活动状态封包 (0x00590005)。
func BuildSignInStatusPacket() *Packet {
	return NewPacket(ActionIDSignInStatus, nil)
}

// BuildSignInInfoPacket 构造查询签到详情封包 (0x00590000)。
func BuildSignInInfoPacket() *Packet {
	return NewPacket(ActionIDSignInInfo, nil)
}

// BuildSignInPlayerPacket 构造执行签到封包 (0x00590002)。
func BuildSignInPlayerPacket() *Packet {
	return NewPacket(ActionIDSignInPlayer, nil)
}

// BuildSignInContinuePacket 构造领取连签奖励封包 (0x00590001)。
func BuildSignInContinuePacket() *Packet {
	return NewPacket(ActionIDSignInContinue, nil)
}

// BuildSignInTotalAwardPacket 构造领取累计签到档位奖励封包 (0x00590004)。
func BuildSignInTotalAwardPacket(tier uint8) *Packet {
	return NewPacket(ActionIDSignInTotalAward, []byte{tier})
}

// ParseSignInStatusResult 解析签到活动状态回包。
func ParseSignInStatusResult(payload []byte) (*SignInStatus, error) {
	r := NewReader(payload)
	a, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in status a: %v", ErrInvalidPayload, err)
	}
	b, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in status b: %v", ErrInvalidPayload, err)
	}
	c, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in status c: %v", ErrInvalidPayload, err)
	}
	return &SignInStatus{
		ActivityOpen: (a == 1 || b == 1) && c == 1,
		HasAward:     b == 1,
		RawA:         a,
		RawB:         b,
		RawC:         c,
	}, nil
}

// ParseSignInInfoResult 解析签到详情回包。
func ParseSignInInfoResult(payload []byte) (*SignInInfo, error) {
	r := NewReader(payload)
	res := &SignInInfo{}
	var err error
	if res.ContinueState, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in continue_state: %v", ErrInvalidPayload, err)
	}
	if res.ContinueDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in continue_day: %v", ErrInvalidPayload, err)
	}
	if res.TotalSignIn, err = readInt32List(r); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in total_sign_in: %v", ErrInvalidPayload, err)
	}
	if res.TotalInfo, err = readSignInTiers(r); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in total_info: %v", ErrInvalidPayload, err)
	}
	if res.TotalDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in total_day: %v", ErrInvalidPayload, err)
	}
	if res.SignState, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in sign_state: %v", ErrInvalidPayload, err)
	}
	if res.RepairSign, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in repair_sign: %v", ErrInvalidPayload, err)
	}
	if res.CurDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in cur_day: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseSignInActionResult 解析签到/累计档位奖励回包，尾部字段缺失时保持零值。
func ParseSignInActionResult(payload []byte) (*SignInActionResult, error) {
	r := NewReader(payload)
	res := &SignInActionResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in result: %v", ErrInvalidPayload, err)
	}
	if res.TotalSignIn, err = readInt32List(r); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in total_sign_in: %v", ErrInvalidPayload, err)
	}
	if res.TotalInfo, err = readSignInTiers(r); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in total_info: %v", ErrInvalidPayload, err)
	}
	if res.TotalDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sign-in total_day: %v", ErrInvalidPayload, err)
	}
	// get_total_award 追加 sign_state 与 cur_day；player_sign_in 不携带。
	if res.SignState, err = r.ReadUint8(); err != nil {
		return res, nil
	}
	if res.CurDay, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// ParseSignInContinueResult 解析连签奖励回包: UByte result, Int continue_day。
func ParseSignInContinueResult(payload []byte) (result uint8, continueDay int32, err error) {
	r := NewReader(payload)
	if result, err = r.ReadUint8(); err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read continue result: %v", ErrInvalidPayload, err)
	}
	if continueDay, err = r.ReadInt32(); err != nil {
		return result, 0, fmt.Errorf("%w: failed to read continue_day: %v", ErrInvalidPayload, err)
	}
	return result, continueDay, nil
}

// readInt32List 读取 Uint16 count + count × Int32 列表。
func readInt32List(r *Reader) ([]int32, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// readSignInTiers 读取 Uint16 count + count × (UByte tier, UByte state)。
func readSignInTiers(r *Reader) ([]SignInTier, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := make([]SignInTier, 0, count)
	for i := 0; i < int(count); i++ {
		tier, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		state, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		out = append(out, SignInTier{Tier: tier, State: state})
	}
	return out, nil
}

// BuildSignInStatusResultPacket 构造签到状态回包，供离线 mock 与单测使用。
func BuildSignInStatusResultPacket(a, b, c uint8) *Packet {
	return NewPacket(ActionIDSignInStatus, []byte{a, b, c})
}

// BuildSignInInfoResultPacket 构造签到详情回包，供离线 mock 与单测使用。
func BuildSignInInfoResultPacket(info SignInInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.ContinueState)
	w.WriteInt32(info.ContinueDay)
	writeInt32List(w, info.TotalSignIn)
	writeSignInTiers(w, info.TotalInfo)
	w.WriteInt32(info.TotalDay)
	w.WriteUint8(info.SignState)
	w.WriteInt32(info.RepairSign)
	w.WriteInt32(info.CurDay)
	return NewPacket(ActionIDSignInInfo, w.Bytes())
}

// BuildSignInActionResultPacket 构造签到/累计档位奖励回包，供离线 mock 与单测使用。
func BuildSignInActionResultPacket(res SignInActionResult, withSignState bool) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	writeInt32List(w, res.TotalSignIn)
	writeSignInTiers(w, res.TotalInfo)
	w.WriteInt32(res.TotalDay)
	if withSignState {
		w.WriteUint8(res.SignState)
		w.WriteInt32(res.CurDay)
	}
	return NewPacket(ActionIDSignInPlayer, w.Bytes())
}

// BuildSignInTotalAwardResultPacket 构造累计签到档位奖励回包，供离线 mock 与单测使用。
func BuildSignInTotalAwardResultPacket(res SignInActionResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	writeInt32List(w, res.TotalSignIn)
	writeSignInTiers(w, res.TotalInfo)
	w.WriteInt32(res.TotalDay)
	w.WriteUint8(res.SignState)
	w.WriteInt32(res.CurDay)
	return NewPacket(ActionIDSignInTotalAward, w.Bytes())
}

// BuildSignInContinueResultPacket 构造连签奖励回包，供离线 mock 与单测使用。
func BuildSignInContinueResultPacket(result uint8, continueDay int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(continueDay)
	return NewPacket(ActionIDSignInContinue, w.Bytes())
}

// writeInt32List 写入 Uint16 count + count × Int32。
func writeInt32List(w *Writer, values []int32) {
	w.WriteUint16(uint16(len(values)))
	for _, v := range values {
		w.WriteInt32(v)
	}
}

// writeSignInTiers 写入 Uint16 count + count × (UByte tier, UByte state)。
func writeSignInTiers(w *Writer, tiers []SignInTier) {
	w.WriteUint16(uint16(len(tiers)))
	for _, t := range tiers {
		w.WriteUint8(t.Tier)
		w.WriteUint8(t.State)
	}
}

// 节日活动在线奖励动作 ID (Mod_SpringActivity_Base, Module 218)。
const (
	ActionIDFestivalOnlineInfo  uint32 = 0x00DA001F
	ActionIDFestivalOnlineAward uint32 = 0x00DA0020
)

// 节日活动在线奖励结果码。
const (
	FestivalOnlineSuccess         uint8 = 14
	FestivalOnlineError           uint8 = 15
	FestivalOnlineNonActivityTime uint8 = 17
	FestivalOnlineHadGet          uint8 = 24
	FestivalOnlineFunctionNotOpen uint8 = 42
	FestivalOnlinePackFull        uint8 = 43
	FestivalOnlineTimesLimit      uint8 = 45
)

// BuildFestivalOnlineInfoPacket 构造节日活动在线奖励面板封包。
func BuildFestivalOnlineInfoPacket() *Packet {
	return NewPacket(ActionIDFestivalOnlineInfo, nil)
}

// BuildFestivalOnlineAwardPacket 构造领取节日在线奖励封包。
func BuildFestivalOnlineAwardPacket(index uint8) *Packet {
	return NewPacket(ActionIDFestivalOnlineAward, []byte{index})
}

// ParseFestivalOnlineTierIndexes 解析 218/31 回包中的奖励档位索引。
//
// [Int, [Byte, UByte, [Int,Int], Int, Int]]。
func ParseFestivalOnlineTierIndexes(payload []byte) ([]uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	if _, err := r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("节日在线奖励头部读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, nil
	}
	indexes := make([]uint8, 0, count)
	for i := 0; i < int(count); i++ {
		idx, err := r.ReadUint8()
		if err != nil {
			return indexes, err
		}
		if _, err = r.ReadUint8(); err != nil {
			return indexes, err
		}
		pairs, err := r.ReadUint16()
		if err != nil {
			return indexes, err
		}
		for j := 0; j < int(pairs); j++ {
			if _, err = r.ReadInt32(); err != nil {
				return indexes, err
			}
			if _, err = r.ReadInt32(); err != nil {
				return indexes, err
			}
		}
		if _, err = r.ReadInt32(); err != nil {
			return indexes, err
		}
		if _, err = r.ReadInt32(); err != nil {
			return indexes, err
		}
		indexes = append(indexes, idx)
	}
	return indexes, nil
}

// FestivalOnlineResultText 返回节日在线奖励结果码文本。
func FestivalOnlineResultText(code uint8) string {
	switch code {
	case FestivalOnlineSuccess:
		return "成功"
	case FestivalOnlineError:
		return "失败"
	case FestivalOnlineNonActivityTime:
		return "非活动时间"
	case FestivalOnlineHadGet:
		return "已领取"
	case FestivalOnlineFunctionNotOpen:
		return "功能未开放"
	case FestivalOnlinePackFull:
		return "背包已满"
	case FestivalOnlineTimesLimit:
		return "次数上限"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
