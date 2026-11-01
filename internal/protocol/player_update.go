// Package protocol 提供神仙道网关二进制协议的编解码实现。
//
// @author Ateng
// @since 2026-11-01
package protocol

import "encoding/binary"

// PlayerPropUpdate 表示 Mod_Player_Base.update_player_data 中的单条属性变更。
type PlayerPropUpdate struct {
	Prop  uint8 // 属性 ID (2026 网关枚举, 见 PlayerProp*2026 常量)
	Value int64 // 属性最新数值
}

// ParsePlayerUpdateData 解析角色属性变更推送包 (Module 0, Action 3)。
//
// 2026 网关实测采用批量布局:
//
//	[Uint16 条数][Uint8 属性ID][Int64 数值] * 条数
//
// 例如 000219000000000004a160140000000003019479 表示
// 经验=303456 (0x19)、铜钱=50435193 (0x14)。
//
// 旧版 pie.exe 的 [Uint8 属性ID][Int32 数值] 重复布局仍作为兼容分支保留,
// 仅当载荷长度不满足 2 + 9*条数 时才按旧格式解析。
// 第二个返回值指示是否为 2026 网关批量布局, 调用方需据此选择属性 ID 枚举。
func ParsePlayerUpdateData(payload []byte) ([]PlayerPropUpdate, bool) {
	if len(payload) < 5 {
		return nil, false
	}

	if updates, ok := parseBatchedPlayerUpdateData(payload); ok {
		return updates, true
	}
	return parseLegacyPlayerUpdateData(payload), false
}

// parseBatchedPlayerUpdateData 按 2026 网关批量布局解析, 布局不吻合时返回 ok=false。
func parseBatchedPlayerUpdateData(payload []byte) ([]PlayerPropUpdate, bool) {
	count := int(binary.BigEndian.Uint16(payload[:2]))
	if count <= 0 || len(payload) != 2+9*count {
		return nil, false
	}

	updates := make([]PlayerPropUpdate, 0, count)
	pos := 2
	for i := 0; i < count; i++ {
		prop := payload[pos]
		// 批量布局中属性 ID 恒非零; 出现 0 说明是旧布局碰巧长度吻合, 交由兼容分支处理。
		if prop == 0 {
			return nil, false
		}
		value := int64(binary.BigEndian.Uint64(payload[pos+1 : pos+9]))
		updates = append(updates, PlayerPropUpdate{Prop: prop, Value: value})
		pos += 9
	}
	return updates, true
}

// parseLegacyPlayerUpdateData 按旧版 [Uint8 属性ID][Int32 数值] 重复布局解析。
func parseLegacyPlayerUpdateData(payload []byte) []PlayerPropUpdate {
	updates := make([]PlayerPropUpdate, 0, len(payload)/5)
	for pos := 0; pos+5 <= len(payload); pos += 5 {
		prop := payload[pos]
		if prop == 0 {
			break
		}
		value := int64(int32(binary.BigEndian.Uint32(payload[pos+1 : pos+5])))
		updates = append(updates, PlayerPropUpdate{Prop: prop, Value: value})
	}
	return updates
}
