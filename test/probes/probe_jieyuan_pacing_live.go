//go:build probe

// 目标: 验证结缘宝箱 528/1 上下文握手对请求间隔的敏感度。
//
// 抓包顺序 (TCP 段长度):
//
//	14B C→S : 8B 帧头 + [07 04 + Uint32 PrevAct]      (528/1 握手)
//	 9B S→C : 握手结果
//	20B C→S : 8B 帧头 + [Int item_id, Int count, Uint32 PrevAct=0x02100001]
//	53B S→C : 开箱结果
//
// 本探针在 s1（有 3937 × 10）上严格按此顺序发送并打印回包。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_jieyuan_pacing_live.go -role 1
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

const (
	actionList    uint32 = 0x00E8001E // 232/30
	actionContext uint32 = 0x02100001 // 528/1
	actionOpen    uint32 = 0x00E80022 // 232/34
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	gap := flag.Duration("gap", 800*time.Millisecond, "列表与握手之间的间隔")
	flag.Parse()
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
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

	call := func(payload []byte, act uint32, name string) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, protocol.NewPacket(act, payload), act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 60 {
			limit = 60
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	// 0. 检查结缘功能 (FunctionType.LinkFateBox = 140) 是否开放。
	if isOpen, fErr := sess.IsFunctionOpen(ctx, 140); fErr != nil {
		slog.Warn("功能 140 查询失败", "err", fErr)
	} else {
		slog.Info("功能 140 开放状态", "open", isOpen)
	}

	// 1. 列表：[PrevAct]（与线上例程一致）
	if call(protocol.BuildJieYuanBoxListPacket(protocol.ActionIDTownEnter).Payload, actionList, "232_30_list") == nil {
		return
	}

	// 2. 设置模块 (528/1 set_setting) 载荷形态对照。
	// 描述符 request=[Byte 设置项, Byte 值]；分别尝试裸 2 字节与追加 PrevAct 的 6 字节。
	variants := []struct {
		name    string
		payload []byte
	}{
		{"bare", []byte{7, 4}},
		{"prev_list", []byte{7, 4, 0x00, 0xE8, 0x00, 0x1E}},
		{"prev_zero", []byte{7, 4, 0, 0, 0, 0}},
		{"prev_context", []byte{7, 4, 0x02, 0x10, 0x00, 0x01}},
	}
	for _, v := range variants {
		time.Sleep(*gap)
		call(v.payload, actionContext, "528_1_"+v.name)
	}

	// 3. 枚举 232/34 的 [Int,Int] 组合，定位真正的参数语义。
	type pair struct{ a, b int32 }
	candidates := []pair{
		{protocol.ItemJieYuanBoxCommon, 4},
		{protocol.ItemJieYuanBoxCommon, 10},
		{protocol.ItemJieYuanBoxCommon, 1},
		{1, 4},
		{1, 10},
		{0, 4},
	}
	for _, c := range candidates {
		payload := protocol.NewWriter().WriteInt32(c.a).WriteInt32(c.b).WriteUint32(actionContext).Bytes()
		time.Sleep(*gap)
		if call(payload, actionOpen, fmt.Sprintf("232_34_%d_%d", c.a, c.b)) != nil {
			break
		}
	}

}
