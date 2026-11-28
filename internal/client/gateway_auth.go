// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件承载主服与跨服网关认证握手及四步角色资产初始化逻辑 (DefaultGatewayAuthenticator)。
package client

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// DefaultGatewayAuthenticator 角色默认主服网关认证握手、城镇场景与全量资产四步初始化实现。
// 严格遵循真实二进制交互规范：
// 0. Module 0, Action 0 (0x00000000) 登录握手并校验 ResultCode=4
// 1. Module 0, Action 72 (0x00000048) 城镇场景初始化同步 TownID
// 2. Module 0, Action 99 (0x00000063) 场景进阶状态同步 (携带前序 0x48)
// 3. Module 165, Action 0 (0x00A50000) 扩展业务模块初始化 (携带前序 0x63)
// 4. Module 0, Action 2 (0x00000002) 全量角色资产拉取 (携带前序 0x00A50000) 并更新真实资产快照
func DefaultGatewayAuthenticator(s *RoleSession) error {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()

	cfg := s.snapshotConfig()
	connCtx := s.currentConnCtx()

	// 0. 构造并发送登录握手请求包 (优先主服 0x00000000，或跨服 0x005E0000)
	var loginPkt *protocol.Packet
	var loginRespActionID uint32 = protocol.ActionIDPlayerLogin

	if cfg.Time1 != 0 && cfg.Hash1 != "" && cfg.Hash == "" {
		// 跨服网关凭据
		loginRespActionID = protocol.ActionIDStLogin
		originNickName := cfg.OriginNickName
		if originNickName == "" {
			originNickName = s.OriginNickName()
		}
		var err error
		loginPkt, err = protocol.BuildStLoginPacket(protocol.StLoginRequest{
			ServerName:     cfg.ServerID,
			PlayerID:       cfg.PlayerID,
			OriginNickName: originNickName,
			Time:           cfg.Time1,
			PassCode:       cfg.Hash1,
		})
		if err != nil {
			return fmt.Errorf("client: failed to build st login packet: %w", err)
		}
	} else {
		// 主服原生网关凭据
		timeStr := fmt.Sprintf("%d", cfg.Time)
		if cfg.Time == 0 {
			timeStr = fmt.Sprintf("%d", time.Now().Unix())
		}
		platform := cfg.Platform
		if platform == "" || platform == "fengwan" {
			platform = "疯玩"
		}
		hash := cfg.Hash
		if hash == "" {
			hash = cfg.Token
		}
		username := cfg.Username
		if username == "" {
			username = cfg.RoleID
		}

		loginReq := protocol.PlayerLoginRequest{
			Username:   username,
			Hash:       hash,
			Time:       timeStr,
			Source:     "",
			Platform:   platform,
			ClientType: "web",
		}

		if cfg.Extra != nil {
			if rd, ok := cfg.Extra["regdate"]; ok {
				if v, err := strconv.ParseInt(rd, 10, 32); err == nil {
					loginReq.RegDate = int32(v)
				}
			}
			if ot, ok := cfg.Extra["open_time"]; ok {
				if v, err := strconv.ParseInt(ot, 10, 32); err == nil {
					loginReq.OpenTime = int32(v)
				}
			}
			if in, ok := cfg.Extra["is_newst"]; ok {
				if v, err := strconv.ParseInt(in, 10, 8); err == nil {
					loginReq.IsNewst = int8(v)
				}
			}
			if stage, ok := cfg.Extra["stage"]; ok && stage != "" {
				loginReq.Stage = stage
			}
			if client, ok := cfg.Extra["client"]; ok && client != "" {
				loginReq.Client = client
			}
			if src, ok := cfg.Extra["source"]; ok {
				loginReq.Source = src
			}
			if idCard, ok := cfg.Extra["id_card"]; ok && idCard != "" {
				loginReq.IDCard = idCard
			}
			if cfg.Extra["non_kid"] == "1" {
				loginReq.IDCard = ""
			}
			if cfg.Extra["hash_code"] != "" {
				loginReq.IsOfficial = true
			}
		}

		if loginReq.Stage == "" && (cfg.Platform == "xindong" || cfg.Platform == "xd") {
			loginReq.Stage = "心动"
		}
		if loginReq.Client == "" {
			loginReq.Client = "web"
		}
		if loginReq.Client == "pc_wd" {
			loginReq.Flag = 1
			if loginReq.TailUsername == "" {
				loginReq.TailUsername = strings.Split(username, ".")[0]
			}
		}

		var err error
		loginPkt, err = protocol.BuildPlayerLoginPacket(loginReq)
		if err != nil {
			return fmt.Errorf("client: failed to build login packet: %w", err)
		}
	}

	// 注册握手回包通道并流水线发送首部封包
	respCh := make(chan *protocol.Packet, 1)
	s.handlerMu.Lock()
	if s.pendingCalls == nil {
		s.pendingCalls = make(map[uint32][]chan *protocol.Packet)
	}
	s.pendingCalls[loginRespActionID] = append(s.pendingCalls[loginRespActionID], respCh)
	s.handlerMu.Unlock()

	defer func() {
		s.handlerMu.Lock()
		calls := s.pendingCalls[loginRespActionID]
		for i, c := range calls {
			if c == respCh {
				s.pendingCalls[loginRespActionID] = append(calls[:i], calls[i+1:]...)
				break
			}
		}
		s.handlerMu.Unlock()
	}()

	if err := s.Send(loginPkt); err != nil {
		return fmt.Errorf("client: failed to send login packet: %w", err)
	}

	// 官方流水线对齐：官方微端在发送 M0:A0 登录请求后，立刻连发 Action 72 触发场景与分线初始化
	if loginRespActionID == protocol.ActionIDPlayerLogin {
		step1Pkt := protocol.BuildPlayerInitStep1Packet(0x00000000)
		_ = s.Send(step1Pkt)
	}

	var connDone <-chan struct{}
	if connCtx != nil {
		connDone = connCtx.Done()
	}

	var respPkt *protocol.Packet
	select {
	case <-ctx.Done():
		return fmt.Errorf("client: login handshake timeout or failed: %w", ctx.Err())
	case <-connDone:
		return fmt.Errorf("client: connection closed during login handshake: %w", ErrNotConnected)
	case p, ok := <-respCh:
		if !ok || p == nil {
			return fmt.Errorf("client: connection closed during login handshake: %w", ErrNotConnected)
		}
		respPkt = p
	}

	// 校验登录回包状态码
	if loginRespActionID == protocol.ActionIDPlayerLogin {
		authResp, err := protocol.ParsePlayerLoginAuthResponse(respPkt.Payload)
		if err == nil {
			// Golden Case 2（4 字节保留 + 结果码）布局下 4 表示成功；
			// 官方 22 字节布局下 0 表示成功。
			loginOK := (!authResp.GoldenLayout && authResp.ResultCode == protocol.PlayerLoginResultSucceed) ||
				(authResp.GoldenLayout && authResp.ResultCode == 4)
			if !loginOK {
				// 记录原始载荷方便区分「需要选角/账号封禁/风控拒绝」等不同失败码。
				head := hex.EncodeToString(respPkt.Payload)
				if len(head) > 64 {
					head = head[:64] + "..."
				}
				switch {
				case !authResp.GoldenLayout && authResp.ResultCode == protocol.PlayerLoginResultFirstTime:
					// FIRST_TIME(2)：服务端要求客户端进入「选择/创建角色」界面。
					// 自动化绝不能自动创建角色，只能等待用户在官方客户端选好角色。
					return fmt.Errorf("%w: resultCode=%d payload_len=%d payload_head=%s",
						ErrRoleSelectionRequired, authResp.ResultCode, len(respPkt.Payload), head)
				case !authResp.GoldenLayout && authResp.ResultCode == protocol.PlayerLoginResultDisableLogin:
					return fmt.Errorf("%w: resultCode=%d(%s) enable_time=%d enable_type=%d payload_head=%s",
						ErrAuthRejected, authResp.ResultCode, protocol.PlayerLoginResultText(authResp.ResultCode),
						authResp.EnableTime, authResp.EnableType, head)
				default:
					return fmt.Errorf("%w: resultCode=%d(%s) payload_len=%d payload_head=%s",
						ErrAuthRejected, authResp.ResultCode, protocol.PlayerLoginResultText(authResp.ResultCode),
						len(respPkt.Payload), head)
				}
			}
			// 成功时也记录一次原始载荷（每次登录一行），便于对比不同区服的
			// 响应格式与失败回包，定位网关/角色级问题。
			okHead := hex.EncodeToString(respPkt.Payload)
			if len(okHead) > 64 {
				okHead = okHead[:64] + "..."
			}
			slog.Info("登录认证通过",
				"role_id", s.cfg.RoleID,
				"result_code", authResp.ResultCode,
				"payload_len", len(respPkt.Payload),
				"payload_head", okHead,
			)
			s.markTicketConsumed()
			if authResp.PlayerID > 0 {
				s.UpdatePlayerState(func(ps *PlayerState) {
					ps.PlayerID = authResp.PlayerID
				})
			}
		} else {
			res, pErr := protocol.ParsePlayerLoginResult(respPkt.Payload)
			if pErr != nil {
				return fmt.Errorf("client: invalid login response payload: %w", err)
			}
			if res.Result != 0 {
				return fmt.Errorf("client: login failed with status code %d", res.Result)
			}
			s.UpdatePlayerState(func(ps *PlayerState) {
				if res.Level > 0 {
					ps.Level = int(res.Level)
				}
				if res.Stamina > 0 {
					ps.Stamina = int(res.Stamina)
				}
				if res.Coins > 0 {
					ps.Coins = res.Coins
				}
				if res.Ingots > 0 {
					ps.Ingots = int64(res.Ingots)
				}
			})
		}
	}

	// 1. Step 1: 自动执行 Action 72 城镇场景初始化同步
	step1Pkt := protocol.BuildPlayerInitStep1Packet(0x0048)
	step1Resp, err := s.Call(ctx, step1Pkt, protocol.ActionIDPlayerInitStep1)
	if err != nil {
		if errors.Is(err, ErrNotConnected) || errors.Is(err, ErrSessionClosed) || (connCtx != nil && connCtx.Err() != nil) {
			return fmt.Errorf("client: init step 1 failed (connection lost): %w", err)
		}
	} else {
		if initRes, pErr := protocol.ParsePlayerInitStep1Result(step1Resp.Payload); pErr == nil {
			s.UpdatePlayerState(func(ps *PlayerState) {
				ps.TownID = int(initRes.TownID)
			})
		}
	}

	// 2. Step 2: 场景进阶同步 (ActionID 0x00000063，携带前序动作 0x0048，单向通知包)
	step2Pkt := protocol.BuildPlayerInitStep2Packet(0x0048)
	if err := s.Send(step2Pkt); err != nil {
		return fmt.Errorf("client: init step 2 send failed: %w", err)
	}

	// 3. Step 3: 扩展模块初始化 (ActionID 0x00A50000，携带前序动作 0x0063，单向通知包)
	step3Pkt := protocol.BuildPlayerInitStep3Packet(0x0063)
	if err := s.Send(step3Pkt); err != nil {
		return fmt.Errorf("client: init step 3 send failed: %w", err)
	}

	// 4. Step 4: 全量角色资产快照获取 (ActionID 0x00000002，携带前序动作 0x00A50000)
	step4Pkt := protocol.BuildPlayerGetInfoPacket(0x00A50000)
	step4Resp, err := s.Call(ctx, step4Pkt, protocol.ActionIDPlayerGetInfo)
	if err != nil {
		if errors.Is(err, ErrNotConnected) || errors.Is(err, ErrSessionClosed) || (connCtx != nil && connCtx.Err() != nil) {
			return fmt.Errorf("client: init step 4 failed (connection lost): %w", err)
		}
	} else {
		infoRes, pErr := protocol.ParsePlayerLoginResult(step4Resp.Payload)
		if pErr == nil && infoRes != nil {
			s.UpdatePlayerState(func(ps *PlayerState) {
				if infoRes.RoleName != "" {
					ps.OriginNickName = infoRes.RoleName
				}
				if ps.PlayerID == 0 && infoRes.RoleID > 0 {
					ps.PlayerID = int32(infoRes.RoleID)
				}
				if infoRes.Level > 0 {
					ps.Level = int(infoRes.Level)
				}
				if infoRes.HasCoins {
					ps.Coins = infoRes.Coins
				}
				if infoRes.HasIngots {
					ps.Ingots = int64(infoRes.Ingots)
				}
				if infoRes.HasStamina {
					ps.Stamina = int(infoRes.Stamina)
				}
				if infoRes.HasExtraStamina {
					ps.ExtraStamina = int(infoRes.ExtraStamina)
				}
				if infoRes.MaxStamina > 0 {
					ps.MaxStamina = int(infoRes.MaxStamina)
				}
				if infoRes.VIP > 0 {
					ps.VIP = int(infoRes.VIP)
				}
				if infoRes.Experience > 0 {
					ps.Experience = infoRes.Experience
				}
				if infoRes.MaxExperience > 0 {
					ps.MaxExperience = infoRes.MaxExperience
				}
				if infoRes.TownMapID > 0 {
					ps.TownMapID = int(infoRes.TownMapID)
				}
			})
			remainingExp := int64(0)
			if infoRes.MaxExperience > infoRes.Experience {
				remainingExp = infoRes.MaxExperience - infoRes.Experience
			}
			slog.Info("网关握手与全量角色资产同步成功",
				"role_id", s.RoleID(),
				"role_name", infoRes.RoleName,
				"town_id", s.GetPlayerState().TownID,
				"level", infoRes.Level,
				"coins", infoRes.Coins,
				"ingots", infoRes.Ingots,
				"stamina", infoRes.Stamina,
				"exp", infoRes.Experience,
				"max_exp", infoRes.MaxExperience,
				"remaining_exp", remainingExp,
			)
		}
	}

	return nil
}
