//go:build probe

// 目标协议: [Module 35] Mod_HeroMission_Base.start_practice / quickly / stop_practice
// 基准对齐: 原版客户端 com.protocols.Mod_HeroMission_Base
//            start_practice request=[Short(town_id),Byte(auto_num),Byte(hero_type)]
//            quickly request=[] / stop_practice request=[]
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_hero_practice_live.go
// 预期表现: 对城镇 4 发起一次真实精英副本扫荡 (auto_num=0 不购买翻牌)，
//           打印 start_practice / quickly 的实际回包结果码，用于判定是否需要在尾部追加 PrevAct。
// 注意: 该探针会真实消耗体力并产生掉落，属于写操作；仅在需要实机验证时运行。
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	st := sess.GetPlayerState()
	slog.Info("会话已激活", "role", sess.RoleName(), "level", st.Level, "stamina", sess.GetStamina())

	const townID uint16 = 35

	// 1. 读取城镇英雄副本列表
	listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
	listResp, listErr := sess.Call(listCtx, protocol.BuildHeroMissionListPacket(townID), protocol.ActionHeroMissionList)
	listCancel()
	if listErr != nil || listResp == nil {
		slog.Error("查询英雄副本列表失败", "err", listErr)
		return
	}
	list, pErr := protocol.ParseHeroMissionListResponse(listResp.Payload)
	if pErr != nil {
		slog.Error("解析英雄副本列表失败", "err", pErr, "hex", hex.EncodeToString(listResp.Payload))
		return
	}
	fmt.Printf("town=%d missions=%d finish=%d buy_price=%d\n", townID, len(list.Missions), list.FinishMissionNum, list.BuyPrice)
	for _, m := range list.Missions {
		fmt.Printf("  mission=%d can_challenge=%d rank=%d finished=%d\n", m.MissionID, m.CanChallenge, m.Rank, m.Finished)
	}

	beforeStamina := sess.GetStamina()

	// 2. start_practice: 先尝试严格 4 字节业务载荷；超时后再尝试追加 PrevAct 的 8 字节变体
	startRes, variant := callHeroStart(ctx, sess, townID)
	if startRes == nil {
		fmt.Println("start_practice 两种载荷变体均失败")
		return
	}
	fmt.Printf("start_practice variant=%s result=%d practice_time=%d\n", variant, startRes.Result, startRes.PracticeTime)
	if startRes.Result != protocol.HeroMissionResultSuccess {
		return
	}

	// 3. quickly: 先尝试空载荷；超时后再尝试 4 字节 PrevAct 变体
	quickRes, quickVariant := callHeroQuickly(ctx, sess)
	if quickRes == nil {
		fmt.Println("quickly 两种载荷变体均失败")
	} else {
		fmt.Printf("quickly variant=%s result=%d\n", quickVariant, quickRes.Result)
	}

	// 4. stop_practice (空载荷尽力而为)
	_ = sess.Send(protocol.NewPacket(protocol.ActionHeroMissionStopPractice, nil))
	time.Sleep(200 * time.Millisecond)

	afterStamina := sess.GetStamina()
	fmt.Printf("stamina: before=%d after=%d cost=%d\n", beforeStamina, afterStamina, beforeStamina-afterStamina)
}

func callHeroStart(ctx context.Context, sess *client.RoleSession, townID uint16) (*protocol.HeroPracticeResult, string) {
	base := protocol.NewWriter()
	base.WriteUint16(townID)
	base.WriteUint8(0) // auto_num=0: 不自动购买翻牌奖励
	base.WriteUint8(0) // hero_type=0: 普通城镇英雄副本

	variants := []struct {
		name    string
		payload []byte
	}{
		{"strict4", base.Bytes()},
		{"withPrevAct", appendPrevAct(base.Bytes(), sess.LastActionID())},
	}
	for _, v := range variants {
		c, cc := context.WithTimeout(ctx, 4*time.Second)
		resp, err := sess.Call(c, protocol.NewPacket(protocol.ActionHeroMissionStartPractice, v.payload), protocol.ActionHeroMissionStartPractice)
		cc()
		if err != nil || resp == nil {
			fmt.Printf("start_practice %s: no response (%v)\n", v.name, err)
			continue
		}
		res, pErr := protocol.ParseHeroPracticeResult(resp.Payload)
		if pErr != nil {
			fmt.Printf("start_practice %s: hex=%X parse err=%v\n", v.name, resp.Payload, pErr)
			continue
		}
		return res, v.name
	}
	return nil, ""
}

func callHeroQuickly(ctx context.Context, sess *client.RoleSession) (*protocol.HeroQuicklyResult, string) {
	variants := []struct {
		name    string
		payload []byte
	}{
		{"empty", nil},
		{"withPrevAct", appendPrevAct(nil, sess.LastActionID())},
	}
	for _, v := range variants {
		c, cc := context.WithTimeout(ctx, 4*time.Second)
		resp, err := sess.Call(c, protocol.NewPacket(protocol.ActionHeroMissionQuickly, v.payload), protocol.ActionHeroMissionQuickly)
		cc()
		if err != nil || resp == nil {
			fmt.Printf("quickly %s: no response (%v)\n", v.name, err)
			continue
		}
		res, pErr := protocol.ParseHeroQuicklyResponse(resp.Payload)
		if pErr != nil {
			fmt.Printf("quickly %s: hex=%X parse err=%v\n", v.name, resp.Payload, pErr)
			continue
		}
		return res, v.name
	}
	return nil, ""
}

func appendPrevAct(payload []byte, prevAct uint32) []byte {
	out := make([]byte, len(payload)+4)
	copy(out, payload)
	out[len(payload)] = byte(prevAct >> 24)
	out[len(payload)+1] = byte(prevAct >> 16)
	out[len(payload)+2] = byte(prevAct >> 8)
	out[len(payload)+3] = byte(prevAct)
	return out
}