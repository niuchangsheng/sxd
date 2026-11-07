// Package protocol 提供仙界虚空协议 (Mod_StVoid_Base, Module 652 / 0x028C)。
//
// 官方客户端 Socket 动作只有以下两项:
//
//	open_panel 652/1 request=[]
//	           response=[UByte result, String st_name, Int st_uid,
//	                     [[Int st_id, Int st_uid, Int match_rank, Int like_num,
//	                       [[Int, String server_name, String nickname, Int role_id, Int cloth_id,
//	                         Int level, Long war_power, Int, Int, Int world_pk_score, [Int role_ids]]]]],
//	                     [Int liked_st_uid]]]
//	like       652/2 request=[Int st_uid] response=[UByte result, [[Int item_id, Int item_num]]]
//
// 重要: 652/3 (get_war_power_rank) 与 652/4 (get_world_pk_rank) **不是 Socket 协议**。
// 官方客户端 com.views.st_void.StVoidRankView 通过 HttpUtil.getData 请求
// server-web/route.php?m=svp|svs&id=<st_name> 获取排行榜数据，
// Mod_StVoid_Base 中的类型描述符只用于 HTTP 返回体解析。
// 因此自动化绝不能把 652/4 发到游戏长连接上 (服务端不会回包)。
package protocol

import "fmt"

// 仙界虚空动作编号 (Mod_StVoid_Base)。
const (
	// ActionIDStVoidOpen 仙界虚空主面板 (Action 1 -> 0x028C0001)
	ActionIDStVoidOpen uint32 = 0x028C0001
	// ActionIDStVoidLike 仙界虚空膜拜/点赞 (Action 2 -> 0x028C0002)
	ActionIDStVoidLike uint32 = 0x028C0002
	// ActionIDStVoidWorldPkRank 世界 PK 榜 (Action 4)。
	//
	// 该编号仅用于 HTTP 排行榜返回体的类型描述，不是游戏 Socket 动作。
	// 保留常量以便文档与探针说明，禁止构造 Socket 请求。
	ActionIDStVoidWorldPkRank uint32 = 0x028C0004
)

// 仙界虚空结果码。
const (
	// StVoidResultSuccess 成功
	StVoidResultSuccess uint8 = 0
	// StVoidResultFailed 失败
	StVoidResultFailed uint8 = 1
	// StVoidResultIsLiked 本周已膜拜该玩家
	StVoidResultIsLiked uint8 = 2
	// StVoidResultPackFull 背包已满
	StVoidResultPackFull uint8 = 3
)

// StVoidMember 表示仙界虚空主面板中的一位可膜拜玩家。
type StVoidMember struct {
	// StID 虚空席位编号
	StID int32
	// StUID 玩家唯一 ID (like 请求参数)
	StUID int32
	// MatchRank 匹配排名
	MatchRank int32
	// LikeNum 已获赞数量
	LikeNum int32
	// ServerName 区服名
	ServerName string
	// Nickname 角色名
	Nickname string
	// RoleID 角色职业编号
	RoleID int32
	// ClothID 时装编号
	ClothID int32
	// Level 等级
	Level int32
	// WarPower 战力
	WarPower int64
	// WorldPKScore 世界 PK 积分
	WorldPKScore int32
}

// StVoidPanel 表示仙界虚空主面板。
type StVoidPanel struct {
	// Result 结果码
	Result uint8
	// StName 虚空名称
	StName string
	// StUID 当前玩家在虚空中的 UID
	StUID int32
	// Members 可膜拜玩家列表
	Members []StVoidMember
	// LikedUIDs 本周已膜拜的玩家 UID
	LikedUIDs []int32
}

// StVoidAward 表示一次膜拜获得的奖励。
type StVoidAward struct {
	// ItemID 道具编号
	ItemID int32
	// Num 数量
	Num int32
}

// StVoidLikeResult 表示膜拜结果。
type StVoidLikeResult struct {
	// Result 结果码
	Result uint8
	// Awards 奖励列表
	Awards []StVoidAward
}

// BuildStVoidOpenPacket 构造仙界虚空主面板查询封包 (652/1)。
func BuildStVoidOpenPacket() *Packet {
	return NewPacket(ActionIDStVoidOpen, nil)
}

// BuildStVoidLikePacket 构造膜拜封包 (652/2)。
//
// 官方 request=[Int st_uid]，无线格式要求追加 PrevAct。
func BuildStVoidLikePacket(stUID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(stUID)
	return NewPacket(ActionIDStVoidLike, w.Bytes())
}

// ParseStVoidPanelResult 解析仙界虚空主面板回包。
func ParseStVoidPanelResult(payload []byte) (*StVoidPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	panel := &StVoidPanel{}
	var err error
	if panel.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 仙界虚空结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if panel.Result != StVoidResultSuccess {
		return panel, nil
	}
	if panel.StName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: 仙界虚空 st_name 读取失败: %v", ErrInvalidPayload, err)
	}
	if panel.StUID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 仙界虚空 st_uid 读取失败: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 仙界虚空成员数量读取失败: %v", ErrInvalidPayload, err)
	}
	panel.Members = make([]StVoidMember, 0, count)
	for i := 0; i < int(count); i++ {
		var m StVoidMember
		if m.StID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空成员[%d].st_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.StUID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空成员[%d].st_uid 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.MatchRank, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空成员[%d].match_rank 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.LikeNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空成员[%d].like_num 读取失败: %v", ErrInvalidPayload, i, err)
		}
		roleCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空成员[%d] 角色数组读取失败: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < int(roleCount); j++ {
			if _, err = r.ReadInt32(); err != nil { // role[0]
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] role[%d] 头部读取失败: %v", ErrInvalidPayload, i, j, err)
			}
			if m.ServerName, err = r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] server_name 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if m.Nickname, err = r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] nickname 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if m.RoleID, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] role_id 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if m.ClothID, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] cloth_id 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if m.Level, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] level 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if m.WarPower, err = r.ReadInt64(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] war_power 读取失败: %v", ErrInvalidPayload, i, err)
			}
			// role[7] / role[8] 为未使用占位。
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] role[7] 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] role[8] 读取失败: %v", ErrInvalidPayload, i, err)
			}
			if m.WorldPKScore, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] world_pk_score 读取失败: %v", ErrInvalidPayload, i, err)
			}
			roleIDs, err := r.ReadUint16()
			if err != nil {
				return nil, fmt.Errorf("%w: 仙界虚空成员[%d] role_list 读取失败: %v", ErrInvalidPayload, i, err)
			}
			for k := 0; k < int(roleIDs); k++ {
				if _, err = r.ReadInt32(); err != nil {
					return nil, fmt.Errorf("%w: 仙界虚空成员[%d] role_list[%d] 读取失败: %v", ErrInvalidPayload, i, k, err)
				}
			}
		}
		panel.Members = append(panel.Members, m)
	}
	likedCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 仙界虚空已膜拜列表读取失败: %v", ErrInvalidPayload, err)
	}
	panel.LikedUIDs = make([]int32, 0, likedCount)
	for i := 0; i < int(likedCount); i++ {
		uid, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空已膜拜[%d] 读取失败: %v", ErrInvalidPayload, i, err)
		}
		panel.LikedUIDs = append(panel.LikedUIDs, uid)
	}
	return panel, nil
}

// ParseStVoidLikeResult 解析膜拜结果。
func ParseStVoidLikeResult(payload []byte) (*StVoidLikeResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res := &StVoidLikeResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 仙界虚空膜拜结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if res.Result != StVoidResultSuccess {
		return res, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return res, nil
	}
	res.Awards = make([]StVoidAward, 0, count)
	for i := 0; i < int(count); i++ {
		var award StVoidAward
		if award.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空奖励[%d] item_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if award.Num, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 仙界虚空奖励[%d] 数量读取失败: %v", ErrInvalidPayload, i, err)
		}
		res.Awards = append(res.Awards, award)
	}
	return res, nil
}

// StVoidResultText 返回仙界虚空结果码的可读文本。
func StVoidResultText(code uint8) string {
	switch code {
	case StVoidResultSuccess:
		return "成功"
	case StVoidResultFailed:
		return "失败"
	case StVoidResultIsLiked:
		return "本周已膜拜"
	case StVoidResultPackFull:
		return "背包已满"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildStVoidPanelResultPacket 构造主面板回包，供离线 mock 与单测使用。
func BuildStVoidPanelResultPacket(panel StVoidPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(panel.Result)
	if panel.Result != StVoidResultSuccess {
		return NewPacket(ActionIDStVoidOpen, w.Bytes())
	}
	w.WriteString(panel.StName)
	w.WriteInt32(panel.StUID)
	w.WriteUint16(uint16(len(panel.Members)))
	for _, m := range panel.Members {
		w.WriteInt32(m.StID)
		w.WriteInt32(m.StUID)
		w.WriteInt32(m.MatchRank)
		w.WriteInt32(m.LikeNum)
		w.WriteUint16(1)
		w.WriteInt32(0)
		w.WriteString(m.ServerName)
		w.WriteString(m.Nickname)
		w.WriteInt32(m.RoleID)
		w.WriteInt32(m.ClothID)
		w.WriteInt32(m.Level)
		w.WriteInt64(m.WarPower)
		w.WriteInt32(0)
		w.WriteInt32(0)
		w.WriteInt32(m.WorldPKScore)
		w.WriteUint16(0)
	}
	w.WriteUint16(uint16(len(panel.LikedUIDs)))
	for _, uid := range panel.LikedUIDs {
		w.WriteInt32(uid)
	}
	return NewPacket(ActionIDStVoidOpen, w.Bytes())
}

// BuildStVoidLikeResultPacket 构造膜拜回包，供离线 mock 与单测使用。
func BuildStVoidLikeResultPacket(res StVoidLikeResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == StVoidResultSuccess {
		w.WriteUint16(uint16(len(res.Awards)))
		for _, award := range res.Awards {
			w.WriteInt32(award.ItemID)
			w.WriteInt32(award.Num)
		}
	}
	return NewPacket(ActionIDStVoidLike, w.Bytes())
}
