//go:build probe

// 目标: 定位界渊之战 818/4 挑战被静默丢弃的原因：对比跨服会话/主服会话，
// 以及 step_monster_id / monster_team_id 两种参数形态、Boss 与普通怪物。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_abyss_fight_diag_live.go
//
// 行为: 登录 → 818/0 开放状态 → 跨服会话 818/2 面板 → 依次尝试
// 818/3/818/4（34、36、12939），随后在主服重试 818/3(34)/818/4(34)。
// 只读+挑战请求，不购买、不消耗元宝。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
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
	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(90 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	st := sess.GetPlayerState()
	slog.Info("会话已激活", "role_id", sess.RoleID(),
		"stamina", st.Stamina, "max_stamina", st.MaxStamina, "extra_stamina", st.ExtraStamina)

	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := s.Call(c, pkt, act)
		ccancel()
		if err != nil {
			slog.Info("无回包", "scope", name, "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		slog.Info("回包", "scope", name, "action", fmt.Sprintf("0x%08X", act),
			"len", len(resp.Payload), "head", fmt.Sprintf("%x", resp.Payload[:min(16, len(resp.Payload))]))
		return resp
	}

	openResp := call(sess, protocol.BuildAbyssClashFuncOpenPacket(), protocol.ActionIDAbyssClashFuncOpen, "main/818/0", 8*time.Second)
	if openResp != nil {
		if open, oErr := protocol.ParseAbyssClashFuncOpen(openResp.Payload); oErr == nil {
			slog.Info("开放状态", "open", open.OpenFlag, "step", open.Step)
		}
	}

	cross, cErr := sess.OpenCrossServerSession(ctx)
	if cErr != nil {
		slog.Error("建立跨服会话失败", "err", cErr)
		return
	}
	defer cross.Close()

	if p := call(cross, protocol.BuildAbyssClashStep2PanelPacket(), protocol.ActionIDAbyssClashStep2Panel, "cross/818/2", 10*time.Second); p != nil {
		if panel, e := protocol.ParseAbyssClashStep2Panel(p.Payload); e == nil {
			slog.Info("step2 面板", "result", panel.Result, "fight_num", panel.FightNum,
				"monsters", len(panel.Monsters))
			for _, m := range panel.Monsters {
				slog.Info("目标", "step_monster_id", m.StepMonsterID, "team_id", m.MonsterTeamID,
					"boss", m.IsBoss, "boss_fight", m.BossCanFightNum)
			}
		}
	}

	// 官方进入地图流程：面板 → score_store_info(818/7) → 挑战。
	call(cross, protocol.NewPacket(protocol.ActionIDAbyssClashScoreStore, append(make([]byte, 4))),
		protocol.ActionIDAbyssClashScoreStore, "cross/818/7", 8*time.Second)

	type attempt struct {
		label     string
		monsterID int32
		session   *client.RoleSession
		info      bool
	}
	attempts := []attempt{
		{"cross/818/3(id=34)", 34, cross, true},
		{"cross/818/4(id=34)", 34, cross, false},
		{"cross/818/3(id=36)", 36, cross, true},
		{"cross/818/4(id=36)", 36, cross, false},
		{"cross/818/4(team=12939)", 12939, cross, false},
		{"main/818/3(id=34)", 34, sess, true},
		{"main/818/4(id=34)", 34, sess, false},
	}
	for _, a := range attempts {
		if a.info {
			call(a.session, protocol.BuildAbyssClashMonsterTeamPacket(a.monsterID),
				protocol.ActionIDAbyssClashMonsterTeam, a.label, 6*time.Second)
		} else {
			call(a.session, protocol.BuildAbyssClashStartFightPacket(a.monsterID),
				protocol.ActionIDAbyssClashStartFight, a.label, 12*time.Second)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
