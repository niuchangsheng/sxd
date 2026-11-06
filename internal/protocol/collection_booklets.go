// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方「神仙收藏册」协议 (Mod_CollectionBooklets_Base, Module 815 / 0x032F)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_CollectionBooklets_Base、
// com.datas.CollectionBookletsData 与 com.views.collection_booklets.CollectionBookletsView：
//   - popularity_award_info Act 9  -> 0x032F0009  查询声望与已领取档位
//   - get_popularity_award  Act 10 -> 0x032F000A  领取指定声望档位奖励
//   - my_friend             Act 5  -> 0x032F0005  查询可拜访/点赞的好友
//   - approve_player        Act 8  -> 0x032F0008  拜访点赞好友获得声望
package protocol

import "fmt"

const (
	// ModuleCollectionBooklets 神仙收藏册模块编号 (0x032F)
	ModuleCollectionBooklets uint16 = 815
)

// 神仙收藏册动作编号 (Mod_CollectionBooklets_Base)。
const (
	// ActionIDCollectionBookletsMyFriend 查询好友列表 (Action 5 -> 0x032F0005)
	ActionIDCollectionBookletsMyFriend uint32 = 0x032F0005
	// ActionIDCollectionBookletsMyRoleInfo 查询展示角色列表 (Action 6 -> 0x032F0006)
	ActionIDCollectionBookletsMyRoleInfo uint32 = 0x032F0006
	// ActionIDCollectionBookletsApprove 拜访点赞好友 (Action 8 -> 0x032F0008)
	ActionIDCollectionBookletsApprove uint32 = 0x032F0008
	// ActionIDCollectionBookletsPopularityInfo 查询声望奖励信息 (Action 9 -> 0x032F0009)
	ActionIDCollectionBookletsPopularityInfo uint32 = 0x032F0009
	// ActionIDCollectionBookletsGetPopularityAward 领取声望奖励 (Action 10 -> 0x032F000A)
	ActionIDCollectionBookletsGetPopularityAward uint32 = 0x032F000A
)

// 神仙收藏册结果码 (Mod_CollectionBooklets_Base)。
const (
	// CollectionBookletsResultSuccess 成功
	CollectionBookletsResultSuccess uint8 = 0
	// CollectionBookletsResultFailed 失败
	CollectionBookletsResultFailed uint8 = 1
	// CollectionBookletsResultFuncNotOpen 功能未开放
	CollectionBookletsResultFuncNotOpen uint8 = 2
	// CollectionBookletsResultNoOpenVisit 未开启拜访
	CollectionBookletsResultNoOpenVisit uint8 = 3
	// CollectionBookletsResultItemPackFull 道具背包已满
	CollectionBookletsResultItemPackFull uint8 = 6
	// CollectionBookletsResultFatePackFull 命格背包已满
	CollectionBookletsResultFatePackFull uint8 = 7
	// CollectionBookletsResultDragonBallPackFull 龙珠背包已满
	CollectionBookletsResultDragonBallPackFull uint8 = 8
	// CollectionBookletsResultNotApproveSelf 不能拜访自己
	CollectionBookletsResultNotApproveSelf uint8 = 10
	// CollectionBookletsResultNotEnoughApproveTimes 今日拜访次数已用尽
	CollectionBookletsResultNotEnoughApproveTimes uint8 = 11
	// CollectionBookletsResultIsGet 该档位已领取
	CollectionBookletsResultIsGet uint8 = 12
	// CollectionBookletsResultNotEnoughPopularity 声望不足
	CollectionBookletsResultNotEnoughPopularity uint8 = 13
)

// CollectionBookletsPopularityAward 表示一档声望奖励。
type CollectionBookletsPopularityAward struct {
	// ID 档位编号
	ID int32
	// Popularity 所需声望
	Popularity int32
}

// CollectionBookletsPopularityThresholds 声望档位静态表 (官方 CollectionBookletsTypeData.arrIpopularity)。
var CollectionBookletsPopularityThresholds = []CollectionBookletsPopularityAward{
	{ID: 1, Popularity: 10},
	{ID: 2, Popularity: 50},
	{ID: 3, Popularity: 100},
	{ID: 4, Popularity: 150},
	{ID: 5, Popularity: 200},
	{ID: 6, Popularity: 300},
	{ID: 7, Popularity: 400},
	{ID: 8, Popularity: 500},
	{ID: 9, Popularity: 600},
	{ID: 10, Popularity: 700},
	{ID: 11, Popularity: 800},
	{ID: 12, Popularity: 900},
	{ID: 13, Popularity: 1000},
}

// CollectionBookletsPopularityInfo 表示声望奖励面板。
type CollectionBookletsPopularityInfo struct {
	// Popularity 当前声望
	Popularity int32
	// ClaimedIDs 已领取的档位编号
	ClaimedIDs []int32
}

// CollectionBookletsFriend 表示一个收藏册好友。
type CollectionBookletsFriend struct {
	// RoleID 角色编号
	RoleID int32
	// HeadID 头像编号
	HeadID int32
	// RimID 头像框编号
	RimID int32
	// Nickname 昵称
	Nickname string
	// PlayerID 玩家 ID
	PlayerID int32
	// ServerName 区服名
	ServerName string
	// Popularity 声望
	Popularity int32
	// IsOpenVisit 是否允许拜访 (1 允许)
	IsOpenVisit uint8
}

// CollectionBookletsApproveResult 表示一次拜访点赞的结果。
type CollectionBookletsApproveResult struct {
	// Result 结果码
	Result uint8
	// LeftApproveTimes 剩余拜访次数
	LeftApproveTimes int32
	// AwardItemIDs 获得物品 ID
	AwardItemIDs []int32
	// AwardItemNums 获得物品数量
	AwardItemNums []int32
}

// BuildCollectionBookletsPopularityInfoPacket 构造查询声望奖励信息封包 (0x032F0009)。
func BuildCollectionBookletsPopularityInfoPacket() *Packet {
	return NewPacket(ActionIDCollectionBookletsPopularityInfo, nil)
}

// BuildCollectionBookletsGetPopularityAwardPacket 构造领取声望奖励封包 (0x032F000A)。
func BuildCollectionBookletsGetPopularityAwardPacket(awardID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(awardID)
	return NewPacket(ActionIDCollectionBookletsGetPopularityAward, w.Bytes())
}

// BuildCollectionBookletsMyFriendPacket 构造查询好友列表封包 (0x032F0005)。
func BuildCollectionBookletsMyFriendPacket() *Packet {
	return NewPacket(ActionIDCollectionBookletsMyFriend, nil)
}

// BuildCollectionBookletsApprovePacket 构造拜访点赞封包 (0x032F0008)。
func BuildCollectionBookletsApprovePacket(playerID int32, serverName string) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	w.WriteString(serverName)
	return NewPacket(ActionIDCollectionBookletsApprove, w.Bytes())
}

// ParseCollectionBookletsPopularityInfoResult 解析声望奖励信息。
func ParseCollectionBookletsPopularityInfoResult(payload []byte) (*CollectionBookletsPopularityInfo, error) {
	r := NewReader(payload)
	info := &CollectionBookletsPopularityInfo{}
	var err error
	if info.Popularity, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read popularity: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read claimed count: %v", ErrInvalidPayload, err)
	}
	info.ClaimedIDs = make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read claimed[%d]: %v", ErrInvalidPayload, i, err)
		}
		info.ClaimedIDs = append(info.ClaimedIDs, id)
	}
	return info, nil
}

// ParseCollectionBookletsMyFriendResult 解析好友列表。
func ParseCollectionBookletsMyFriendResult(payload []byte) (uint8, []CollectionBookletsFriend, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read friend result: %v", ErrInvalidPayload, err)
	}
	if result != CollectionBookletsResultSuccess {
		return result, nil, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read friend count: %v", ErrInvalidPayload, err)
	}
	friends := make([]CollectionBookletsFriend, 0, count)
	for i := 0; i < int(count); i++ {
		var f CollectionBookletsFriend
		for _, field := range []*int32{&f.RoleID, &f.HeadID, &f.RimID} {
			if *field, err = r.ReadInt32(); err != nil {
				return 0, nil, fmt.Errorf("%w: failed to read friend[%d]: %v", ErrInvalidPayload, i, err)
			}
		}
		if f.Nickname, err = r.ReadString(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read friend[%d].nickname: %v", ErrInvalidPayload, i, err)
		}
		if f.PlayerID, err = r.ReadInt32(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read friend[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if f.ServerName, err = r.ReadString(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read friend[%d].server_name: %v", ErrInvalidPayload, i, err)
		}
		if f.Popularity, err = r.ReadInt32(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read friend[%d].popularity: %v", ErrInvalidPayload, i, err)
		}
		if f.IsOpenVisit, err = r.ReadUint8(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read friend[%d].is_open_visit: %v", ErrInvalidPayload, i, err)
		}
		friends = append(friends, f)
	}
	return result, friends, nil
}

// ParseCollectionBookletsApproveResult 解析拜访点赞结果。
func ParseCollectionBookletsApproveResult(payload []byte) (*CollectionBookletsApproveResult, error) {
	r := NewReader(payload)
	res := &CollectionBookletsApproveResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read approve result: %v", ErrInvalidPayload, err)
	}
	if res.Result != CollectionBookletsResultSuccess {
		return res, nil
	}
	if res.LeftApproveTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read left approve times: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read approve award count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read approve award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		itemNum, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read approve award[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		res.AwardItemIDs = append(res.AwardItemIDs, itemID)
		res.AwardItemNums = append(res.AwardItemNums, itemNum)
	}
	return res, nil
}

// ParseCollectionBookletsActionCode 解析仅返回单结果码的动作 (get_popularity_award)。
func ParseCollectionBookletsActionCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty collection booklets action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildCollectionBookletsPopularityInfoResultPacket 构造声望信息回包，供离线 mock 与单测使用。
func BuildCollectionBookletsPopularityInfoResultPacket(info CollectionBookletsPopularityInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.Popularity)
	w.WriteUint16(uint16(len(info.ClaimedIDs)))
	for _, id := range info.ClaimedIDs {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDCollectionBookletsPopularityInfo, w.Bytes())
}

// BuildCollectionBookletsMyFriendResultPacket 构造好友列表回包，供离线 mock 与单测使用。
func BuildCollectionBookletsMyFriendResultPacket(result uint8, friends []CollectionBookletsFriend) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	if result == CollectionBookletsResultSuccess {
		w.WriteUint16(uint16(len(friends)))
		for _, f := range friends {
			w.WriteInt32(f.RoleID)
			w.WriteInt32(f.HeadID)
			w.WriteInt32(f.RimID)
			w.WriteString(f.Nickname)
			w.WriteInt32(f.PlayerID)
			w.WriteString(f.ServerName)
			w.WriteInt32(f.Popularity)
			w.WriteUint8(f.IsOpenVisit)
		}
	}
	return NewPacket(ActionIDCollectionBookletsMyFriend, w.Bytes())
}

// BuildCollectionBookletsApproveResultPacket 构造拜访回包，供离线 mock 与单测使用。
func BuildCollectionBookletsApproveResultPacket(res CollectionBookletsApproveResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == CollectionBookletsResultSuccess {
		w.WriteInt32(res.LeftApproveTimes)
		w.WriteUint16(uint16(len(res.AwardItemIDs)))
		for i := range res.AwardItemIDs {
			w.WriteInt32(res.AwardItemIDs[i])
			var num int32
			if i < len(res.AwardItemNums) {
				num = res.AwardItemNums[i]
			}
			w.WriteInt32(num)
		}
	}
	return NewPacket(ActionIDCollectionBookletsApprove, w.Bytes())
}

// BuildCollectionBookletsActionCodePacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildCollectionBookletsActionCodePacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}
