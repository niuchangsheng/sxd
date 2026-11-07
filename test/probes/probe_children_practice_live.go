//go:build probe

// 目标: 孩子修行 (Mod_Children_Base, Module 312) 只读探测
//
//	get_info          (Act 0,  0x01380000) request=[]            -> 孩子列表
//	get_practice_info (Act 29, 0x0138001D) request=[Int childID] -> [[type,level,flag,pattern,[monsters]]]
//	challenge         (Act 32, 0x01380020) request=[Int childID, Byte type]
//
// 注意: 不要先调用 0x2A, 否则 312/315 请求会被静默丢弃。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_children_practice_live.go
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	challengeType := flag.Int("challenge-type", -1, "指定修行 type 时发起一次挑战 (-1 只读)")
	deploy := flag.Bool("deploy", false, "挑战前先 up_deploy (孩子上阵)")
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
		return resp
	}
	infoPkt := protocol.BuildChildrenInfoPacket()
	infoResp := call(protocol.ActionIDChildrenInfo, infoPkt.Payload, 8*time.Second)
	if infoResp == nil {
		slog.Error("孩子主面板无回包")
		return
	}
	panel, err := protocol.ParseChildrenPanel(infoResp.Payload)
	if err != nil {
		slog.Error("解析孩子主面板失败", "err", err)
		return
	}
	slog.Info("孩子列表", "child_ids", panel.ChildIDs, "result", panel.Result)
	if len(panel.ChildIDs) == 0 {
		return
	}
	childID := panel.ChildIDs[0]
	w := protocol.NewWriter()
	w.WriteInt32(childID)
	w.WriteUint32(0)
	pracResp := call(0x0138001D, w.Bytes(), 8*time.Second)
	if pracResp == nil {
		slog.Error("孩子修行面板无回包")
		return
	}
	slog.Info("孩子修行回包", "len", len(pracResp.Payload), "hex", hex.EncodeToString(pracResp.Payload))
	types := parsePracticeTypes(pracResp.Payload)
	slog.Info("孩子修行条目", "child_id", childID, "types", types)
	if *challengeType < 0 {
		return
	}
	if *deploy {
		dw := protocol.NewWriter()
		dw.WriteInt32(childID)
		dw.WriteUint32(0)
		if r := call(0x01380010, dw.Bytes(), 8*time.Second); r != nil {
			slog.Info("孩子上阵回包", "hex", hex.EncodeToString(r.Payload))
		}
	}
	ch := protocol.NewWriter()
	ch.WriteInt32(childID)
	ch.WriteUint8(uint8(*challengeType))
	ch.WriteUint32(0)
	st := sess.GetPlayerState()
	if r := call(0x01380020, ch.Bytes(), 20*time.Second); r != nil {
		head := r.Payload
		if len(head) > 1 {
			head = head[:1]
		}
		slog.Info("孩子挑战回包", "result_hex", hex.EncodeToString(head), "len", len(r.Payload))
	}
	fin := sess.GetPlayerState()
	slog.Info("孩子挑战后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots,
		"stamina_delta", fin.Stamina-st.Stamina)
}

// parsePracticeTypes 解析 [[type(Byte), level(Byte), challengeFlag(UByte), pattern(Byte), monsterList]]
func parsePracticeTypes(payload []byte) []map[string]int32 {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil
	}
	out := make([]map[string]int32, 0, count)
	for i := 0; i < int(count); i++ {
		if r.Remaining() <= 0 {
			break
		}
		t, _ := r.ReadUint8()
		lv, _ := r.ReadUint8()
		flag, _ := r.ReadUint8()
		pat, _ := r.ReadUint8()
		mn, _ := r.ReadUint16()
		for j := 0; j < int(mn); j++ {
			_, _ = r.ReadUint8()  // monsterFlag
			_, _ = r.ReadInt32()  // monster_id
			_, _ = r.ReadString() // name
			_, _ = r.ReadUint8()  // deploy_grid_id
			_, _ = r.ReadInt32()  // stunt
			_, _ = r.ReadInt32()  // supernatural?
		}
		out = append(out, map[string]int32{"type": int32(t), "level": int32(lv), "challenge_flag": int32(flag), "pattern": int32(pat)})
	}
	return out
}
