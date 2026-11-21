// Package protocol 提供万古神墟 (Mod_EternalGamesPresent_Base, Module 840) 协议。
//
// 模块映射以原版反编译类 com.protocols.Mod_EternalGamesPresent_Base 与
// 2026-11-21 抓包 /tmp/wangushenxu.pcapng 为准:
//
//	main_info   840/0 request=[]          response=[UByte result, [Player...], Long can_get_coin]
//	worship     840/1 request=[Int type]  response=[UByte result, Int worship_ingot]
//	get_award   840/2 request=[]          response=[UByte result, Long get_coin]
//
// 抓包实测:
//   - 三个请求均在主服会话 (81.70.56.121:8200) 上收发，载荷末尾携带 4 字节 PrevAct；
//   - worship 的 type 对应万古神墟大神像类型 (1/2/3/4/5)，成功回包中的
//     worship_ingot 是本次膜拜给**被膜拜榜一**带来的元宝奖励：抓包客户端自身
//     就是榜一 (司空测试号)，因此在同一会话能看到 0/3 update_player_data
//     (PLAYER_INGOT=19) 余额按 worship_ingot 增加；
//     2026-11-21 实测普通膜拜者 (非榜一) 膜拜他人成功，但自身元宝不变。
//     膜拜对膜拜者免费，不消耗任何元宝；
//   - get_award 领取的是被膜拜者累计的铜钱奖励 (can_get_coin)，普通角色通常为 0。
package protocol

import "fmt"

const (
	// ActionIDWanguShenxuMainInfo 万古神墟主面板 (840/0 -> 0x03480000)
	ActionIDWanguShenxuMainInfo uint32 = 0x03480000
	// ActionIDWanguShenxuWorship 膜拜大神像 (840/1 -> 0x03480001)
	ActionIDWanguShenxuWorship uint32 = 0x03480001
	// ActionIDWanguShenxuGetAward 领取累计膜拜奖励 (840/2 -> 0x03480002)
	ActionIDWanguShenxuGetAward uint32 = 0x03480002
)

// 万古神墟结果码 (Mod_EternalGamesPresent_Base)。
const (
	// WanguShenxuResultSuccess 成功
	WanguShenxuResultSuccess uint8 = 0
	// WanguShenxuResultFailed 通用失败
	WanguShenxuResultFailed uint8 = 1
	// WanguShenxuResultFuncNotOpen 功能/活动未开放
	WanguShenxuResultFuncNotOpen uint8 = 2
	// WanguShenxuResultHasWorshipped 今日已膜拜该大神像
	WanguShenxuResultHasWorshipped uint8 = 3
	// WanguShenxuResultNotAward 暂无可领取奖励
	WanguShenxuResultNotAward uint8 = 4
)

// WanguShenxuSeason 表示大神像的赛季/阶段条目。
type WanguShenxuSeason struct {
	// Season 赛季编号
	Season int32
	// Step 阶段编号
	Step int32
}

// WanguShenxuPlayer 表示万古神墟主面板中的一个膜拜对象。
type WanguShenxuPlayer struct {
	// Type 大神像类型 (1~5)
	Type int32
	// Season 当前赛季
	Season int32
	// Step 当前阶段
	Step int32
	// PlayerID 玩家平台 ID；0 表示该神像暂无对象
	PlayerID int32
	// ServerName 区服标识
	ServerName string
	// Nickname 角色名
	Nickname string
	// RoleID 职业角色编号
	RoleID int32
	// SuitID 时装编号
	SuitID int32
	// WarPower 战力
	WarPower int64
	// IsWorship 今日是否已膜拜
	IsWorship bool
	// WorshipNum 该神像累计膜拜次数
	WorshipNum int32
	// Seasons 可选赛季/阶段列表
	Seasons []WanguShenxuSeason
	// WeedAwardCoin 本周被膜拜累计铜钱
	WeedAwardCoin int64
	// TotalAwardCoin 累计被膜拜铜钱
	TotalAwardCoin int64
}

// WanguShenxuPanel 表示 840/0 主面板回包。
type WanguShenxuPanel struct {
	// Result 结果码
	Result uint8
	// Players 大神像列表
	Players []WanguShenxuPlayer
	// CanGetCoin 当前可领取的累计铜钱奖励
	CanGetCoin int64
}

// WanguShenxuWorshipResult 表示 840/1 膜拜回包。
type WanguShenxuWorshipResult struct {
	// Result 结果码
	Result uint8
	// Ingot 本次膜拜给被膜拜榜一带来的元宝奖励 (仅成功时有效)。
	// 注意: 普通膜拜者自身不会因此增加元宝；抓包客户端自身就是榜一。
	Ingot int32
}

// WanguShenxuAwardResult 表示 840/2 领取奖励回包。
type WanguShenxuAwardResult struct {
	// Result 结果码
	Result uint8
	// Coin 本次领取的铜钱数量 (仅成功时有效)
	Coin int64
}

// BuildWanguShenxuMainInfoPacket 构造主面板查询封包 (840/0)。
//
// 官方请求为空，但抓包中载荷末尾始终携带 4 字节 PrevAct。
func BuildWanguShenxuMainInfoPacket() *Packet {
	pkt := NewPacket(ActionIDWanguShenxuMainInfo, make([]byte, 4))
	pkt.HasPrevAct = true
	return pkt
}

// BuildWanguShenxuWorshipPacket 构造膜拜封包 (840/1)，载荷为 [Int type] + PrevAct。
func BuildWanguShenxuWorshipPacket(typ int32) *Packet {
	w := NewWriter()
	w.WriteInt32(typ)
	w.WriteUint32(0)
	pkt := NewPacket(ActionIDWanguShenxuWorship, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildWanguShenxuGetAwardPacket 构造领取累计奖励封包 (840/2)。
//
// 官方请求为空，抓包风格与 main_info 一致，载荷末尾携带 4 字节 PrevAct。
func BuildWanguShenxuGetAwardPacket() *Packet {
	pkt := NewPacket(ActionIDWanguShenxuGetAward, make([]byte, 4))
	pkt.HasPrevAct = true
	return pkt
}

// ParseWanguShenxuPanel 解析 840/0 主面板回包。
func ParseWanguShenxuPanel(payload []byte) (*WanguShenxuPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	panel := &WanguShenxuPanel{}
	var err error
	if panel.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 万古神墟结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if panel.Result != WanguShenxuResultSuccess {
		return panel, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 万古神墟大神像数量读取失败: %v", ErrInvalidPayload, err)
	}
	panel.Players = make([]WanguShenxuPlayer, 0, count)
	for i := 0; i < int(count); i++ {
		var p WanguShenxuPlayer
		if p.Type, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].type 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.Season, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].season 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.Step, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].step 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].player_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.ServerName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].server_name 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.Nickname, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].nickname 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].role_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.SuitID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].suit_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.WarPower, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].war_power 读取失败: %v", ErrInvalidPayload, i, err)
		}
		isWorship, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].is_worship 读取失败: %v", ErrInvalidPayload, i, err)
		}
		p.IsWorship = isWorship == 1
		if p.WorshipNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].worship_num 读取失败: %v", ErrInvalidPayload, i, err)
		}
		seasonCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].seasons 数量读取失败: %v", ErrInvalidPayload, i, err)
		}
		p.Seasons = make([]WanguShenxuSeason, 0, seasonCount)
		for j := 0; j < int(seasonCount); j++ {
			var s WanguShenxuSeason
			if s.Season, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 万古神墟大神像[%d].seasons[%d].season 读取失败: %v", ErrInvalidPayload, i, j, err)
			}
			if s.Step, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: 万古神墟大神像[%d].seasons[%d].step 读取失败: %v", ErrInvalidPayload, i, j, err)
			}
			p.Seasons = append(p.Seasons, s)
		}
		if p.WeedAwardCoin, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].weed_award_coin 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if p.TotalAwardCoin, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: 万古神墟大神像[%d].total_award_coin 读取失败: %v", ErrInvalidPayload, i, err)
		}
		panel.Players = append(panel.Players, p)
	}
	if panel.CanGetCoin, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: 万古神墟 can_get_coin 读取失败: %v", ErrInvalidPayload, err)
	}
	return panel, nil
}

// ParseWanguShenxuWorshipResult 解析 840/1 膜拜回包 [UByte result, Int ingot]。
func ParseWanguShenxuWorshipResult(payload []byte) (*WanguShenxuWorshipResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &WanguShenxuWorshipResult{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 万古神墟膜拜结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if out.Result != WanguShenxuResultSuccess {
		return out, nil
	}
	if out.Ingot, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 万古神墟膜拜元宝读取失败: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// ParseWanguShenxuAwardResult 解析 840/2 领取奖励回包 [UByte result, Long coin]。
func ParseWanguShenxuAwardResult(payload []byte) (*WanguShenxuAwardResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &WanguShenxuAwardResult{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 万古神墟领奖结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if out.Result != WanguShenxuResultSuccess {
		return out, nil
	}
	if out.Coin, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: 万古神墟领取铜钱读取失败: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// WanguShenxuResultText 返回结果码文本。
func WanguShenxuResultText(code uint8) string {
	switch code {
	case WanguShenxuResultSuccess:
		return "成功"
	case WanguShenxuResultFailed:
		return "失败"
	case WanguShenxuResultFuncNotOpen:
		return "功能未开放"
	case WanguShenxuResultHasWorshipped:
		return "今日已膜拜"
	case WanguShenxuResultNotAward:
		return "暂无可领取奖励"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
