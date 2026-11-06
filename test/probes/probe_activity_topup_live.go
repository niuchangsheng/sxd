//go:build probe

// 目标: 在不消耗元宝的前提下，补齐每日活跃度缺口 (目标 1200)
//
// 依次尝试已知可用的免费/铜钱玩法，每步执行后重新读取活跃度并打印增量：
//
//	猎命        Mod_Fate_Base (22)  get_fate_npc(0) + appoint_fate_npc(9)
//	命宫碎片    Mod_Fate_Base (22)  get_ming_gong_info(31) + exchange_ming_gong_scrap(34)
//	召唤仙桃    Mod_GetPeach_Base (40) call_peach(6)
//	五行天仪    Mod_Laba_Base (172)  laba_info(0) + laba_draw(1, 仅 free/coin 档)
//
// 任一时刻活跃度达到 1200 即停止；所有动作严格跳过元宝档。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_activity_topup_live.go
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
	actionFateNPC      uint32 = 0x00160000
	actionFateAppoint  uint32 = 0x00160009
	actionMingGongInfo uint32 = 0x0016001F
	actionMingGongEx   uint32 = 0x00160022
	actionPeachCall    uint32 = 0x00280006
	actionLabaInfo     uint32 = 0x00AC0000
	actionLabaDraw     uint32 = 0x00AC0001
	targetActivity            = 1200
)

type activitySnapshot struct {
	total  int
	remain map[int32]int
	ingots int64
	coins  int64
}

func readActivity(sess *client.RoleSession, ctx context.Context) (*activitySnapshot, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	if err != nil || resp == nil {
		return nil, false
	}
	r := protocol.NewReader(resp.Payload)
	for i := 0; i < 33; i++ {
		if i == 1 || i == 3 {
			_, _ = r.ReadInt64()
		} else {
			_, _ = r.ReadInt32()
		}
	}
	readList := func() [][4]int32 {
		n, _ := r.ReadUint16()
		out := make([][4]int32, 0, n)
		for i := 0; i < int(n); i++ {
			var row [4]int32
			for j := 0; j < 4; j++ {
				row[j], _ = r.ReadInt32()
			}
			out = append(out, row)
		}
		return out
	}
	_ = readList()
	active := readList()
	snap := &activitySnapshot{remain: make(map[int32]int)}
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		snap.total += int(row[1] * row[3])
		snap.remain[row[0]] = int(row[2] - row[1])
	}
	st := sess.GetPlayerState()
	snap.ingots = st.Ingots
	snap.coins = st.Coins
	return snap, true
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	snap, ok := readActivity(sess, ctx)
	if !ok {
		slog.Error("读取活跃度失败")
		return
	}
	start := snap.total
	slog.Info("起始状态", "daily_activity", start, "gap", targetActivity-start,
		"ingots", snap.ingots, "coins", snap.coins)

	report := func(stage string) {
		cur, ok2 := readActivity(sess, ctx)
		if !ok2 {
			slog.Warn("读取活跃度失败", "stage", stage)
			return
		}
		slog.Info("阶段小结", "stage", stage, "daily_activity", cur.total,
			"delta_total", cur.total-start,
			"coin_delta", cur.coins-snap.coins, "ingot_delta", cur.ingots-snap.ingots)
	}

	// 1. 猎命：先读猎命师列表，再按列表 id 依次尝试免费指定。
	if snap.remain[4] > 0 {
		fatePkt := protocol.NewPacket(actionFateNPC, []byte{0, 0, 0, 0})
		fatePkt.HasPrevAct = true
		if resp := call(fatePkt, actionFateNPC, 8*time.Second); resp != nil {
			r := protocol.NewReader(resp.Payload)
			free, _ := r.ReadUint8()
			n, _ := r.ReadUint16()
			ids := make([]uint8, 0, n)
			for i := 0; i < int(n); i++ {
				id, _ := r.ReadUint8()
				_, _ = r.ReadUint8()
				_, _ = r.ReadUint8()
				_, _ = r.ReadUint8()
				_, _ = r.ReadInt16()
				ids = append(ids, id)
			}
			tail, _ := r.ReadInt32()
			slog.Info("猎命师面板", "free_appoint", free, "ids", ids, "tail", tail)
			for _, id := range ids {
				if ctx.Err() != nil {
					break
				}
				apptResp := call(protocol.NewPacket(actionFateAppoint, []byte{id}), actionFateAppoint, 8*time.Second)
				if apptResp == nil || len(apptResp.Payload) == 0 {
					continue
				}
				slog.Info("猎命指定", "npc_id", id, "result", apptResp.Payload[0],
					"hex", hex.EncodeToString(apptResp.Payload))
				if apptResp.Payload[0] == 0 {
					break
				}
			}
		}
	}
	report("猎命")

	// 2. 命宫碎片：读取兑换信息后兑换 1 次铜钱档。
	cur, _ := readActivity(sess, ctx)
	if cur != nil && cur.remain[56] > 0 {
		infoPkt := protocol.NewPacket(actionMingGongInfo, []byte{0, 0, 0, 0})
		infoPkt.HasPrevAct = true
		if resp := call(infoPkt, actionMingGongInfo, 8*time.Second); resp != nil {
			slog.Info("命宫信息", "hex", hex.EncodeToString(resp.Payload))
			w := protocol.NewWriter()
			w.WriteInt32(1)
			exResp := call(protocol.NewPacket(actionMingGongEx, w.Bytes()), actionMingGongEx, 8*time.Second)
			if exResp != nil {
				slog.Info("命宫碎片兑换", "hex", hex.EncodeToString(exResp.Payload))
			}
		}
	}
	report("命宫碎片")

	// 3. 召唤仙桃。
	cur, _ = readActivity(sess, ctx)
	if cur != nil && cur.remain[64] > 0 {
		resp := call(protocol.NewPacket(actionPeachCall, nil), actionPeachCall, 8*time.Second)
		if resp != nil && len(resp.Payload) > 0 {
			slog.Info("召唤仙桃", "result", resp.Payload[0])
		}
	}
	report("召唤仙桃")

	// 4. 五行天仪：仅使用免费/铜钱档。
	cur, _ = readActivity(sess, ctx)
	if cur != nil && cur.total < targetActivity && cur.remain[41] > 0 {
		infoPkt := protocol.NewPacket(actionLabaInfo, []byte{0, 0, 0, 0})
		infoPkt.HasPrevAct = true
		infoResp := call(infoPkt, actionLabaInfo, 8*time.Second)
		if infoResp != nil {
			ir := protocol.NewReader(infoResp.Payload)
			free, _ := ir.ReadUint8()
			coin, _ := ir.ReadUint8()
			ingot, _ := ir.ReadUint8()
			tenIngot, _ := ir.ReadUint8()
			slog.Info("五行天仪面板", "free", free, "coin", coin,
				"ingot", ingot, "ten_ingot", tenIngot)
			for int(free)+int(coin) > 0 && ctx.Err() == nil {
				drawResp := call(protocol.NewPacket(actionLabaDraw, []byte{0}), actionLabaDraw, 10*time.Second)
				if drawResp == nil || len(drawResp.Payload) == 0 {
					break
				}
				dr := protocol.NewReader(drawResp.Payload)
				result, _ := dr.ReadUint8()
				free, _ = dr.ReadUint8()
				coin, _ = dr.ReadUint8()
				ingot, _ = dr.ReadUint8()
				slog.Info("五行天仪抽奖", "result", result, "free", free,
					"coin", coin, "ingot", ingot)
				if result != 2 {
					break
				}
				time.Sleep(150 * time.Millisecond)
			}
		}
	}
	report("五行天仪")

	final, ok := readActivity(sess, ctx)
	if !ok {
		return
	}
	slog.Info("收尾",
		"daily_activity", final.total,
		"target", targetActivity,
		"met", final.total >= targetActivity,
		"delta_total", final.total-start,
		"coin_delta", final.coins-snap.coins,
		"ingot_delta", final.ingots-snap.ingots,
	)
}
