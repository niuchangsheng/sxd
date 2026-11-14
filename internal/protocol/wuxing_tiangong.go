// Package protocol 提供五行天宫 (Module 453 / 0x01C5) 协议编解码。
//
// 协议基准: 2026-11-14 官方客户端抓包 /tmp/wuxingtiangong.pcapng：
//
//	453/10 [空 + PrevAct]                    分类/列表面板
//	453/0  [空 + PrevAct]                    主面板（五行条目）
//	453/1  [空 + PrevAct]                    阵容列表（含伙伴名）
//	453/2  [Int role_id][Byte pos][PrevAct]  上阵到指定号位
//	453/3  [Byte mode][PrevAct]              挑战：1=一键挑战，3=失败关卡单独挑战
//	453/4  [Int index][PrevAct]              战报/结算
//
// 面板 453/0 结构（抓包 143 字节）：
//
//	[Int 0][UByte 24] + 5 ×
//	[Byte element][Byte state][Int id][Int stage][Int hp][Int a][Int b][Int c]
package protocol

import "fmt"

const (
	// ActionIDWuxingPanel10 分类/列表面板 (453/10)
	ActionIDWuxingPanel10 uint32 = 0x01C5000A
	// ActionIDWuxingPanel 主面板 (453/0)
	ActionIDWuxingPanel uint32 = 0x01C50000
	// ActionIDWuxingLineup 阵容列表 (453/1)
	ActionIDWuxingLineup uint32 = 0x01C50001
	// ActionIDWuxingDeploy 上阵号位 (453/2)
	ActionIDWuxingDeploy uint32 = 0x01C50002
	// ActionIDWuxingFight 发起挑战 (453/3)
	ActionIDWuxingFight uint32 = 0x01C50003
	// ActionIDWuxingReport 战报/结算 (453/4)
	ActionIDWuxingReport uint32 = 0x01C50004
)

// 挑战模式 (453/3 首字节)。
const (
	// WuxingFightModeAll 一键挑战全部关卡
	WuxingFightModeAll uint8 = 1
	// WuxingFightModeRetry 失败关卡单独挑战
	WuxingFightModeRetry uint8 = 3
)

// WuxingEntry 表示五行天宫主面板中的一个条目。
type WuxingEntry struct {
	// Element 五行序号 (1..5)
	Element uint8
	// State 关卡状态（抓包中 4=未挑战，3=失败/已挑战）
	State uint8
	// ID 关卡配置 ID
	ID int32
	// Stage 阶段/序号
	Stage int32
	// HP 敌方气血/战力等
	HP int32
}

// WuxingRole 表示 453/1 阵容列表中的一个伙伴。
type WuxingRole struct {
	RoleID int32
	Name   string
}

func buildWuxingPrevActPacket(actionID uint32, prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(actionID, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildWuxingPanel10Packet 构造分类/列表面板封包 (453/10)。
func BuildWuxingPanel10Packet(prevAct uint32) *Packet {
	return buildWuxingPrevActPacket(ActionIDWuxingPanel10, prevAct)
}

// BuildWuxingPanelPacket 构造主面板封包 (453/0)。
func BuildWuxingPanelPacket(prevAct uint32) *Packet {
	return buildWuxingPrevActPacket(ActionIDWuxingPanel, prevAct)
}

// BuildWuxingLineupPacket 构造阵容列表封包 (453/1)。
func BuildWuxingLineupPacket(prevAct uint32) *Packet {
	return buildWuxingPrevActPacket(ActionIDWuxingLineup, prevAct)
}

// BuildWuxingDeployPacket 构造上阵号位封包 (453/2)：[Int role_id][Byte pos][PrevAct]。
func BuildWuxingDeployPacket(roleID int32, pos uint8, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	w.WriteUint8(pos)
	if prevAct == 0 {
		prevAct = ActionIDWuxingLineup
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDWuxingDeploy, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildWuxingFightPacket 构造挑战封包 (453/3)：[Byte mode][PrevAct]。
func BuildWuxingFightPacket(mode uint8, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint8(mode)
	if prevAct == 0 {
		prevAct = ActionIDWuxingPanel
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDWuxingFight, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildWuxingReportPacket 构造战报/结算封包 (453/4)：[Int index][PrevAct]。
func BuildWuxingReportPacket(index int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(index)
	if prevAct == 0 {
		prevAct = ActionIDWuxingFight
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDWuxingReport, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseWuxingPanel 解析 453/0 主面板。
//
// 头部 5 字节后为 5 个 26 字节条目：
// [Byte element][Byte state][Int id][Int stage][Int hp][Int a][Int b][Int c]。
func ParseWuxingPanel(payload []byte) ([]WuxingEntry, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) < 5 {
		return nil, fmt.Errorf("%w: wuxing panel too short", ErrInvalidPayload)
	}
	entries := make([]WuxingEntry, 0, 5)
	// 抓包头部为 7 字节：[Int 0][UByte 0x18][UByte 0][UByte 5]。
	off := 7
	for i := 0; i < 5; i++ {
		if off+14 > len(p) {
			break
		}
		e := WuxingEntry{
			Element: p[off],
			State:   p[off+1],
		}
		e.ID = int32(readU32(p[off+2:]))
		e.Stage = int32(readU32(p[off+6:]))
		e.HP = int32(readU32(p[off+10:]))
		entries = append(entries, e)
		off += 26
	}
	return entries, nil
}

// ParseWuxingLineup 解析 453/1 阵容列表，返回 role_id 与伙伴名。
//
// 回包为非固定结构，这里按「Int role_id + 若干字段 + Short 长度 + UTF-8 名称」
// 的顺序做宽松扫描，仅用于按名称映射主角/李逍遥/玲珑等。
func ParseWuxingLineup(payload []byte) ([]WuxingRole, error) {
	p := DecompressPayloadIfNeeded(payload)
	roles := make([]WuxingRole, 0, 8)
	for i := 0; i+6 < len(p); i++ {
		l := int(p[i])<<8 | int(p[i+1])
		if l < 2 || l > 24 || i+2+l > len(p) {
			continue
		}
		name := p[i+2 : i+2+l]
		if !isMostlyUTF8Chinese(name) {
			continue
		}
		if i < 4 {
			continue
		}
		roles = append(roles, WuxingRole{
			RoleID: int32(readU32(p[i-4:])),
			Name:   string(name),
		})
		i += 1 + l
	}
	return roles, nil
}

func readU32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func isMostlyUTF8Chinese(b []byte) bool {
	if len(b) < 4 || len(b)%3 != 0 {
		return false
	}
	for i := 0; i+2 < len(b); i += 3 {
		if b[i] != 0xE0 && (b[i] < 0xE4 || b[i] > 0xE9) && b[i] != 0xEF {
			return false
		}
	}
	return true
}
