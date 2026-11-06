//go:build probe

// 目标协议: 五行天仪 (Mod_Laba_Base, Module 172) 免费/铜钱抽奖与活跃度验证
//
// 官方 LabaData 字段:
//
//	laba_info: [Byte free_count, Byte coin_count, Byte ingot_count, Byte ten_ingot_count,
//	            Int score, [Byte,Byte] scraps, Int proficiency]
//	draw(Act 1) request=[UByte type(0=ONE,1=TEN)]，SUCCESS=2。
//
// 服务端按 free_count -> coin_count -> ingot_count 顺序扣费。本探针仅在
// free_count+coin_count>0 时抽 ONE，绝不触发 ingot_count，避免消耗元宝。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_LABA_DRAWS=6 go run -tags=probe test/probes/probe_laba_live.go
// 预期表现: 打印每抽结果与剩余次数；元宝 delta=0；活跃度任务 41 每抽 +5。
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
	actionLabaInfo uint32 = 0x00AC0000
	actionLabaDraw uint32 = 0x00AC0001

	labaOne     uint8 = 0
	labaSuccess uint8 = 2
)

type labaInfo struct {
	freeCount      uint8
	coinCount      uint8
	ingotCount     uint8
	tenIngotCount  uint8
	score          int32
	proficiency    int32
}

func parseLabaInfo(payload []byte) (*labaInfo, error) {
	r := protocol.NewReader(payload)
	info := &labaInfo{}
	var err error
	if info.freeCount, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if info.coinCount, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if info.ingotCount, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if info.tenIngotCount, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if info.score, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	n, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(n); i++ {
		_, _ = r.ReadUint8()
		_, _ = r.ReadUint8()
	}
	if info.proficiency, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return info, nil
}

func parseDrawState(payload []byte) (uint8, *labaInfo, error) {
	r := protocol.NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, err
	}
	if result != labaSuccess {
		return result, nil, nil
	}
	// [[Byte]]
	outer, err := r.ReadUint16()
	if err != nil {
		return 0, nil, err
	}
	for i := 0; i < int(outer); i++ {
		inner, _ := r.ReadUint16()
		for j := 0; j < int(inner); j++ {
			_, _ = r.ReadUint8()
		}
	}
	info := &labaInfo{}
	if info.score, err = r.ReadInt32(); err != nil {
		return 0, nil, err
	}
	if info.freeCount, err = r.ReadUint8(); err != nil {
		return 0, nil, err
	}
	if info.coinCount, err = r.ReadUint8(); err != nil {
		return 0, nil, err
	}
	if info.ingotCount, err = r.ReadUint8(); err != nil {
		return 0, nil, err
	}
	if info.tenIngotCount, err = r.ReadUint8(); err != nil {
		return 0, nil, err
	}
	n, err := r.ReadUint16()
	if err != nil {
		return 0, nil, err
	}
	for i := 0; i < int(n); i++ {
		_, _ = r.ReadUint8()
		_, _ = r.ReadUint8()
	}
	if info.proficiency, err = r.ReadInt32(); err != nil {
		return 0, nil, err
	}
	return result, info, nil
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	draws := 6
	if raw := os.Getenv("SXD_PROBE_LABA_DRAWS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			draws = v
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

	infoPkt := protocol.NewPacket(actionLabaInfo, []byte{0, 0, 0, 0})
	infoPkt.HasPrevAct = true
	infoResp := call(infoPkt, actionLabaInfo, 8*time.Second)
	if infoResp == nil {
		slog.Error("查询五行天仪失败")
		return
	}
	info, err := parseLabaInfo(infoResp.Payload)
	if err != nil {
		slog.Error("解析五行天仪失败", "err", err, "hex", fmt.Sprintf("%x", infoResp.Payload))
		return
	}
	slog.Info("五行天仪面板",
		"free", info.freeCount, "coin", info.coinCount,
		"ingot", info.ingotCount, "ten_ingot", info.tenIngotCount,
		"score", info.score, "proficiency", info.proficiency,
	)

	before := sess.GetPlayerState()
	done := 0
	for done < draws {
		if ctx.Err() != nil {
			break
		}
		if int(info.freeCount)+int(info.coinCount) <= 0 {
			slog.Info("免费/铜钱抽奖次数已用尽，停止 (绝不使用元宝档)")
			break
		}
		drawPkt := protocol.NewPacket(actionLabaDraw, []byte{labaOne})
		resp := call(drawPkt, actionLabaDraw, 10*time.Second)
		if resp == nil {
			break
		}
		result, next, pErr := parseDrawState(resp.Payload)
		if pErr != nil {
			slog.Warn("解析抽奖回包失败", "err", pErr, "hex", fmt.Sprintf("%x", resp.Payload))
			break
		}
		if result != labaSuccess {
			slog.Info("抽奖未成功", "result", result)
			break
		}
		done++
		info = next
		slog.Info("五行天仪抽奖成功",
			"draw", done, "free", info.freeCount, "coin", info.coinCount,
			"ingot", info.ingotCount, "score", info.score)
		time.Sleep(150 * time.Millisecond)
	}

	after := sess.GetPlayerState()
	slog.Info("五行天仪完成",
		"draws", done,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}