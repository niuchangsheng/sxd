//go:build probe

// 目标协议: [Module 57] Mod_BeelzebubTrials_Base create_team (0x00390004)
// 基准对齐: s0 测试服 create_team 恒定静默无回包 (context deadline exceeded) 根因定位；
//
//	同版本 s1 服相同载荷可正常建队，故重点排查服务端角色状态与载荷形态差异。
//
// 运行命令: go run -tags=probe test/probes/probe_beelzebub_create_diag.go -config configs/s0.yaml
// 观察点:
//  1. 建队前的队伍信息 / 可挑战列表 (角色是否已残留在某支队伍或战场中)
//  2. create_team 之后 5 秒内服务端是否下发任意 Mod 57 动作 (哪怕不是 Action 4 回包)
//  3. 队伍类型枚举 (0/2/3) 与载荷宽度 (uint8 / int32) 差异
//  4. 建队动作后 get_player_team_info 是否出现新队伍 (判定服务端是否已建队但回包动作错位)
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

type packetLog struct {
	action uint32
	length int
	hex    string
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfgPath := flag.String("config", "configs/s0.yaml", "配置文件路径")
	timeout := flag.Duration("timeout", 4*time.Second, "单次业务请求等待上限")
	bossFlag := flag.Int("boss", 0, "强制指定测试魔王编号 (0 表示自动选择最低等级魔王)")
	readsOnly := flag.Bool("reads", false, "仅执行只读状态采集，不发送任何建队请求")
	onlyPrefix := flag.String("only", "", "仅执行名称以该前缀开头的建队变体 (如 V0)")
	cleanup := flag.Bool("cleanup", false, "仅退出当前魔王试炼队伍后结束 (用于清理探针残留队伍)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
		RefreshCredentials: func(c context.Context, sCfg *client.SessionConfig) error {
			ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
			freshTicket, err := ticketCache.GetOrFetch(c, acc.Platform, acc.Username, acc.Password, role.ServerID)
			if err != nil {
				return err
			}
			client.ApplyTicket(sCfg, freshTicket)
			if freshTicket.GatewayURL != "" {
				sCfg.ServerAddr = freshTicket.GatewayURL
			}
			return nil
		},
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未在时限内激活", "state", sess.State())
		return
	}

	observed := make(chan packetLog, 128)
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		if pkt == nil || pkt.ActionID == protocol.ActionHeartbeat {
			return
		}
		select {
		case observed <- packetLog{action: pkt.ActionID, length: len(pkt.Payload), hex: hex.EncodeToString(pkt.Payload)}:
		default:
		}
	})

	drain := func() {
		for {
			select {
			case <-observed:
			default:
				return
			}
		}
	}
	waitStray := func(tag string, d time.Duration) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		for {
			select {
			case p := <-observed:
				slog.Info("建队观测窗口内收到非心跳报文",
					"tag", tag,
					"action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", p.action, p.action>>16, p.action&0xFFFF),
					"len", p.length, "payload", p.hex)
			case <-timer.C:
				return
			}
		}
	}

	call := func(tag string, pkt *protocol.Packet, respAction uint32, d time.Duration) (*protocol.Packet, bool) {
		cCtx, cCancel := context.WithTimeout(ctx, d)
		defer cCancel()
		resp, err := sess.Call(cCtx, pkt, respAction)
		if err != nil {
			slog.Warn("请求未获得目标回包", "tag", tag, "action_id", fmt.Sprintf("0x%08X", respAction), "err", err)
			return nil, false
		}
		slog.Info("请求收到回包", "tag", tag, "action_id", fmt.Sprintf("0x%08X", respAction),
			"len", len(resp.Payload), "payload", hex.EncodeToString(resp.Payload))
		return resp, true
	}

	logTeamInfo := func(tag string) (int32, bool) {
		drain()
		resp, ok := call(tag, protocol.BuildBeelzebubTrialsGetPlayerTeamInfoPacket(sess.LastActionID()),
			protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, *timeout)
		if !ok {
			waitStray(tag+":after-timeout", 2*time.Second)
			return 0, false
		}
		teamID, members, err := protocol.ParseBeelzebubTrialsPlayerTeamInfoResponse(resp.Payload)
		if err != nil {
			slog.Warn("解析队伍信息失败", "tag", tag, "err", err)
			return 0, false
		}
		slog.Info("当前队伍信息", "tag", tag, "team_id", teamID, "members", len(members), "detail", fmt.Sprintf("%+v", members))
		return teamID, true
	}

	slog.Info("=== 会话已激活，开始魔王试炼建队诊断 ===", "role_id", sess.RoleID(), "server_id", role.ServerID)

	if *cleanup {
		_ = sess.BeelzebubTrialsExitTeam(ctx)
		teamID, _ := logTeamInfo("after-cleanup")
		slog.Info("=== 队伍清理结束 ===", "team_id", teamID)
		return
	}

	// 1. 先读取角色当前是否已残留在队伍/战场状态。
	initialTeamID, _ := logTeamInfo("before-create")

	// 2. 读取挑战资格与可挑战列表。
	if resp, ok := call("check_can_challenge", protocol.BuildBeelzebubTrialsCheckCanChallengePacket(sess.LastActionID()),
		protocol.ActionIDBeelzebubTrialsCheckCanChallenge, *timeout); ok {
		can, pErr := protocol.ParseBeelzebubTrialsCheckCanChallengeResponse(resp.Payload)
		slog.Info("今日挑战资格", "can_challenge", can, "err", pErr)
	}
	if resp, ok := call("open_panel", protocol.BuildBeelzebubTrialsOpenPacket(sess.LastActionID()),
		protocol.ActionIDBeelzebubTrialsOpen, *timeout); ok {
		openData, pErr := protocol.ParseBeelzebubTrialsOpenResponse(resp.Payload)
		if pErr != nil {
			slog.Warn("解析面板失败", "err", pErr)
		} else {
			available := 0
			for _, b := range openData.Bosses {
				if b.RemainTimes > 0 {
					available++
				}
			}
			slog.Info("面板关卡可用性", "total", len(openData.Bosses), "available", available,
				"moral_exp", openData.MoralExp, "moral_lv", openData.MoralLv, "in_activity", openData.InActivity)
			if len(openData.Bosses) > 0 && available == 0 {
				slog.Warn("面板显示全部关卡已无剩余次数：服务端将静默丢弃 create_team，自动化应提前跳过")
			}
		}
	} else {
		slog.Warn("打开面板无回包，继续后续步骤")
	}
	if resp, ok := call("can_list", protocol.BuildBeelzebubTrialsGetCanChallengeListPacket(sess.LastActionID()),
		protocol.ActionIDBeelzebubTrialsGetCanChallengeList, *timeout); ok {
		list, pErr := protocol.ParseBeelzebubTrialsGetCanChallengeListResponse(resp.Payload)
		slog.Info("可挑战魔王列表", "count", len(list), "first", fmt.Sprintf("%v", list), "err", pErr)
	}

	bossID := int32(*bossFlag)
	if bossID == 0 {
		bossID = 7 // s0 上已知可挑战的最低等级魔王
	}
	slog.Info("选定测试魔王", "boss_id", bossID)
	if initialTeamID != 0 {
		slog.Warn("角色建队前已处于队伍中，先执行退队清理", "team_id", initialTeamID)
		_ = sess.BeelzebubTrialsExitTeam(ctx)
		time.Sleep(time.Second)
		logTeamInfo("after-preclean")
	}

	// 建队前的服务端残留状态排查：未结算战报与开放队伍列表。
	if resp, ok := call("pre_war_result", protocol.BuildBeelzebubTrialsGetWarResultPacket(sess.LastActionID()),
		protocol.ActionIDBeelzebubTrialsGetWarResult, *timeout); ok {
		slog.Info("建队前战报状态", "len", len(resp.Payload), "payload", hex.EncodeToString(resp.Payload))
	}
	if resp, ok := call("pre_team_list", protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetTeamList, nil),
		protocol.ActionIDBeelzebubTrialsGetTeamList, *timeout); ok {
		slog.Info("建队前队伍列表", "len", len(resp.Payload), "payload", hex.EncodeToString(resp.Payload))
	}
	if resp, ok := call("pre_open_moral", protocol.BuildBeelzebubTrialsOpenMoralPacket(sess.LastActionID()),
		protocol.ActionIDBeelzebubTrialsOpenMoral, *timeout); ok {
		slog.Info("建队前道行面板", "len", len(resp.Payload), "payload", hex.EncodeToString(resp.Payload))
	}
	// 挑战冷却 / 开启时间 / 助战列表等只读状态，排除服务端冷却或未开放导致的静默丢弃。
	for _, c := range []struct {
		name string
		act  uint32
	}{
		{"pre_cd_time", protocol.ActionIDBeelzebubTrialsGetPlayerCDTime},
		{"pre_open_time", protocol.ActionIDBeelzebubTrialsGetOpenTime},
		{"pre_helper_list", protocol.ActionIDBeelzebubTrialsGetHelperRoleList},
		{"pre_deploy", protocol.ActionIDBeelzebubTrialsGetDeploy},
	} {
		if resp, ok := call(c.name, protocol.NewPacket(c.act, nil), c.act, *timeout); ok {
			slog.Info("建队前只读状态", "tag", c.name, "len", len(resp.Payload), "payload", hex.EncodeToString(resp.Payload))
		}
	}

	if *readsOnly {
		slog.Info("=== 只读状态采集结束 ===")
		return
	}

	type variant struct {
		name   string
		desc   string
		action uint32 // 0 表示使用 create_team
		prep   func() // 发送建队请求前需要完成的前序动作，用于构造被测 PrevAct 链
		build  func() *protocol.Packet
	}
	baseCreate := func(teamType uint8, withPrev bool) func() *protocol.Packet {
		return func() *protocol.Packet {
			pkt := protocol.BuildBeelzebubTrialsCreateTeamPacket(bossID, teamType, sess.LastActionID())
			if withPrev {
				pkt.Payload = append(pkt.Payload, 0, 0, 0, 0)
				pkt.HasPrevAct = true
			}
			return pkt
		}
	}
	variants := []variant{
		{name: "V0_int32+uint8_type0", desc: "int32 boss + uint8 teamType=0 (现行实现)", build: baseCreate(protocol.BeelzebubTrialsNotLimit, false)},
		{name: "V1_int32+uint8_type2", desc: "uint8 teamType=2 (ON_ACTIVITY)", build: baseCreate(protocol.BeelzebubTrialsOnActivity, false)},
		{name: "V2_int32+uint8_type3", desc: "uint8 teamType=3 (NON_ACTIVITY)", build: baseCreate(protocol.BeelzebubTrialsNonActivity, false)},
		{
			name: "V3_int32+int32_type", desc: "int32 boss + int32 teamType (8 字节载荷)",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID),
					0, 0, 0, 0,
				})
			},
		},
		{
			name: "V4_prevact_canlist", desc: "现行载荷 + PrevAct=0x00390016 (can_challenge_list)",
			prep: func() {
				_, _ = call("V4:prep_can_list", protocol.BuildBeelzebubTrialsGetCanChallengeListPacket(sess.LastActionID()),
					protocol.ActionIDBeelzebubTrialsGetCanChallengeList, *timeout)
			},
			build: baseCreate(protocol.BeelzebubTrialsNotLimit, true),
		},
		{
			name: "V5_prevact_open", desc: "现行载荷 + PrevAct=0x00390001 (open_panel)",
			prep: func() {
				_, _ = call("V5:prep_open_panel", protocol.BuildBeelzebubTrialsOpenPacket(sess.LastActionID()),
					protocol.ActionIDBeelzebubTrialsOpen, *timeout)
			},
			build: baseCreate(protocol.BeelzebubTrialsNotLimit, true),
		},
		{
			name: "V6_after_team_list", desc: "先拉取队伍列表 (Action 2) 再发出现行建队载荷",
			prep: func() {
				_, _ = call("V6:prep_team_list", protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetTeamList, nil),
					protocol.ActionIDBeelzebubTrialsGetTeamList, *timeout)
			},
			build: baseCreate(protocol.BeelzebubTrialsNotLimit, false),
		},
		{
			name: "V7_payload_boss_only", desc: "仅 int32 bossID (4 字节载荷)",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID),
				})
			},
		},
		{
			name: "V8_payload_empty", desc: "空载荷 create_team",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, nil)
			},
		},
		{
			name: "V9_payload_6bytes", desc: "int32 boss + uint8 teamType + uint8 保留位",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID), 0, 0,
				})
			},
		},
		{
			name: "V10_join_team_0", action: protocol.ActionIDBeelzebubTrialsJoinTeam, desc: "加入队伍 Action 6, teamID=0 (验证入队动作是否同样被丢弃)",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsJoinTeam, []byte{0, 0, 0, 0})
			},
		},
		{
			name: "V11_dissolve_team", action: protocol.ActionIDBeelzebubTrialsDissolveTeam, desc: "解散队伍 Action 5 空载荷 (验证队伍写操作是否整体不可达)",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsDissolveTeam, nil)
			},
		},
		{
			name: "V12_prevact_prefix", desc: "PrevAct 前缀 + int32 boss + uint8 type (验证前序字段位置)",
			prep: func() {
				_, _ = call("V12:prep_can_list", protocol.BuildBeelzebubTrialsGetCanChallengeListPacket(sess.LastActionID()),
					protocol.ActionIDBeelzebubTrialsGetCanChallengeList, *timeout)
			},
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					0x00, 0x39, 0x00, 0x16,
					byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID), 0,
				})
			},
		},
		{
			name: "V13_zero_prevact_prefix", desc: "全零前缀 + int32 boss + uint8 type",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					0, 0, 0, 0,
					byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID), 0,
				})
			},
		},
		{
			name: "V14_zero_trailing_prevact", desc: "int32 boss + uint8 type + 4 字节全零尾 (非 HasPrevAct，不会被改写)",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID), 0, 0, 0, 0, 0,
				})
			},
		},
		{
			name: "V15_teamtype_first", desc: "uint8 teamType 在前 + int32 bossID",
			build: func() *protocol.Packet {
				return protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{
					0, byte(bossID >> 24), byte(bossID >> 16), byte(bossID >> 8), byte(bossID),
				})
			},
		},
	}

	for _, v := range variants {
		if *onlyPrefix != "" && !strings.HasPrefix(v.name, *onlyPrefix) {
			continue
		}
		drain()
		if v.prep != nil {
			v.prep()
		}
		action := v.action
		if action == 0 {
			action = protocol.ActionIDBeelzebubTrialsCreateTeam
		}
		slog.Info("--- 测试建队载荷变体 ---", "variant", v.name, "desc", v.desc, "payload_hex", hex.EncodeToString(v.build().Payload))
		_, ok := call(v.name, v.build(), action, *timeout)
		if ok {
			slog.Info("目标动作回包正常，建队成功", "variant", v.name)
		}
		waitStray(v.name, 2*time.Second)
		teamID, teamOK := logTeamInfo(v.name + ":team_info")
		if teamOK && teamID != 0 {
			slog.Info("服务端已产生队伍", "variant", v.name, "team_id", teamID)
			break
		}
		if initialTeamID == 0 && teamID == 0 {
			// 清理可能被服务端单方面建立的隐式队伍，避免污染后续变体。
			_ = sess.BeelzebubTrialsExitTeam(ctx)
			time.Sleep(500 * time.Millisecond)
		}
	}

	slog.Info("=== 建队诊断结束 ===")
}
