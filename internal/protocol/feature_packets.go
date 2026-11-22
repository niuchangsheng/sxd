// Package protocol 提供奇门、寻仙、纪元游历、灵道、幻兽入侵、五法等玩法封包与通用解压辅助。
package protocol

import (
	"bytes"
	"compress/zlib"
	"io"
)

// BuildQiMenArrayInfoPacket 构造查询奇门法阵封包 (Module 486, Action 7)。
func BuildQiMenArrayInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0) // Type
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenArrayInfo, w.Bytes())
}

// BuildQiMenPuppetInfoPacket 构造查询阵眼傀儡属性封包 (Module 485, Action 0)。
func BuildQiMenPuppetInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenArrayInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenPuppetInfo, w.Bytes())
}

// BuildQiMenUpgradeStarPacket 构造奇门星图升级注灵封包 (Module 486, Action 1)。
func BuildQiMenUpgradeStarPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0)
	if prevAct == 0 {
		prevAct = ActionIDQiMenPuppetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenUpgradeStar, w.Bytes())
}

// BuildQiMenFormationDeployPacket 构造奇门遁甲布阵生效封包 (Module 584, Action 1)。
func BuildQiMenFormationDeployPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenUpgradeStar
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenFormationDeploy, w.Bytes())
}

// BuildQiMenFormationConfirmPacket 构造奇门遁甲阵法确认封包 (Module 584, Action 3)。
func BuildQiMenFormationConfirmPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenFormationDeploy
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenFormationConfirm, w.Bytes())
}

// --------------------------------------------------------------------------------
// 5. 淬炼粗铁 (协议实现见 refine_pool.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 6. 天神赐福
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 6. 天神赐福 (协议见 god_welfare.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 7. 劫镖
// --------------------------------------------------------------------------------

// BuildXunXianGetProgressPacket 构造查询寻仙之路进度封包 (Module 466, Action 1)。
func BuildXunXianGetProgressPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianGetProgress, w.Bytes())
}

// BuildXunXianChallengePacket 构造挑战寻仙之路关卡节点封包 (Module 466, Action 3)。
func BuildXunXianChallengePacket(nodeID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(nodeID)
	if prevAct == 0 {
		prevAct = ActionIDXunXianGetProgress
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianChallenge, w.Bytes())
}

// BuildXunXianClaimChestPacket 构造领取寻仙之路通关宝箱封包 (Module 466, Action 13)。
//
// Deprecated: Action 13 官方为 fairyland_status（查询，不领奖）。领奖请用
// BuildXunXianOneClickAwardPacket (Action 16)。
func BuildXunXianClaimChestPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDXunXianChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianClaimChest, w.Bytes())
}

// BuildXunXianOneClickAwardPacket 构造一键领取寻仙之路全部奖励封包 (Module 466, Action 16)。
func BuildXunXianOneClickAwardPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDXunXianChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianOneClickAward, w.Bytes())
}

// --------------------------------------------------------------------------------
// 9. 锁妖塔
// --------------------------------------------------------------------------------

// BuildEraTravelGetStatePacket 构造查询山河游历状态封包 (Module 850, Action 4)。
func BuildEraTravelGetStatePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraTravelGetState, w.Bytes())
}

// BuildEraTravelRollDicePacket 构造山河游历掷骰移动封包 (Module 850, Action 0)。
func BuildEraTravelRollDicePacket(step int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(step)
	if prevAct == 0 {
		prevAct = ActionIDEraTravelGetState
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraTravelRollDice, w.Bytes())
}

// --------------------------------------------------------------------------------
// 11. 灵道初解
// --------------------------------------------------------------------------------

// BuildLingDaoGetStatusPacket 构造查询灵道初解进度封包 (Module 658, Action 0)。
func BuildLingDaoGetStatusPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDLingDaoGetStatus, w.Bytes())
}

// BuildLingDaoOneKeyAwardPacket 构造灵道战斗通行证一键领取封包 (Module 711, Action 3)。
func BuildLingDaoOneKeyAwardPacket() *Packet {
	return NewPacket(ActionIDLingDaoOneKeyAward, nil)
}

// BuildLingDaoComprehendPacket 构造查询灵道战斗通行证面板封包 (Module 711, Action 0)。
func BuildLingDaoComprehendPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDLingDaoGetStatus
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDLingDaoComprehend, w.Bytes())
}

// --------------------------------------------------------------------------------
// 12. 山海幻兽
// --------------------------------------------------------------------------------

// BuildHuanshouPanelPacket 构造查询山海幻兽主面板封包 (Module 701, Action 0)。
func BuildHuanshouPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouPanel, w.Bytes())
}

// BuildHuanshouInvadeChallengePacket 构造挑战幻兽入侵副本封包 (Module 696, Action 2)。
// 依据 haha.pcapng 抓包事实: 载荷仅包含 PrevAct。
func BuildHuanshouInvadeChallengePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDHuanshouPanel
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeChallenge, w.Bytes())
}

// BuildHuanshouInvadeNodePacket 构造山海幻兽入侵逐关挑战扫荡封包 (Module 696, Action 4)。
func BuildHuanshouInvadeNodePacket(nodeID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(nodeID)
	if prevAct == 0 {
		prevAct = ActionIDHuanshouInvadeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeSettle, w.Bytes())
}

// BuildHuanshouInvadeSweepPacket 构造秒完成幻兽入侵扫荡封包 (Module 696, Action 3)。
func BuildHuanshouInvadeSweepPacket(prevAct uint32) *Packet {
	w := NewWriter()
	// 依据抓包实采: 00 0f cc af 06 + PrevAct
	w.WriteUint32(0x000FCCAF)
	w.WriteUint8(0x06)
	if prevAct == 0 {
		prevAct = ActionIDHuanshouInvadeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeSweep, w.Bytes())
}

// --------------------------------------------------------------------------------
// 14. 悟法领域
// --------------------------------------------------------------------------------

// BuildWuFaFieldInfoPacket 构造查询悟法领域面板封包 (Module 658, Action 0)。
//
// 对齐官方 Mod_WuFaField_Base.panel_info：request 为空，仅携带 PrevAct。
// 响应体由 ParseWuFaPanel 解析；官方协议中该回包没有 result_code。
func BuildWuFaFieldInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDWuFaFieldInfo, w.Bytes())
}

// BuildWuFaDrawCardPacket 构造悟法领域感悟抽卡封包 (Module 658, Action 1)。
//
// 对齐官方 Mod_WuFaField_Base.draw：request=[Int field_type, UByte draw_type]，
// draw_type 取 WuFaDrawOne (0) 或 WuFaDrawTen (1)。
//
// 线上实采 (2026-11-03): draw 处理器对载荷做严格长度校验，末尾追加 4 字节 PrevAct
// 会被服务端静默丢弃且无任何回包；去掉 PrevAct 后同一载荷立即返回结果码 2。
// 因此本请求固定为 5 字节，禁止再拼 prevAct。
func BuildWuFaDrawCardPacket(fieldType int32, drawType uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(fieldType)
	w.WriteUint8(drawType)
	return NewPacket(ActionIDWuFaDrawCard, w.Bytes())
}

// --------------------------------------------------------------------------------
// 15. 宗师修行 (协议常量与编解码见 master_practice.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 16. 天帝宝库
// --------------------------------------------------------------------------------

// Helper: DecompressPayloadIfNeeded 透明尝试解压 zlib 载荷
func DecompressPayloadIfNeeded(payload []byte) []byte {
	if len(payload) >= 2 && payload[0] == 0x78 {
		zr, err := zlib.NewReader(bytes.NewReader(payload))
		if err == nil {
			defer zr.Close()
			decomp, err := io.ReadAll(zr)
			if err == nil {
				return decomp
			}
		}
	}
	return payload
}
