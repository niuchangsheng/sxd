// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

var (
	// ErrPlayerLoginPayloadTooShort 登录回包载荷长度不足
	ErrPlayerLoginPayloadTooShort = errors.New("protocol: player login payload too short")
)

// 官方 Mod_Player_Base.login 回包结果码 (取自 AS3 Mod_Player_Base 常量表)。
const (
	// PlayerLoginResultSucceed 登录成功
	PlayerLoginResultSucceed uint8 = 0
	// PlayerLoginResultFailed 通用失败
	PlayerLoginResultFailed uint8 = 1
	// PlayerLoginResultFirstTime 服务端要求客户端选择/创建角色 (无角色或需选中角色)
	PlayerLoginResultFirstTime uint8 = 2
	// PlayerLoginResultKidLoginControl 防沉迷登录管控
	PlayerLoginResultKidLoginControl uint8 = 4
	// PlayerLoginResultDisableLogin 账号被禁止登录 (回包带 enableTime)
	PlayerLoginResultDisableLogin uint8 = 6
	// PlayerLoginResultIPLoginLimit IP 登录受限
	PlayerLoginResultIPLoginLimit uint8 = 7
)

// PlayerLoginResultText 返回登录结果码的可读文本。
func PlayerLoginResultText(code uint8) string {
	switch code {
	case PlayerLoginResultSucceed:
		return "成功"
	case PlayerLoginResultFailed:
		return "失败"
	case PlayerLoginResultFirstTime:
		return "需要选择/创建角色"
	case PlayerLoginResultKidLoginControl:
		return "防沉迷登录管控"
	case PlayerLoginResultDisableLogin:
		return "账号被禁止登录"
	case PlayerLoginResultIPLoginLimit:
		return "IP 登录受限"
	default:
		return "未知结果码"
	}
}

// PlayerLoginRequest 代表角色主服登录请求协议模型 (Mod_Player_Base.login, ActionID=0x00000000)。
type PlayerLoginRequest struct {
	// Username 平台登录账号
	Username string
	// Hash 平台登录动态签名散列
	Hash string
	// Time 平台登录时间戳 (字符串形式)
	Time string
	// Source 渠道来源标识 (默认 "sxd_baidu_pinpai_bt")
	Source string
	// Platform 平台显示名称 (如 "疯玩" / "心动")
	Platform string
	// ClientType 客户端接入类型 (默认 "web")
	ClientType string

	// 官方 10/12 字段入场参数 (Mod_Player_Base.login 0x00000000)
	RegDate      int32
	IDCard       string
	OpenTime     int32
	IsNewst      int8
	Stage        string
	Client       string
	Flag         int8
	TailUsername string
	IsOfficial   bool
}

// PlayerLoginResult 代表角色主服登录成功后服务端返回的核心角色属性快照。
type PlayerLoginResult struct {
	// Result 状态结果码 (0=成功)
	Result int16
	// RoleID 角色在当前区服的主键序号
	RoleID int16
	// RoleName 角色名 (UTF-8)
	RoleName string
	// Level 角色当前等级
	Level int32
	// Ingots 当前元宝数量
	Ingots int32
	// Coins 当前铜钱数量
	Coins int64
	// Health 当前生命值
	Health int64
	// MaxHealth 最大生命值
	MaxHealth int64
	// Stamina 当前基础体力点数 (基准上限 300)
	Stamina int32
	// Experience 当前角色经验
	Experience int64
	// MaxExperience 角色当前等级升级所需总经验 (满级为 uint64 最大值或 0)
	MaxExperience int64
	// TownMapID 角色当前所在城镇地图编号 (Mod_Town_Base.enter_town 使用的 townMapId)
	TownMapID int32
	// ExtraStamina 当前存储/额外/赠送体力点数 (Offset 116)
	ExtraStamina int32
	// MaxStamina 最大基础体力上限
	MaxStamina int32
	// VIP 角色 VIP 等级
	VIP int32

	// HasCoins/HasIngots/HasStamina/HasExtraStamina 表示对应字段在回包中确实存在。
	// 服务端体力/货币可以合法为 0，不能用 >0 作为“是否解析成功”的判据，
	// 否则会保留客户端初值 (体力 201 / 铜钱 36 亿) 造成幽灵资源。
	HasCoins        bool
	HasIngots       bool
	HasStamina      bool
	HasExtraStamina bool
}

// BuildPlayerLoginPacket 构造角色主服登录请求二进制协议封包 (ActionID 0x00000000)。
//
// @param req 主服登录请求参数模型
// @return 编码好的封包实例或错误
func BuildPlayerLoginPacket(req PlayerLoginRequest) (*Packet, error) {
	if req.Username == "" {
		return nil, errors.New("protocol: username cannot be empty for player login")
	}

	// 若显式指定为官方协议格式，或携带官方特有字段 (如 IDCard / RegDate / OpenTime)
	if req.IsOfficial || req.IDCard != "" || req.RegDate != 0 || req.OpenTime != 0 {
		w := NewWriter()
		w.WriteString(req.Username)
		w.WriteString(req.Hash)
		w.WriteString(req.Time)
		w.WriteString(req.Source)
		w.WriteInt32(req.RegDate)
		w.WriteString(req.IDCard)
		w.WriteInt32(req.OpenTime)
		w.WriteInt8(req.IsNewst)
		w.WriteString(req.Stage)
		w.WriteString(req.Client)
		if req.Client == "pc_wd" || req.Flag != 0 || req.TailUsername != "" {
			flag := req.Flag
			if flag == 0 {
				flag = 1
			}
			w.WriteInt8(flag)
			tailUser := req.TailUsername
			if tailUser == "" {
				tailUser = strings.Split(req.Username, ".")[0]
			}
			w.WriteString(tailUser)
		}
		return NewPacket(ActionIDPlayerLogin, w.Bytes()), nil
	}

	// 默认向下兼容 Golden Case 1 封包规范
	if req.Source == "" {
		req.Source = "sxd_baidu_pinpai_bt"
	}
	if req.Platform == "" || req.Platform == "fengwan" {
		req.Platform = "疯玩"
	}
	if req.ClientType == "" {
		req.ClientType = "web"
	}

	w := NewWriter()
	// 1. 账号用户名 (2 字节长度前缀 + 字符串)
	w.WriteString(req.Username)
	// 2. 动态验签哈希
	w.WriteString(req.Hash)
	// 3. 动态验签时间戳
	w.WriteString(req.Time)
	// 4. 渠道标识
	w.WriteString(req.Source)
	// 5. 空占位字符串 * 3
	w.WriteString("")
	w.WriteString("")
	w.WriteString("")
	// 6. 客户端固定参数
	w.WriteInt16(0x62B4)
	w.WriteInt16(0x0160)
	w.WriteInt8(0)
	// 7. 平台名称
	w.WriteString(req.Platform)
	// 8. 接入协议类型
	w.WriteString(req.ClientType)
	// 9. 标志位与尾部账号
	w.WriteInt8(1)
	w.WriteString(req.Username)

	return NewPacket(ActionIDPlayerLogin, w.Bytes()), nil
}

// ParsePlayerLoginResult 解析主服登录响应载荷并提取核心角色状态数据。
// 若载荷首字节命中 zlib 魔数 (0x78)，将透明进行流式解压后再做结构反序列化。
//
// @param payload 原始封包载荷切片
// @return 解析出的角色核心数据模型或错误
func ParsePlayerLoginResult(payload []byte) (*PlayerLoginResult, error) {
	if len(payload) == 0 {
		return nil, ErrPlayerLoginPayloadTooShort
	}

	body := payload
	if body[0] == ZlibMagicByte {
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("protocol: failed to decompress player login response: %w", err)
		}
		defer r.Close()

		decompressed, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("protocol: failed to read decompressed player login response: %w", err)
		}
		body = decompressed
	}

	if len(body) < 40 {
		return nil, ErrPlayerLoginPayloadTooShort
	}

	res := &PlayerLoginResult{
		MaxStamina: 300,
	}

	var nameStart int
	var nameLen int
	if nLen, ok := isLikelyName(body, 0); ok {
		nameStart = 2
		nameLen = nLen
		res.RoleName = string(body[nameStart : nameStart+nameLen])
	} else if nLen, ok := isLikelyName(body, 4); ok {
		res.Result = int16(binary.BigEndian.Uint16(body[0:2]))
		res.RoleID = int16(binary.BigEndian.Uint16(body[2:4]))
		nameStart = 6
		nameLen = nLen
		res.RoleName = string(body[nameStart : nameStart+nameLen])
	} else if nLen, ok := isLikelyName(body, 2); ok {
		res.RoleID = int16(binary.BigEndian.Uint16(body[0:2]))
		nameStart = 4
		nameLen = nLen
		res.RoleName = string(body[nameStart : nameStart+nameLen])
	} else {
		return nil, ErrPlayerLoginPayloadTooShort
	}

	pos := nameStart + nameLen
	if pos+16 <= len(body) {
		res.Level = int32(binary.BigEndian.Uint32(body[pos : pos+4]))
		if res.Level > 0 {
			res.MaxStamina = res.Level
		}
		res.Ingots = int32(binary.BigEndian.Uint32(body[pos+4 : pos+8]))
		res.Coins = int64(binary.BigEndian.Uint64(body[pos+8 : pos+16]))
		res.HasIngots = true
		res.HasCoins = true
	}

	if pos+32 <= len(body) {
		res.Health = int64(binary.BigEndian.Uint64(body[pos+16 : pos+24]))
		res.MaxHealth = int64(binary.BigEndian.Uint64(body[pos+24 : pos+32]))
	}

	if pos+36 <= len(body) {
		res.Stamina = int32(binary.BigEndian.Uint32(body[pos+32 : pos+36]))
		res.HasStamina = true
	}

	if pos+52 <= len(body) {
		res.Experience = int64(binary.BigEndian.Uint64(body[pos+36 : pos+44]))
		res.MaxExperience = int64(binary.BigEndian.Uint64(body[pos+44 : pos+52]))
	}

	// 官方 get_player_info 字段顺序 (Mod_Player_Base, Action 2):
	// level, ingot, coins, health, maxHealth, power, experience, maxExperience,
	// townMapId, bossMapId, jihuisuoId, mounts, mountsCD, vipLevel, ...
	if pos+56 <= len(body) {
		res.TownMapID = int32(binary.BigEndian.Uint32(body[pos+52 : pos+56]))
	}

	if pos+76 <= len(body) {
		res.VIP = int32(binary.BigEndian.Uint32(body[pos+72 : pos+76]))
	}

	if pos+109 <= len(body) {
		res.ExtraStamina = int32(binary.BigEndian.Uint32(body[pos+105 : pos+109]))
		res.HasExtraStamina = true
	}

	return res, nil
}

// isLikelyName 校验指定偏移处是否为合法的 UTF-8 角色名称长度与字节内容。
func isLikelyName(b []byte, offset int) (int, bool) {
	if len(b) < offset+2 {
		return 0, false
	}
	nLen := int(binary.BigEndian.Uint16(b[offset : offset+2]))
	if nLen <= 0 || nLen > 30 || offset+2+nLen > len(b) {
		return 0, false
	}
	nameBytes := b[offset+2 : offset+2+nLen]
	if !utf8.Valid(nameBytes) {
		return 0, false
	}
	for _, ch := range string(nameBytes) {
		if ch < 32 {
			return 0, false
		}
	}
	return nLen, true
}

// PlayerLoginAuthResponse 代表角色主服网关认证握手响应 (Module 0, Action 0)。
//
// 官方 AS3 的 login 回包 schema 为
// [UByte result, Int playerID, UByte _, Int enableTime, Byte enableType,
//
//	Int allActivity, Byte isOpenMusic, UByte warEffectType, Int uiSetValue, Byte isNewUI]，
//
// 共 22 字节；结果码 0=SUCCEED、1=FAILED、2=FIRST_TIME(要求选择/创建角色)、
// 4=KID_LOGIN_CONTROL、6=DISABLE_LOGIN、7=IP_LOGIN_LIMIT 等。
// Golden Case 2（疯玩网关）是另一套布局：前 4 字节 Reserved=0，第 5 字节为结果码，
// 该格式下 4 代表登录成功。
type PlayerLoginAuthResponse struct {
	Reserved   uint32
	ResultCode uint8
	PlayerID   int32
	// GoldenLayout 为 true 表示命中 Golden Case 2（4 字节保留 + 结果码）布局。
	GoldenLayout bool
	// EnableTime/EnableType 仅在 DISABLE_LOGIN 等封禁类结果码下有意义。
	EnableTime int32
	EnableType int8
	// UISetValue 与 IsNewUI 为官方回包附带的前端配置。
	UISetValue int32
	IsNewUI    uint8
	RawPayload []byte
}

// ParsePlayerLoginAuthResponse 解析角色网关登录认证握手响应。
// 兼容官方协议 (首字节为 ResultCode，紧跟 4B PlayerID) 与 Golden Case 2
// (前导 4 字节保留，第 5 字节为 ResultCode，4 表示成功)。
//
// @param payload 原始封包载荷切片
// @return 登录认证响应模型或错误
func ParsePlayerLoginAuthResponse(payload []byte) (*PlayerLoginAuthResponse, error) {
	if len(payload) < 1 {
		return nil, ErrPlayerLoginPayloadTooShort
	}

	// 1. 优先校验 Golden Case 2 抓包规范: 前导 4 字节 Reserved=0，第 5 字节 ResultCode 为 1~4
	if len(payload) >= 5 {
		r := NewReader(payload)
		reserved, _ := r.ReadUint32()
		code, _ := r.ReadUint8()
		if reserved == 0 && (code >= 1 && code <= 4) {
			return &PlayerLoginAuthResponse{
				Reserved:     reserved,
				ResultCode:   code,
				GoldenLayout: true,
				RawPayload:   payload,
			}, nil
		}
	}

	// 2. 官方协议格式: 首字节为 ResultCode，随后紧跟 4 字节 PlayerID。
	// 注意不能只在 ResultCode==0 时解析：失败码 2/6/7 同样带完整 22 字节结构，
	// 只有解析出 enableTime/enableType 才能给用户明确原因。
	res := &PlayerLoginAuthResponse{
		ResultCode: payload[0],
		RawPayload: payload,
	}
	if len(payload) >= 5 {
		r := NewReader(payload[1:])
		pid, _ := r.ReadInt32()
		res.PlayerID = pid
	}
	if len(payload) >= 22 {
		// [UByte result][Int pid][UByte x][Int enableTime][Byte enableType]
		// [Int allActivity][Byte openMusic][UByte warEffect][Int uiSet][Byte newUI]
		r := NewReader(payload[5:])
		_, _ = r.ReadUint8() // 保留字段
		enableTime, _ := r.ReadInt32()
		enableType, _ := r.ReadInt8()
		_, _ = r.ReadInt32() // allActivity
		_, _ = r.ReadUint8() // isOpenMusic
		_, _ = r.ReadUint8() // warEffectType
		uiSet, _ := r.ReadInt32()
		newUI, _ := r.ReadUint8()
		res.EnableTime = enableTime
		res.EnableType = enableType
		res.UISetValue = uiSet
		res.IsNewUI = newUI
	}
	return res, nil
}

// BuildPlayerInitStep1Packet 构造角色场景初始化握手步 1 封包 (ActionID 0x00000048)。
// 对应 Golden Case 3: 00 00 00 08 00 00 00 48 00 00 00 00
//
// @param param 初始化参数 (通常为 0)
// @return 编码好的封包实例
func BuildPlayerInitStep1Packet(param int32) *Packet {
	w := NewWriter()
	w.WriteInt32(param)
	return NewPacket(ActionIDPlayerInitStep1, w.Bytes())
}

// PlayerInitStep1Result 角色场景初始化响应模型。
type PlayerInitStep1Result struct {
	TownID   int32
	TownLine int16
	SceneID  int32
	TargetID int32
}

// ParsePlayerInitStep1Result 解析场景初始化步 1 响应。
// 对应 Golden Case 3: 00 00 00 12 00 00 00 48 00 00 13 0f 00 02 00 00 13 0f 00 00 13 12
//
// @param payload 原始封包载荷切片
// @return 场景初始化响应结果或错误
func ParsePlayerInitStep1Result(payload []byte) (*PlayerInitStep1Result, error) {
	if len(payload) < 14 {
		return nil, ErrPayloadTruncated
	}

	r := NewReader(payload)
	townID, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	townLine, err := r.ReadInt16()
	if err != nil {
		return nil, err
	}
	sceneID, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	targetID, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	return &PlayerInitStep1Result{
		TownID:   townID,
		TownLine: townLine,
		SceneID:  sceneID,
		TargetID: targetID,
	}, nil
}

// BuildPlayerInitStep2Packet 构造角色场景初始化握手步 2 封包 (ActionID 0x00000063)。
//
// @param prevActionID 前序动作号 (通常为 0x0048)
// @return 编码好的封包实例
func BuildPlayerInitStep2Packet(prevActionID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(prevActionID)
	return NewPacket(ActionIDPlayerInitStep2, w.Bytes())
}

// BuildPlayerInitStep3Packet 构造角色扩展模块初始化握手步 3 封包 (ActionID 0x00A50000)。
//
// @param prevActionID 前序动作号 (通常为 0x0063)
// @return 编码好的封包实例
func BuildPlayerInitStep3Packet(prevActionID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(prevActionID)
	return NewPacket(ActionIDPlayerInitStep3, w.Bytes())
}

// BuildPlayerGetInfoPacket 构造拉取全量角色资产快照封包 (ActionID 0x00000002)。
//
// @param prevActionID 前序动作号 (通常为 0x00A50000)
// @return 编码好的封包实例
func BuildPlayerGetInfoPacket(prevActionID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(prevActionID)
	return NewPacket(ActionIDPlayerGetInfo, w.Bytes())
}

// BuildPlayerHeartbeatPacket 构造长连接心跳保活封包 (Module 0, Action 75 -> 0x0000004B)。
// 官方 AS3 协议定义 request=[] / response=[Byte]，心跳请求不带任何载荷。
// 参数 seq 仅为兼容旧调用方保留，不写入封包。
//
// @param seq 旧版心跳序列号参数 (已废弃，忽略)
// @return 编码好的封包实例
func BuildPlayerHeartbeatPacket(_ int32) *Packet {
	return NewPacket(ActionIDPlayerHeartbeat, nil)
}

// BuildPlayerServerTimePacket 构造同步服务器系统时间封包 (Module 0, Action 22 -> 0x00000016)。
//
// @param param 参数
// @return 编码好的封包实例
func BuildPlayerServerTimePacket(param int32) *Packet {
	w := NewWriter()
	w.WriteInt32(param)
	return NewPacket(ActionIDPlayerServerTime, w.Bytes())
}
