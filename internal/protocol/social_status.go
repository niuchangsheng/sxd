// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现仙盟/圣盟/住宅的状态探测协议：
//   - Mod_Sect_Base.get_open_status (Module 571, Action 6) 仙盟功能开放状态
//   - Mod_SectAlliance_Base.get_now_season (Module 884, Action 0) 圣盟当前赛季
//   - Mod_MarryHome_Base.get_self_home_info (Module 210, Action 0) 自身住宅状态
//
// 这些玩法依赖账号是否加入仙盟/圣盟或处于婚姻状态，自动化必须先探测再决定是否执行。
package protocol

import "fmt"

const (
	// ActionIDSectOpenStatus 查询仙盟功能开放状态 (Module 571, Action 6 -> 0x023B0006)
	ActionIDSectOpenStatus uint32 = 0x023B0006
	// ActionIDSectPanelInfo 查询仙盟面板信息 (Module 571, Action 0 -> 0x023B0000)
	ActionIDSectPanelInfo uint32 = 0x023B0000
	// ActionIDSectAllianceSeason 查询圣盟当前赛季 (Module 884, Action 0 -> 0x03740000)
	ActionIDSectAllianceSeason uint32 = 0x03740000
	// ActionIDMarryHomeSelfInfo 查询自身住宅状态 (Module 210, Action 0 -> 0x00D20000)
	ActionIDMarryHomeSelfInfo uint32 = 0x00D20000
)

// MarryHomeResultNonMarry 未处于婚姻状态。
const MarryHomeResultNonMarry uint8 = 3

// BuildSectOpenStatusPacket 构造查询仙盟开放状态封包 (0x023B0006)。
func BuildSectOpenStatusPacket() *Packet {
	return NewPacket(ActionIDSectOpenStatus, nil)
}

// BuildSectPanelInfoPacket 构造查询仙盟面板封包 (0x023B0000)。
func BuildSectPanelInfoPacket() *Packet {
	return NewPacket(ActionIDSectPanelInfo, nil)
}

// BuildSectAllianceSeasonPacket 构造查询圣盟赛季封包 (0x03740000)。
func BuildSectAllianceSeasonPacket() *Packet {
	return NewPacket(ActionIDSectAllianceSeason, nil)
}

// BuildMarryHomeSelfInfoPacket 构造查询自身住宅状态封包 (0x00D20000)。
func BuildMarryHomeSelfInfoPacket(playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	return NewPacket(ActionIDMarryHomeSelfInfo, w.Bytes())
}

// ParseSectOpenStatusResult 解析仙盟开放状态 (UByte status)。
func ParseSectOpenStatusResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty sect open status", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseSectAllianceSeasonResult 解析圣盟赛季 (Int season, Int ?)。
func ParseSectAllianceSeasonResult(payload []byte) (season int32, endTime int32, err error) {
	r := NewReader(payload)
	if season, err = r.ReadInt32(); err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read alliance season: %v", ErrInvalidPayload, err)
	}
	if endTime, err = r.ReadInt32(); err != nil {
		return season, 0, fmt.Errorf("%w: failed to read alliance season end: %v", ErrInvalidPayload, err)
	}
	return season, endTime, nil
}

// ParseMarryHomeSelfInfoResult 解析住宅状态 (Byte result)。
func ParseMarryHomeSelfInfoResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty marry home result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildSectOpenStatusResultPacket 构造仙盟开放状态回包，供离线 mock 与单测使用。
func BuildSectOpenStatusResultPacket(status uint8) *Packet {
	return NewPacket(ActionIDSectOpenStatus, []byte{status})
}

// BuildSectAllianceSeasonResultPacket 构造圣盟赛季回包，供离线 mock 与单测使用。
func BuildSectAllianceSeasonResultPacket(season, endTime int32) *Packet {
	w := NewWriter()
	w.WriteInt32(season)
	w.WriteInt32(endTime)
	return NewPacket(ActionIDSectAllianceSeason, w.Bytes())
}

// BuildMarryHomeSelfInfoResultPacket 构造住宅状态回包，供离线 mock 与单测使用。
func BuildMarryHomeSelfInfoResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDMarryHomeSelfInfo, []byte{result})
}

// SectLocation 表示仙盟面板中的一个宗门地点。
type SectLocation struct {
	// LocationID 地点编号
	LocationID int32
	// SectID 占据该地点的宗门 ID (0 表示空位)
	SectID int32
	// Group 所在 socket 组
	Group int32
	// SectLevel 宗门等级
	SectLevel int32
}

// SectPanelSummary 表示仙盟面板中与城镇会话相关的关键字段。
type SectPanelSummary struct {
	// MyAppID 当前玩家宗门 appId
	MyAppID int32
	// MySectID 所属宗门 ID
	MySectID int32
	// MySectGroup 宗门所在 socket 组
	MySectGroup int32
	// MySectLevel 宗门等级
	MySectLevel int32
	// MySectName 宗门名称
	MySectName string
	// Locations 宗门地点列表
	Locations []SectLocation
}

// ParseSectPanelSummary 解析仙盟面板关键字段，跳过宗门地点列表。
func ParseSectPanelSummary(payload []byte) (*SectPanelSummary, error) {
	r := NewReader(payload)
	// begin_create_time / end_create_time / begin_vote_time / end_vote_time
	for i := 0; i < 4; i++ {
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect panel time[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sect location count: %v", ErrInvalidPayload, err)
	}
	res := &SectPanelSummary{}
	for i := 0; i < int(count); i++ {
		loc := SectLocation{}
		locID, err := r.ReadInt32() // sect_location_id
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read sect location[%d].id: %v", ErrInvalidPayload, i, err)
		}
		loc.LocationID = locID
		sectID, err := r.ReadInt32() // sect_id
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read sect location[%d].sect_id: %v", ErrInvalidPayload, i, err)
		}
		loc.SectID = sectID
		if _, err := r.ReadString(); err != nil { // sect_name
			return nil, fmt.Errorf("%w: failed to read sect location[%d].sect_name: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadString(); err != nil { // leader_name
			return nil, fmt.Errorf("%w: failed to read sect location[%d].leader_name: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil { // vote_appid
			return nil, fmt.Errorf("%w: failed to read sect location[%d].vote_appid: %v", ErrInvalidPayload, i, err)
		}
		applyCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read sect location[%d].apply count: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < int(applyCount); j++ {
			if _, err := r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read sect location[%d].apply[%d].a: %v", ErrInvalidPayload, i, j, err)
			}
			if _, err := r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: failed to read sect location[%d].apply[%d].b: %v", ErrInvalidPayload, i, j, err)
			}
			if _, err := r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: failed to read sect location[%d].apply[%d].c: %v", ErrInvalidPayload, i, j, err)
			}
			if _, err := r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: failed to read sect location[%d].apply[%d].d: %v", ErrInvalidPayload, i, j, err)
			}
			if _, err := r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read sect location[%d].apply[%d].e: %v", ErrInvalidPayload, i, j, err)
			}
		}
		group, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read sect location[%d].group: %v", ErrInvalidPayload, i, err)
		}
		loc.Group = group
		lv, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read sect location[%d].sect_lv: %v", ErrInvalidPayload, i, err)
		}
		loc.SectLevel = lv
		res.Locations = append(res.Locations, loc)
	}
	if res.MyAppID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read my_appid: %v", ErrInvalidPayload, err)
	}
	if res.MySectID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read my_sect_id: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // god_rank_job
		return nil, fmt.Errorf("%w: failed to read god_rank_job: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // salary
		return nil, fmt.Errorf("%w: failed to read salary: %v", ErrInvalidPayload, err)
	}
	if res.MySectGroup, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read my_sect_group: %v", ErrInvalidPayload, err)
	}
	if res.MySectLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read my_sect_lv: %v", ErrInvalidPayload, err)
	}
	if res.MySectName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read my_sect_name: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// SectLoginInfo 表示仙盟城镇登录凭据。
//
// 线格式: UByte result, String host, String port, Int appId, String node,
// String serverName, Int time, String passCode, Int group。
type SectLoginInfo struct {
	// Result 结果码
	Result uint8
	// Host 城镇服务器地址
	Host string
	// Port 城镇服务器端口
	Port int32
	// AppID 应用编号
	AppID int32
	// Node 节点标识
	Node string
	// ServerName 服务器名
	ServerName string
	// Time 时间戳
	Time int32
	// PassCode 验签串
	PassCode string
	// Group 分组
	Group int32
}

// BuildSectLoginInfoPacket 构造查询仙盟城镇登录凭据封包 (0x023F0001)。
func BuildSectLoginInfoPacket(group int32) *Packet {
	w := NewWriter()
	w.WriteInt32(group)
	return NewPacket(0x023F0001, w.Bytes())
}

// BuildSectLoginPacket 构造仙盟城镇登录封包 (0x023F0002)。
func BuildSectLoginPacket(info SectLoginInfo, playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(info.AppID)
	w.WriteInt32(playerID)
	w.WriteString(info.ServerName)
	w.WriteString(info.Node)
	w.WriteInt32(info.Time)
	w.WriteString(info.PassCode)
	return NewPacket(0x023F0002, w.Bytes())
}

// BuildSectEnterTownPacket 构造进入仙盟城镇封包 (0x023F0003)。
func BuildSectEnterTownPacket(townID, sectID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(townID)
	w.WriteInt32(sectID)
	return NewPacket(0x023F0003, w.Bytes())
}

// ParseSectLoginInfoResult 解析仙盟城镇登录凭据。
func ParseSectLoginInfoResult(payload []byte) (*SectLoginInfo, error) {
	r := NewReader(payload)
	res := &SectLoginInfo{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login result: %v", ErrInvalidPayload, err)
	}
	if res.Host, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login host: %v", ErrInvalidPayload, err)
	}
	portStr, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login port: %v", ErrInvalidPayload, err)
	}
	if res.Port, err = parseInt32(portStr); err != nil {
		return nil, fmt.Errorf("%w: invalid sect login port %q", ErrInvalidPayload, portStr)
	}
	if res.AppID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login app_id: %v", ErrInvalidPayload, err)
	}
	if res.Node, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login node: %v", ErrInvalidPayload, err)
	}
	if res.ServerName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login server_name: %v", ErrInvalidPayload, err)
	}
	if res.Time, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login time: %v", ErrInvalidPayload, err)
	}
	if res.PassCode, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login pass_code: %v", ErrInvalidPayload, err)
	}
	if res.Group, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect login group: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseSectActionResult 解析仙盟城镇动作结果 (首字节)。
func ParseSectActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty sect action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

func parseInt32(s string) (int32, error) {
	var v int32
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0, err
	}
	return v, nil
}

// BuildSectLoginInfoResultPacket 构造仙盟城镇凭据回包，供离线 mock 与单测使用。
func BuildSectLoginInfoResultPacket(info SectLoginInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Result)
	w.WriteString(info.Host)
	w.WriteString(fmt.Sprintf("%d", info.Port))
	w.WriteInt32(info.AppID)
	w.WriteString(info.Node)
	w.WriteString(info.ServerName)
	w.WriteInt32(info.Time)
	w.WriteString(info.PassCode)
	w.WriteInt32(info.Group)
	return NewPacket(0x023F0001, w.Bytes())
}

// BuildSectMainInfoPacket 构造仙盟主页信息封包 (Module 572, Action 1 -> 0x023C0001)。
func BuildSectMainInfoPacket(sectID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(sectID)
	return NewPacket(0x023C0001, w.Bytes())
}

// BuildSectBonusPanelPacket 构造仙盟福利面板封包 (Module 591, Action 1 -> 0x024F0001)。
func BuildSectBonusPanelPacket(sectID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(sectID)
	return NewPacket(0x024F0001, w.Bytes())
}

// BuildSectWorshipPacket 构造仙盟膜拜封包 (Module 572, Action 13 -> 0x023C000D)。
func BuildSectWorshipPacket(sectID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(sectID)
	return NewPacket(0x023C000D, w.Bytes())
}

// SectMainInfo 表示仙盟主页关键字段。
type SectMainInfo struct {
	// Result 结果码
	Result uint8
	// SelfSectID 所属宗门 ID
	SelfSectID int32
	// SectName 宗门名称
	SectName string
	// SectLevel 宗门等级
	SectLevel int32
	// AllianceID 所属圣盟 ID
	AllianceID int32
	// SectScene 宗门城镇场景 ID
	SectScene int32
}

// ParseSectMainInfoResult 解析仙盟主页信息，跳过成员展示列表。
func ParseSectMainInfoResult(payload []byte) (*SectMainInfo, error) {
	r := NewReader(payload)
	res := &SectMainInfo{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect main result: %v", ErrInvalidPayload, err)
	}
	if res.SelfSectID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read self_sect_id: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // self_job_id
		return nil, fmt.Errorf("%w: failed to read self_job_id: %v", ErrInvalidPayload, err)
	}
	if res.SectName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect_name: %v", ErrInvalidPayload, err)
	}
	if res.SectLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect_level: %v", ErrInvalidPayload, err)
	}
	// sect_exp / sect_salary / sect_pos_id
	for i := 0; i < 3; i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect scalar[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	// sect_stunt / sect_purpose
	if _, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect_stunt: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect_purpose: %v", ErrInvalidPayload, err)
	}
	// sect_create_ts / leader_app_id
	for i := 0; i < 2; i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect scalar2[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	// leader_name / creater_name
	if _, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read leader_name: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read creater_name: %v", ErrInvalidPayload, err)
	}
	// member1_num / member1_limit / member2_num / member2_limit / god_rank_top / pk_ranking_top
	for i := 0; i < 6; i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect scalar3[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sect top player count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read top player[%d].id: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 2; j++ {
			if _, err = r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: failed to read top player[%d].str[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
		for j := 0; j < 3; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read top player[%d].int[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
		if _, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: failed to read top player[%d].long: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 3; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read top player[%d].tail[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
	}
	if res.AllianceID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read alliance_id: %v", ErrInvalidPayload, err)
	}
	if res.SectScene, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect_scene: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// SectBonusGift 表示仙盟福利中的一档礼包。
type SectBonusGift struct {
	// GiftID 礼包编号
	GiftID int32
	// LeftNum 剩余可领数量
	LeftNum int32
}

// SectBonusPanel 表示仙盟福利面板。
//
// 线格式: UByte result, [Int gift_id, Int left_num], Int left_get_times,
// Int today_self_cont, Int sect_cont_limit, Int sect_cont。
type SectBonusPanel struct {
	// Result 结果码
	Result uint8
	// Gifts 礼包列表
	Gifts []SectBonusGift
	// LeftGetTimes 剩余领取次数
	LeftGetTimes int32
	// TodaySelfCont 今日个人贡献
	TodaySelfCont int32
	// SectContLimit 宗门贡献上限
	SectContLimit int32
	// SectCont 宗门当前贡献
	SectCont int32
}

// BuildSectBonusGetGiftPacket 构造领取仙盟福利封包 (Module 591, Action 3 -> 0x024F0003)。
func BuildSectBonusGetGiftPacket(giftID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(giftID)
	return NewPacket(0x024F0003, w.Bytes())
}

// ParseSectBonusPanelResult 解析仙盟福利面板。
func ParseSectBonusPanelResult(payload []byte) (*SectBonusPanel, error) {
	r := NewReader(payload)
	res := &SectBonusPanel{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect bonus result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sect bonus gift count: %v", ErrInvalidPayload, err)
	}
	res.Gifts = make([]SectBonusGift, 0, count)
	for i := 0; i < int(count); i++ {
		var g SectBonusGift
		if g.GiftID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect bonus gift[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if g.LeftNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect bonus gift[%d].left_num: %v", ErrInvalidPayload, i, err)
		}
		res.Gifts = append(res.Gifts, g)
	}
	if res.LeftGetTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect bonus left_get_times: %v", ErrInvalidPayload, err)
	}
	if res.TodaySelfCont, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect bonus today_self_cont: %v", ErrInvalidPayload, err)
	}
	if res.SectContLimit, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect bonus sect_cont_limit: %v", ErrInvalidPayload, err)
	}
	if res.SectCont, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sect bonus sect_cont: %v", ErrInvalidPayload, err)
	}
	return res, nil
}
