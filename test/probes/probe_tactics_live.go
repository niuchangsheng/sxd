//go:build probe

// 目标协议: 兵法 (Mod_Tactics_Base, Module 299) 小圣水购买与每日活跃度增量验证
//
// 基准对齐: 官方 com.datas.TacticsData.get_tactics_info 字段
//   [box_type, cost_type, cost_num, sheng_shui_num, buy_num, buy_limit, [list_exp]]
//   其中 box_type 0=小圣水(铜钱)、1=大圣水(元宝)；cost_type 2=铜钱、3=元宝。
//   buy_item(Act 2) request=[UByte box_type]，SUCCEED=7。
//
// 每日活跃度任务 54「兵法」：每次购买圣水 +5，上限 25 次 (官方 AssistantTypeData)。
// 本探针只购买小圣水 (铜钱)，永不购买大圣水 (元宝)。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_tactics_live.go
// 预期表现: 打印每个圣水礼盒的买价/已买/上限；买满活跃度上限后元宝 delta=0。
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
	actionTacticsInfo uint32 = 0x012B0000
	actionTacticsBuy  uint32 = 0x012B0002
	actionAssistant   uint32 = 0x0000002A

	tacticsSmallWater uint8 = 0
	tacticsCostCoin   uint8 = 2
	tacticsSucceed    uint8 = 7
	tacticsBuyLimit   uint8 = 14
	tacticsNoCoin     uint8 = 13
	tacticsNoOpen     uint8 = 8
)

type tacticsBox struct {
	boxType     uint8
	costType    uint8
	costNum     int32
	waterNum    int32
	buyNum      int32
	buyLimit    int32
	listExp     []int32
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

	// 解析 0x2A 的 active_degree_list，返回每日累计活跃度与任务 54 剩余次数。
	fetchActivity := func() (int, int, int, bool) {
		resp := call(protocol.NewPacket(actionAssistant, nil), actionAssistant, 8*time.Second)
		if resp == nil {
			return 0, 0, 0, false
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
		_ = readList() // back_times_list
		active := readList()
		earned, remain := 0, 0
		for _, row := range active {
			if row[0] > 83 {
				continue // 每周任务
			}
			earned += int(row[1] * row[3])
			if row[0] == 54 {
				remain = int(row[2] - row[1])
			}
		}
		return earned, remain, len(active), true
	}

	beforeEarned, remainTask, _, ok := fetchActivity()
	if !ok {
		slog.Error("抓取活跃度面板失败")
		return
	}
	slog.Info("兵法购买前活跃度", "daily_earned", beforeEarned, "task54_remain", remainTask)

	infoResp := call(protocol.BuildTacticsInfoPacket(sess.LastActionID()), actionTacticsInfo, 8*time.Second)
	if infoResp == nil {
		slog.Error("查询兵法面板无回包")
		return
	}
	slog.Info("兵法面板原始回包", "hex", hex.EncodeToString(infoResp.Payload))
	r := protocol.NewReader(infoResp.Payload)
	level, _ := r.ReadInt32()
	exp, _ := r.ReadInt32()
	boxCount, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析兵法礼盒数量失败", "err", err)
		return
	}
	boxes := make([]tacticsBox, 0, boxCount)
	for i := 0; i < int(boxCount); i++ {
		var b tacticsBox
		b.boxType, _ = r.ReadUint8()
		b.costType, _ = r.ReadUint8()
		b.costNum, _ = r.ReadInt32()
		b.waterNum, _ = r.ReadInt32()
		b.buyNum, _ = r.ReadInt32()
		b.buyLimit, _ = r.ReadInt32()
		n, _ := r.ReadUint16()
		for j := 0; j < int(n); j++ {
			v, _ := r.ReadInt32()
			b.listExp = append(b.listExp, v)
		}
		boxes = append(boxes, b)
	}
	slog.Info("兵法面板", "level", level, "exp", exp, "boxes", len(boxes))
	for _, b := range boxes {
		slog.Info("兵法礼盒",
			"box_type", b.boxType,
			"cost_type", b.costType,
			"cost_num", b.costNum,
			"water_num", b.waterNum,
			"buy_num", b.buyNum,
			"buy_limit", b.buyLimit,
		)
	}

	var small *tacticsBox
	for i := range boxes {
		if boxes[i].boxType == tacticsSmallWater && boxes[i].costType == tacticsCostCoin {
			small = &boxes[i]
			break
		}
	}
	if small == nil {
		slog.Warn("未找到铜钱小圣水礼盒，停止")
		return
	}
	canBuy := int(small.buyLimit - small.buyNum)
	if canBuy > remainTask {
		canBuy = remainTask
	}
	if canBuy <= 0 {
		slog.Info("小圣水购买次数或活跃度上限已用尽", "can_buy", canBuy)
		return
	}
	slog.Info("开始购买小圣水", "count", canBuy, "unit_cost", small.costNum)

	bought := 0
	// 先确定 buy_item 的线上线格式：官方客户端会追加 4 字节 PrevAct，
	// 但不同网关对 PrevAct/参数宽度的容忍度不同，逐一验证。
	type buyVariant struct {
		name    string
		payload []byte
		prevAct bool
	}
	var variants []buyVariant
	// A: 仅 UByte 参数，无 PrevAct
	variants = append(variants, buyVariant{"u8_no_prev", []byte{tacticsSmallWater}, false})
	// B: UByte + 动态 PrevAct
	{
		w := protocol.NewWriter()
		w.WriteUint8(tacticsSmallWater)
		w.WriteUint32(0)
		variants = append(variants, buyVariant{"u8_dynamic_prev", w.Bytes(), true})
	}
	// C: UByte + 显式 PrevAct=0x012B0000
	{
		w := protocol.NewWriter()
		w.WriteUint8(tacticsSmallWater)
		w.WriteUint32(actionTacticsInfo)
		variants = append(variants, buyVariant{"u8_explicit_prev", w.Bytes(), false})
	}
	// D: Int 参数 + 动态 PrevAct
	{
		w := protocol.NewWriter()
		w.WriteInt32(int32(tacticsSmallWater))
		w.WriteUint32(0)
		variants = append(variants, buyVariant{"i32_dynamic_prev", w.Bytes(), true})
	}

	workingIdx := -1
	for idx, v := range variants {
		if ctx.Err() != nil {
			return
		}
		slog.Info("尝试 buy_item 编码", "variant", v.name)
		pkt := protocol.NewPacket(actionTacticsBuy, v.payload)
		pkt.HasPrevAct = v.prevAct
		resp := call(pkt, actionTacticsBuy, 12*time.Second)
		if resp == nil {
			continue
		}
		if len(resp.Payload) == 0 {
			slog.Warn("buy_item 回包为空", "variant", v.name)
			continue
		}
		code := resp.Payload[0]
		slog.Info("buy_item 结果", "variant", v.name, "result", code, "hex", hex.EncodeToString(resp.Payload))
		if code == tacticsSucceed {
			workingIdx = idx
			bought++
			break
		}
	}

	if workingIdx < 0 {
		slog.Warn("未找到可用的 buy_item 编码，停止购买")
	} else {
		working := variants[workingIdx]
		slog.Info("命中 buy_item 编码", "variant", working.name, "already_bought", bought)
		for bought < canBuy {
			if ctx.Err() != nil {
				break
			}
			pkt := protocol.NewPacket(actionTacticsBuy, working.payload)
			pkt.HasPrevAct = working.prevAct
			resp := call(pkt, actionTacticsBuy, 10*time.Second)
			if resp == nil {
				break
			}
			if len(resp.Payload) == 0 {
				break
			}
			code := resp.Payload[0]
			switch code {
			case tacticsSucceed:
				bought++
			case tacticsBuyLimit:
				slog.Info("小圣水今日购买次数已用尽")
				bought = canBuy
			case tacticsNoCoin:
				slog.Info("铜钱不足，停止购买小圣水")
				bought = canBuy
			case tacticsNoOpen:
				slog.Info("兵法功能未开放，停止")
				bought = canBuy
			default:
				slog.Info("购买小圣水未成功", "result", code)
				bought = canBuy
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	slog.Info("小圣水购买完成", "bought", bought)

	afterEarned, afterRemain, _, ok := fetchActivity()
	if !ok {
		slog.Warn("抓取购买后活跃度失败")
		return
	}
	state := sess.GetPlayerState()
	slog.Info("兵法购买结果",
		"daily_before", beforeEarned,
		"daily_after", afterEarned,
		"daily_delta", afterEarned-beforeEarned,
		"task54_remain_before", remainTask,
		"task54_remain_after", afterRemain,
		"coins", state.Coins,
		"ingots", state.Ingots,
	)
}