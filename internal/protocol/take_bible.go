// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现基于官方 AS3 客户端 Mod_TakeBible_Base (Module 30 / 0x001E) 逆向对齐的西天取经与护送功能的二进制封包编解码。
//
// @author Ateng
// @since 2026-10-28
package protocol

import (
	"fmt"
)

// --------------------------------------------------------------------------------
// 西天取经使者等级 (Courier / Angel Type)
// --------------------------------------------------------------------------------

const (
	TakeBibleBaiLongMa int8 = 0 // 白龙马 (基础使者)
	TakeBibleShaWuJing int8 = 1 // 沙悟净
	TakeBibleZhuBaJie  int8 = 2 // 猪八戒
	TakeBibleSunWuKong int8 = 3 // 孙悟空
	TakeBibleTangSeng  int8 = 4 // 唐僧 (最高收益使者)
)

// AngelName 返回使者对应的中文名称。
func AngelName(angel int8) string {
	switch angel {
	case TakeBibleBaiLongMa:
		return "白龙马"
	case TakeBibleShaWuJing:
		return "沙悟净"
	case TakeBibleZhuBaJie:
		return "猪八戒"
	case TakeBibleSunWuKong:
		return "孙悟空"
	case TakeBibleTangSeng:
		return "唐僧"
	default:
		return fmt.Sprintf("未知使者(%d)", angel)
	}
}

// --------------------------------------------------------------------------------
// 西天取经业务状态码 (Result Code)
// --------------------------------------------------------------------------------

const (
	TakeBibleSuccess            uint8 = 8  // 成功
	TakeBibleInvalidPlayerID    uint8 = 9  // 好友ID无效
	TakeBibleFullProtectTimes   uint8 = 10 // 护送好友取经中/次数已满
	TakeBibleNotOnline          uint8 = 11 // 好友不在线
	TakeBibleStatusBusy         uint8 = 12 // 护送取经状态不能重复护送
	TakeBibleCancelApply        uint8 = 13 // 好友已取消申请护送
	TakeBibleProtectStatus      uint8 = 14 // 该玩家已经在护送中
	TakeBibleNotEnoughIngot     uint8 = 15 // 元宝不足
	TakeBibleRequestVip         uint8 = 16 // VIP等级不足
	TakeBibleFullTakeBibleTimes uint8 = 17 // 今日护送取经次数已满 (每日上限3次)
	TakeBibleReRob              uint8 = 18 // 重复拦截
	TakeBibleFullRobTimes       uint8 = 19 // 今日拦截次数已满 (每日上限4次)
	TakeBibleHaveCDTime         uint8 = 20 // 拦截冷却中
	TakeBibleBeRobAll           uint8 = 21 // 目标队伍已被抢光
)

// --------------------------------------------------------------------------------
// 实体结构体定义
// --------------------------------------------------------------------------------

// TakeBibleFriend 表示可邀请护送的好友信息。
type TakeBibleFriend struct {
	PlayerID           int32  // 好友角色 ID
	PlayerName         string // 好友角色名称
	RemainProtectTimes int8   // 剩余护送次数
	OnlineStatus       int8   // 在线状态 (1:在线, 0:离线)
	IsEscorting        int8   // 是否正在护送中 (1:护送中, 0:空闲)
}

// TakeBibleOpenResult 表示打开取经场景 (0x001E0000) 返回的概览信息。
type TakeBibleOpenResult struct {
	CanTakeTimes int8  // 剩余可护送次数
	ProtectTimes int8  // 剩余可保护次数
	RobTimes     int8  // 剩余打劫次数
	CDSeconds    int32 // 打劫冷却倒计时秒数
}

// TakeBibleGetInfoResult 表示取经准备面板 (0x001E0002) 详情回包。
type TakeBibleGetInfoResult struct {
	ResultCode      uint8             // 状态码 (8 为成功)
	CurrentAngel    int8              // 当前使者类型 (0:白龙马 .. 4:唐僧)
	ProtectPlayerID int32             // 当前绑定的保护好友 ID (0 为无)
	SpeedUpCD       int16             // 加速冷却/保护倒计时
	ProtectFriends  []TakeBibleFriend // 在线好友保护者列表
	RobTimes        int8              // 剩余拦截次数
	TotalTakeTimes  int8              // 今日已取经次数
	IsBusy          bool              // 是否当前正在取经途中
}

// TakeBibleRefreshResult 表示刷新取经使者 (0x001E0008) 响应结果。
type TakeBibleRefreshResult struct {
	ResultCode      uint8 // 状态码 (8 为成功)
	NewAngel        int8  // 刷新后的新使者 (0..4)
	NextRefreshCost int16 // 下次刷新所需元宝
}

// TakeBibleRobCDResult 表示查询打劫 CD 与剩余次数响应结果。
type TakeBibleRobCDResult struct {
	CDSeconds   int32 // 冷却倒计时秒数
	CanRobTimes int8  // 剩余拦截次数
}

// --------------------------------------------------------------------------------
// 封包构造函数 (Packet Builders)
// --------------------------------------------------------------------------------

// BuildTakeBibleOpenPacket 构造打开取经主面板封包 (Module 30, Action 0 -> 0x001E0000)。
func BuildTakeBibleOpenPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleOpen, w.Bytes())
}

// BuildTakeBibleClosePacket 构造关闭取经面板封包 (Module 30, Action 1 -> 0x001E0001)。
func BuildTakeBibleClosePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleClose, w.Bytes())
}

// BuildTakeBibleGetInfoPacket 构造拉取取经准备详情封包 (Module 30, Action 2 -> 0x001E0002)。
func BuildTakeBibleGetInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleGetInfo, w.Bytes())
}

// BuildTakeBibleApplyFriendPacket 构造申请好友护送封包 (Module 30, Action 3 -> 0x001E0003)。
func BuildTakeBibleApplyFriendPacket(friendPlayerID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(friendPlayerID)
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleApplyFriend, w.Bytes())
}

// BuildTakeBibleCancelApplyPacket 构造取消好友护送申请封包 (Module 30, Action 4 -> 0x001E0004)。
func BuildTakeBibleCancelApplyPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleApplyFriend
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleCancelApply, w.Bytes())
}

// BuildTakeBibleApproveApplyPacket 构造同意好友护送申请封包 (Module 30, Action 5 -> 0x001E0005)。
func BuildTakeBibleApproveApplyPacket(applyRoleID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(applyRoleID)
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleApproveApply, w.Bytes())
}

// BuildTakeBibleBuyProtectionPacket 构造购买元宝保护封包 (Module 30, Action 7 -> 0x001E0007)。
func BuildTakeBibleBuyProtectionPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleBuyProtection, w.Bytes())
}

// BuildTakeBibleRefreshPacket 构造刷新取经使者封包 (Module 30, Action 8 -> 0x001E0008)。
func BuildTakeBibleRefreshPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleRefresh, w.Bytes())
}

// BuildTakeBibleStartPacket 构造正式启程开始护送取经封包 (Module 30, Action 10 -> 0x001E000A)。
func BuildTakeBibleStartPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleStart, w.Bytes())
}

// BuildTakeBibleRobPacket 构造发起拦截打劫队伍封包 (Module 30, Action 11 -> 0x001E000B)。
func BuildTakeBibleRobPacket(targetPlayerID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(targetPlayerID)
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleRob, w.Bytes())
}

// BuildTakeBibleQuicklyPacket 构造加速到达/快速完成取经封包 (Module 30, Action 14 -> 0x001E000E)。
func BuildTakeBibleQuicklyPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleQuickly, w.Bytes())
}

// BuildTakeBibleGetRobCDPacket 构造查询拦截打劫 CD 封包 (Module 30, Action 16 -> 0x001E0010)。
func BuildTakeBibleGetRobCDPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleGetRobCD, w.Bytes())
}

// BuildTakeBibleBuyBlessPacket 构造购买取经鼓舞祝福封包 (Module 30, Action 18 -> 0x001E0012)。
func BuildTakeBibleBuyBlessPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTakeBibleGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDTakeBibleBuyBless, w.Bytes())
}

// --------------------------------------------------------------------------------
// 载荷解析函数 (Response Parsers)
// --------------------------------------------------------------------------------

// ParseTakeBibleOpenResult 解析打开取经场景响应 (0x001E0000)。
func ParseTakeBibleOpenResult(payload []byte) (*TakeBibleOpenResult, error) {
	if len(payload) == 0 {
		return &TakeBibleOpenResult{CanTakeTimes: 3, ProtectTimes: 3, RobTimes: 4}, nil
	}
	r := NewReader(payload)
	res := &TakeBibleOpenResult{CanTakeTimes: 3, ProtectTimes: 3, RobTimes: 4}

	if r.Remaining() >= 4 {
		_, _ = r.ReadInt32() // 场景标识或流水号
	}
	if r.Remaining() >= 1 {
		b, _ := r.ReadUint8()
		res.CanTakeTimes = int8(b)
	}
	if r.Remaining() >= 4 {
		_, _ = r.ReadInt32()
	}
	if r.Remaining() >= 4 {
		sec, _ := r.ReadInt32()
		res.CDSeconds = sec
	}
	if r.Remaining() >= 2 {
		_, _ = r.ReadUint16()
	}
	if r.Remaining() >= 1 {
		b, _ := r.ReadUint8()
		res.ProtectTimes = int8(b)
	}
	if r.Remaining() >= 1 {
		b, _ := r.ReadUint8()
		res.RobTimes = int8(b)
	}
	return res, nil
}

// ParseTakeBibleGetInfoResult 解析取经准备详情回包 (0x001E0002)。
func ParseTakeBibleGetInfoResult(payload []byte) (*TakeBibleGetInfoResult, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: take bible get info payload too short (%d bytes)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	res := &TakeBibleGetInfoResult{
		ResultCode:   TakeBibleSuccess,
		CurrentAngel: TakeBibleBaiLongMa,
		RobTimes:     4,
	}

	code, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	res.ResultCode = code

	if r.Remaining() >= 1 {
		angel, _ := r.ReadInt8()
		res.CurrentAngel = angel
	}
	if r.Remaining() >= 4 {
		pID, _ := r.ReadInt32()
		res.ProtectPlayerID = pID
	}
	if r.Remaining() >= 2 {
		cd, _ := r.ReadInt16()
		res.SpeedUpCD = cd
	}

	// 解析在线好友保护列表 (数组长度 2 字节)
	if r.Remaining() >= 2 {
		fCount, rErr := r.ReadUint16()
		if rErr == nil && fCount > 0 && fCount < 200 {
			res.ProtectFriends = make([]TakeBibleFriend, 0, fCount)
			for i := 0; i < int(fCount); i++ {
				if r.Remaining() < 4 {
					break
				}
				pID, _ := r.ReadInt32()
				name, _ := r.ReadString()
				remainProtect, _ := r.ReadInt8()
				onlineStatus, _ := r.ReadInt8()
				isEscorting, _ := r.ReadInt8()
				res.ProtectFriends = append(res.ProtectFriends, TakeBibleFriend{
					PlayerID:           pID,
					PlayerName:         name,
					RemainProtectTimes: remainProtect,
					OnlineStatus:       onlineStatus,
					IsEscorting:        isEscorting,
				})
			}
		}
	}

	// 剩余尾部状态字段
	if r.Remaining() >= 1 {
		tTimes, _ := r.ReadInt8()
		res.TotalTakeTimes = tTimes
	}
	if r.Remaining() >= 1 {
		rTimes, _ := r.ReadInt8()
		res.RobTimes = rTimes
	}
	if r.Remaining() >= 1 {
		status, _ := r.ReadUint8()
		// 状态为 1 表示正在护送途中
		res.IsBusy = (status == 1 || res.ResultCode == TakeBibleStatusBusy)
	}

	return res, nil
}

// ParseTakeBibleRefreshResult 解析刷新取经使者响应 (0x001E0008)。
func ParseTakeBibleRefreshResult(payload []byte) (*TakeBibleRefreshResult, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: take bible refresh payload empty", ErrInvalidPayload)
	}
	r := NewReader(payload)
	res := &TakeBibleRefreshResult{ResultCode: TakeBibleSuccess}

	code, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	res.ResultCode = code

	if r.Remaining() >= 1 {
		angel, _ := r.ReadInt8()
		res.NewAngel = angel
	}
	if r.Remaining() >= 2 {
		cost, _ := r.ReadInt16()
		res.NextRefreshCost = cost
	}
	return res, nil
}

// ParseTakeBibleApplyFriendResult 解析申请好友保护响应 (0x001E0003)。
func ParseTakeBibleApplyFriendResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return TakeBibleSuccess, nil
	}
	r := NewReader(payload)
	return r.ReadUint8()
}

// ParseTakeBibleStartResult 解析开始取经响应 (0x001E000A)。
func ParseTakeBibleStartResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return TakeBibleSuccess, nil
	}
	r := NewReader(payload)
	return r.ReadUint8()
}

// ParseTakeBibleGetRobCDResult 解析查询打劫冷却时间回包 (0x001E0010)。
func ParseTakeBibleGetRobCDResult(payload []byte) (*TakeBibleRobCDResult, error) {
	res := &TakeBibleRobCDResult{CanRobTimes: 4}
	if len(payload) == 0 {
		return res, nil
	}
	r := NewReader(payload)
	if r.Remaining() >= 4 {
		sec, _ := r.ReadInt32()
		res.CDSeconds = sec
	}
	if r.Remaining() >= 1 {
		times, _ := r.ReadInt8()
		res.CanRobTimes = times
	}
	return res, nil
}
