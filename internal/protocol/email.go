// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方客户端 Main.swf 中 Mod_Email_Base (Module 345 / 0x0159) 的邮箱协议：
//   - get_email_info:         module 345, action 0, request [], response 见官方描述符
//   - read_email:             module 345, action 1, request [Int]
//   - get_attachment_award:   module 345, action 2, request [Int]
//   - delete_email:           module 345, action 3, request [UByte, Int]
//   - notify_new_email:       module 345, action 4, request []
//   - is_have_new_email:      module 345, action 5, request []
//   - one_key_get_award:      module 345, action 6, request []，即「邮箱一键收取」
//
// 客户端 DataBase.netCall 会在所有主服请求末尾自动追加 4 字节 PrevAct (上一请求的
// Module+Action)，因此上述空请求实际线格式为 uint32 PrevAct。
package protocol

import "fmt"

const (
	// ModuleEmail 邮箱模块编号 (Mod_Email_Base)
	ModuleEmail uint16 = 345

	// ActionIDEmailGetInfo 查询邮箱面板与邮件列表 (Module 345, Action 0 -> 0x01590000)
	ActionIDEmailGetInfo uint32 = 0x01590000
	// ActionIDEmailRead 阅读指定邮件 (Module 345, Action 1 -> 0x01590001)
	ActionIDEmailRead uint32 = 0x01590001
	// ActionIDEmailGetAttachmentAward 领取单封邮件附件 (Module 345, Action 2 -> 0x01590002)
	ActionIDEmailGetAttachmentAward uint32 = 0x01590002
	// ActionIDEmailDelete 删除指定邮件 (Module 345, Action 3 -> 0x01590003)
	ActionIDEmailDelete uint32 = 0x01590003
	// ActionIDEmailNotifyNew 新邮件服务端推送 (Module 345, Action 4 -> 0x01590004)
	ActionIDEmailNotifyNew uint32 = 0x01590004
	// ActionIDEmailIsHaveNew 查询是否存在新邮件 (Module 345, Action 5 -> 0x01590005)
	ActionIDEmailIsHaveNew uint32 = 0x01590005
	// ActionIDEmailOneKeyGetAward 一键领取全部邮件附件 (Module 345, Action 6 -> 0x01590006)
	ActionIDEmailOneKeyGetAward uint32 = 0x01590006
)

// 邮箱业务结果码 (对齐 Mod_Email_Base 静态常量)。
const (
	// EmailResultSuccess 收取成功 (SUCCESS = 8)
	EmailResultSuccess uint8 = 8
	// EmailResultIDError 邮件编号无效 (ID_ERROR = 9)
	EmailResultIDError uint8 = 9
	// EmailResultNotThisPlayer 邮件不属于当前角色 (NOT_THIS_PLAYER = 10)
	EmailResultNotThisPlayer uint8 = 10
	// EmailResultFailure 通用失败 (FAILURE = 11)
	EmailResultFailure uint8 = 11
	// EmailResultHadGet 附件已领取过 (HAD_GET = 12)
	EmailResultHadGet uint8 = 12
	// EmailResultItemPackFull 物品背包已满 (ITEM_PACKAGE_FULL = 13)
	EmailResultItemPackFull uint8 = 13
	// EmailResultFatePackFull 命格背包已满 (FATE_PACKAGE_FULL = 14)
	EmailResultFatePackFull uint8 = 14
	// EmailResultLinkFatePackFull 命格链接背包已满 (LINK_FATE_PACK_FULL = 15)
	EmailResultLinkFatePackFull uint8 = 15
	// EmailResultDragonballPackFull 龙珠背包已满 (DRAGONBALL_PACK_FULL = 16)
	EmailResultDragonballPackFull uint8 = 16
	// EmailResultNoAward 没有可领取的附件 (NO_AWARD = 22)
	EmailResultNoAward uint8 = 22
)

// 邮箱状态与附件状态常量 (对齐 Mod_Email_Base 静态常量)。
const (
	// EmailStateRead 已读 (READ = 0)
	EmailStateRead uint8 = 0
	// EmailStateUnread 未读 (UNREAD = 1)
	EmailStateUnread uint8 = 1
	// EmailTypeCommon 普通邮件 (COMMON = 2)
	EmailTypeCommon uint8 = 2
	// EmailTypeAttachment 带附件邮件 (ATTACHMENT = 3)
	EmailTypeAttachment uint8 = 3
	// EmailAttachmentGet 附件已领取 (GET = 4)
	EmailAttachmentGet uint8 = 4
	// EmailAttachmentUnGet 附件未领取 (UNGET = 5)
	EmailAttachmentUnGet uint8 = 5
)

// BuildEmailGetInfoPacket 构造查询邮箱面板与邮件列表封包 (Module 345, Action 0)。
func BuildEmailGetInfoPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDEmailGetInfo, prevAct)
}

// BuildEmailIsHaveNewPacket 构造查询是否存在新邮件封包 (Module 345, Action 5)。
func BuildEmailIsHaveNewPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDEmailIsHaveNew, prevAct)
}

// BuildEmailOneKeyGetAwardPacket 构造一键领取全部邮件附件封包 (Module 345, Action 6)。
func BuildEmailOneKeyGetAwardPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDEmailOneKeyGetAward, prevAct)
}

// BuildEmailGetAttachmentPacket 构造领取单封邮件附件封包 (Module 345, Action 2)。
// 线格式对齐 docs/protocol-spec.md: Int mailID + prevAct (尾部)。
func BuildEmailGetAttachmentPacket(mailID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(mailID)
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEmailGetAttachmentAward, w.Bytes())
}

// ParseEmailGetAttachmentResult 解析单封邮件附件领取回包: uint8 业务结果码。
func ParseEmailGetAttachmentResult(payload []byte) (uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read get_attachment_award result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// ParseEmailIsHaveNewResult 解析新邮件查询回包: uint8 状态。
func ParseEmailIsHaveNewResult(payload []byte) (uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	state, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read is_have_new_email state: %v", ErrInvalidPayload, err)
	}
	return state, nil
}

// ParseEmailOneKeyGetAwardResult 解析一键收取回包: uint8 业务结果码。
func ParseEmailOneKeyGetAwardResult(payload []byte) (uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read one_key_get_award result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// EmailResultText 返回邮箱业务结果码的中文说明。
func EmailResultText(result uint8) string {
	switch result {
	case EmailResultSuccess:
		return "附件收取成功"
	case EmailResultIDError:
		return "邮件编号无效"
	case EmailResultNotThisPlayer:
		return "邮件不属于当前角色"
	case EmailResultFailure:
		return "服务端处理失败"
	case EmailResultHadGet:
		return "附件已领取"
	case EmailResultItemPackFull:
		return "物品背包已满"
	case EmailResultFatePackFull:
		return "命格背包已满"
	case EmailResultLinkFatePackFull:
		return "命格链接背包已满"
	case EmailResultDragonballPackFull:
		return "龙珠背包已满"
	case EmailResultNoAward:
		return "没有可领取的附件"
	default:
		return "未识别的邮箱结果码"
	}
}
