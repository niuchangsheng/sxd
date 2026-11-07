//go:build probe

// 目标: 侦察九空无界 (Mod_NineRegions_Base, Module 58) 面板与挑战前置状态。
//
//	open_panel             58/0  response=[Byte is_first, Int cur_jie, Int cur_level, Int hidden_level]
//	get_player_cd_time     58/21 response=[Short cd_seconds]
//	check_is_can_challenge 58/20 request=[Int jie, Int level] response=[Byte result]
//	get_player_team_info   58/4  response=[Int leader_id, [[Int player_id, String nick, String sign, Short, Int, Int, Int, String]]]
//
// 本探针只读，不发起 fight_region / challenge_hidden_level。
//
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_nine_regions_live.go
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const (
	actionNinePanel     uint32 = 0x003A0000
	actionNineTeamInfo  uint32 = 0x003A0004
	actionNineCheck     uint32 = 0x003A0014
	actionNineCD        uint32 = 0x003A0015
	actionNineCheckAlt  uint32 = 0x003A0014
	actionNineOpenAlt   uint32 = 0x003A0000
	actionNineTeamReset uint32 = 0x003A0004
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s1.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	roleCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)
	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	// 1. open_panel：先空载荷，失败再带 PrevAct。
	openResp := call(protocol.NewPacket(actionNineOpenAlt, nil), actionNineOpenAlt, 6*time.Second)
	if openResp == nil {
		pkt := protocol.NewPacket(actionNineOpenAlt, []byte{0, 0, 0, 0})
		pkt.HasPrevAct = true
		openResp = call(pkt, actionNineOpenAlt, 6*time.Second)
	}
	curJie, curLevel, hiddenLevel := int32(-1), int32(-1), int32(-1)
	if openResp != nil {
		slog.Info("九空无界面板", "len", len(openResp.Payload), "hex", hex.EncodeToString(openResp.Payload))
		r := protocol.NewReader(openResp.Payload)
		isFirst, e0 := r.ReadUint8()
		jie, e1 := r.ReadInt32()
		level, e2 := r.ReadInt32()
		hidden, e3 := r.ReadInt32()
		curJie, curLevel, hiddenLevel = jie, level, hidden
		_ = hiddenLevel
		slog.Info("九空无界面板字段", "is_first", isFirst,
			"cur_jie", jie, "cur_level", level, "hidden_level", hidden,
			"errs", []error{e0, e1, e2, e3})
	}

	// 2. CD 时间。
	if cdResp := call(protocol.NewPacket(actionNineCD, nil), actionNineCD, 6*time.Second); cdResp != nil {
		r := protocol.NewReader(cdResp.Payload)
		cd, cdErr := r.ReadInt16()
		slog.Info("九空无界CD", "cd_seconds", cd, "err", cdErr, "hex", hex.EncodeToString(cdResp.Payload))
	}

	// 3. check_is_can_challenge。
	if curJie >= 0 && curLevel >= 0 {
		w := protocol.NewWriter()
		w.WriteInt32(curJie)
		w.WriteInt32(curLevel)
		if resp := call(protocol.NewPacket(actionNineCheck, w.Bytes()), actionNineCheck, 6*time.Second); resp != nil {
			slog.Info("九空无界挑战资格", "jie", curJie, "level", curLevel,
				"result", resp.Payload[0], "hex", hex.EncodeToString(resp.Payload))
		}
	}

	// 4. 当前队伍。
	if teamResp := call(protocol.NewPacket(actionNineTeamInfo, nil), actionNineTeamInfo, 6*time.Second); teamResp != nil {
		slog.Info("九空无界队伍信息", "len", len(teamResp.Payload), "hex", hex.EncodeToString(teamResp.Payload))
	}
}
