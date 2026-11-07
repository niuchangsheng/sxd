//go:build probe

// 目标: 神龙龙珠一键合成 (Mod_Dragonball_Base, Module 111)
//
//	get_dragonball_list (Act 3, 0x006F0003) response=[ball records..., Int x7]
//	  记录: [player_dragonball_id, dragonball_id, level, read_level, exp,
//	         merge_green_count, merge_blue_count, merge_purple_count,
//	         merge_gold_count, is_locked, grade, position]
//	one_key_merge (Act 6, 0x006F0006) request=[[Int quality]] response=[UByte result,[Int ids]]
//
// 本探针先尝试扁平编码 [1,2,3] (紫及以下) 与 [6] (火龙珠) 两组一键合成。
// quality 取自 Pieb.db DragonBall.quality_id: 1-5 普通龙珠, 6 火龙珠, 7 碎片。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_dragonball_merge_live.go
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

type ball struct {
	PlayerID   int32
	DragonID   int32
	Level      int16
	ReadLevel  uint8
	Exp        int32
	MergeGreen uint8
	MergeBlue  uint8
	MergePurp  uint8
	MergeGold  uint8
	Locked     uint8
	Grade      int32
	Position   int32
	Tail       int16
}

func mustInt16(r *protocol.Reader) int16 {
	v, _ := r.ReadInt16()
	return v
}

func (b *ball) readLocked(r *protocol.Reader) int32 {
	v, _ := r.ReadInt32()
	return v
}

func parseBalls(payload []byte) ([]ball, error) {
	r := protocol.NewReader(payload)
	n, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := make([]ball, 0, n)
	for i := 0; i < int(n); i++ {
		var b ball
		b.PlayerID, _ = r.ReadInt32()
		b.DragonID = int32(mustInt16(r))
		b.Level = 0
		lv, _ := r.ReadUint8()
		b.Level = int16(lv)
		b.ReadLevel, _ = r.ReadUint8()
		b.Exp, _ = r.ReadInt32()
		b.MergeGreen, _ = r.ReadUint8()
		b.MergeBlue, _ = r.ReadUint8()
		b.MergePurp, _ = r.ReadUint8()
		b.MergeGold, _ = r.ReadUint8()
		b.Locked = uint8(b.readLocked(r))
		b.Grade, _ = r.ReadInt32()
		b.Position = int32(mustInt16(r))
		b.Tail = 0
		out = append(out, b)
	}
	return out, nil
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
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
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	dump := func(tag string) {
		resp := call(0x006F0003, []byte{0, 0, 0, 0})
		if resp == nil {
			return
		}
		balls, perr := parseBalls(resp.Payload)
		if perr != nil {
			slog.Warn("解析龙珠列表失败", "err", perr, "hex", hex.EncodeToString(resp.Payload))
			return
		}
		slog.Info("龙珠列表", "tag", tag, "count", len(balls))
		for _, b := range balls {
			slog.Info("龙珠", "tag", tag, "player_id", b.PlayerID, "dragon_id", b.DragonID,
				"level", b.Level, "exp", b.Exp, "merge_g", b.MergeGreen, "merge_b", b.MergeBlue,
				"merge_p", b.MergePurp, "merge_gold", b.MergeGold, "locked", b.Locked,
				"grade", b.Grade, "pos", b.Position)
		}
	}
	dump("before")
	// 扁平编码: Uint16 count + Int32 quality
	merge := func(qualities []int32) *protocol.Packet {
		// 末尾 4 字节为 PrevAct 占位，避免会话层覆盖最后一个 quality。
		w := protocol.NewWriter()
		w.WriteUint16(uint16(len(qualities)))
		for _, q := range qualities {
			w.WriteInt32(q)
		}
		w.WriteUint32(0)
		return call(0x006F0006, w.Bytes())
	}
	if resp := merge([]int32{1, 2, 3}); resp != nil {
		slog.Info("一键合成(紫及以下)", "hex", hex.EncodeToString(resp.Payload))
	}
	time.Sleep(300 * time.Millisecond)
	dump("after_normal")
	if resp := merge([]int32{6}); resp != nil {
		slog.Info("一键合成(火龙珠)", "hex", hex.EncodeToString(resp.Payload))
	}
	time.Sleep(300 * time.Millisecond)
	dump("after_fire")
}
