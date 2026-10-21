// Package platform 提供多运营平台 Web 自动化认证、凭据换取与区服标识归一化驱动层。
//
// @author Ateng
// @since 2026-10-08
package platform

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MainServerTicket 代表本服主会话认证凭据。
type MainServerTicket struct {
	// Code 游戏进入授权码
	Code string `json:"code"`
	// Time 本服登录时间戳
	Time int32 `json:"time"`
	// Hash 本服登录校验散列
	Hash string `json:"hash"`
}

// CrossServerTicket 代表跨服/仙界/圣域认证凭据。
type CrossServerTicket struct {
	// Time1 跨服登录时间戳 (对应 Mod_StLogin_Base)
	Time1 int32 `json:"time1"`
	// Hash1 跨服登录校验散列 (对应 Mod_StLogin_Base)
	Hash1 string `json:"hash1"`
}

// Ticket 代表双轨复合认证凭据模型 (Dual-Track Ticket)。
type Ticket struct {
	// Platform 平台标识符 (如 "fengwanyx")
	Platform string `json:"platform"`
	// ServerID 归一化后的协议区服标识 (如 "fengwanyx_s813")
	ServerID string `json:"server_id"`
	// RawServerID 原始输入的区服标识 (如 "s813")
	RawServerID string `json:"raw_server_id"`
	// RoleName 角色名称 (如 "梦一场")
	RoleName string `json:"role_name"`
	// GatewayURL 游戏网关根地址 (如 "http://s813.sxd.fengwanyx.3fangyuan.com/")
	GatewayURL string `json:"gateway_url"`
	// MainServer 本服主网关票据
	MainServer MainServerTicket `json:"main_server"`
	// CrossServer 跨服网关票据
	CrossServer CrossServerTicket `json:"cross_server"`
	// CreatedAt 票据获取时间
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt 票据预期过期时间
	ExpiresAt time.Time `json:"expires_at"`
	// Extra 存放平台特定的扩展入场参数 (例如官方 10 字段所需: regdate, id_card, open_time, is_newst, source 等)
	Extra map[string]string `json:"extra,omitempty"`
}

// IsExpired 判断当前票据是否已过期或即将过期 (预留 5 分钟缓冲)。
func (t *Ticket) IsExpired() bool {
	if t == nil || t.ExpiresAt.IsZero() {
		return true
	}
	return time.Now().Add(5 * time.Minute).After(t.ExpiresAt)
}

// PlatformAuthenticator 定义各游戏运营平台登录认证与票据换取的标准接口。
type PlatformAuthenticator interface {
	// Platform 返回当前认证器对应的平台唯一标识
	Platform() string
	// Login 执行平台模拟登录并提取对应区服的双轨认证票据
	Login(ctx context.Context, username, password, serverID string) (*Ticket, error)
}

// NormalizeServerID 执行区服标识归一化处理。
// 若输入 "s813" 或 "813" 且平台为 "fengwanyx"，将自动补齐为 "fengwanyx_s813"；
// 若输入 "S0" 或 "0" 且平台为 "xindong"，归一化为 "s0"；
// 若输入已包含平台前缀，则原样保留。
func NormalizeServerID(platformName, serverID string) string {
	s := strings.TrimSpace(serverID)
	if s == "" {
		return ""
	}

	p := strings.ToLower(strings.TrimSpace(platformName))
	switch p {
	case "fengwan", "fengwanyx":
		prefix := "fengwanyx_"
		if strings.HasPrefix(s, prefix) {
			return s
		}
		// 去除首部的 s
		serverNum := strings.TrimPrefix(s, "s")
		serverNum = strings.TrimPrefix(serverNum, "S")
		return fmt.Sprintf("fengwanyx_s%s", serverNum)
	case "xindong", "xd":
		serverNum := strings.TrimPrefix(s, "s")
		serverNum = strings.TrimPrefix(serverNum, "S")
		return fmt.Sprintf("s%s", serverNum)
	default:
		// 默认直接返回原始标识
		return s
	}
}
