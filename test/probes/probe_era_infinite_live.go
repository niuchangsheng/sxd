//go:build probe

// 目标: 纪元湮劫 / 无尽洞天 (Mod_EraInfiniteMission_Base, Module 853) 只读探测
//
//	get_now_season     (Act 0, 0x03550000) request=[]            -> Int season
//	get_panel_info     (Act 1, 0x03550001) request=[]            -> Int × 6
//	get_chapter_panel  (Act 2, 0x03550002) request=[Int chapter] -> 章节/关卡列表
//	get_monster_team_info (Act 3, 0x03550003) request=[Int level, Int deployType]
//	fight              (Act 4, 0x03550004) request=[Int level, [Int deployType, Int seq]]
//
// 本探针默认只读；-fight 时才以 [level,[deployType,seq]] 发起一次战斗。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_era_infinite_live.go
package main

import (
	"context"
	"encoding/hex"
	"flag"
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
	actEraSeason   uint32 = 0x03550000
	actEraPanel    uint32 = 0x03550001
	actEraChapter  uint32 = 0x03550002
	actEraMonsters uint32 = 0x03550003
	actEraFight    uint32 = 0x03550004
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	chapter := flag.Int("chapter", 1, "章节编号 (get_chapter_panel 参数)")
	level := flag.Int("level", 1, "战斗关卡 level")
	deployType := flag.Int("deploy", 1, "阵容类型 deployType")
	seq := flag.Int("seq", 1, "战斗序号 seq")
	doFight := flag.Bool("fight", false, "以 [level,[deployType,seq]] 发起一次战斗")
	afterActivity := flag.Bool("after-activity", false, "先调用活跃度面板 0x2A, 复现 routine 的会话链")
	flag.Parse()
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	call := func(action uint32, payload []byte, timeout time.Duration) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		slog.Info("回包", "action", fmt.Sprintf("0x%08X", action), "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload))
		return resp
	}
	st := sess.GetPlayerState()
	if *afterActivity {
		call(0x0000002A, nil, 8*time.Second)
	}
	zero := []byte{0, 0, 0, 0}
	call(actEraSeason, zero, 8*time.Second)
	call(actEraPanel, zero, 8*time.Second)
	cw := protocol.NewWriter()
	cw.WriteInt32(int32(*chapter))
	cw.WriteUint32(0)
	call(actEraChapter, cw.Bytes(), 8*time.Second)
	mw := protocol.NewWriter()
	mw.WriteInt32(int32(*level))
	mw.WriteInt32(int32(*deployType))
	mw.WriteUint32(0)
	call(actEraMonsters, mw.Bytes(), 8*time.Second)
	if !*doFight {
		return
	}
	fw := protocol.NewWriter()
	fw.WriteInt32(int32(*level))
	fw.WriteInt32(int32(*deployType))
	fw.WriteInt32(int32(*seq))
	fw.WriteUint32(0)
	call(actEraFight, fw.Bytes(), 15*time.Second)
	fin := sess.GetPlayerState()
	slog.Info("战斗后资产", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots,
		"stamina_delta", fin.Stamina-st.Stamina)
}
