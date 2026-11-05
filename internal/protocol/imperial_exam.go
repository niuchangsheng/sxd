// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方金榜题名活动状态协议 (Mod_StJinBangTiMing_Base, Module 237 / 0xED)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_StJinBangTiMing_Base：
// 先查询活动时间与阶段，仅在活动进行中才允许开始作答。
// 题目内容由客户端资源 SWF 提供，自动化在缺少题库时只做状态检测、不提交答案。
package protocol

import "fmt"

const (
	// ActionIDImperialExamStatus 查询金榜题名活动状态 (Module 237, Action 0 -> 0x00ED0000)
	ActionIDImperialExamStatus uint32 = 0x00ED0000
)

// Mod_StJinBangTiMing_Base 活动阶段码。
const (
	// ImperialExamActivityStart 活动进行中
	ImperialExamActivityStart uint8 = 0
	// ImperialExamActivityEnd 活动已结束
	ImperialExamActivityEnd uint8 = 1
	// ImperialExamActivityClose 活动关闭
	ImperialExamActivityClose uint8 = 2
	// ImperialExamAnswerOver 答题已结束
	ImperialExamAnswerOver uint8 = 3
)

// ImperialExamStatus 表示活动时间与阶段。
type ImperialExamStatus struct {
	// StartTime 活动开始时间
	StartTime int32
	// EndTime 活动结束时间
	EndTime int32
	// Status 阶段码
	Status uint8
}

// BuildImperialExamStatusPacket 构造查询活动状态封包 (0x00ED0000)。
func BuildImperialExamStatusPacket() *Packet {
	return NewPacket(ActionIDImperialExamStatus, nil)
}

// ParseImperialExamStatusResult 解析活动状态: Int start_time, Int end_time, UByte status。
func ParseImperialExamStatusResult(payload []byte) (*ImperialExamStatus, error) {
	r := NewReader(payload)
	res := &ImperialExamStatus{}
	var err error
	if res.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read imperial exam start_time: %v", ErrInvalidPayload, err)
	}
	if res.EndTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read imperial exam end_time: %v", ErrInvalidPayload, err)
	}
	if res.Status, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read imperial exam status: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildImperialExamStatusResultPacket 构造活动状态回包，供离线 mock 与单测使用。
func BuildImperialExamStatusResultPacket(status ImperialExamStatus) *Packet {
	w := NewWriter()
	w.WriteInt32(status.StartTime)
	w.WriteInt32(status.EndTime)
	w.WriteUint8(status.Status)
	return NewPacket(ActionIDImperialExamStatus, w.Bytes())
}
