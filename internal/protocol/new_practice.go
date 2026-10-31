// Package protocol 提供现代新版打坐练功系统 (Mod_NewPractice_Base / Module 584) 编解码实现。
//
// 对应官方微端: com.protocols:Mod_NewPractice_Base
// Module ID: 584 (0x0248)
// Action 1: get_practice_info (0x02480001)
// Action 2: collect_air (0x02480002)
// Action 3: collect_all (0x02480003)
package protocol

import (
	"encoding/binary"
	"fmt"
)

// NewPracticeAirItem 现代打坐练功产出的灵气或道具条目。
type NewPracticeAirItem struct {
	ItemID int32 // 道具/灵气 ID
	Count  int32 // 数量
}

// NewPracticeInfo 现代城镇离线打坐练功全局状态快照。
type NewPracticeInfo struct {
	Result        uint8                // 响应状态码 (0: SUCCESS, 1: FAILED)
	AirList       []NewPracticeAirItem // 待领取灵气列表
	GatherTime    uint32               // 当前已累积打坐时长 (秒)
	MaxGatherTime uint32               // 最大累计上限时长 (秒，通常为 259200 秒即 72 小时)
	PracticeExp   int64                // 当前已累积待领取的经验值 (即将领取的经验)
	RouteLevel    int32                // 境界/练功等级
	AirAccRate    int32                // 灵气累积速率加成
	ExpAccRate    int32                // 经验累积速率加成
}

// NewPracticeCollectResult 现代打坐一键领取返回结果。
type NewPracticeCollectResult struct {
	Result  uint8                // 响应状态码 (0: SUCCESS, 1: FAILED)
	AirList []NewPracticeAirItem // 本次领取的灵气列表
	GainExp int64                // 本次领取的实际经验增加值
}

// BuildNewPracticeGetInfoPacket 构建获取打坐练功累积状态与经验请求封包 (Mod_NewPractice_Base.get_practice_info)。
func BuildNewPracticeGetInfoPacket() *Packet {
	return &Packet{
		ActionID: ActionIDNewPracticeGetInfo,
		Payload:  nil,
	}
}

// ParseNewPracticeGetInfoResponse 解析打坐练功累积状态与经验回包。
func ParseNewPracticeGetInfoResponse(payload []byte) (*NewPracticeInfo, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("new_practice get_info: payload too short: %d", len(payload))
	}

	info := &NewPracticeInfo{
		Result: payload[0],
	}
	if info.Result != NewPracticeSuccess {
		return info, nil
	}

	offset := 1
	if len(payload) < offset+2 {
		return nil, fmt.Errorf("new_practice get_info: missing air_list length")
	}
	airCount := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2

	if len(payload) < offset+airCount*8 {
		return nil, fmt.Errorf("new_practice get_info: incomplete air_list elements")
	}
	info.AirList = make([]NewPracticeAirItem, airCount)
	for i := 0; i < airCount; i++ {
		info.AirList[i] = NewPracticeAirItem{
			ItemID: int32(binary.BigEndian.Uint32(payload[offset : offset+4])),
			Count:  int32(binary.BigEndian.Uint32(payload[offset+4 : offset+8])),
		}
		offset += 8
	}

	// 剩余固定字段: gather_time (4B) + max_gather_time (4B) + practice_exp (8B) + route_level (4B) + air_acc_rate (4B) + exp_acc_rate (4B) = 28B
	if len(payload) < offset+28 {
		return nil, fmt.Errorf("new_practice get_info: missing fields, need %d bytes, got %d", offset+28, len(payload))
	}

	info.GatherTime = binary.BigEndian.Uint32(payload[offset : offset+4])
	offset += 4

	info.MaxGatherTime = binary.BigEndian.Uint32(payload[offset : offset+4])
	offset += 4

	info.PracticeExp = int64(binary.BigEndian.Uint64(payload[offset : offset+8]))
	offset += 8

	info.RouteLevel = int32(binary.BigEndian.Uint32(payload[offset : offset+4]))
	offset += 4

	info.AirAccRate = int32(binary.BigEndian.Uint32(payload[offset : offset+4]))
	offset += 4

	info.ExpAccRate = int32(binary.BigEndian.Uint32(payload[offset : offset+4]))

	return info, nil
}

// BuildNewPracticeCollectAllPacket 构建一键领取新版打坐全部经验与灵气请求封包 (Mod_NewPractice_Base.collect_all)。
func BuildNewPracticeCollectAllPacket() *Packet {
	return &Packet{
		ActionID: ActionIDNewPracticeCollectAll,
		Payload:  nil,
	}
}

// ParseNewPracticeCollectAllResponse 解析一键领取打坐全部经验与灵气回包。
func ParseNewPracticeCollectAllResponse(payload []byte) (*NewPracticeCollectResult, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("new_practice collect_all: payload too short: %d", len(payload))
	}

	res := &NewPracticeCollectResult{
		Result: payload[0],
	}
	if res.Result != NewPracticeSuccess {
		return res, nil
	}

	offset := 1
	if len(payload) < offset+2 {
		return nil, fmt.Errorf("new_practice collect_all: missing air_list length")
	}
	airCount := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2

	if len(payload) < offset+airCount*8 {
		return nil, fmt.Errorf("new_practice collect_all: incomplete air_list elements")
	}
	res.AirList = make([]NewPracticeAirItem, airCount)
	for i := 0; i < airCount; i++ {
		res.AirList[i] = NewPracticeAirItem{
			ItemID: int32(binary.BigEndian.Uint32(payload[offset : offset+4])),
			Count:  int32(binary.BigEndian.Uint32(payload[offset+4 : offset+8])),
		}
		offset += 8
	}

	// 实际获取经验: gain_exp (8B)
	if len(payload) < offset+8 {
		return nil, fmt.Errorf("new_practice collect_all: missing gain_exp (8B), need %d bytes, got %d", offset+8, len(payload))
	}

	res.GainExp = int64(binary.BigEndian.Uint64(payload[offset : offset+8]))
	return res, nil
}
