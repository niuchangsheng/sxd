//go:build probe

// 目标协议: [Module 10, Action 22] faction_god_info + [Module 10, Action 23] incense
// 基准对齐: 对齐原版 Mod_Faction_Base 帮派祭神上香闭环，验证 2026 服务端 incense 请求载荷编码
// 运行命令: go run -tags=probe test/probes/probe_faction_blessing_live.go
// 运行命令 (诊断): go run -tags=probe test/probes/probe_faction_blessing_live.go -offering 99
// 预期表现: 先只读查询神像信息与祝福次数，再按多种候选载荷发送 incense 并打印服务端回包;
//
//	offering=99 等非法供品不会消耗真实上香次数，仅用于确认请求格式与响应动作号。
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	offering := flag.Int("offering", 99, "上香供品编号 (99 为诊断用非法供品, 不消耗次数)")
	timeout := flag.Duration("timeout", 4*time.Second, "每个候选载荷等待回包的超时时间")
	configPath := flag.String("config", "configs/config.yaml", "本地配置路径")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load(*configPath)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	for i := 0; i < 60; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("=== 会话已激活，开始帮派祭神协议探针 ===",
		"role_id", sess.RoleID(), "server_id", role.ServerID, "player_id", sess.PlayerID(), "offering", *offering)

	packets := make(chan *protocol.Packet, 64)
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		select {
		case packets <- pkt:
		default:
		}
	})

	// 1. 只读查询神像信息 (已知可用的基准动作)
	infoPkt := protocol.BuildFactionGodInfoPacket(sess.LastActionID())
	godInfo, err := callAndParse(ctx, sess, infoPkt, protocol.ActionIDFactionGodInfo, *timeout)
	if err != nil {
		slog.Error("查询神像信息失败", "err", err)
		return
	}
	slog.Info("神像信息", "payload", hex.EncodeToString(godInfo), "remain", hex.EncodeToString(godInfo))

	// 2. 只读查询祝福次数 (验证 action 24 编号是否正确)
	countPkt := protocol.BuildFactionGetBlessingCountPacket(sess.LastActionID())
	countPayload, err := callAndParse(ctx, sess, countPkt, protocol.ActionIDFactionGetBlessingCount, *timeout)
	if err != nil {
		slog.Warn("查询祝福次数失败", "err", err)
	} else {
		slog.Info("祝福次数回包", "payload", hex.EncodeToString(countPayload))
	}

	// 3. 用多种候选载荷测试 incense，非法供品不会消耗真实次数
	variants := buildIncenseVariants(int32(*offering), protocol.ActionIDFactionGodInfo)
	for _, v := range variants {
		if sess.State() != client.StateActive {
			slog.Error("会话已断开，停止后续候选测试", "state", sess.State())
			return
		}

		drain(packets)
		pkt := protocol.NewPacket(protocol.ActionIDFactionIncense, v.payload)
		if err := sess.Send(pkt); err != nil {
			slog.Warn("发送候选载荷失败", "variant", v.name, "err", err)
			continue
		}
		slog.Info("已发送候选载荷",
			"variant", v.name,
			"payload_hex", hex.EncodeToString(v.payload),
			"payload_len", len(v.payload),
		)

		resp, ok := waitAnyPacket(packets, *timeout)
		if !ok {
			slog.Warn("候选载荷未收到任何回包", "variant", v.name, "timeout", timeout.String())
			continue
		}
		slog.Info("候选载荷收到回包",
			"variant", v.name,
			"action_id", fmt.Sprintf("0x%08X", resp.ActionID),
			"payload_hex", hex.EncodeToString(resp.Payload),
		)
		return
	}

	slog.Warn("所有候选载荷均未收到回包，incense 动作号或请求结构仍需进一步确认")
}

func callAndParse(ctx context.Context, sess *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout time.Duration) ([]byte, error) {
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := sess.Call(cCtx, pkt, respActionID)
	if err != nil {
		return nil, err
	}
	return resp.Payload, nil
}

type incenseVariant struct {
	name    string
	payload []byte
}

func buildIncenseVariants(offering int32, prevAct uint32) []incenseVariant {
	var (
		int32Buf = make([]byte, 4)
		int16Buf = make([]byte, 2)
		uint8Buf = make([]byte, 1)
		int64Buf = make([]byte, 8)
		prevBuf  = make([]byte, 4)
		zeroPrev = make([]byte, 4)
	)
	binary.BigEndian.PutUint32(int32Buf, uint32(offering))
	binary.BigEndian.PutUint16(int16Buf, uint16(offering))
	uint8Buf[0] = byte(offering)
	binary.BigEndian.PutUint64(int64Buf, uint64(offering))
	binary.BigEndian.PutUint32(prevBuf, prevAct)

	concat := func(parts ...[]byte) []byte {
		out := make([]byte, 0)
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	return []incenseVariant{
		{name: "int32 only (线上确认格式)", payload: concat(int32Buf)},
		{name: "int32+prevAct (旧实现)", payload: concat(int32Buf, prevBuf)},
		{name: "int16+prevAct", payload: concat(int16Buf, prevBuf)},
		{name: "uint8+prevAct", payload: concat(uint8Buf, prevBuf)},
		{name: "prevAct+int32", payload: concat(prevBuf, int32Buf)},
		{name: "int64+prevAct", payload: concat(int64Buf, prevBuf)},
		{name: "int32+zeroPrev", payload: concat(int32Buf, zeroPrev)},
	}
}

func drain(ch <-chan *protocol.Packet) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitAnyPacket(ch <-chan *protocol.Packet, timeout time.Duration) (*protocol.Packet, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case pkt := <-ch:
			// 忽略心跳等非业务回包
			if pkt.ActionID == protocol.ActionHeartbeat {
				continue
			}
			return pkt, true
		case <-timer.C:
			return nil, false
		}
	}
}
