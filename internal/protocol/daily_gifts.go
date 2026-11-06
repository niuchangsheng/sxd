// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件汇总每日免费礼包类协议：
//   - 每日俸禄 Mod_Player_Base.get_player_camp_salary (Module 0, Action 20 -> 0x00000014)
//   - 每日灵石 Mod_SealSoul_Base.get_day_stone        (Module 34, Action 18 -> 0x00220012)
//   - PC 微端礼包 Mod_Player_Base.pc_client_gift_info (Module 0, Action 55 -> 0x00000037)
//   - PC 微端礼包领取 Mod_Player_Base.receive_client_gift (Module 0, Action 56 -> 0x00000038)
package protocol

import "fmt"

// 每日礼包动作编号。
const (
	// ActionIDPlayerCampSalary 领取每日俸禄 (Module 0, Action 20 -> 0x00000014)
	ActionIDPlayerCampSalary uint32 = 0x00000014
	// ActionIDSealSoulDayStone 领取每日封灵石 (Module 34, Action 18 -> 0x00220012)
	ActionIDSealSoulDayStone uint32 = 0x00220012
	// ActionIDPlayerClientGiftInfo 查询 PC 微端礼包 (Module 0, Action 55 -> 0x00000037)
	ActionIDPlayerClientGiftInfo uint32 = 0x00000037
	// ActionIDPlayerReceiveClientGift 领取 PC 微端礼包 (Module 0, Action 56 -> 0x00000038)
	ActionIDPlayerReceiveClientGift uint32 = 0x00000038
)

// 每日俸禄结果码 (Mod_Player_Base)。
const (
	// PlayerCampSalarySuccess 领取成功
	PlayerCampSalarySuccess uint8 = 97
	// PlayerCampSalaryAlreadyGet 今日已领取
	PlayerCampSalaryAlreadyGet uint8 = 98
	// PlayerCampSalaryNotJoinCamp 未加入阵营
	PlayerCampSalaryNotJoinCamp uint8 = 99
	// PlayerCampSalaryFailed 领取失败
	PlayerCampSalaryFailed uint8 = 100
	// PlayerCampSalaryPackFull 背包已满
	PlayerCampSalaryPackFull uint8 = 101
)

// 每日灵石状态码 (SealStoneView.message)。
const (
	// SealSoulDayStoneFail 领取失败
	SealSoulDayStoneFail uint8 = 0
	// SealSoulDayStoneSuccess 领取成功
	SealSoulDayStoneSuccess uint8 = 1
	// SealSoulDayStoneNoIngot 元宝不足
	SealSoulDayStoneNoIngot uint8 = 2
	// SealSoulDayStoneNoVIP VIP 不足
	SealSoulDayStoneNoVIP uint8 = 3
)

// PC 微端礼包领取结果码 (Mod_Player_Base)。
const (
	// PlayerClientGiftSuccess 领取成功
	PlayerClientGiftSuccess uint8 = 89
	// PlayerClientGiftBagFull 背包已满
	PlayerClientGiftBagFull uint8 = 110
)

// DailyGiftItem 表示礼包中的一件道具。
type DailyGiftItem struct {
	// ItemID 道具 ID
	ItemID int32
	// ItemNum 数量
	ItemNum int64
}

// ClientGiftInfo 表示 PC 微端礼包状态。
type ClientGiftInfo struct {
	// Status 普通礼包状态 (0 表示需要打开微端)
	Status uint8
	// Awards 普通礼包奖励
	Awards []DailyGiftItem
	// MonthStatus 月卡礼包状态
	MonthStatus uint8
	// MonthAwards 月卡礼包奖励
	MonthAwards []DailyGiftItem
}

// BuildCampSalaryPacket 构造领取每日俸禄封包 (0x00000014)。
func BuildCampSalaryPacket() *Packet {
	return NewPacket(ActionIDPlayerCampSalary, nil)
}

// ParseCampSalaryResult 解析每日俸禄回包。
func ParseCampSalaryResult(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read camp salary result: %v", ErrInvalidPayload, err)
	}
	coin, err := r.ReadInt32()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read camp salary coin: %v", ErrInvalidPayload, err)
	}
	return result, coin, nil
}

// BuildSealSoulDayStonePacket 构造领取每日灵石封包 (0x00220012)。
//
// useType 0 为免费领取；1/2 分别消耗 100/200 元宝双倍/三倍，自动化只使用 0。
func BuildSealSoulDayStonePacket(useType uint8) *Packet {
	return NewPacket(ActionIDSealSoulDayStone, []byte{useType})
}

// ParseSealSoulDayStoneResult 解析每日灵石回包。
func ParseSealSoulDayStoneResult(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	state, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read day stone state: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadInt32()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read day stone count: %v", ErrInvalidPayload, err)
	}
	return state, count, nil
}

// BuildClientGiftInfoPacket 构造查询 PC 微端礼包封包 (0x00000037)。
func BuildClientGiftInfoPacket() *Packet {
	return NewPacket(ActionIDPlayerClientGiftInfo, nil)
}

// BuildClientGiftReceivePacket 构造领取 PC 微端礼包封包 (0x00000038)。
func BuildClientGiftReceivePacket() *Packet {
	return NewPacket(ActionIDPlayerReceiveClientGift, nil)
}

// ParseClientGiftInfoResult 解析 PC 微端礼包状态。
func ParseClientGiftInfoResult(payload []byte) (*ClientGiftInfo, error) {
	r := NewReader(payload)
	info := &ClientGiftInfo{}
	var err error
	if info.Status, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read client gift status: %v", ErrInvalidPayload, err)
	}
	if info.Awards, err = readDailyGiftItems(r, "client gift awards"); err != nil {
		return nil, err
	}
	if info.MonthStatus, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read client gift month status: %v", ErrInvalidPayload, err)
	}
	if info.MonthAwards, err = readDailyGiftItems(r, "client gift month awards"); err != nil {
		return nil, err
	}
	return info, nil
}

func readDailyGiftItems(r *Reader, name string) ([]DailyGiftItem, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read %s count: %v", ErrInvalidPayload, name, err)
	}
	items := make([]DailyGiftItem, 0, count)
	for i := 0; i < int(count); i++ {
		var it DailyGiftItem
		if it.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read %s[%d].item_id: %v", ErrInvalidPayload, name, i, err)
		}
		if it.ItemNum, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: failed to read %s[%d].item_num: %v", ErrInvalidPayload, name, i, err)
		}
		items = append(items, it)
	}
	return items, nil
}

// ParseClientGiftReceiveResult 解析 PC 微端礼包领取回包。
func ParseClientGiftReceiveResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty client gift receive result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildCampSalaryResultPacket 构造每日俸禄回包，供离线 mock 与单测使用。
func BuildCampSalaryResultPacket(result uint8, coin int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(coin)
	return NewPacket(ActionIDPlayerCampSalary, w.Bytes())
}

// BuildSealSoulDayStoneResultPacket 构造每日灵石回包，供离线 mock 与单测使用。
func BuildSealSoulDayStoneResultPacket(state uint8, count int32) *Packet {
	w := NewWriter()
	w.WriteUint8(state)
	w.WriteInt32(count)
	return NewPacket(ActionIDSealSoulDayStone, w.Bytes())
}

// BuildClientGiftInfoResultPacket 构造 PC 微端礼包回包，供离线 mock 与单测使用。
func BuildClientGiftInfoResultPacket(info ClientGiftInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Status)
	w.WriteUint16(uint16(len(info.Awards)))
	for _, it := range info.Awards {
		w.WriteInt32(it.ItemID)
		w.WriteInt64(it.ItemNum)
	}
	w.WriteUint8(info.MonthStatus)
	w.WriteUint16(uint16(len(info.MonthAwards)))
	for _, it := range info.MonthAwards {
		w.WriteInt32(it.ItemID)
		w.WriteInt64(it.ItemNum)
	}
	return NewPacket(ActionIDPlayerClientGiftInfo, w.Bytes())
}

// BuildClientGiftReceiveResultPacket 构造 PC 微端礼包领取回包，供离线 mock 与单测使用。
func BuildClientGiftReceiveResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDPlayerReceiveClientGift, []byte{result})
}
