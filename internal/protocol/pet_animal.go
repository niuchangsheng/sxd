// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现「叶公好龙」(Mod_PetAnimal_Base, Module 48 / 0x0030) 协议编解码，
// 对齐官方客户端 com.controllers.PetController、com.datas.PetAnimalData 与
// com.assist.view.info.PetLongInfo：
//
//	pet_animal_info (Act 0, 0x00300000) request=[Int player_id]
//	                 response=[String name, Byte lv, Byte star, Int exp, Int feed_num]
//	feed_pet_animal (Act 2, 0x00300002) request=[Byte feed_type]
//	                 response=[UByte result, Byte lv, Byte star, Int exp, Int exp_add, Byte feed_type, Int feed_num, Byte hit]
//	up_pet_animal   (Act 3, 0x00300003) request=[] response=[Byte lv, Byte star, Int exp]
//
// feed_type=0 为铜钱普通培养，feed_type=1 为元宝培养。自动化只允许调用 0，
// 永不调用 1，也不主动购买培养次数。
package protocol

import "fmt"

// 叶公好龙动作编号 (Mod_PetAnimal_Base)。
const (
	// ActionIDPetAnimalInfo 查询叶公好龙面板 (Action 0 -> 0x00300000)
	ActionIDPetAnimalInfo uint32 = 0x00300000
	// ActionIDPetAnimalFeed 培养叶公好龙 (Action 2 -> 0x00300002)
	ActionIDPetAnimalFeed uint32 = 0x00300002
	// ActionIDPetAnimalUp 叶公好龙升阶 (Action 3 -> 0x00300003)
	ActionIDPetAnimalUp uint32 = 0x00300003
)

// 叶公好龙培养方式 (Mod_PetAnimal_Base)。
const (
	// PetAnimalFeedTypeCoin 铜钱普通培养 (自动化唯一允许的方式)
	PetAnimalFeedTypeCoin uint8 = 0
	// PetAnimalFeedTypeIngot 元宝培养 (自动化永不使用)
	PetAnimalFeedTypeIngot uint8 = 1
)

// 叶公好龙结果码 (Mod_PetAnimal_Base)。
const (
	// PetAnimalResultSuccess 成功
	PetAnimalResultSuccess uint8 = 0
	// PetAnimalResultFailed 失败
	PetAnimalResultFailed uint8 = 1
	// PetAnimalResultTooMuchExp 经验已满，需先升阶
	PetAnimalResultTooMuchExp uint8 = 2
	// PetAnimalResultNoEnoughIngot 元宝不足
	PetAnimalResultNoEnoughIngot uint8 = 3
	// PetAnimalResultNoEnoughCoin 铜钱不足
	PetAnimalResultNoEnoughCoin uint8 = 4
	// PetAnimalResultNoEnoughLv 等级不足
	PetAnimalResultNoEnoughLv uint8 = 5
	// PetAnimalResultNotVIPLv VIP 等级不足
	PetAnimalResultNotVIPLv uint8 = 6
	// PetAnimalResultMaxLv 已达满级
	PetAnimalResultMaxLv uint8 = 7
)

// PetAnimalInfo 表示叶公好龙面板。
//
// 线格式 (pet_animal_info):
// String name, Byte lv, Byte star, Int exp, Int feed_num。
type PetAnimalInfo struct {
	// Name 龙王名称 (服务端可能返回空串)
	Name string
	// Lv 龙阶 (1..10)
	Lv uint8
	// Star 星级 (1..10)
	Star uint8
	// Exp 当前经验
	Exp int32
	// FeedNum 今日剩余培养次数
	FeedNum int32
}

// PetAnimalFeedResult 表示一次普通培养结果。
//
// 线格式 (feed_pet_animal):
// UByte result, Byte lv, Byte star, Int exp, Int exp_add,
// Byte feed_type, Int feed_num, Byte hit。
type PetAnimalFeedResult struct {
	// Result 结果码 (PetAnimalResultSuccess 等)
	Result uint8
	// Lv 培养后的龙阶
	Lv uint8
	// Star 培养后的星级
	Star uint8
	// Exp 培养后的经验
	Exp int32
	// ExpAdd 本次增加的经验
	ExpAdd int32
	// FeedType 培养方式 (0 铜钱 / 1 元宝)
	FeedType uint8
	// FeedNum 培养后剩余次数
	FeedNum int32
	// Hit 暴击标记 (0 无 / 1 小暴击 / 2 大暴击)
	Hit uint8
}

// BuildPetAnimalInfoPacket 构造叶公好龙面板查询封包 (0x00300000)。
//
// 官方请求为 [Int player_id]；末尾追加 PrevAct 由会话层在发包瞬间覆盖。
func BuildPetAnimalInfoPacket(playerID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPetAnimalInfo, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildPetAnimalInfoPacketBare 构造不带 PrevAct 的叶公好龙面板查询封包。
//
// Module 48 在部分会话上下文中会静默丢弃带 PrevAct 的面板请求；
// 自动化在主请求超时后可用该裸载荷重试一次。
func BuildPetAnimalInfoPacketBare(playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	return NewPacket(ActionIDPetAnimalInfo, w.Bytes())
}

// BuildPetAnimalFeedPacket 构造叶公好龙培养封包 (0x00300002)。
//
// 官方请求为 [Byte feed_type]；末尾追加 PrevAct。
func BuildPetAnimalFeedPacket(feedType uint8, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint8(feedType)
	if prevAct == 0 {
		prevAct = ActionIDPetAnimalInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPetAnimalFeed, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildPetAnimalUpPacket 构造叶公好龙升阶封包 (0x00300003)。
//
// 该动作免费，但仅在经验已满时由客户端调用；自动化暂不主动调用，
// 保留构造器供后续需要时复用。
func BuildPetAnimalUpPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDPetAnimalFeed
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPetAnimalUp, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParsePetAnimalInfoResult 解析叶公好龙面板回包。
func ParsePetAnimalInfoResult(payload []byte) (*PetAnimalInfo, error) {
	r := NewReader(payload)
	info := &PetAnimalInfo{}
	name, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 name 读取失败: %v", ErrInvalidPayload, err)
	}
	info.Name = name
	if info.Lv, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 lv 读取失败: %v", ErrInvalidPayload, err)
	}
	if info.Star, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 star 读取失败: %v", ErrInvalidPayload, err)
	}
	if info.Exp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 exp 读取失败: %v", ErrInvalidPayload, err)
	}
	if info.FeedNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 feed_num 读取失败: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// ParsePetAnimalFeedResult 解析叶公好龙培养回包。
func ParsePetAnimalFeedResult(payload []byte) (*PetAnimalFeedResult, error) {
	r := NewReader(payload)
	res := &PetAnimalFeedResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙培养结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if res.Result != PetAnimalResultSuccess {
		return res, nil
	}
	if res.Lv, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙培养后 lv 读取失败: %v", ErrInvalidPayload, err)
	}
	if res.Star, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙培养后 star 读取失败: %v", ErrInvalidPayload, err)
	}
	if res.Exp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙培养后 exp 读取失败: %v", ErrInvalidPayload, err)
	}
	if res.ExpAdd, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 exp_add 读取失败: %v", ErrInvalidPayload, err)
	}
	if res.FeedType, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 feed_type 读取失败: %v", ErrInvalidPayload, err)
	}
	if res.FeedNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙培养后 feed_num 读取失败: %v", ErrInvalidPayload, err)
	}
	if res.Hit, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 叶公好龙 hit 读取失败: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// PetAnimalResultText 返回叶公好龙结果码的可读文本。
func PetAnimalResultText(code uint8) string {
	switch code {
	case PetAnimalResultSuccess:
		return "成功"
	case PetAnimalResultFailed:
		return "失败"
	case PetAnimalResultTooMuchExp:
		return "经验已满，需先升阶"
	case PetAnimalResultNoEnoughIngot:
		return "元宝不足"
	case PetAnimalResultNoEnoughCoin:
		return "铜钱不足"
	case PetAnimalResultNoEnoughLv:
		return "等级不足"
	case PetAnimalResultNotVIPLv:
		return "VIP等级不足"
	case PetAnimalResultMaxLv:
		return "已达满级"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildPetAnimalInfoResultPacket 构造叶公好龙面板回包，供离线 Mock 与单测使用。
func BuildPetAnimalInfoResultPacket(info PetAnimalInfo) *Packet {
	w := NewWriter()
	w.WriteString(info.Name)
	w.WriteUint8(info.Lv)
	w.WriteUint8(info.Star)
	w.WriteInt32(info.Exp)
	w.WriteInt32(info.FeedNum)
	return NewPacket(ActionIDPetAnimalInfo, w.Bytes())
}

// BuildPetAnimalFeedResultPacket 构造叶公好龙培养回包，供离线 Mock 与单测使用。
func BuildPetAnimalFeedResultPacket(res PetAnimalFeedResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == PetAnimalResultSuccess {
		w.WriteUint8(res.Lv)
		w.WriteUint8(res.Star)
		w.WriteInt32(res.Exp)
		w.WriteInt32(res.ExpAdd)
		w.WriteUint8(res.FeedType)
		w.WriteInt32(res.FeedNum)
		w.WriteUint8(res.Hit)
	}
	return NewPacket(ActionIDPetAnimalFeed, w.Bytes())
}
