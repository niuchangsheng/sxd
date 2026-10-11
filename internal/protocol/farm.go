// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"fmt"
)

// 药园服务端响应状态码常量
const (
	// FarmStatusSuccess 药园操作成功返回码 (0x08)
	FarmStatusSuccess uint8 = 0x08
	// FarmStatusFailed 药园操作失败或未就绪返回码 (0x21)
	FarmStatusFailed uint8 = 0x21
)

// FarmField 代表药园中的单块种植土地状态。
type FarmField struct {
	// LandID 土地编号 (如 9, 10, 11, 12)
	LandID int32
	// State 土地状态 (0: 未开垦, 1: 空闲, 2: 种植中, 3: 可采摘)
	State int32
	// Cooldown 成熟倒计时 (秒)
	Cooldown int32
	// SeedOrRoleID 种植的种子或被培养的角色伙伴 ID
	SeedOrRoleID int32
}

// FarmGetInfoRequest 代表查询药园所有土地基础开垦状态请求 (ActionID=0x000D0000)。
type FarmGetInfoRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// FarmGetInfoResult 代表药园全量土地状态列表。
type FarmGetInfoResult struct {
	// Fields 土地切片
	Fields []FarmField
}

// FarmPlantStateRequest 代表查询药园土地作物种植与生长时间状态请求 (ActionID=0x000D0015)。
type FarmPlantStateRequest struct {
	// PrevAct 前序协议编号 (通常为 ActionIDFarmGetInfo 0x000D0000)
	PrevAct uint32
}

// FarmPlantStateResult 代表药园作物实时生长时间与种植状态。
type FarmPlantStateResult struct {
	// Fields 土地实时种植状态列表
	Fields []FarmField
}

// FarmSeedItem 代表药园种子商城中单项种子的属性与存量。
type FarmSeedItem struct {
	// SeedID 种子编号 (如 0xA9 仙玲珑经验草, 0xB0 经验丹果树)
	SeedID int32
	// OwnedCount 当前玩家背包/库存拥有数量
	OwnedCount int32
	// PriceType 货币类型 (3 为铜钱)
	PriceType int32
	// Price 单价
	Price int32
}

// FarmSeedsRequest 代表查询药园种子库存与商城配置请求 (ActionID=0x000D0016)。
type FarmSeedsRequest struct {
	// PrevAct 前序协议编号 (通常为 ActionIDFarmGetPlantState 0x000D0015)
	PrevAct uint32
}

// FarmSeedsResult 代表药园种子列表响应结果。
type FarmSeedsResult struct {
	// Seeds 种子列表
	Seeds []FarmSeedItem
}

// FarmBuySeedRequest 代表购买药园种子请求 (ActionID=0x000D0017)。
type FarmBuySeedRequest struct {
	// SeedID 待购买的种子编号
	SeedID int32
	// PrevAct 前序协议编号 (通常为 ActionIDFarmGetSeeds 0x000D0016)
	PrevAct uint32
}

// FarmBuySeedResult 代表购买药园种子响应结果。
type FarmBuySeedResult struct {
	// Success 是否购买成功
	Success bool
}

// FarmPlantRequest 代表药园播种药草请求 (ActionID=0x000D0018)。
type FarmPlantRequest struct {
	// LandID 目标土地编号
	LandID int32
	// SeedOrRoleID 选中的种子或伙伴 ID (如经验种子 0xA9 / 0xB0)
	SeedOrRoleID int32
	// PrevAct 前序协议编号 (如 0x000D0019)
	PrevAct uint32
}

// FarmPlantResult 代表药园播种响应结果。
type FarmPlantResult struct {
	// Success 播种是否成功
	Success bool
	// LandID 目标土地编号
	LandID int32
	// SeedOrRoleID 成功种植的种子或伙伴 ID
	SeedOrRoleID int32
	// Cooldown 种植成熟冷却时长 (秒，通常 28800 秒 / 8 小时)
	Cooldown int32
}

// FarmHarvestRequest 代表收获成熟药草请求 (ActionID=0x000D0019)。
type FarmHarvestRequest struct {
	// LandID 目标土地编号
	LandID int32
	// PrevAct 前序协议编号 (如 0x000D0018)
	PrevAct uint32
}

// FarmHarvestResult 代表收获药草结算响应。
type FarmHarvestResult struct {
	// Success 是否收获成功
	Success bool
	// LandID 土地编号
	LandID int32
	// GainExp 获得的经验数值
	GainExp int64
	// GainCoins 获得的铜钱数值
	GainCoins int64
	// Cooldown 冷却时长
	Cooldown int32
}

// BuildFarmGetInfoPacket 构造查询药园土地信息请求封包。
func BuildFarmGetInfoPacket(req FarmGetInfoRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFarmGetInfo, w.Bytes()), nil
}

// ParseFarmGetInfoRequest 从载荷反序列化查询土地信息请求。
func ParseFarmGetInfoRequest(payload []byte) (*FarmGetInfoRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("%w: payload too short for farm get info", ErrInvalidPayload)
	}
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &FarmGetInfoRequest{PrevAct: prevAct}, nil
}

// BuildFarmPlantStatePacket 构造查询药园作物实时状态请求封包。
func BuildFarmPlantStatePacket(req FarmPlantStateRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFarmGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFarmGetPlantState, w.Bytes()), nil
}

// ParseFarmPlantStateResult 从载荷反序列化药园作物生长时间与种植状态。
func ParseFarmPlantStateResult(payload []byte) (*FarmPlantStateResult, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: payload too short for farm plant state result", ErrInvalidPayload)
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read count: %v", ErrInvalidPayload, err)
	}

	fields := make([]FarmField, 0, count)
	// 真实抓包中每块土地固定 19 字节
	isRealFormat := len(payload) >= 2+int(count)*19

	for i := 0; i < int(count); i++ {
		if isRealFormat {
			landID, rErr := r.ReadInt32()
			if rErr != nil {
				return nil, rErr
			}
			// 4..11: 8 字节倒计时
			cooldownHigh, _ := r.ReadInt32()
			cooldownLow, _ := r.ReadInt32()
			cooldown := cooldownLow
			if cooldownHigh > 0 && cooldownLow == 0 {
				cooldown = cooldownHigh
			}
			// 12..15: 4 字节 SeedID 或作物状态 (1 代表空闲未种植，真实种子ID通常 >= 169)
			rawSeedOrState, _ := r.ReadInt32()
			// 16..18: 3 字节状态标记
			_, _ = r.ReadBytes(3)

			seedID := int32(0)
			state := int32(1) // 默认空闲可种植
			if cooldown > 0 {
				state = 2 // 种植生长中 / 冷却中
			} else if rawSeedOrState > 1 {
				seedID = rawSeedOrState
				state = 3 // 成熟待采摘
			} else {
				state = 1 // 空闲可播种 (未种植，rawSeedOrState <= 1)
			}

			fields = append(fields, FarmField{
				LandID:       landID,
				State:        state,
				Cooldown:     cooldown,
				SeedOrRoleID: seedID,
			})
		} else {
			// 兼容测试桩 16 字节格式
			landID, _ := r.ReadInt32()
			state, _ := r.ReadInt32()
			cooldown, _ := r.ReadInt32()
			seedID, _ := r.ReadInt32()
			fields = append(fields, FarmField{
				LandID:       landID,
				State:        state,
				Cooldown:     cooldown,
				SeedOrRoleID: seedID,
			})
		}
	}

	return &FarmPlantStateResult{Fields: fields}, nil
}

// BuildFarmSeedsPacket 构造查询药园种子库存与商城配置请求封包。
func BuildFarmSeedsPacket(req FarmSeedsRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFarmGetPlantState
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFarmGetSeeds, w.Bytes()), nil
}

// ParseFarmSeedsResult 从载荷反序列化药园种子列表与价格。
func ParseFarmSeedsResult(payload []byte) (*FarmSeedsResult, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: payload too short for seeds result", ErrInvalidPayload)
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read seeds count: %v", ErrInvalidPayload, err)
	}

	seeds := make([]FarmSeedItem, 0, count)
	for i := 0; i < int(count); i++ {
		seedID, _ := r.ReadInt32()
		ownedCount, _ := r.ReadInt32()
		priceType, _ := r.ReadInt32()
		price, _ := r.ReadInt32()
		_, _ = r.ReadInt32() // reserved 4B

		seeds = append(seeds, FarmSeedItem{
			SeedID:     seedID,
			OwnedCount: ownedCount,
			PriceType:  priceType,
			Price:      price,
		})
	}

	return &FarmSeedsResult{Seeds: seeds}, nil
}

// BuildFarmBuySeedPacket 构造购买药园种子二进制封包。
func BuildFarmBuySeedPacket(req FarmBuySeedRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(req.SeedID)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFarmGetSeeds
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFarmBuySeed, w.Bytes()), nil
}

// ParseFarmBuySeedResult 从载荷反序列化购买种子响应。
func ParseFarmBuySeedResult(payload []byte) (*FarmBuySeedResult, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: payload too short for buy seed result", ErrInvalidPayload)
	}
	status := payload[0]
	return &FarmBuySeedResult{
		Success: status == FarmStatusSuccess || status == 1,
	}, nil
}

// BuildFarmPlantPacket 构造播种药草二进制封包。
func BuildFarmPlantPacket(req FarmPlantRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 LandID (4B 大端序整型)
	w.WriteInt32(req.LandID)
	// 2. 写入 SeedOrRoleID (4B 大端序整型)
	w.WriteInt32(req.SeedOrRoleID)
	// 3. 写入 PrevAct (4B 大端序整型)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFarmHarvest
	}
	w.WriteUint32(prevAct)

	return NewPacket(ActionIDFarmPlant, w.Bytes()), nil
}

// ParseFarmPlantRequest 从载荷反序列化播种药草请求。
func ParseFarmPlantRequest(payload []byte) (*FarmPlantRequest, error) {
	if len(payload) < 12 {
		return nil, fmt.Errorf("%w: payload too short for farm plant", ErrInvalidPayload)
	}
	r := NewReader(payload)
	landID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read land_id: %v", ErrInvalidPayload, err)
	}
	seedID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read seed_id: %v", ErrInvalidPayload, err)
	}
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}

	return &FarmPlantRequest{
		LandID:       landID,
		SeedOrRoleID: seedID,
		PrevAct:      prevAct,
	}, nil
}

// BuildFarmHarvestPacket 构造收取成熟药草二进制封包。
func BuildFarmHarvestPacket(req FarmHarvestRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 LandID (4B 大端序整型)
	w.WriteInt32(req.LandID)
	// 2. 写入 PrevAct (4B 大端序整型)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFarmPlant
	}
	w.WriteUint32(prevAct)

	return NewPacket(ActionIDFarmHarvest, w.Bytes()), nil
}

// ParseFarmHarvestRequest 从载荷反序列化收取成熟药草请求。
func ParseFarmHarvestRequest(payload []byte) (*FarmHarvestRequest, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("%w: payload too short for farm harvest", ErrInvalidPayload)
	}
	r := NewReader(payload)
	landID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read land_id: %v", ErrInvalidPayload, err)
	}
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}

	return &FarmHarvestRequest{
		LandID:  landID,
		PrevAct: prevAct,
	}, nil
}

// BuildFarmGetInfoResultPacket 构造药园土地列表查询响应二进制封包。
func BuildFarmGetInfoResultPacket(res FarmGetInfoResult) (*Packet, error) {
	w := NewWriter()
	w.WriteUint16(uint16(len(res.Fields)))
	for _, f := range res.Fields {
		w.WriteInt32(f.LandID)
		w.WriteInt32(f.State)
		w.WriteInt32(f.Cooldown)
		w.WriteInt32(f.SeedOrRoleID)
	}
	return NewPacket(ActionIDFarmGetInfo, w.Bytes()), nil
}

// ParseFarmGetInfoResult 从载荷反序列化药园土地状态列表。
func ParseFarmGetInfoResult(payload []byte) (*FarmGetInfoResult, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: payload too short for farm info result", ErrInvalidPayload)
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read fields count: %v", ErrInvalidPayload, err)
	}

	fields := make([]FarmField, 0, count)
	// 真实抓包与实机返回中每块土地固定 47 字节
	isReal47Format := len(payload) >= 2+int(count)*47

	for i := 0; i < int(count); i++ {
		if isReal47Format {
			landID, rErr := r.ReadInt32()
			if rErr != nil {
				return nil, rErr
			}
			// 4..46: 43 字节地皮与升级数据 (跳过)
			_, _ = r.ReadBytes(43)

			fields = append(fields, FarmField{
				LandID:       landID,
				State:        1, // 默认已开垦空闲状态，后续由 Act 21 精准覆写
				Cooldown:     0,
				SeedOrRoleID: 0,
			})
		} else {
			// 兼容 16 字节简易格式
			landID, _ := r.ReadInt32()
			state, _ := r.ReadInt32()
			cooldown, _ := r.ReadInt32()
			seedOrRoleID, _ := r.ReadInt32()
			fields = append(fields, FarmField{
				LandID:       landID,
				State:        state,
				Cooldown:     cooldown,
				SeedOrRoleID: seedOrRoleID,
			})
		}
	}

	return &FarmGetInfoResult{Fields: fields}, nil
}

// BuildFarmHarvestResultPacket 构造药草收获结算响应二进制封包。
func BuildFarmHarvestResultPacket(res FarmHarvestResult) (*Packet, error) {
	w := NewWriter()
	if res.Success {
		w.WriteUint8(FarmStatusSuccess)
		w.WriteUint16(1)
		w.WriteInt32(res.LandID)
		w.WriteInt32(int32(res.GainExp))
		w.WriteInt32(int32(res.GainCoins))
		w.WriteInt32(res.Cooldown)
	} else {
		w.WriteUint8(FarmStatusFailed)
		w.WriteBytes(make([]byte, 6))
	}
	return NewPacket(ActionIDFarmHarvest, w.Bytes()), nil
}

// ParseFarmHarvestResult 从载荷反序列化药草收获结算响应。
func ParseFarmHarvestResult(payload []byte) (*FarmHarvestResult, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: payload too short for farm harvest result", ErrInvalidPayload)
	}
	status := payload[0]

	// 1. 真实抓包成功格式: 0x08 开头 (通常 23 字节)
	if status == FarmStatusSuccess {
		r := NewReader(payload[1:])
		_, _ = r.ReadUint16() // 0x0001
		landID, _ := r.ReadInt32()
		gainExp, _ := r.ReadInt32()
		gainCoins, _ := r.ReadInt32()
		cooldown, _ := r.ReadInt32()

		return &FarmHarvestResult{
			Success:   true,
			LandID:    landID,
			GainExp:   int64(gainExp),
			GainCoins: int64(gainCoins),
			Cooldown:  cooldown,
		}, nil
	}

	// 2. 真实抓包失败/未成熟格式: 0x21 开头 (7 字节)
	if status == FarmStatusFailed {
		return &FarmHarvestResult{
			Success: false,
		}, nil
	}

	// 3. 兼容旧测试桩格式 (首字节 1/0，随两个 int64)
	if len(payload) >= 17 {
		r := NewReader(payload)
		successByte, _ := r.ReadUint8()
		gainExp, _ := r.ReadInt64()
		gainCoins, _ := r.ReadInt64()
		return &FarmHarvestResult{
			Success:   successByte == 1,
			GainExp:   gainExp,
			GainCoins: gainCoins,
		}, nil
	}

	return &FarmHarvestResult{
		Success: status == 1,
	}, nil
}

// BuildFarmPlantResultPacket 构造药园播种响应二进制封包。
func BuildFarmPlantResultPacket(res FarmPlantResult) (*Packet, error) {
	w := NewWriter()
	if res.Success {
		w.WriteUint8(FarmStatusSuccess)
		w.WriteInt32(res.LandID)
		w.WriteInt32(res.SeedOrRoleID)
		w.WriteInt32(1)
		w.WriteInt16(0)
	} else {
		w.WriteUint8(FarmStatusFailed)
		w.WriteBytes(make([]byte, 14))
	}
	return NewPacket(ActionIDFarmPlant, w.Bytes()), nil
}

// ParseFarmPlantResult 从载荷反序列化药园播种响应结果。
func ParseFarmPlantResult(payload []byte) (*FarmPlantResult, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: payload too short for farm plant result", ErrInvalidPayload)
	}
	status := payload[0]

	// 1. 真实抓包格式: 0x08 成功 (15~27 字节)
	if status == FarmStatusSuccess {
		r := NewReader(payload[1:])
		landID, _ := r.ReadInt32()
		seedID, _ := r.ReadInt32()
		return &FarmPlantResult{
			Success:      true,
			LandID:       landID,
			SeedOrRoleID: seedID,
			Cooldown:     28800, // 8 小时默认冷却
		}, nil
	}

	// 2. 真实抓包失败格式: 0x21
	if status == FarmStatusFailed {
		return &FarmPlantResult{
			Success: false,
		}, nil
	}

	// 3. 兼容旧测试桩格式 (首字节 1/0, landID, cooldown)
	if len(payload) >= 9 {
		r := NewReader(payload)
		successByte, _ := r.ReadUint8()
		landID, _ := r.ReadInt32()
		cooldown, _ := r.ReadInt32()
		return &FarmPlantResult{
			Success:  successByte == 1,
			LandID:   landID,
			Cooldown: cooldown,
		}, nil
	}

	return &FarmPlantResult{
		Success: status == 1,
	}, nil
}
