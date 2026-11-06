//go:build probe

// 目标协议: 法力星图 (Mod_StarPicture_Base, Module 485) 免费占星与活跃度验证
//
// 官方 get_info response:
//   [Int has_divination_count, Int free_divination_count, Int ingot_divination_count,
//    Int chip_count, Int reset_count, Int cur_deploy_level,
//    [Short,Int,Int,Short] award_list, Byte, Byte, Byte, Byte]
//
// divination(Act 1) 无请求参数；服务端按 free_divination_count -> ingot 扣费。
// 本探针仅在 free_divination_count>0 时占星一次，绝不触发元宝档。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_star_picture_live.go
// 预期表现: 打印免费占星次数；占星后活跃度 +10；元宝 delta=0。
package main

import (
	"context"
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
	actionStarPictureInfo  uint32 = 0x01E50000
	actionStarPictureDivin uint32 = 0x01E50001

	starPictureSucceed uint8 = 0
)

func dailyActivity(sess *client.RoleSession, ctx context.Context) (int, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	if err != nil || resp == nil {
		return 0, false
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
	earned := 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
	}
	return earned, true
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

	infoPkt := protocol.NewPacket(actionStarPictureInfo, []byte{0, 0, 0, 0})
	infoPkt.HasPrevAct = true
	infoResp := call(infoPkt, actionStarPictureInfo, 8*time.Second)
	if infoResp == nil {
		slog.Error("查询法力星图失败")
		return
	}
	r := protocol.NewReader(infoResp.Payload)
	vals := make([]int32, 6)
	for i := range vals {
		v, err := r.ReadInt32()
		if err != nil {
			slog.Error("解析法力星图字段失败", "err", err, "hex", fmt.Sprintf("%x", infoResp.Payload))
			return
		}
		vals[i] = v
	}
	slog.Info("法力星图面板",
		"has_divination", vals[0],
		"free_divination", vals[1],
		"ingot_divination", vals[2],
		"chip_count", vals[3],
		"reset_count", vals[4],
		"deploy_level", vals[5],
	)

	if vals[1] <= 0 {
		slog.Info("今日无免费占星次数，跳过 (绝不使用元宝档)")
		return
	}

	before := sess.GetPlayerState()
	beforeAct, _ := dailyActivity(sess, ctx)

	divPkt := protocol.NewPacket(actionStarPictureDivin, []byte{0, 0, 0, 0})
	divPkt.HasPrevAct = true
	divResp := call(divPkt, actionStarPictureDivin, 10*time.Second)
	if divResp == nil {
		slog.Error("占星无回包")
		return
	}
	if len(divResp.Payload) == 0 {
		slog.Error("占星回包为空")
		return
	}
	result := divResp.Payload[0]
	slog.Info("占星结果", "result", result, "hex", fmt.Sprintf("%x", divResp.Payload))

	after := sess.GetPlayerState()
	afterAct, _ := dailyActivity(sess, ctx)
	slog.Info("法力星图完成",
		"success", result == starPictureSucceed,
		"daily_before", beforeAct,
		"daily_after", afterAct,
		"activity_delta", afterAct-beforeAct,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}