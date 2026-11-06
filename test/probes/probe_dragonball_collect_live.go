//go:build probe

// 目标协议: Mod_Dragonball_Base.collect_dragonball (Module 111, Action 1)
//
// 背景: dragonball_offer 例程在祭坛临时格满时收取龙珠，线上返回
// INVALID_DRAGONBALL(2)。官方 AS3 描述符为 request=[[Int]]，
// 但 Haloer 序列化器对数组长度/元素布局存在多种可能。本探针按官方
// DragonBallData 的字段语义 (player_d_b_id, d_b_id, lv) 穷举请求编码，
// 以确定服务端真正接受的线格式。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_dragonball_collect_live.go
// 预期表现: 打印每种编码的 result；命中 SUCCESS(0)/PACKAGE_FULL(1) 后立即停止。
// 注意: 成功收取会把祭坛临时格龙珠移入背包，不消耗元宝。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

type ball struct {
	playerDBID int32
	dragonball int16
	level      uint8
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
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

	infoResp := call(protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo, 6*time.Second)
	if infoResp == nil {
		slog.Error("查询临时格失败")
		return
	}
	r := protocol.NewReader(infoResp.Payload)
	_, _ = r.ReadInt16()
	_, _ = r.ReadInt16()
	_, _ = r.ReadUint8()
	for i := 0; i < 3; i++ {
		_, _ = r.ReadInt16()
	}
	count, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析临时格数量失败", "err", err)
		return
	}
	balls := make([]ball, 0, count)
	for i := 0; i < int(count); i++ {
		var b ball
		b.playerDBID, _ = r.ReadInt32()
		b.dragonball, _ = r.ReadInt16()
		b.level, _ = r.ReadUint8()
		balls = append(balls, b)
	}
	slog.Info("临时格龙珠", "count", len(balls))
	for _, b := range balls {
		slog.Info("龙珠", "player_d_b_id", b.playerDBID, "d_b_id", b.dragonball, "lv", b.level)
	}
	if len(balls) == 0 {
		slog.Info("临时格为空，无需收取")
		return
	}
	sorted := append([]ball(nil), balls...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].playerDBID < sorted[j].playerDBID })

	byPlayerID := func(list []ball) []int32 {
		out := make([]int32, 0, len(list))
		for _, b := range list {
			out = append(out, b.playerDBID)
		}
		return out
	}
	byDragonballID := func(list []ball) []int32 {
		out := make([]int32, 0, len(list))
		for _, b := range list {
			out = append(out, int32(b.dragonball))
		}
		return out
	}

	// 每种编码函数返回完整 payload。
	variants := []struct {
		name     string
		prevAct  bool
		build    func() []byte
	}{
		{"flat_u16_sorted_player_id", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byPlayerID(sorted) {
				w.WriteInt32(id)
			}
			return w.Bytes()
		}},
		{"nested_u16_sorted_player_id", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byPlayerID(sorted) {
				w.WriteInt32(id)
			}
			return w.Bytes()
		}},
		{"flat_u16_prevact", true, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byPlayerID(sorted) {
				w.WriteInt32(id)
			}
			w.WriteUint32(0) // 占位，Send 时会替换为 PrevAct
			return w.Bytes()
		}},
		{"nested_u16_prevact", true, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byPlayerID(sorted) {
				w.WriteInt32(id)
			}
			w.WriteUint32(0)
			return w.Bytes()
		}},
		{"nested_u16_dbid_prevact", true, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byDragonballID(sorted) {
				w.WriteInt32(id)
			}
			w.WriteUint32(0)
			return w.Bytes()
		}},
		{"flat_u16_player_id_pairs", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for _, b := range sorted {
				w.WriteInt32(b.playerDBID)
				w.WriteInt32(int32(b.dragonball))
			}
			return w.Bytes()
		}},
		{"nested_u16_player_id_pairs", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(uint16(len(sorted)))
			for _, b := range sorted {
				w.WriteInt32(b.playerDBID)
				w.WriteInt32(int32(b.dragonball))
			}
			return w.Bytes()
		}},
		{"nested_u16_player_id_pairs_prevact", true, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(uint16(len(sorted)))
			for _, b := range sorted {
				w.WriteInt32(b.playerDBID)
				w.WriteInt32(int32(b.dragonball))
			}
			w.WriteUint32(0)
			return w.Bytes()
		}},
		{"flat_u16_dbid", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byDragonballID(sorted) {
				w.WriteInt32(id)
			}
			return w.Bytes()
		}},
		{"flat_u8_player_id", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint8(uint8(len(sorted)))
			for _, id := range byPlayerID(sorted) {
				w.WriteInt32(id)
			}
			return w.Bytes()
		}},
		{"flat_u8_index", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint8(uint8(len(sorted)))
			for i := range sorted {
				w.WriteUint8(uint8(i))
			}
			return w.Bytes()
		}},
		{"flat_u16_index", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for i := range sorted {
				w.WriteUint16(uint16(i))
			}
			return w.Bytes()
		}},
		{"array_of_arrays_u16_player_id", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for _, id := range byPlayerID(sorted) {
				w.WriteUint16(1)
				w.WriteInt32(id)
			}
			return w.Bytes()
		}},
		{"flat_u16_single_player_id", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(sorted[0].playerDBID)
			return w.Bytes()
		}},
		{"nested_u16_single_player_id", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(1)
			w.WriteInt32(sorted[0].playerDBID)
			return w.Bytes()
		}},
		{"flat_u16_index_int32", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(sorted)))
			for i := range sorted {
				w.WriteInt32(int32(i))
			}
			return w.Bytes()
		}},
		{"nested_u16_index_int32", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteUint16(uint16(len(sorted)))
			for i := range sorted {
				w.WriteInt32(int32(i))
			}
			return w.Bytes()
		}},
		{"flat_u16_original_order", false, func() []byte {
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(balls)))
			for _, b := range balls {
				w.WriteInt32(b.playerDBID)
			}
			return w.Bytes()
		}},
		{"empty_payload", false, func() []byte {
			return nil
		}},
	}

	fetchCount := func() int {
		resp := call(protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo, 6*time.Second)
		if resp == nil {
			return -1
		}
		rr := protocol.NewReader(resp.Payload)
		_, _ = rr.ReadInt16()
		_, _ = rr.ReadInt16()
		_, _ = rr.ReadUint8()
		for i := 0; i < 3; i++ {
			_, _ = rr.ReadInt16()
		}
		n, err := rr.ReadUint16()
		if err != nil {
			return -1
		}
		return int(n)
	}

	for _, v := range variants {
		slog.Info("尝试收取编码", "variant", v.name, "prev_act", v.prevAct)
		pkt := protocol.NewPacket(protocol.ActionIDDragonballCollect, v.build())
		pkt.HasPrevAct = v.prevAct
		resp := call(pkt, protocol.ActionIDDragonballCollect, 8*time.Second)
		if resp == nil {
			continue
		}
		code, ids, pErr := protocol.ParseDragonballCollectResult(resp.Payload)
		if pErr != nil {
			slog.Warn("解析收取回包失败", "variant", v.name, "err", pErr)
			continue
		}
		remain := fetchCount()
		slog.Info("收取结果", "variant", v.name, "result", code, "collected", ids,
			"remain_altar", remain, "hex", fmt.Sprintf("%x", resp.Payload))
		if remain >= 0 && remain < len(balls) {
			slog.Info("临时格龙珠已减少，命中可用编码", "variant", v.name, "before", len(balls), "after", remain)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 逐颗单独收取：批次中只要混有失效记录，服务端会整批返回 INVALID_DRAGONBALL，
	// 因此必须逐颗发送并跳过失效记录。
	fetchBalls := func() []ball {
		resp := call(protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo, 6*time.Second)
		if resp == nil {
			return nil
		}
		rr := protocol.NewReader(resp.Payload)
		_, _ = rr.ReadInt16()
		_, _ = rr.ReadInt16()
		_, _ = rr.ReadUint8()
		for i := 0; i < 3; i++ {
			_, _ = rr.ReadInt16()
		}
		n, err := rr.ReadUint16()
		if err != nil {
			return nil
		}
		out := make([]ball, 0, n)
		for i := 0; i < int(n); i++ {
			var b ball
			b.playerDBID, _ = rr.ReadInt32()
			b.dragonball, _ = rr.ReadInt16()
			b.level, _ = rr.ReadUint8()
			out = append(out, b)
		}
		return out
	}

	current := fetchBalls()
	collectedCount, invalidCount := 0, 0
	for _, b := range current {
		if ctx.Err() != nil {
			return
		}
		w := protocol.NewWriter()
		w.WriteUint16(1)
		w.WriteInt32(b.playerDBID)
		pkt := protocol.NewPacket(protocol.ActionIDDragonballCollect, w.Bytes())
		resp := call(pkt, protocol.ActionIDDragonballCollect, 8*time.Second)
		if resp == nil {
			continue
		}
		code, ids, pErr := protocol.ParseDragonballCollectResult(resp.Payload)
		if pErr != nil {
			slog.Warn("解析单颗收取回包失败", "player_d_b_id", b.playerDBID, "err", pErr)
			continue
		}
		switch code {
		case protocol.DragonballResultSuccess, protocol.DragonballResultPackageFull:
			collectedCount++
			slog.Info("单颗收取成功", "player_d_b_id", b.playerDBID,
				"d_b_id", b.dragonball, "lv", b.level, "collected", ids)
		case protocol.DragonballResultInvalidDragonball:
			invalidCount++
			slog.Info("单颗收取跳过失效记录", "player_d_b_id", b.playerDBID, "d_b_id", b.dragonball)
		default:
			slog.Info("单颗收取未成功", "player_d_b_id", b.playerDBID, "result", code)
		}
		time.Sleep(200 * time.Millisecond)
	}
	slog.Info("逐颗收取完成", "before", len(current), "collected", collectedCount,
		"invalid", invalidCount, "remain", fetchCount())
	if collectedCount > 0 {
		return
	}

	slog.Warn("所有编码均未被接受")
}