//go:build probe

// 目标: 实机探测圣域「无尽龙域 / 龙域战场」(Mod_SaDragonArea_Base, Module 362) 的
//       面板回包布局、连接路由（主服 vs 圣域独立连接）以及挑战/扫荡状态。
//
// 运行命令:
//   SXD_CONFIG=configs/niuchangsheng.yaml SXD_SERVER=s1 go run -tags=probe test/probes/probe_sa_dragon_area_live.go
//
// 预期表现:
//   - 打印主服 362/0 与圣域连接 362/0 的原始回包长度、前 128B hex 及解码字段；
//   - 仅执行只读面板查询（除非显式设置 SXD_DO_FIGHT=1），不消耗元宝。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
)

const (
	actDragonAreaInfo       uint32 = (362 << 16) | 0
	actDragonAreaFight      uint32 = (362 << 16) | 2
	actDragonAreaQuickFight uint32 = (362 << 16) | 4
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	wantServer := os.Getenv("SXD_SERVER")
	if wantServer == "" {
		wantServer = "s1"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}

	var acc config.AccountConfig
	var role config.RoleConfig
	found := false
	for _, a := range cfg.Accounts {
		for _, r := range a.Roles {
			if strings.EqualFold(r.ServerID, wantServer) {
				acc = a
				role = r
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		slog.Error("配置中未找到目标区服角色", "server", wantServer)
		return
	}

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建心动认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	if err := sess.AwaitActive(ctx); err != nil {
		slog.Error("等待主服会话激活失败", "err", err)
		return
	}
	slog.Info("主服会话已激活", "role_id", sess.RoleID(), "level", sess.GetPlayerState().Level, "player_id", sess.PlayerID())

	if os.Getenv("SXD_RUN_ROUTINE") == "1" {
		beforeIngots := sess.GetPlayerState().Ingots
		err := dungeon.NewSaDragonAreaRoutine().Execute(ctx, sess, nil)
		afterIngots := sess.GetPlayerState().Ingots
		slog.Info("实机执行 sa_dragon_area 例程完成",
			"role_id", sess.RoleID(),
			"err", err,
			"ingots_before", beforeIngots,
			"ingots_after", afterIngots,
			"ingots_delta", afterIngots-beforeIngots,
		)
		return
	}

	// 1. 测试主服 362/0
	mCtx, mCancel := context.WithTimeout(ctx, 5*time.Second)
	mResp, mErr := sess.Call(mCtx, protocol.NewPacket(actDragonAreaInfo, nil), actDragonAreaInfo)
	mCancel()
	if mErr != nil {
		slog.Info("主服 362/0 无回包或报错", "err", mErr)
	} else {
		dumpHex("主服 362/0", mResp.Payload)
	}

	// 2. 建立圣域独立连接并测试 362/0
	sa, saErr := sess.OpenSaintAreaSession(ctx)
	if saErr != nil {
		slog.Warn("建立圣域会话失败", "err", saErr)
		return
	}
	defer sa.Close()
	slog.Info("圣域会话已激活", "sa_role_id", sa.RoleID(), "sa_player_id", sa.PlayerID())

	iCtx, iCancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := sa.Call(iCtx, protocol.NewPacket(actDragonAreaInfo, nil), actDragonAreaInfo)
	iCancel()
	if err != nil {
		slog.Warn("圣域 362/0 失败", "err", err)
		return
	}
	dumpHex("圣域 362/0", resp.Payload)
	decodeInfo(resp.Payload)
}

func dumpHex(label string, payload []byte) {
	h := hex.EncodeToString(payload)
	if len(h) > 256 {
		h = h[:256] + "..."
	}
	slog.Info(label, "len", len(payload), "hex", h)
}

func decodeInfo(payload []byte) {
	r := protocol.NewReader(payload)
	seq, err := r.ReadInt32()
	if err != nil {
		slog.Warn("解码 seq 失败", "err", err)
		return
	}
	awardCnt, err := r.ReadUint16()
	if err != nil {
		slog.Warn("解码 award_list count 失败", "err", err)
		return
	}
	for i := 0; i < int(awardCnt); i++ {
		_, _ = r.ReadInt32()
		_, _ = r.ReadInt32()
	}
	completeCnt, _ := r.ReadInt16()
	decDiff, _ := r.ReadInt8()
	firstNick, _ := r.ReadString()
	firstReportID, _ := r.ReadInt32()
	firstVer, _ := r.ReadString()
	bestNick, _ := r.ReadString()
	bestReportID, _ := r.ReadInt32()
	bestVer, _ := r.ReadString()
	specialPat, _ := r.ReadInt32()
	monsterTeamID, _ := r.ReadInt32()
	dbCnt, _ := r.ReadUint16()
	for i := 0; i < int(dbCnt); i++ {
		_, _ = r.ReadInt8()
		_, _ = r.ReadInt32()
		_, _ = r.ReadInt8()
	}
	monCnt, _ := r.ReadUint16()
	for i := 0; i < int(monCnt); i++ {
		for j := 0; j < 7; j++ {
			_, _ = r.ReadInt32()
		}
	}
	fiveElem, _ := r.ReadInt32()
	lastWeekSeq, _ := r.ReadInt32()
	quickSeq, _ := r.ReadInt32()
	isClear, err := r.ReadInt32()
	slog.Info("圣域 362/0 解码结果",
		"seq", seq,
		"awards", awardCnt,
		"complete_count", completeCnt,
		"decrease_difficulty", decDiff,
		"first_nick", firstNick,
		"first_report_id", firstReportID,
		"first_ver", firstVer,
		"best_nick", bestNick,
		"best_report_id", bestReportID,
		"best_ver", bestVer,
		"special_pattern", specialPat,
		"monster_team_id", monsterTeamID,
		"dragonballs", dbCnt,
		"monsters", monCnt,
		"five_elem", fiveElem,
		"last_week_seq", lastWeekSeq,
		"quick_seq", quickSeq,
		"is_dragon_clear", isClear,
		"remaining_bytes", r.Remaining(),
		"tail_err", err,
	)
}
