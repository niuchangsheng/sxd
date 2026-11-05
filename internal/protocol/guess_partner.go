// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方伙伴猜猜看协议 (Mod_GuessPartner_Base, Module 369 / 0x171)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_GuessPartner_Base 与
// com.datas.GuessPartnerData：查询剩余次数 → 开始一局 → 刮图 → 提交答案。
package protocol

import "fmt"

const (
	// ActionIDGuessPartnerInfo 查询玩法剩余次数 (Module 369, Action 0 -> 0x01710000)
	ActionIDGuessPartnerInfo uint32 = 0x01710000
	// ActionIDGuessPartnerStart 开始一局 (Module 369, Action 1 -> 0x01710001)
	ActionIDGuessPartnerStart uint32 = 0x01710001
	// ActionIDGuessPartnerScrape 刮图 (Module 369, Action 2 -> 0x01710002)
	ActionIDGuessPartnerScrape uint32 = 0x01710002
	// ActionIDGuessPartnerGuess 提交答案 (Module 369, Action 3 -> 0x01710003)
	ActionIDGuessPartnerGuess uint32 = 0x01710003
)

// Mod_GuessPartner_Base 结果码。
const (
	// GuessPartnerResultSuccess 成功
	GuessPartnerResultSuccess uint8 = 0
	// GuessPartnerResultPackFull 背包已满
	GuessPartnerResultPackFull uint8 = 1
	// GuessPartnerResultError 错误
	GuessPartnerResultError uint8 = 2
	// GuessPartnerResultRight 猜对
	GuessPartnerResultRight uint8 = 3
	// GuessPartnerResultWrong 猜错
	GuessPartnerResultWrong uint8 = 4
)

// GuessPartnerRound 表示一局猜伙伴的状态。
type GuessPartnerRound struct {
	// Result 结果码
	Result uint8
	// PictureID 题目图片编号
	PictureID int32
	// RemainTime 剩余时间
	RemainTime int32
	// RemainScrapeTimes 剩余刮图次数
	RemainScrapeTimes int32
	// RemainGuessTimes 剩余作答次数
	RemainGuessTimes int32
	// RoleIDs 候选伙伴编号
	RoleIDs []int32
}

// GuessPartnerAward 表示猜对后的奖励。
type GuessPartnerAward struct {
	// ItemID 道具编号
	ItemID int32
	// Count 数量
	Count int32
}

// GuessPartnerGuessResult 表示提交答案结果。
type GuessPartnerGuessResult struct {
	// Result 结果码 (RIGHT/WRONG)
	Result uint8
	// RemainGuessTimes 剩余作答次数
	RemainGuessTimes int32
	// Award 奖励 (猜对时)
	Award GuessPartnerAward
}

// BuildGuessPartnerInfoPacket 构造查询剩余次数封包 (0x01710000)。
func BuildGuessPartnerInfoPacket() *Packet {
	return NewPacket(ActionIDGuessPartnerInfo, nil)
}

// BuildGuessPartnerStartPacket 构造开始一局封包 (0x01710001)。
func BuildGuessPartnerStartPacket() *Packet {
	return NewPacket(ActionIDGuessPartnerStart, nil)
}

// BuildGuessPartnerScrapePacket 构造刮图封包 (0x01710002)。
func BuildGuessPartnerScrapePacket() *Packet {
	return NewPacket(ActionIDGuessPartnerScrape, nil)
}

// BuildGuessPartnerGuessPacket 构造提交答案封包 (0x01710003)。
func BuildGuessPartnerGuessPacket(roleID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	return NewPacket(ActionIDGuessPartnerGuess, w.Bytes())
}

// ParseGuessPartnerInfoResult 解析剩余次数 (Int remain_player_times)。
func ParseGuessPartnerInfoResult(payload []byte) (int32, error) {
	r := NewReader(payload)
	n, err := r.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read guess partner remain times: %v", ErrInvalidPayload, err)
	}
	return n, nil
}

// ParseGuessPartnerStartResult 解析开局回包。
func ParseGuessPartnerStartResult(payload []byte) (*GuessPartnerRound, error) {
	r := NewReader(payload)
	res := &GuessPartnerRound{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess partner result: %v", ErrInvalidPayload, err)
	}
	if res.PictureID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess partner picture_id: %v", ErrInvalidPayload, err)
	}
	if res.RemainTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess partner remain_time: %v", ErrInvalidPayload, err)
	}
	if res.RemainScrapeTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess partner remain_scrape_times: %v", ErrInvalidPayload, err)
	}
	if res.RemainGuessTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess partner remain_guess_times: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read guess partner role count: %v", ErrInvalidPayload, err)
	}
	res.RoleIDs = make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read guess partner role[%d]: %v", ErrInvalidPayload, i, err)
		}
		res.RoleIDs = append(res.RoleIDs, id)
	}
	return res, nil
}

// ParseGuessPartnerScrapeResult 解析刮图结果: UByte result, Int remain_scrape_times。
func ParseGuessPartnerScrapeResult(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read scrape result: %v", ErrInvalidPayload, err)
	}
	remain, err := r.ReadInt32()
	if err != nil {
		return result, 0, fmt.Errorf("%w: failed to read scrape remain times: %v", ErrInvalidPayload, err)
	}
	return result, remain, nil
}

// ParseGuessPartnerGuessResult 解析作答结果: UByte result, Int remain_guess_times, [Int item_id, Int item_count]。
func ParseGuessPartnerGuessResult(payload []byte) (*GuessPartnerGuessResult, error) {
	r := NewReader(payload)
	res := &GuessPartnerGuessResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess result: %v", ErrInvalidPayload, err)
	}
	if res.RemainGuessTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess remain times: %v", ErrInvalidPayload, err)
	}
	if res.Award.ItemID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess award item_id: %v", ErrInvalidPayload, err)
	}
	if res.Award.Count, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess award count: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildGuessPartnerInfoResultPacket 构造剩余次数回包，供离线 mock 与单测使用。
func BuildGuessPartnerInfoResultPacket(times int32) *Packet {
	w := NewWriter()
	w.WriteInt32(times)
	return NewPacket(ActionIDGuessPartnerInfo, w.Bytes())
}

// BuildGuessPartnerStartResultPacket 构造开局回包，供离线 mock 与单测使用。
func BuildGuessPartnerStartResultPacket(round GuessPartnerRound) *Packet {
	w := NewWriter()
	w.WriteUint8(round.Result)
	w.WriteInt32(round.PictureID)
	w.WriteInt32(round.RemainTime)
	w.WriteInt32(round.RemainScrapeTimes)
	w.WriteInt32(round.RemainGuessTimes)
	w.WriteUint16(uint16(len(round.RoleIDs)))
	for _, id := range round.RoleIDs {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDGuessPartnerStart, w.Bytes())
}

// BuildGuessPartnerScrapeResultPacket 构造刮图回包，供离线 mock 与单测使用。
func BuildGuessPartnerScrapeResultPacket(result uint8, remain int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(remain)
	return NewPacket(ActionIDGuessPartnerScrape, w.Bytes())
}

// BuildGuessPartnerGuessResultPacket 构造作答回包，供离线 mock 与单测使用。
func BuildGuessPartnerGuessResultPacket(result uint8, remain int32, itemID, count int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(remain)
	w.WriteInt32(itemID)
	w.WriteInt32(count)
	return NewPacket(ActionIDGuessPartnerGuess, w.Bytes())
}
