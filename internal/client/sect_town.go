// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件实现仙盟城镇独立会话：主服获取仙盟分组与城镇凭据后，
// 在独立连接上执行 Mod 575 登录与 enter_town，供仙盟/圣盟日常玩法使用。
package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// ErrNotInSect 当前角色未加入仙盟，无法进入仙盟城镇。
var ErrNotInSect = errors.New("client: 当前角色未加入仙盟")

// 仙盟城镇 ID (TownTypeData: ZongMen1=146, ZongMen2=148)。
const (
	// sectTownID 宗门地图 (ZongMen1)
	sectTownID int32 = 146
	// sectTownFullID 宗门地图完全体 (ZongMen2)
	sectTownFullID int32 = 148
)

// SectTownCredentials 表示仙盟城镇登录所需信息。
type SectTownCredentials struct {
	// SectID 跨服宗门 ID (如 10000007)
	SectID int32
	// LocalSectID 本服宗门 ID (地点列表中的 sect_id)
	LocalSectID int32
	// Group 仙盟 socket 分组
	Group int32
	// SectName 宗门名称
	SectName string
	// Login 城镇登录凭据
	Login protocol.SectLoginInfo
}

// FetchSectTownCredentials 通过主服会话获取仙盟城镇登录凭据。
//
// 主服只读查询使用独立短超时：角色未加入仙盟时服务端不会回包，
// 不能让该探测占用调用方为仙盟城镇会话预留的长超时预算。
func (s *RoleSession) FetchSectTownCredentials(ctx context.Context) (*SectTownCredentials, error) {
	pCtx, pCancel := context.WithTimeout(ctx, s.cfg.SectQueryTimeout)
	panelResp, err := s.Call(pCtx, protocol.BuildSectPanelInfoPacket(), protocol.ActionIDSectPanelInfo)
	pCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("client: 查询仙盟面板无回包（角色可能未加入仙盟或仙盟功能未开放）: %w", err)
		}
		return nil, fmt.Errorf("client: 查询仙盟面板失败: %w", err)
	}
	summary, err := protocol.ParseSectPanelSummary(panelResp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙盟面板失败: %w", err)
	}
	if summary.MySectID <= 0 || summary.MySectGroup <= 0 {
		return nil, ErrNotInSect
	}
	localSectID := summary.MySectID % 10000000
	for _, loc := range summary.Locations {
		if loc.SectID == localSectID || loc.SectID == summary.MySectID {
			if loc.SectID == localSectID {
				localSectID = loc.SectID
			}
			break
		}
	}
	iCtx, iCancel := context.WithTimeout(ctx, s.cfg.SectQueryTimeout)
	infoResp, err := s.Call(iCtx, protocol.BuildSectLoginInfoPacket(summary.MySectGroup), 0x023F0001)
	iCancel()
	if err != nil {
		return nil, fmt.Errorf("client: 查询仙盟城镇凭据失败: %w", err)
	}
	login, err := protocol.ParseSectLoginInfoResult(infoResp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙盟城镇凭据失败: %w", err)
	}
	if login.Result != 0 || login.Host == "" || login.Port <= 0 {
		return nil, fmt.Errorf("client: 仙盟城镇凭据不可用: result=%d host=%q port=%d", login.Result, login.Host, login.Port)
	}
	return &SectTownCredentials{
		SectID:      summary.MySectID,
		LocalSectID: localSectID,
		Group:       summary.MySectGroup,
		SectName:    summary.MySectName,
		Login:       *login,
	}, nil
}

// OpenSectTownSession 建立已进入仙盟城镇的独立会话。
// 调用方负责在使用结束后 Close。
func (s *RoleSession) OpenSectTownSession(ctx context.Context) (*RoleSession, error) {
	cred, err := s.FetchSectTownCredentials(ctx)
	if err != nil {
		return nil, err
	}
	return s.OpenSectTownSessionWithCredentials(ctx, cred)
}

// OpenSectTownSessionWithCredentials 使用已获取的凭据建立仙盟城镇会话。
func (s *RoleSession) OpenSectTownSessionWithCredentials(ctx context.Context, cred *SectTownCredentials) (*RoleSession, error) {
	if cred == nil {
		return nil, ErrNotInSect
	}
	cfg := s.cfg
	cfg.RoleID = s.cfg.RoleID + "-sect"
	cfg.ServerAddr = net.JoinHostPort(cred.Login.Host, strconv.Itoa(int(cred.Login.Port)))
	cfg.ServerID = cred.Login.ServerName
	cfg.Platform = s.cfg.Platform
	cfg.HeartbeatInterval = 5 * time.Second
	cfg.MaxReconnectAttempts = 1
	cfg.RefreshCredentials = nil

	playerID := s.PlayerID()
	loginInfo := cred.Login
	localSectID := cred.LocalSectID
	townID := sectTownID
	if cred.Login.ServerName == "" {
		townID = sectTownFullID
	}
	cfg.Authenticator = func(cs *RoleSession) error {
		aCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		loginResp, lErr := cs.Call(aCtx, protocol.BuildSectLoginPacket(loginInfo, playerID), 0x023F0002)
		if lErr != nil {
			return fmt.Errorf("client: 仙盟城镇登录失败: %w", lErr)
		}
		result, pErr := protocol.ParseSectActionResult(loginResp.Payload)
		if pErr != nil {
			return fmt.Errorf("client: 解析仙盟城镇登录回包失败: %w", pErr)
		}
		if result != 0 {
			return fmt.Errorf("client: 仙盟城镇登录被拒绝: result=%d", result)
		}

		enterResp, eErr := cs.Call(aCtx, protocol.BuildSectEnterTownPacket(townID, localSectID), 0x023F0003)
		if eErr != nil {
			return fmt.Errorf("client: 进入仙盟城镇失败: %w", eErr)
		}
		eResult, epErr := protocol.ParseSectActionResult(enterResp.Payload)
		if epErr != nil {
			return fmt.Errorf("client: 解析进入仙盟城镇回包失败: %w", epErr)
		}
		if eResult != 0 {
			return fmt.Errorf("client: 进入仙盟城镇被拒绝: result=%d", eResult)
		}
		return nil
	}

	slog.Info("正在建立仙盟城镇会话",
		"role_id", s.cfg.RoleID,
		"sect_role_id", cfg.RoleID,
		"addr", cfg.ServerAddr,
		"sect_id", cred.SectID,
		"local_sect_id", localSectID,
		"group", cred.Group,
		"sect_name", cred.SectName,
	)

	sess := NewRoleSession(cfg)
	if err := sess.Start(ctx); err != nil {
		return nil, fmt.Errorf("client: 启动仙盟城镇会话失败: %w", err)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch sess.State() {
		case StateActive:
			return sess, nil
		case StateClosed:
			sess.Close()
			return nil, errors.New("client: 仙盟城镇会话在激活前关闭")
		}
		select {
		case <-ctx.Done():
			sess.Close()
			return nil, fmt.Errorf("client: 等待仙盟城镇会话激活超时: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
