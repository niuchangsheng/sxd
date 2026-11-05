// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-08
package protocol

import (
	"errors"
	"fmt"
)

const (
	// ActionHeartbeat 心跳保活协议号 (Module 0, Action 23 -> 0x00000017)
	ActionHeartbeat uint32 = 0x00000017
	// ActionPlayerLogin 角色登录握手认证协议号
	ActionPlayerLogin uint32 = 0x00000000
	// ActionPlayerInfo 角色属性与体力同步协议号
	ActionPlayerInfo uint32 = 0x00000002
	// ActionEnterTown 进入城镇协议号
	ActionEnterTown uint32 = 0x00010000
	// ActionMissionEnter 进入/查询关卡协议号 (Module 35, Action 0)
	ActionMissionEnter uint32 = 0x00230000
	// ActionMissionSweep 关卡副本扫荡请求与响应协议号 (Module 35, Action 2)
	ActionMissionSweep uint32 = 0x00230002
	// ActionMissionQuickFinish 关卡扫荡快速完成协议号 (Module 35, Action 7)
	ActionMissionQuickFinish uint32 = 0x00230007

	// ActionUIFunctionOpen 打开功能界面 (Module 0, Action 39)
	ActionUIFunctionOpen uint32 = 0x00000027
	// ActionTownMissionActive 激活城镇关卡界面 (Module 2, Action 41)
	ActionTownMissionActive uint32 = 0x00020029
	// 英雄副本协议族 (Mod_HeroMission_Base, Module 35 -> 0x0023)
	// 对齐原版客户端 com.protocols.Mod_HeroMission_Base。
	// ActionHeroMissionList 查询指定城镇的英雄副本列表 (Module 35, Action 0)
	ActionHeroMissionList uint32 = 0x00230000
	// ActionHeroMissionBuyChallenge 购买英雄副本挑战次数 (Module 35, Action 1)
	ActionHeroMissionBuyChallenge uint32 = 0x00230001
	// ActionHeroMissionStartPractice 开始英雄副本扫荡 (Module 35, Action 2)
	ActionHeroMissionStartPractice uint32 = 0x00230002
	// ActionHeroMissionStopPractice 停止英雄副本扫荡 (Module 35, Action 3)
	ActionHeroMissionStopPractice uint32 = 0x00230003
	// ActionHeroMissionNotify 英雄副本扫荡战报/结束推送 (Module 35, Action 4)
	ActionHeroMissionNotify uint32 = 0x00230004
	// ActionHeroMissionQuickly 英雄副本扫荡快速完成 (Module 35, Action 7)
	ActionHeroMissionQuickly uint32 = 0x00230007
	// ActionHeroMissionSpecialList 查询特殊/超级英雄副本列表 (Module 35, Action 8)
	ActionHeroMissionSpecialList uint32 = 0x00230008
	// ActionHeroMissionFinishInfo 查询英雄副本首通信息 (Module 35, Action 9)
	ActionHeroMissionFinishInfo uint32 = 0x00230009

	// Module 4 (Mod_Mission_Base) 剧情副本推进与首通挑战协议族
	// ActionIDMissionGetSections 查询大地图章节已开放副本列表与状态 (Module 4, Action 0)
	ActionIDMissionGetSections uint32 = 0x00040000
	// ActionIDMissionPlotEnter 进入剧情副本关卡 (Module 4, Action 1)
	ActionIDMissionPlotEnter uint32 = 0x00040001
	// ActionIDMissionFightMonster 挑战副本内指定怪物编队 (Module 4, Action 2)
	ActionIDMissionFightMonster uint32 = 0x00040002
	// ActionIDMissionRank 结算副本通关星级与评价 (Module 4, Action 3)
	ActionIDMissionRank uint32 = 0x00040003
	// ActionIDMissionOpenBox 开启副本通关宝箱 (Module 4, Action 4)
	ActionIDMissionOpenBox uint32 = 0x00040004
	// ActionIDMissionPickupAward 拾取副本结算奖励 (Module 4, Action 5)
	ActionIDMissionPickupAward uint32 = 0x00040005

	// MissionResultSuccess Module 4 副本动作成功状态码 (0x0A = 10)
	MissionResultSuccess uint8 = 0x0A
	// MissionResultNotEnoughPower 体力不足状态码 (0x0C = 12)
	MissionResultNotEnoughPower uint8 = 0x0C
	// MissionResultPackFull 背包已满状态码 (0x0D = 13)
	MissionResultPackFull uint8 = 0x0D
)

var (
	// ErrInvalidPayload 载荷格式不合法或数据解析异常
	ErrInvalidPayload = errors.New("protocol: invalid payload format")
)

// SweepRequest 代表关卡副本扫荡请求参数。
type SweepRequest struct {
	// MissionID 关卡唯一标识符
	MissionID uint32
	// Times 扫荡次数 (通常单次或体力折算多次)
	Times uint16
}

// SweepResult 代表关卡副本扫荡执行结果及收益。
type SweepResult struct {
	// Success 扫荡是否成功
	Success bool
	// MissionID 对应关卡标识符
	MissionID uint32
	// Times 实际完成扫荡次数
	Times uint16
	// CostPower 扣除总体力点数
	CostPower int
	// GainExp 获得的经验数值
	GainExp int64
	// GainCoins 获得的铜钱数值
	GainCoins int64
	// Message 结算描述信息 (如 "扫荡完成", "体力不足", "背包已满")
	Message string
}

// BuildEnterMissionPacket 构造进入/激活关卡请求封包。
func BuildEnterMissionPacket(missionID uint32) *Packet {
	w := NewWriter()
	// 真实网关二进制帧格式: [2B MissionID] + [4B 00 23 00 00]
	w.WriteUint16(uint16(missionID))
	w.WriteBytes([]byte{0x00, 0x23, 0x00, 0x00})
	return NewPacket(ActionMissionEnter, w.Bytes())
}

// BuildQuickFinishPacket 构造快速完成关卡扫荡请求封包 (跳过倒计时)。
func BuildQuickFinishPacket() *Packet {
	w := NewWriter()
	// 真实网关二进制帧格式: [4B 前置动作号 ActionMissionSweep (0x00230002)]
	w.WriteUint32(ActionMissionSweep)
	return NewPacket(ActionMissionQuickFinish, w.Bytes())
}

// BuildSweepPacket 构造关卡扫荡二进制协议请求封包。
func BuildSweepPacket(req SweepRequest) (*Packet, error) {
	w := NewWriter()
	// 真实网关二进制帧格式: [2B MissionID] + [1B Times] + [5B 扩展控制标记]
	w.WriteUint16(uint16(req.MissionID))
	w.WriteUint8(uint8(req.Times))
	w.WriteBytes([]byte{0x00, 0x00, 0x23, 0x00, 0x00})
	return NewPacket(ActionMissionSweep, w.Bytes()), nil
}

// ParseSweepRequest 从封包载荷中反序列化扫荡请求参数。
func ParseSweepRequest(payload []byte) (*SweepRequest, error) {
	if len(payload) < 3 {
		return nil, fmt.Errorf("%w: payload too short", ErrInvalidPayload)
	}
	r := NewReader(payload)
	missionID, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read mission_id: %v", ErrInvalidPayload, err)
	}
	times, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read times: %v", ErrInvalidPayload, err)
	}
	return &SweepRequest{
		MissionID: uint32(missionID),
		Times:     uint16(times),
	}, nil
}

// BuildSweepResultPacket 构造关卡扫荡结算响应封包。
func BuildSweepResultPacket(res SweepResult) (*Packet, error) {
	w := NewWriter()
	var successFlag uint8
	if res.Success {
		successFlag = 0x03 // 真实网关成功标识码 0x03
	}
	w.WriteUint8(successFlag)
	w.WriteUint32(res.MissionID)
	w.WriteUint16(res.Times)
	w.WriteInt32(int32(res.CostPower))
	w.WriteInt64(res.GainExp)
	w.WriteInt64(res.GainCoins)
	w.WriteString(res.Message)

	return NewPacket(ActionMissionSweep, w.Bytes()), nil
}

// ParseSweepResult 从响应封包载荷中反序列化扫荡结果。
func ParseSweepResult(payload []byte) (*SweepResult, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: empty sweep payload", ErrInvalidPayload)
	}
	r := NewReader(payload)
	flag, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read status flag: %v", ErrInvalidPayload, err)
	}

	// 真实网关短帧协议兼容: 首字节为 0x03 或 0x01 表示成功
	isSuccess := flag == 0x03 || flag == 0x01
	res := &SweepResult{
		Success: isSuccess,
		Message: "扫荡完成",
	}

	// 若载荷为完整长包 (测试桩或带元数据的回包) 则继续深度反序列化
	if r.Remaining() >= 30 {
		if mid, err := r.ReadUint32(); err == nil {
			res.MissionID = mid
		}
		if times, err := r.ReadUint16(); err == nil {
			res.Times = times
		}
		if costPower, err := r.ReadInt32(); err == nil {
			res.CostPower = int(costPower)
		}
		if gainExp, err := r.ReadInt64(); err == nil {
			res.GainExp = gainExp
		}
		if gainCoins, err := r.ReadInt64(); err == nil {
			res.GainCoins = gainCoins
		}
		if msg, err := r.ReadString(); err == nil && msg != "" {
			res.Message = msg
		}
	} else if r.Remaining() >= 4 {
		// 真实 5 字节短帧回包: 携带剩余次数或消耗
		if v, err := r.ReadInt32(); err == nil {
			res.CostPower = int(v)
		}
	}

	return res, nil
}

// BuildLoginPacket 构造角色登录握手协议封包。
func BuildLoginPacket(roleName, token string) (*Packet, error) {
	w := NewWriter()
	w.WriteString(roleName)
	w.WriteString(token)
	return NewPacket(ActionPlayerLogin, w.Bytes()), nil
}

// ParseLoginPacket 从登录封包载荷中解析角色名称与认证 Token。
func ParseLoginPacket(payload []byte) (roleName, token string, err error) {
	r := NewReader(payload)
	name, err := r.ReadString()
	if err != nil {
		return "", "", fmt.Errorf("%w: failed to read role_name: %v", ErrInvalidPayload, err)
	}
	tok, err := r.ReadString()
	if err != nil {
		return "", "", fmt.Errorf("%w: failed to read token: %v", ErrInvalidPayload, err)
	}
	return name, tok, nil
}

// 英雄副本 (Mod_HeroMission_Base) 业务结果码与推送类型。
// 对齐原版客户端 com.protocols.Mod_HeroMission_Base 静态常量。
const (
	// HeroMissionScoreA/B/C 英雄副本评价等级 (非业务结果码)。
	HeroMissionScoreA uint8 = 0
	HeroMissionScoreB uint8 = 1
	HeroMissionScoreC uint8 = 2
	// HeroMissionResultSuccess 操作成功。
	HeroMissionResultSuccess uint8 = 3
	// HeroMissionResultNotEnoughIngot 元宝不足。
	HeroMissionResultNotEnoughIngot uint8 = 4
	// HeroMissionResultFullBuyTimes 购买次数已达上限。
	HeroMissionResultFullBuyTimes uint8 = 5
	// HeroMissionResultBagFull 背包已满。
	HeroMissionResultBagFull uint8 = 6
	// HeroMissionResultNotEnoughPower 体力不足。
	HeroMissionResultNotEnoughPower uint8 = 7
	// HeroMissionResultNoMission 当前城镇暂无可扫荡英雄副本。
	HeroMissionResultNoMission uint8 = 8
	// HeroMissionNotifyNewWarReport 扫荡战报推送。
	HeroMissionNotifyNewWarReport uint8 = 9
	// HeroMissionNotifyFinish 扫荡队列结束推送。
	HeroMissionNotifyFinish uint8 = 10
)

// HeroMissionItem 代表城镇内单个英雄副本条目。
// 对齐官方响应 [IntUtil, ByteUtil, UByteUtil, ByteUtil]。
type HeroMissionItem struct {
	MissionID    int32 // 关卡配置编号
	CanChallenge int8  // 是否可挑战 (1=可挑战)
	Rank         uint8 // 评价等级 (HeroMissionScoreA/B/C)
	Finished     int8  // 是否已首通 (1=已通关)
}

// HeroMissionList 代表指定城镇的英雄副本列表与购买状态。
// 对齐官方响应 [[IntUtil,ByteUtil,UByteUtil,ByteUtil],ByteUtil,ByteUtil,ShortUtil,IntUtil]。
type HeroMissionList struct {
	Missions         []HeroMissionItem // 城镇内英雄副本条目
	AllBuyTimes      int8              // 当日总可购买次数
	RemainBuyTimes   int8              // 剩余可购买次数
	BuyPrice         int16             // 单次购买价格 (元宝)
	FinishMissionNum int32             // 已通关英雄副本数量
}

// BuildHeroMissionListPacket 构造查询指定城镇英雄副本列表请求封包 (Module 35, Action 0)。
// 官方请求 schema: request=[ShortUtil(town_id)]，实机验证严格 2 字节，不追加 PrevAct。
func BuildHeroMissionListPacket(townID uint16) *Packet {
	w := NewWriter()
	w.WriteUint16(townID)
	return NewPacket(ActionHeroMissionList, w.Bytes())
}

// ParseHeroMissionListResponse 从响应载荷中反序列化英雄副本列表。
func ParseHeroMissionListResponse(payload []byte) (*HeroMissionList, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read hero mission count: %v", ErrInvalidPayload, err)
	}
	out := &HeroMissionList{Missions: make([]HeroMissionItem, 0, count)}
	for i := uint16(0); i < count; i++ {
		missionID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read hero mission #%d id: %v", ErrInvalidPayload, i, err)
		}
		canChallenge, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read hero mission #%d can_challenge: %v", ErrInvalidPayload, i, err)
		}
		rank, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read hero mission #%d rank: %v", ErrInvalidPayload, i, err)
		}
		finished, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read hero mission #%d finished: %v", ErrInvalidPayload, i, err)
		}
		out.Missions = append(out.Missions, HeroMissionItem{
			MissionID:    missionID,
			CanChallenge: canChallenge,
			Rank:         rank,
			Finished:     finished,
		})
	}
	if out.AllBuyTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read all_buy_times: %v", ErrInvalidPayload, err)
	}
	if out.RemainBuyTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read remain_buy_times: %v", ErrInvalidPayload, err)
	}
	if out.BuyPrice, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read buy_price: %v", ErrInvalidPayload, err)
	}
	if out.FinishMissionNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read finish_mission_num: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// HeroMissionNotify 代表英雄副本扫荡过程中的服务端主动推送。
type HeroMissionNotify struct {
	Type              uint8 // HeroMissionNotifyNewWarReport / HeroMissionNotifyFinish / 错误码
	NewWarReportCount int   // 本轮战报条数
	RankReportCount   int   // 本轮评价结算条数
	EmptyBagNumber    int16 // 剩余背包空格
}

// ParseHeroMissionNotify 解析英雄副本扫荡推送 (Module 35, Action 4)。
// 官方响应 schema:
//
//	[UByte type,
//	 [[Int,Int],[Int,Int],Short] new_war_reports,
//	 [Int,Int,Short,Int,[Int,Int],[Int,Int],Short,Short,Int,Int] rank_reports,
//	 Short empty_bag_number]
func ParseHeroMissionNotify(payload []byte) (*HeroMissionNotify, error) {
	r := NewReader(payload)
	typ, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read hero notify type: %v", ErrInvalidPayload, err)
	}
	out := &HeroMissionNotify{Type: typ}

	// 1. new_war_reports
	warCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read new_war_report count: %v", ErrInvalidPayload, err)
	}
	for i := uint16(0); i < warCount; i++ {
		if err := skipIntPairArray(r); err != nil {
			return nil, fmt.Errorf("%w: new_war_report #%d experience: %v", ErrInvalidPayload, i, err)
		}
		if err := skipIntPairArray(r); err != nil {
			return nil, fmt.Errorf("%w: new_war_report #%d award: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: new_war_report #%d worship: %v", ErrInvalidPayload, i, err)
		}
	}
	out.NewWarReportCount = int(warCount)

	// 2. rank_reports
	rankCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read rank_report count: %v", ErrInvalidPayload, err)
	}
	for i := uint16(0); i < rankCount; i++ {
		fields := []func(*Reader) error{
			skipInt32, skipInt32, skipInt16, skipInt32,
			skipIntPairArray, skipIntPairArray,
			skipInt16, skipInt16, skipInt32, skipInt32,
		}
		for _, f := range fields {
			if err := f(r); err != nil {
				return nil, fmt.Errorf("%w: rank_report #%d: %v", ErrInvalidPayload, i, err)
			}
		}
	}
	out.RankReportCount = int(rankCount)

	emptyBag, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read empty_bag_number: %v", ErrInvalidPayload, err)
	}
	out.EmptyBagNumber = emptyBag
	return out, nil
}

func skipInt32(r *Reader) error {
	_, err := r.ReadInt32()
	return err
}

func skipInt16(r *Reader) error {
	_, err := r.ReadInt16()
	return err
}

// skipIntPairArray 跳过一个 [IntUtil, IntUtil] 数组 (2 字节数量前缀 + 每项 8 字节)。
func skipIntPairArray(r *Reader) error {
	count, err := r.ReadUint16()
	if err != nil {
		return err
	}
	for i := uint16(0); i < count; i++ {
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	return nil
}

// MissionSectionItem 代表 Module 4 get_sections 返回的单个章节关卡状态条目 (10 字节)。
type MissionSectionItem struct {
	MissionID     int32
	Status        int8  // 1=已通关, 0=已开放未通关
	Rank          uint8 // 评价星级
	IsBoss        int8  // 是否 Boss 关
	CompleteCount int16 // 完成次数
	Difficulty    int8  // 难度
}

// BuildMissionGetSectionsPacket 构造查询大地图章节副本进度请求封包 (Module 4, Action 0)。
func BuildMissionGetSectionsPacket(mapID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(mapID)
	if prevAct == 0 {
		prevAct = ActionIDPlayerGetInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMissionGetSections, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseMissionGetSectionsResponse 解析大地图章节副本进度回包 (Module 4, Action 0)。
func ParseMissionGetSectionsResponse(payload []byte) ([]MissionSectionItem, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: get_sections payload too short", ErrInvalidPayload)
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	items := make([]MissionSectionItem, 0, count)
	for i := uint16(0); i < count; i++ {
		if r.Remaining() < 10 {
			break
		}
		mid, _ := r.ReadInt32()
		status, _ := r.ReadInt8()
		rank, _ := r.ReadUint8()
		isBoss, _ := r.ReadInt8()
		compCount, _ := r.ReadInt16()
		diff, _ := r.ReadInt8()
		items = append(items, MissionSectionItem{
			MissionID:     mid,
			Status:        status,
			Rank:          rank,
			IsBoss:        isBoss,
			CompleteCount: compCount,
			Difficulty:    diff,
		})
	}
	return items, nil
}

// BuildMissionPlotEnterPacket 构造进入剧情副本关卡请求封包 (Module 4, Action 1)。
func BuildMissionPlotEnterPacket(missionID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(missionID)
	if prevAct == 0 {
		prevAct = ActionIDMissionGetSections
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMissionPlotEnter, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildMissionFightMonsterPacket 构造挑战副本内怪物编队请求封包 (Module 4, Action 2)。
// 2026 网关实测格式: [Int32(monsterTeamID), Int8(attackType), Int32(hireRoleID), Uint32(prevAct)] (共 13 字节)。
// 注意: 省略 hireRoleID 的 9 字节变体会被服务端静默丢弃且不产生任何回包。
func BuildMissionFightMonsterPacket(monsterTeamID int32, attackType int8, hireRoleID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(monsterTeamID)
	w.WriteInt8(attackType)
	w.WriteInt32(hireRoleID)
	if prevAct == 0 {
		prevAct = ActionIDMissionPlotEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMissionFightMonster, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildMissionRankPacket 构造副本通关星级评价结算请求封包 (Module 4, Action 3)。
func BuildMissionRankPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDMissionFightMonster
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMissionRank, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildMissionOpenBoxPacket 构造开启副本通关宝箱请求封包 (Module 4, Action 4)。
func BuildMissionOpenBoxPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDMissionRank
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMissionOpenBox, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildMissionPickupAwardPacket 构造拾取副本通关奖励请求封包 (Module 4, Action 5)。
func BuildMissionPickupAwardPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDMissionOpenBox
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMissionPickupAward, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}
