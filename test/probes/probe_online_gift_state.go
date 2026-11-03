//go:build probe

// 目标协议: [Module 0, Action 3] update_player_data 中的在线礼包状态属性
//
//	[Module 0, Action 31] get_player_current_online_gift 查询下一份在线礼包
//	[Module 0, Action 30] player_get_online_gift 领取在线礼包
//
// 基准对齐: 官方客户端 Main.swf com.datas.PlayerData / com.views.ActivitiesView:
//   - 在线礼包状态来自 update_player_data 属性 81 (ONLINE_GIFT, 下次可领取时间戳)
//     与属性 82 (GET_ONLINE_GIFT, 0=倒计时中 / 1=可领取 / -1=无礼包);
//   - player_get_online_gift 结果码: 97 ACTION_SUCCESS / 98 ALREADY_GET /
//     100 ACTION_FAILED (时间未到) / 101 PACK_FULL。
//
// 运行命令: go run -tags=probe test/probes/probe_online_gift_state.go
// 预期表现: 打印登录期间收到的全部属性更新, 重点展示 81/82 的原始值; 随后查询并尝试领取。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDPlayerUpdateData {
			return
		}
		props, ok := protocol.ParsePlayerUpdateData(pkt.Payload)
		fmt.Printf("\n[update_player_data] ok=%v len=%d payload=%s\n", ok, len(pkt.Payload), hex.EncodeToString(pkt.Payload))
		for _, p := range props {
			mark := ""
			if p.Prop == 81 || p.Prop == 82 {
				mark = "  <== ONLINE_GIFT STATE"
			}
			fmt.Printf("    prop=%d value=int32=%d%s\n", p.Prop, p.Value, mark)
		}
	})

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
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	st := sess.GetPlayerState()
	fmt.Printf("\n角色: %s Lv.%d 体力=%d\n", sess.OriginNickName(), st.Level, st.Stamina)

	qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
	qresp, qerr := sess.Call(qctx, protocol.BuildOnlineGiftCurrentPacket(), protocol.ActionIDPlayerGetCurrentOnlineGift)
	qcancel()
	if qerr != nil {
		fmt.Printf("查询失败: %v\n", qerr)
	} else {
		cur, perr := protocol.ParseOnlineGiftCurrentResult(qresp.Payload)
		fmt.Printf("查询回包 payload=%s err=%v parsed=%+v\n", hex.EncodeToString(qresp.Payload), perr, cur)
	}

	cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
	cresp, cerr := sess.Call(cctx, protocol.BuildOnlineGiftClaimPacket(), protocol.ActionIDPlayerGetOnlineGift)
	ccancel()
	if cerr != nil {
		fmt.Printf("领取失败: %v\n", cerr)
	} else {
		claim, perr := protocol.ParseOnlineGiftClaimResult(cresp.Payload)
		fmt.Printf("领取回包 payload=%s err=%v parsed=%+v\n", hex.EncodeToString(cresp.Payload), perr, claim)
	}
}
