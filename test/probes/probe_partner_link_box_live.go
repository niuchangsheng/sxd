//go:build probe

// 目标协议: 天缘录缘魂宝箱 (Mod_PartnerLink_Base, Module 290)
//
//	get_shopping_info (Act 2) response: [[UByte box_type, Int once_num, Int times,
//	    Int total_times, UByte money_type, Int money_num]]
//	  money_type: 8=道缘, 9=铜钱, 10=元宝 (Mod_PartnerLink_Base 常量)
//	buy_item (Act 3) request=[UByte box_type]，SUCCESS=2。
//
// 每日活跃度任务 55「每日开启魂缘宝箱」每次 +5，上限 9 次。
// 本探针只购买道缘/铜钱结算的宝箱，严格跳过元宝档。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_BOX_BUY=6 go run -tags=probe test/probes/probe_partner_link_box_live.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const (
	actionPartnerLinkShop uint32 = 0x01220002
	actionPartnerLinkBuy  uint32 = 0x01220003

	moneyDaoYuan uint8 = 8
	moneyCoin    uint8 = 9
	moneyIngot   uint8 = 10
	linkSuccess  uint8 = 2
)

type linkBox struct {
	boxType    uint8
	onceNum    int32
	times      int32
	totalTimes int32
	moneyType  uint8
	moneyNum   int32
}

func dailyActivity(sess *client.RoleSession, ctx context.Context) (int, int, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	if err != nil || resp == nil {
		return 0, 0, false
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
	earned, task55 := 0, 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
		if row[0] == 55 {
			task55 = int(row[2] - row[1])
		}
	}
	return earned, task55, true
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	want := 6
	if raw := os.Getenv("SXD_PROBE_BOX_BUY"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			want = v
		}
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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

	shopPkt := protocol.NewPacket(actionPartnerLinkShop, []byte{0, 0, 0, 0})
	shopPkt.HasPrevAct = true
	shopResp := call(shopPkt, actionPartnerLinkShop, 8*time.Second)
	if shopResp == nil {
		slog.Error("查询天缘录宝箱商店失败")
		return
	}
	r := protocol.NewReader(shopResp.Payload)
	n, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析宝箱数量失败", "err", err, "hex", fmt.Sprintf("%x", shopResp.Payload))
		return
	}
	boxes := make([]linkBox, 0, n)
	for i := 0; i < int(n); i++ {
		var b linkBox
		b.boxType, _ = r.ReadUint8()
		b.onceNum, _ = r.ReadInt32()
		b.times, _ = r.ReadInt32()
		b.totalTimes, _ = r.ReadInt32()
		b.moneyType, _ = r.ReadUint8()
		b.moneyNum, _ = r.ReadInt32()
		boxes = append(boxes, b)
	}
	slog.Info("天缘录宝箱商店", "count", len(boxes))
	var target *linkBox
	for i := range boxes {
		b := &boxes[i]
		slog.Info("宝箱", "box_type", b.boxType, "once_num", b.onceNum,
			"times", b.times, "total_times", b.totalTimes,
			"money_type", b.moneyType, "money_num", b.moneyNum)
		if b.moneyType == moneyIngot {
			continue
		}
		// 实测 times 为剩余可购次数 (times=0 时服务端返回 NOT_BUY_COUNT)。
		if b.times <= 0 {
			continue
		}
		if target == nil || b.moneyType == moneyCoin {
			target = b
		}
	}
	if target == nil {
		slog.Info("没有道缘/铜钱结算的可购宝箱，跳过 (不使用元宝)")
		return
	}

	_, remainTask, _ := dailyActivity(sess, ctx)
	if want > remainTask {
		want = remainTask
	}
	if int(target.times) < want {
		want = int(target.times)
	}
	slog.Info("开始购买缘魂宝箱", "box_type", target.boxType,
		"money_type", target.moneyType, "money_num", target.moneyNum, "count", want)

	before := sess.GetPlayerState()
	beforeAct, _, _ := dailyActivity(sess, ctx)
	bought := 0
	for bought < want {
		if ctx.Err() != nil {
			break
		}
		buyPkt := protocol.NewPacket(actionPartnerLinkBuy, []byte{target.boxType})
		resp := call(buyPkt, actionPartnerLinkBuy, 10*time.Second)
		if resp == nil || len(resp.Payload) == 0 {
			break
		}
		code := resp.Payload[0]
		slog.Info("购买缘魂宝箱", "box_type", target.boxType, "result", code)
		if code != linkSuccess {
			break
		}
		bought++
		time.Sleep(150 * time.Millisecond)
	}
	after := sess.GetPlayerState()
	afterAct, afterTask, _ := dailyActivity(sess, ctx)
	slog.Info("缘魂宝箱完成",
		"bought", bought,
		"task55_remain_after", afterTask,
		"daily_before", beforeAct,
		"daily_after", afterAct,
		"activity_delta", afterAct-beforeAct,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}