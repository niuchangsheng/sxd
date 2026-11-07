//go:build probe

// 目标: 确认摘仙桃第二棵桃树返回 FAILURE(3) 的原因，以及清空仙桃后
// call_peach 是否解锁。
//
// 官方客户端在每次 get_peach 前调用:
//
//	Mod_Player_Base.get_player_war_cd_time (0/41) request=[Int type]
//	WarCdTimeType.Peach = 17
//
// 本探针依次打印：摘取前 CD、peach_info、摘取结果、摘取后 CD、peach_info、
// player_info（7 字段按官方 controller 顺序解析）。只执行免费摘取，不召唤。
//
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_peach_war_cd_live.go
package main

import (
	"context"
	"encoding/hex"
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
	actionWarCdTime    uint32 = 0x00000029
	actionPeachInfo    uint32 = 0x00280003
	actionPeachPlayer  uint32 = 0x00280004
	actionPeachHarvest uint32 = 0x00280001
	warCdTypePeach     int32  = 17
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s1.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	queryCD := func(label string) int32 {
		w := protocol.NewWriter()
		w.WriteInt32(warCdTypePeach)
		w.WriteUint32(sess.LastActionID())
		pkt := protocol.NewPacket(actionWarCdTime, w.Bytes())
		pkt.HasPrevAct = true
		resp := call(pkt, actionWarCdTime, 6*time.Second)
		if resp == nil || len(resp.Payload) < 4 {
			slog.Info(label, "cd", -1, "raw", "no response")
			return -1
		}
		cd := int32(resp.Payload[0])<<24 | int32(resp.Payload[1])<<16 | int32(resp.Payload[2])<<8 | int32(resp.Payload[3])
		slog.Info(label, "cd", cd, "hex", hex.EncodeToString(resp.Payload))
		return cd
	}

	queryPanel := func(label string) {
		pkt := protocol.NewPacket(actionPeachInfo, []byte{0, 0, 0, 0})
		pkt.HasPrevAct = true
		if resp := call(pkt, actionPeachInfo, 6*time.Second); resp != nil {
			slog.Info(label, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		}
	}

	queryPlayer := func(label string) {
		pkt := protocol.NewPacket(actionPeachPlayer, []byte{0, 0, 0, 0})
		pkt.HasPrevAct = true
		resp := call(pkt, actionPeachPlayer, 6*time.Second)
		if resp == nil {
			return
		}
		r := protocol.NewReader(resp.Payload)
		vals := make([]int, 0, 7)
		for i := 0; i < 5; i++ {
			v, _ := r.ReadUint8()
			vals = append(vals, int(v))
		}
		a, _ := r.ReadInt32()
		b, _ := r.ReadInt32()
		vals = append(vals, int(a), int(b))
		slog.Info(label, "playerCount/haveMonkeyNum/canBuyCount/call_coin_times/call_ingot_times/call_need_coin/call_need_ingot", "values", vals,
			"hex", hex.EncodeToString(resp.Payload))
	}

	queryCD("摘取前战斗CD")
	queryPanel("摘取前 peach_info")
	queryPlayer("摘取前 player_info")

	warPkt := protocol.NewPacket(actionPeachHarvest, []byte{0, 0, 0, 0})
	warPkt.HasPrevAct = true
	if resp := call(warPkt, actionPeachHarvest, 20*time.Second); resp != nil {
		slog.Info("摘取结果", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}

	queryCD("摘取后战斗CD")
	queryPanel("摘取后 peach_info")
	queryPlayer("摘取后 player_info")
}