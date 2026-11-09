//go:build probe

// 目标协议: 圣盟 (Mod_SectAlliance_Base, Module 884) 在仙盟城镇会话上的可达性。
//
// 背景: 主服 884/0 能返回赛季，但祭祀/奖励面板需要仙盟城镇会话与 alliance_id。
// 本探针在仙盟城镇会话上依次调用:
//
//	884/0  get_now_season
//	884/1  top_sect_panel  (取 self_alliance_id)
//	884/16 sacrifice_award_panel(alliance_id)
//
// 仅只读查询，不领取/不消耗任何资源。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_sect_alliance_live.go
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
	actionSectAllianceSeason      uint32 = 0x03740000
	actionSectAllianceTopPanel    uint32 = 0x03740001
	actionSectAllianceSacrifice   uint32 = 0x03740010
	actionSectAlliancePersonAward uint32 = 0x03740011
	actionSectAllianceProfitAward uint32 = 0x03740012
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	town, err := sess.OpenSectTownSession(ctx)
	if err != nil {
		slog.Error("建立仙盟城镇会话失败", "err", err)
		return
	}
	defer town.Close()
	slog.Info("仙盟城镇会话已建立", "player_id", town.PlayerID(), "role_id", town.RoleID())

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		defer cCancel()
		resp, cErr := town.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 700 {
			limit = 700
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	call(protocol.NewPacket(actionSectAllianceSeason, nil), actionSectAllianceSeason, "884_0_season")
	top := call(protocol.NewPacket(actionSectAllianceTopPanel, nil), actionSectAllianceTopPanel, "884_1_top_panel")
	if top == nil {
		return
	}
	allianceID := parseTopPanelSelfAllianceID(top.Payload)
	slog.Info("解析 top_sect_panel", "self_alliance_id", allianceID)
	if allianceID <= 0 {
		slog.Info("账号没有 alliance_id，跳过祭祀面板")
		return
	}
	w := protocol.NewWriter()
	w.WriteInt32(allianceID)
	panel := call(protocol.NewPacket(actionSectAllianceSacrifice, w.Bytes()), actionSectAllianceSacrifice, "884_16_sacrifice_panel")
	if panel == nil {
		return
	}
	parseSacrificePanel(panel.Payload)
}

// parseTopPanelSelfAllianceID 跳过 alliance_list，读取尾部的 self_alliance_id。
func parseTopPanelSelfAllianceID(payload []byte) int32 {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		slog.Warn("读取圣盟列表数量失败", "err", err)
		return 0
	}
	for i := 0; i < int(count); i++ {
		// 行格式: [Int, String, Int, String, String, Int, Int, Int]
		a, _ := r.ReadInt32()
		b, _ := r.ReadString()
		c, _ := r.ReadInt32()
		d, _ := r.ReadString()
		e, _ := r.ReadString()
		f, _ := r.ReadInt32()
		g, _ := r.ReadInt32()
		h, _ := r.ReadInt32()
		if i < 3 {
			slog.Info("top_sect_panel 行", "i", i, "a", a, "b", b, "c", c,
				"d", d, "e", e, "f", f, "g", g, "h", h)
		}
	}
	slog.Info("top_sect_panel 剩余字节", "remaining", r.Remaining(),
		"hex", hex.EncodeToString(payload[len(payload)-r.Remaining():]))
	selfSectID, _ := r.ReadInt32()
	isLeader, _ := r.ReadInt32()
	selfAllianceID, _ := r.ReadInt32()
	inviteNum, _ := r.ReadInt32()
	slog.Info("top_sect_panel 尾部字段", "self_sect_id", selfSectID, "is_leader", isLeader,
		"self_alliance_id", selfAllianceID, "invite_num", inviteNum)
	return selfAllianceID
}

func parseSacrificePanel(payload []byte) {
	r := protocol.NewReader(payload)
	result, _ := r.ReadUint8()
	sectWeekSacrifice, _ := r.ReadInt32()
	sectWeekProfit, _ := r.ReadInt32()
	sectWeekCost, _ := r.ReadInt32()
	sectTotalProfit, _ := r.ReadInt32()
	allianceTotalProfit, _ := r.ReadInt32()
	slog.Info("祭祀面板",
		"result", result,
		"sect_week_sacrifice", sectWeekSacrifice,
		"sect_week_profit", sectWeekProfit,
		"sect_week_cost", sectWeekCost,
		"sect_total_profit", sectTotalProfit,
		"alliance_total_profit", allianceTotalProfit,
		"remaining_hex", hex.EncodeToString(payload[len(payload)-r.Remaining():]))
}
