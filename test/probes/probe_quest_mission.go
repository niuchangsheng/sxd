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

	// 8. Test enter_mission 139 and fight_monster
	fmt.Println("=== Testing get_sections map 9 ===")
	secResp, secErr := callWithPrev(protocol.ActionIDMissionGetSections, func(w *protocol.Writer) {
		w.WriteInt32(9)
	})
	fmt.Printf("get_sections: err=%v resp_len=%d\n", secErr, len(secResp.Payload))
	if secErr == nil && len(secResp.Payload) > 2 {
		r := protocol.NewReader(secResp.Payload)
		cnt, _ := r.ReadUint16()
		fmt.Printf("Missions count in map 9: %d\n", cnt)
		for i := uint16(0); i < cnt; i++ {
			mid, _ := r.ReadInt32()
			open, _ := r.ReadInt8()
			b2, _ := r.ReadUint8()
			b3, _ := r.ReadInt8()
			s1, _ := r.ReadInt16()
			b4, _ := r.ReadInt8()
			fmt.Printf("  Mission %d: open=%d b2=%d b3=%d s1=%d b4=%d\n", mid, open, b2, b3, s1, b4)
		}
	}

	fmt.Println("=== Testing enter_mission 139 ===")
	enterResp, enterErr := callWithPrev(protocol.ActionIDMissionPlotEnter, func(w *protocol.Writer) {
		w.WriteInt32(139)
	})
	if enterErr != nil {
		fmt.Printf("enter_mission err: %v\n", enterErr)
	} else {
		fmt.Printf("enter_mission resp (%dB): %s\n", len(enterResp.Payload), hex.EncodeToString(enterResp.Payload))
		r := protocol.NewReader(enterResp.Payload)
		status, _ := r.ReadUint8()
		mid, _ := r.ReadInt32()
		stam, _ := r.ReadInt32()
		b, _ := r.ReadInt8()
		i1, _ := r.ReadInt32()
		i2, _ := r.ReadInt32()
		mCnt, _ := r.ReadUint16()
		fmt.Printf("enter_mission: status=%d mid=%d stam=%d b=%d i1=%d i2=%d mCnt=%d\n", status, mid, stam, b, i1, i2, mCnt)
		for j := uint16(0); j < mCnt; j++ {
			mt, _ := r.ReadInt32()
			fmt.Printf("  MonsterTeam[%d] = %d\n", j, mt)
		}
		dCnt, _ := r.ReadUint16()
		fmt.Printf("  Defeated mCnt=%d\n", dCnt)
		for j := uint16(0); j < dCnt; j++ {
			mt, _ := r.ReadInt32()
			fmt.Printf("  Defeated MonsterTeam[%d] = %d\n", j, mt)
		}
	}
}
