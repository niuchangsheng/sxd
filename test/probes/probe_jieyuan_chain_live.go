//go:build probe

// 目标: 验证结缘宝箱 528/1 / 232/34 的 PrevAct 链锚点。
//
// 官方客户端 DataBase.netCall 对每个请求自动追加「上一条请求的
// [Module,Action]」作为 4 字节尾链 (tmpBuff)，见反编译
// decompiled/scripts/com/DataBase.as:255-270。结缘抓包
// /tmp/jieyuan2.pcapng 中 528/1 = [07 04 + 上一条请求 232/30]，
// 232/34 = [item, autoMerge, 528/1]。
//
// 本探针对照三种链路：
//
//	A: 官方链 —— 列表 → 528/1(prev=上一条发送 232/30) → 232/34(prev=528/1)
//	B: 中间插入背包查询 —— 列表 → 2/41 → 528/1(prev=2/41) → 232/34
//	C: 旧实现 —— 528/1 固定 prev=232/30（链路已错位）后开箱
//
// 运行: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_jieyuan_chain_live.go -case A
package main

import (
	"context"
	"encoding/binary"
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
	actList uint32 = 0x00E8001E
	actCtx  uint32 = 0x02100001
	actOpen uint32 = 0x00E80022
	actPack uint32 = 0x00020029
	itemID  int32  = 3937
)

func be(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func main() {
	scenario := flag.String("case", "A", "A|B|C")
	item := flag.Int("item", int(itemID), "box item id")
	flag.Parse()
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(200 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活")
		return
	}
	slog.Info("会话激活", "role", sess.RoleID())

	call := func(pkt *protocol.Packet, want uint32, name string) (*protocol.Packet, error) {
		c, cc := context.WithTimeout(ctx, 6*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, pkt, want)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr, "sent_prev", hex.EncodeToString(pkt.Payload))
			return nil, cErr
		}
		limit := len(resp.Payload)
		if limit > 80 {
			limit = 80
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp, nil
	}

	count := func(tag string) int32 {
		resp, err := call(protocol.NewPacket(actList, be(sess.LastActionID())), actList, "list_"+tag)
		if err != nil {
			return -1
		}
		boxes, _ := protocol.ParseJieYuanBoxListResult(resp.Payload)
		var n int32 = -1
		for _, b := range boxes {
			if b.ItemID == int32(*item) {
				n = b.Count
			}
		}
		slog.Info("库存", "tag", tag, "item", *item, "count", n)
		return n
	}

	if c := count("before"); c <= 0 {
		slog.Warn("该箱型无库存，终止")
		return
	}

	switch *scenario {
	case "A":
		// 官方链：列表回包后，prev=列表 (232/30)。
		prev := sess.LastActionID()
		ctxPkt := protocol.NewPacket(actCtx, append([]byte{0x07, 0x04}, be(prev)...))
		slog.Info("528/1", "prev", fmt.Sprintf("0x%08X", prev))
		if _, err := call(ctxPkt, actCtx, "ctx"); err != nil {
			return
		}
		prevOpen := sess.LastActionID()
		openPkt := protocol.NewPacket(actOpen,
			append(append(be(uint32(*item)), be(4)...), be(prevOpen)...))
		slog.Info("232/34", "prev", fmt.Sprintf("0x%08X", prevOpen))
		call(openPkt, actOpen, "open")
	case "B":
		// 中间插入 2/41 背包查询，528/1 的 prev 跟随到最后一条请求。
		call(protocol.NewPacket(actPack, be(sess.LastActionID())), actPack, "pack")
		prev := sess.LastActionID()
		ctxPkt := protocol.NewPacket(actCtx, append([]byte{0x07, 0x04}, be(prev)...))
		slog.Info("528/1", "prev", fmt.Sprintf("0x%08X", prev))
		if _, err := call(ctxPkt, actCtx, "ctx"); err != nil {
			return
		}
		prevOpen := sess.LastActionID()
		openPkt := protocol.NewPacket(actOpen,
			append(append(be(uint32(*item)), be(4)...), be(prevOpen)...))
		slog.Info("232/34", "prev", fmt.Sprintf("0x%08X", prevOpen))
		call(openPkt, actOpen, "open")
	case "C":
		// 旧实现：528/1 固定 232/30，随后 232/34。
		ctxPkt := protocol.NewPacket(actCtx, append([]byte{0x07, 0x04}, be(actList)...))
		slog.Info("528/1", "prev", fmt.Sprintf("0x%08X", actList), "last_sent", fmt.Sprintf("0x%08X", sess.LastActionID()))
		if _, err := call(ctxPkt, actCtx, "ctx"); err != nil {
			// 模拟例程兜底：裸 2 字节
			call(protocol.NewPacket(actCtx, []byte{0x07, 0x04}), actCtx, "ctx_bare")
		}
		prevOpen := sess.LastActionID()
		openPkt := protocol.NewPacket(actOpen,
			append(append(be(uint32(*item)), be(4)...), be(prevOpen)...))
		slog.Info("232/34", "prev", fmt.Sprintf("0x%08X", prevOpen))
		call(openPkt, actOpen, "open")
	}

	time.Sleep(500 * time.Millisecond)
	count("after")
}
