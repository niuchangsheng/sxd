//go:build probe

// 目标协议: [Module 0, Action 31] get_player_current_online_gift 查询在线礼包状态
//
//	[Module 0, Action 30] player_get_online_gift 领取在线礼包
//
// 基准对齐: 官方客户端 Main.swf Mod_Player_Base.cinit 协议描述表; DataBase.netCall 会在
// 主服请求末尾自动追加 4 字节 PrevAct (上一请求的 Module+Action)。
//
// 与 probe_online_gift_ground.go 的差异:
//   - 查询请求携带 PrevAct (登录后最后动作 0x00000002);
//   - 领取请求携带 PrevAct = 0x0000001F (紧邻的查询动作), 模拟客户端原子链;
//   - 同一会话内先查询再领取, 验证服务端是否因 PrevAct 链不匹配返回 result=100。
//
// 运行命令: go run -tags=probe test/probes/probe_online_gift_prevact.go
// 预期表现: 打印两次请求的原始回包 Hex; 若领取结果由 100 变为其他值, 则确认 PrevAct 链是根因。
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

	// 1. 查询: 按客户端 netCall 契约在末尾追加 PrevAct (登录初始化后的最后动作)。
	prevAct := sess.LastActionID()
	queryPayload := protocol.NewWriter().WriteUint32(prevAct).Bytes()
	query := protocol.NewPacket(protocol.ActionIDPlayerGetCurrentOnlineGift, queryPayload)
	qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
	qresp, qerr := sess.Call(qctx, query, protocol.ActionIDPlayerGetCurrentOnlineGift)
	qcancel()
	if qerr != nil {
		fmt.Printf("查询失败: %v (state=%s)\n", qerr, sess.State())
		return
	}
	fmt.Printf("查询 prevAct=0x%08X\n  请求=%s\n  回包=%s\n",
		prevAct, hex.EncodeToString(queryPayload), hex.EncodeToString(qresp.Payload))

	// 2. 领取: PrevAct 固定为紧邻的查询动作 (0x0000001F)。
	claimPrevAct := protocol.ActionIDPlayerGetCurrentOnlineGift
	claimPayload := protocol.NewWriter().WriteUint32(claimPrevAct).Bytes()
	claim := protocol.NewPacket(protocol.ActionIDPlayerGetOnlineGift, claimPayload)
	cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
	cresp, cerr := sess.Call(cctx, claim, protocol.ActionIDPlayerGetOnlineGift)
	ccancel()
	if cerr != nil {
		fmt.Printf("领取失败: %v (state=%s)\n", cerr, sess.State())
		return
	}
	fmt.Printf("领取 prevAct=0x%08X\n  请求=%s\n  回包=%s\n",
		claimPrevAct, hex.EncodeToString(claimPayload), hex.EncodeToString(cresp.Payload))

	time.Sleep(1500 * time.Millisecond)
	st := sess.GetPlayerState()
	fmt.Printf("资产: 铜钱=%d 元宝=%d 体力=%d state=%s\n", st.Coins, st.Ingots, st.Stamina, sess.State())
}
