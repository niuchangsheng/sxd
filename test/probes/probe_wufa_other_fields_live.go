//go:build probe

// 目标: 验证悟法其他秘馆的道具映射 (冥想竹简 8358 / 冥想玄简 8700)
//
//	panel_info (658/0) 查看各秘馆与资源余量
//	draw       (658/1) request=[Int field_type, UByte draw_type]
//	每抽 +10 活跃度，任务 69 上限 20 次/日
//
// 依次尝试 field_type=1 与 7 单抽，观察资源扣减与活跃度变化。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_wufa_other_fields_live.go
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
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
	call := func(action uint32, payload []byte, prev bool) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = prev
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	act := func() (int, int) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if err != nil || resp == nil {
			return 0, -1
		}
		p, err := protocol.ParseGameAssistantInfo(resp.Payload)
		if err != nil {
			return 0, -1
		}
		return p.DailyTotal(), p.RemainingTimesFor(69)
	}
	before, remain := act()
	slog.Info("悟法前", "daily_total", before, "task69_remain", remain)

	info := call(protocol.ActionIDWuFaFieldInfo, []byte{0, 0, 0, 0}, true)
	if info == nil {
		slog.Error("面板无回包")
		return
	}
	if panel, perr := protocol.ParseWuFaPanel(info.Payload); perr == nil {
		for _, it := range panel.Items {
			slog.Info("资源", "item_id", it.ItemID, "num", it.ItemNum)
		}
	}
	for _, field := range []int32{1, 7} {
		if ctx.Err() != nil || remain <= 0 {
			break
		}
		w := protocol.NewWriter()
		w.WriteInt32(field)
		w.WriteUint8(0)
		resp := call(protocol.ActionIDWuFaDrawCard, w.Bytes(), false)
		if resp == nil {
			continue
		}
		result, perr := protocol.ParseWuFaDrawResult(resp.Payload)
		if perr != nil {
			slog.Warn("解析抽卡失败", "field", field, "err", perr)
			continue
		}
		time.Sleep(200 * time.Millisecond)
		cur, rem := act()
		slog.Info("抽卡", "field", field, "result", result.Result,
			"daily_total", cur, "task69_remain", rem, "delta", cur-before)
		// 重新拉面板查看资源变化
		if r2 := call(protocol.ActionIDWuFaFieldInfo, []byte{0, 0, 0, 0}, true); r2 != nil {
			if p2, e2 := protocol.ParseWuFaPanel(r2.Payload); e2 == nil {
				for _, it := range p2.Items {
					slog.Info("资源", "after_field", field, "item_id", it.ItemID, "num", it.ItemNum)
				}
			}
		}
	}
}
