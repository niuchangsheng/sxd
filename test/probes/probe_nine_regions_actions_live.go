//go:build probe

// 目标: 验证九空无界 (Mod_NineRegions_Base, Module 58) 的完整单人挑战链路。
//
//	open_panel         58/0
//	check_is_can_challenge 58/20 [jie, level]
//	start_challenge    58/1  [jie, level] -> [Byte first, UByte result]
//	create_region_team 58/5  [jie, level] -> [UByte result, Int team_id]
//	get_player_team_info 58/4
//	get_helper_role_list 58/9
//	fight_region       58/13 [jie, level] -> [UByte result, ...war...]
//
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_nine_regions_actions_live.go
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
	actOpen     uint32 = 0x003A0000
	actCheck    uint32 = 0x003A0014
	actStart    uint32 = 0x003A0001
	actCreate   uint32 = 0x003A0005
	actTeamInfo uint32 = 0x003A0004
	actHelpers  uint32 = 0x003A0009
	actChoose   uint32 = 0x003A000A
	actFight    uint32 = 0x003A000D
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
	ints2 := func(a, b int32) []byte {
		w := protocol.NewWriter()
		w.WriteInt32(a)
		w.WriteInt32(b)
		return w.Bytes()
	}

	// open_panel
	curJie, curLevel := int32(1), int32(0)
	if resp := call(protocol.NewPacket(actOpen, nil), actOpen, 6*time.Second); resp != nil {
		r := protocol.NewReader(resp.Payload)
		first, _ := r.ReadUint8()
		curJie, _ = r.ReadInt32()
		curLevel, _ = r.ReadInt32()
		hidden, _ := r.ReadInt32()
		slog.Info("九空面板", "first", first, "jie", curJie, "level", curLevel, "hidden", hidden)
	}

	// 面板 cur_level=0，但客户端 onWar 在 jie=0 时会改用 (1,1)；
	// 因此依次尝试 (curJie,1)、(curJie,curLevel)、(curJie,curLevel+1)。
	candidates := [][2]int32{{curJie, 1}, {curJie, curLevel}, {curJie, curLevel + 1}}
	var chosen [2]int32
	chosenOK := false
	for _, c := range candidates {
		jie, level := c[0], c[1]
		if level <= 0 {
			continue
		}
		if resp := call(protocol.NewPacket(actCheck, ints2(jie, level)), actCheck, 6*time.Second); resp != nil {
			slog.Info("挑战资格", "jie", jie, "level", level,
				"result", resp.Payload[0], "hex", hex.EncodeToString(resp.Payload))
		}
		resp := call(protocol.NewPacket(actStart, ints2(jie, level)), actStart, 6*time.Second)
		if resp == nil || len(resp.Payload) < 2 {
			continue
		}
		slog.Info("开始挑战", "jie", jie, "level", level, "hex", hex.EncodeToString(resp.Payload))
		if resp.Payload[1] == 0 { // CHALLENGE_SUCCESS
			chosen = c
			chosenOK = true
			break
		}
	}
	if !chosenOK {
		slog.Warn("没有可用的九空无界挑战坐标，结束")
		return
	}
	curJie, curLevel = chosen[0], chosen[1]

	// create_region_team
	if resp := call(protocol.NewPacket(actCreate, ints2(curJie, curLevel)), actCreate, 6*time.Second); resp != nil {
		slog.Info("创建队伍", "hex", hex.EncodeToString(resp.Payload))
	}

	// team info / helpers
	if resp := call(protocol.NewPacket(actTeamInfo, nil), actTeamInfo, 6*time.Second); resp != nil {
		slog.Info("队伍信息", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}
	var helperIDs []int32
	if resp := call(protocol.NewPacket(actHelpers, nil), actHelpers, 6*time.Second); resp != nil {
		slog.Info("助战列表", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		r := protocol.NewReader(resp.Payload)
		n, _ := r.ReadUint16()
		for i := 0; i < int(n); i++ {
			id, _ := r.ReadInt32()
			name, _ := r.ReadString()
			lv, _ := r.ReadInt32()
			job, _ := r.ReadInt32()
			state, _ := r.ReadUint8()
			helperIDs = append(helperIDs, id)
			slog.Info("助战角色", "role_id", id, "name", name, "lv", lv, "job", job, "state", state)
		}
	}

	// 尝试把助战角色放进队伍 (byte 参数按槽位 0/1 试探)。
	for slot, id := range helperIDs {
		if slot >= 2 {
			break
		}
		w := protocol.NewWriter()
		w.WriteInt32(id)
		w.WriteUint8(uint8(slot))
		if resp := call(protocol.NewPacket(actChoose, w.Bytes()), actChoose, 6*time.Second); resp != nil {
			slog.Info("选择助战", "role_id", id, "slot", slot, "hex", hex.EncodeToString(resp.Payload))
		}
	}
	if resp := call(protocol.NewPacket(actTeamInfo, nil), actTeamInfo, 6*time.Second); resp != nil {
		slog.Info("助战后队伍信息", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}

	// fight
	if resp := call(protocol.NewPacket(actFight, ints2(curJie, curLevel)), actFight, 20*time.Second); resp != nil {
		slog.Info("挑战结果", "len", len(resp.Payload), "result", resp.Payload[0],
			"hex_head", hex.EncodeToString(resp.Payload[:min(32, len(resp.Payload))]))
	}

	// 再次读取面板，观察 cur_level 是否推进。
	if resp := call(protocol.NewPacket(actOpen, nil), actOpen, 6*time.Second); resp != nil {
		r := protocol.NewReader(resp.Payload)
		first, _ := r.ReadUint8()
		jie, _ := r.ReadInt32()
		level, _ := r.ReadInt32()
		hidden, _ := r.ReadInt32()
		slog.Info("挑战后面板", "first", first, "jie", jie, "level", level, "hidden", hidden)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
