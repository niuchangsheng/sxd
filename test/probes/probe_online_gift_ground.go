//go:build probe

// 目标协议: [Module 0, Action 31] get_player_current_online_gift 查询在线礼包状态
//
//	[Module 0, Action 30] player_get_online_gift 领取在线礼包
//
// 基准对齐: 原版客户端 Main.swf(frame1 ABC) Mod_Player_Base.cinit 协议描述表:
//   - get_player_current_online_gift -> module=0, action=31, request=[], response=[IntUtil, [IntUtil,IntUtil,IntUtil]]
//   - player_get_online_gift         -> module=0, action=30, request=[], response=[UByteUtil, [IntUtil,IntUtil,IntUtil], IntUtil]
//
// 运行命令: go run -tags=probe test/probes/probe_online_gift_ground.go
// 预期表现: 登录 s0 -> 查询返回 (time_stamp, gift_list) -> 领取返回 (result, gift_list, next_time_stamp)，
//           同时打印领取前后铜钱/元宝快照，确认服务端是否真实发放奖励。
//
// 持续观测: ONLINE_GIFT_WATCH_SECONDS=600 时保持会话在线, 每 60 秒重发一次查询与领取,
//           用于验证在线时长累积后礼包状态是否从未激活转为可领取。
package main

import (
	"context"
	"encoding/hex"
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
	actionIDGetCurrentOnlineGift uint32 = 0x0000001F // Module 0, Action 31
	actionIDPlayerGetOnlineGift  uint32 = 0x0000001E // Module 0, Action 30
)

func main() {
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

	watchSeconds, _ := strconv.Atoi(os.Getenv("ONLINE_GIFT_WATCH_SECONDS"))
	if watchSeconds < 0 {
		watchSeconds = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(120+watchSeconds+60)*time.Second)
	defer cancel()

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
		MaxReconnectAttempts: 0,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		fmt.Printf("会话未能激活: state=%s\n", sess.State())
		return
	}

	sniff := func(tag string) func(*protocol.Packet) {
		return func(p *protocol.Packet) {
			fmt.Printf("[%s] <<< IN(%s) action=0x%08X len=%d payload=%s\n",
				time.Now().Format("15:04:05.000"), tag, p.ActionID, len(p.Payload),
				hex.EncodeToString(p.Payload))
		}
	}
	sess.RegisterHandler(actionIDGetCurrentOnlineGift, sniff("current"))
	sess.RegisterHandler(actionIDPlayerGetOnlineGift, sniff("claim"))

	st := sess.GetPlayerState()
	fmt.Printf("\n角色: %s (Lv.%d) 区服: %s\n", sess.OriginNickName(), st.Level, role.ServerID)
	fmt.Printf("领取前资产: 铜钱=%d 元宝=%d\n", st.Coins, st.Ingots)
	fmt.Printf("============================================================\n")

	watchDeadline := time.Now().Add(time.Duration(watchSeconds) * time.Second)
	for round := 1; ; round++ {
		fmt.Printf("---- 第 %d 轮查询/领取 %s ----\n", round, time.Now().Format("15:04:05"))

		// 1. 查询当前在线礼包状态
		query := protocol.NewPacket(actionIDGetCurrentOnlineGift, nil)
		qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
		fmt.Printf("[%s] >>> 查询 action=0x%08X payload=%s\n",
			time.Now().Format("15:04:05.000"), query.ActionID, hex.EncodeToString(query.Payload))
		qresp, qerr := sess.Call(qctx, query, actionIDGetCurrentOnlineGift)
		qcancel()
		if qerr != nil {
			fmt.Printf("查询失败: %v (state=%s)\n", qerr, sess.State())
		} else {
			fmt.Printf("[%s] <<< 查询回包 len=%d hex=%s\n",
				time.Now().Format("15:04:05.000"), len(qresp.Payload), hex.EncodeToString(qresp.Payload))
			parseCurrentOnlineGift(qresp.Payload)
		}

		// 2. 领取在线礼包（原版客户端请求体为空）
		claim := protocol.NewPacket(actionIDPlayerGetOnlineGift, nil)
		cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
		fmt.Printf("[%s] >>> 领取 action=0x%08X payload=%s\n",
			time.Now().Format("15:04:05.000"), claim.ActionID, hex.EncodeToString(claim.Payload))
		cresp, cerr := sess.Call(cctx, claim, actionIDPlayerGetOnlineGift)
		ccancel()
		if cerr != nil {
			fmt.Printf("领取失败: %v (state=%s)\n", cerr, sess.State())
		} else {
			fmt.Printf("[%s] <<< 领取回包 len=%d hex=%s\n",
				time.Now().Format("15:04:05.000"), len(cresp.Payload), hex.EncodeToString(cresp.Payload))
			parseClaimOnlineGift(cresp.Payload)
		}

		remaining := time.Until(watchDeadline)
		if remaining <= 0 {
			break
		}
		if remaining > time.Minute {
			remaining = time.Minute
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(remaining):
		}
		if ctx.Err() != nil {
			break
		}
	}

	time.Sleep(1500 * time.Millisecond)
	after := sess.GetPlayerState()
	fmt.Printf("领取后资产: 铜钱=%d 元宝=%d 最终 state=%s\n", after.Coins, after.Ingots, sess.State())
	fmt.Printf("============================================================\n\n")
}

// parseCurrentOnlineGift 解析 Action 31 回包: int32 time_stamp + uint16 count + count*(int32 type,int32 item_id,int32 value)。
func parseCurrentOnlineGift(payload []byte) {
	r := protocol.NewReader(protocol.DecompressPayloadIfNeeded(payload))
	ts, err := r.ReadInt32()
	if err != nil {
		fmt.Printf("    [parse] 读取 time_stamp 失败: %v\n", err)
		return
	}
	count, err := r.ReadUint16()
	if err != nil {
		fmt.Printf("    [parse] time_stamp=%d 读取列表长度失败: %v\n", ts, err)
		return
	}
	fmt.Printf("    [parse] time_stamp=%d gift_count=%d\n", ts, count)
	for i := 0; i < int(count); i++ {
		typ, e1 := r.ReadInt32()
		itemID, e2 := r.ReadInt32()
		value, e3 := r.ReadInt32()
		if e1 != nil || e2 != nil || e3 != nil {
			fmt.Printf("    [parse] 第 %d 条记录读取失败\n", i)
			return
		}
		fmt.Printf("    [parse] gift[%d] type=%d item_id=%d value=%d\n", i, typ, itemID, value)
	}
}

// parseClaimOnlineGift 解析 Action 30 回包: uint8 result + uint16 count + count*(int32,int32,int32) + int32 next_time_stamp。
func parseClaimOnlineGift(payload []byte) {
	r := protocol.NewReader(protocol.DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		fmt.Printf("    [parse] 读取 result 失败: %v\n", err)
		return
	}
	count, err := r.ReadUint16()
	if err != nil {
		fmt.Printf("    [parse] result=%d 读取列表长度失败: %v\n", result, err)
		return
	}
	fmt.Printf("    [parse] result=%d gift_count=%d\n", result, count)
	for i := 0; i < int(count); i++ {
		typ, e1 := r.ReadInt32()
		itemID, e2 := r.ReadInt32()
		value, e3 := r.ReadInt32()
		if e1 != nil || e2 != nil || e3 != nil {
			fmt.Printf("    [parse] 第 %d 条记录读取失败\n", i)
			return
		}
		fmt.Printf("    [parse] gift[%d] type=%d item_id=%d value=%d\n", i, typ, itemID, value)
	}
	if nextTS, e := r.ReadInt32(); e == nil {
		fmt.Printf("    [parse] next_time_stamp=%d\n", nextTS)
	}
}