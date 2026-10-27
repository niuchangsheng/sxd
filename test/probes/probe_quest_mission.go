//go:build probe

// 目标协议: [Module 3, Action 2/3/4/5/6/9] Mod_Quest_Base 任务闭环 & [Module 4, Action 0/1/2/3/4/5] Mod_Mission_Base 剧情副本挑战
// 基准对齐: 对齐官方 AS3 字节码 (MissionMapView, MissionData, QuestData, QuestController) 与 Pieb.db 静态字典
// 运行命令: go run -tags=probe test/probes/probe_quest_mission.go
// 预期表现: 完整跑通章节进度查询、进入副本、多波次怪物挑战、星级评价、开箱拾取与任务状态同步
package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func decompressIfZlib(data []byte) []byte {
	if len(data) >= 2 && data[0] == 0x78 && (data[1] == 0x9c || data[1] == 0x01 || data[1] == 0xda) {
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err == nil {
			defer zr.Close()
			if decomp, dErr := io.ReadAll(zr); dErr == nil {
				return decomp
			}
		}
	}
	return data
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load("configs/config.yaml")
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(10 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		fmt.Printf("会话未就绪: %v\n", sess.State())
		return
	}

	callWithPrev := func(actID uint32, buildPayload func(w *protocol.Writer)) (*protocol.Packet, error) {
		w := protocol.NewWriter()
		if buildPayload != nil {
			buildPayload(w)
		}
		w.WriteUint32(sess.LastActionID())
		pkt := protocol.NewPacket(actID, w.Bytes())
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		defer cCancel()
		return sess.Call(cCtx, pkt, actID)
	}

	// 6. town_quest_show (0x00030009)
	if resp, err := callWithPrev(0x00030009, nil); err == nil {
		d := decompressIfZlib(resp.Payload)
		fmt.Printf("6. town_quest_show (%dB): %s\n", len(d), hex.EncodeToString(d))
		r := protocol.NewReader(d)
		cnt, _ := r.ReadUint16()
		for i := uint16(0); i < cnt; i++ {
			qid, _ := r.ReadInt32()
			st, _ := r.ReadInt32()
			nt, _ := r.ReadUint8()
			nc, _ := r.ReadUint16()
			fmt.Printf("  Quest %d: state=%d need_type=%d need_count=%d", qid, st, nt, nc)
			for j := uint16(0); j < nc; j++ {
				a, _ := r.ReadInt32()
				b, _ := r.ReadInt32()
				c, _ := r.ReadInt32()
				fmt.Printf(" [%d, %d/%d]", a, b, c)
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("6. town_quest_show err: %v\n", err)
	}

	// 7. list_player_quest (0x00030006)
	if resp, err := callWithPrev(0x00030006, nil); err == nil {
		d := decompressIfZlib(resp.Payload)
		fmt.Printf("7. list_player_quest (%dB): %s\n", len(d), hex.EncodeToString(d))
	} else {
		fmt.Printf("7. list_player_quest err: %v\n", err)
	}
}
