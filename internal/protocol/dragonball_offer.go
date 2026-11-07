// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现神龙上供协议 (Mod_Dragonball_Base, Module 111 / 0x006F) 中的普通上供动作。
// 普通上供每次消耗铜钱并获得活跃度与龙珠经验，不消耗元宝。
package protocol

import "fmt"

const (
	// ModuleDragonball 神龙/龙珠模块编号 (0x006F)
	ModuleDragonball uint16 = 111
)

// 神龙上供动作与参数 (Mod_Dragonball_Base)。
const (
	// ActionIDDragonballTmpInfo 查询祭坛临时格龙珠 (Module 111, Action 0 -> 0x006F0000)
	ActionIDDragonballTmpInfo uint32 = 0x006F0000
	// ActionIDDragonballCollect 收取临时格龙珠到背包 (Module 111, Action 1 -> 0x006F0001)
	ActionIDDragonballCollect uint32 = 0x006F0001
	// ActionIDDragonballSacrifice 上供/祭坛 (Module 111, Action 2 -> 0x006F0002)
	ActionIDDragonballSacrifice uint32 = 0x006F0002
	// ActionIDDragonballOneKeyMerge 按品质一键合成龙珠 (Module 111, Action 6 -> 0x006F0006)
	ActionIDDragonballOneKeyMerge uint32 = 0x006F0006
	// ActionIDDragonballSell 出售祭坛/背包龙珠 (Module 111, Action 7 -> 0x006F0007)
	ActionIDDragonballSell uint32 = 0x006F0007

	// DragonballSacrificeNormal 普通上供 (消耗铜钱)
	DragonballSacrificeNormal uint8 = 4
	// DragonballSacrificeHighLevel 高级上供 (消耗元宝，自动化永不使用)
	DragonballSacrificeHighLevel uint8 = 5
	// DragonballSacrificeAuto 自动选择
	DragonballSacrificeAuto uint8 = 6
	// DragonballSacrificeNotAuto 非自动选择
	DragonballSacrificeNotAuto uint8 = 7
)

// 神龙上供结果码 (Mod_Dragonball_Base)。
const (
	// DragonballResultSuccess 成功
	DragonballResultSuccess uint8 = 0
	// DragonballResultPackageFull 背包已满
	DragonballResultPackageFull uint8 = 1
	// DragonballResultInvalidDragonball 无效龙珠
	DragonballResultInvalidDragonball uint8 = 2
	// DragonballResultFunctionNoOpen 功能未开放
	DragonballResultFunctionNoOpen uint8 = 3
	// DragonballResultTmpGridFull 临时格子已满
	DragonballResultTmpGridFull uint8 = 8
	// DragonballResultTmpGridNotEnough 临时格子不足
	DragonballResultTmpGridNotEnough uint8 = 9
	// DragonballResultNoEnoughCoin 铜钱不足
	DragonballResultNoEnoughCoin uint8 = 10
	// DragonballResultNoEnoughIngot 元宝不足
	DragonballResultNoEnoughIngot uint8 = 11
	// DragonballResultTimesUseUp 今日上供次数已用尽
	DragonballResultTimesUseUp uint8 = 12
	// DragonballResultNotEnoughLevel 等级不足
	DragonballResultNotEnoughLevel uint8 = 13
	// DragonballResultFailure 失败
	DragonballResultFailure uint8 = 29
)

// DragonballAlert 表示上供后临时格中的龙珠提示。
type DragonballAlert struct {
	// ItemID 龙珠道具 ID
	ItemID int32
	// Num 数量
	Num int16
	// Flag 状态标志
	Flag uint8
}

// DragonballSacrificeResult 表示一次神龙上供结果。
type DragonballSacrificeResult struct {
	// Result 结果码
	Result uint8
	// Alerts 临时格龙珠提示
	Alerts []DragonballAlert
	// RemainTimes 今日剩余上供次数
	RemainTimes uint8
	// Score 龙珠经验
	Score int16
	// TotalScore 累计龙珠经验
	TotalScore int32
	// EventID 事件 ID
	EventID int32
}

// BuildDragonballSacrificePacket 构造神龙上供封包 (0x006F0002)。
func BuildDragonballSacrificePacket(mode, auto uint8) *Packet {
	return NewPacket(ActionIDDragonballSacrifice, []byte{mode, auto})
}

// ParseDragonballSacrificeResult 解析神龙上供回包。
func ParseDragonballSacrificeResult(payload []byte) (*DragonballSacrificeResult, error) {
	r := NewReader(payload)
	res := &DragonballSacrificeResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read dragonball result: %v", ErrInvalidPayload, err)
	}
	if res.Result != DragonballResultSuccess {
		return res, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read dragonball alert count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		var a DragonballAlert
		if a.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read alert[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		if a.Num, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read alert[%d].num: %v", ErrInvalidPayload, i, err)
		}
		if a.Flag, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read alert[%d].flag: %v", ErrInvalidPayload, i, err)
		}
		res.Alerts = append(res.Alerts, a)
	}
	if res.RemainTimes, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read dragonball remain times: %v", ErrInvalidPayload, err)
	}
	if res.Score, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read dragonball score: %v", ErrInvalidPayload, err)
	}
	if res.TotalScore, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read dragonball total score: %v", ErrInvalidPayload, err)
	}
	if res.EventID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read dragonball event: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildDragonballSacrificeResultPacket 构造神龙上供回包，供离线 mock 与单测使用。
func BuildDragonballSacrificeResultPacket(res DragonballSacrificeResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == DragonballResultSuccess {
		w.WriteUint16(uint16(len(res.Alerts)))
		for _, a := range res.Alerts {
			w.WriteInt32(a.ItemID)
			w.WriteInt16(a.Num)
			w.WriteUint8(a.Flag)
		}
		w.WriteUint8(res.RemainTimes)
		w.WriteInt16(res.Score)
		w.WriteInt32(res.TotalScore)
		w.WriteInt32(res.EventID)
	}
	return NewPacket(ActionIDDragonballSacrifice, w.Bytes())
}

// DragonballTmpBall 表示祭坛临时格中的一颗龙珠。
type DragonballTmpBall struct {
	// PlayerDBID 玩家龙珠实例 ID (收取时使用)
	PlayerDBID int32
	// Level 龙珠等级
	Level int16
	// Quality 品质
	Quality uint8
}

// BuildDragonballTmpInfoPacket 构造查询祭坛临时格封包 (0x006F0000)。
func BuildDragonballTmpInfoPacket() *Packet {
	return NewPacket(ActionIDDragonballTmpInfo, nil)
}

// ParseDragonballTmpInfoResult 解析祭坛临时格龙珠列表。
func ParseDragonballTmpInfoResult(payload []byte) ([]DragonballTmpBall, error) {
	r := NewReader(payload)
	// 6 个标量字段: Short, Short, Byte, Short, Short, Short。
	if _, err := r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read tmp dragonball field0: %v", ErrInvalidPayload, err)
	}
	if _, err := r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read tmp dragonball field1: %v", ErrInvalidPayload, err)
	}
	if _, err := r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read tmp dragonball field2: %v", ErrInvalidPayload, err)
	}
	for i := 3; i < 6; i++ {
		if _, err := r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read tmp dragonball field%d: %v", ErrInvalidPayload, i, err)
		}
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read tmp dragonball count: %v", ErrInvalidPayload, err)
	}
	balls := make([]DragonballTmpBall, 0, count)
	for i := 0; i < int(count); i++ {
		var b DragonballTmpBall
		if b.PlayerDBID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read tmp ball[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if b.Level, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read tmp ball[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if b.Quality, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read tmp ball[%d].quality: %v", ErrInvalidPayload, i, err)
		}
		balls = append(balls, b)
	}
	return balls, nil
}

// BuildDragonballCollectPacket 构造收取临时格龙珠封包 (0x006F0001)。
//
// 官方 AS3 描述符 request=[[IntUtil]]，经线上实测：Haloer 序列化器把参数
// 展开为「Uint16 count + Int32 player_d_b_id」的扁平数组；若再包一层
// outer count=1，服务端会返回 INVALID_DRAGONBALL(2)。同时服务端对批次
// 做整体校验：只要混入一条失效记录，整批都会被拒，因此调用方应逐颗收取。
func BuildDragonballCollectPacket(ids []int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(ids)))
	for _, id := range ids {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDDragonballCollect, w.Bytes())
}

// ParseDragonballCollectResult 解析收取临时格龙珠回包: UByte result, [Int id]。
func ParseDragonballCollectResult(payload []byte) (uint8, []int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read collect result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return result, nil, nil
	}
	ids := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return result, ids, fmt.Errorf("%w: failed to read collected[%d]: %v", ErrInvalidPayload, i, err)
		}
		ids = append(ids, id)
	}
	return result, ids, nil
}

// 龙珠品质编号 (对齐 Pieb.db DragonBall.quality_id)。
const (
	// DragonballQualityGreen 绿色 (quality_id=1)
	DragonballQualityGreen int32 = 1
	// DragonballQualityBlue 蓝色 (quality_id=2)
	DragonballQualityBlue int32 = 2
	// DragonballQualityPurple 紫色 (quality_id=3)
	DragonballQualityPurple int32 = 3
	// DragonballQualityOrange 橙色 (quality_id=4，不合成)
	DragonballQualityOrange int32 = 4
	// DragonballQualityRed 红色 (quality_id=5，不合成)
	DragonballQualityRed int32 = 5
	// DragonballQualityFire 火龙珠 (quality_id=6，单独合并)
	DragonballQualityFire int32 = 6
)

// DragonballNormalMergeQualities 返回「紫色及以下」普通龙珠的品质列表。
func DragonballNormalMergeQualities() []int32 {
	return []int32{DragonballQualityGreen, DragonballQualityBlue, DragonballQualityPurple}
}

// BuildDragonballSellPacket 构造出售龙珠封包 (0x006F0007)。
//
// 官方 request=[[IntUtil]]，与 collect 相同的扁平编码：Uint16 count + Int32
// player_d_b_id。用于清理祭坛临时格中长期占位的失效龙珠。
func BuildDragonballSellPacket(ids []int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(ids)))
	for _, id := range ids {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDDragonballSell, w.Bytes())
}

// ParseDragonballSellResult 解析出售龙珠回包 (仅一个结果码)。
func ParseDragonballSellResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty sell result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildDragonballSellResultPacket 构造出售龙珠回包，供离线 mock 与单测使用。
func BuildDragonballSellResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDDragonballSell, []byte{result})
}

// BuildDragonballOneKeyMergePacket 构造按品质一键合成龙珠封包 (0x006F0006)。
//
// 官方 request=[[IntUtil]]，线上实测与收取一致采用「Uint16 count + Int32 quality」
// 的扁平编码；末尾追加 4 字节 PrevAct 占位，由会话层用最近动作号覆盖，
// 避免占位缺失时最后一个品质被覆盖。
func BuildDragonballOneKeyMergePacket(qualities []int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(qualities)))
	for _, q := range qualities {
		w.WriteInt32(q)
	}
	w.WriteUint32(0)
	pkt := NewPacket(ActionIDDragonballOneKeyMerge, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseDragonballOneKeyMergeResult 解析一键合成回包: UByte result, [Int id]。
func ParseDragonballOneKeyMergeResult(payload []byte) (uint8, []int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read merge result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return result, nil, nil
	}
	ids := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return result, ids, fmt.Errorf("%w: failed to read merged[%d]: %v", ErrInvalidPayload, i, err)
		}
		ids = append(ids, id)
	}
	return result, ids, nil
}

// BuildDragonballOneKeyMergeResultPacket 构造一键合成回包，供离线 mock 与单测使用。
func BuildDragonballOneKeyMergeResultPacket(result uint8, ids []int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16(len(ids)))
	for _, id := range ids {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDDragonballOneKeyMerge, w.Bytes())
}

// BuildDragonballTmpInfoResultPacket 构造临时格回包，供离线 mock 与单测使用。
func BuildDragonballTmpInfoResultPacket(balls []DragonballTmpBall) *Packet {
	w := NewWriter()
	w.WriteInt16(10)
	w.WriteInt16(0)
	w.WriteUint8(7)
	for i := 0; i < 3; i++ {
		w.WriteInt16(0)
	}
	w.WriteUint16(uint16(len(balls)))
	for _, b := range balls {
		w.WriteInt32(b.PlayerDBID)
		w.WriteInt16(b.Level)
		w.WriteUint8(b.Quality)
	}
	return NewPacket(ActionIDDragonballTmpInfo, w.Bytes())
}

// BuildDragonballCollectResultPacket 构造收取回包，供离线 mock 与单测使用。
func BuildDragonballCollectResultPacket(result uint8, ids []int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16(len(ids)))
	for _, id := range ids {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDDragonballCollect, w.Bytes())
}
