//go:build probe

// 目标协议: 竞技场战报 (0x001C0002 回包) 胜负位与新名次定位
// 基准对齐: 采集挑战前后面板 (0x001C0000) 与完整战报，用排名变化做差分对照
// 运行命令: go run -tags=probe test/probes/probe_arena_report.go [输出目录]
// 预期表现: 输出 6 个载荷文件 (panel/opponents/times 的 before/after 与 report)，会消耗 1 次挑战机会
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func dump(dir, name string, payload []byte) {
	path := filepath.Join(dir, name+".hex")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(payload)), 0o644); err != nil {
		fmt.Printf("DUMP %s ERR: %v\n", name, err)
		return
	}
	fmt.Printf("DUMP %-16s len=%d -> %s\n", name, len(payload), path)
}

func main() {
	dir := "/tmp/arena_capture"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("创建输出目录失败", "err", err)
		return
	}

	slog.SetLogLoggerLevel(slog.LevelWarn)
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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

	call := func(action uint32, payload []byte, respAction uint32) []byte {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.NewPacket(action, payload), respAction)
		if err != nil {
			fmt.Printf("CALL 0x%08X ERR: %v\n", action, err)
			return nil
		}
		return resp.Payload
	}

	getTimes := func() []byte {
		p, _ := protocol.BuildArenaGetTimesPacket(protocol.ArenaGetTimesRequest{PrevAct: protocol.ActionIDTownEnter})
		return call(protocol.ActionIDArenaGetTimes, p.Payload, protocol.ActionIDArenaGetTimes)
	}

	panel := func() []byte {
		return call(0x001C0000, []byte{0x00, 0x1C, 0x00, 0x0D}, 0x001C0000)
	}

	opponents := func() []byte {
		p, _ := protocol.BuildArenaGetOpponentsPacket(protocol.ArenaGetOpponentsRequest{PrevAct: protocol.ActionIDArenaGetTimes})
		return call(protocol.ActionIDArenaGetOpponents, p.Payload, protocol.ActionIDArenaGetOpponents)
	}

	if p := getTimes(); p != nil {
		dump(dir, "times_before", p)
	}
	if p := panel(); p != nil {
		dump(dir, "panel_before", p)
	}
	oppBefore := opponents()
	if oppBefore == nil {
		return
	}
	dump(dir, "opps_before", oppBefore)

	res, _ := protocol.ParseArenaGetOpponentsResult(oppBefore)
	if len(res.Opponents) == 0 {
		fmt.Println("NO OPPONENT")
		return
	}
	target := res.Opponents[0]
	fmt.Printf("TARGET rank=%d playerID=%d roleID=%d name=%s lv=%d\n",
		target.Rank, target.PlayerID, target.RoleID, target.Name, target.Level)

	chPkt, _ := protocol.BuildArenaChallengePacket(protocol.ArenaChallengeRequest{TargetRank: target.Rank})
	fmt.Printf("CHALLENGE payload=%s\n", hex.EncodeToString(chPkt.Payload))
	c, cc := context.WithTimeout(ctx, 15*time.Second)
	chResp, err := sess.Call(c, chPkt, protocol.ActionIDArenaChallenge)
	cc()
	if err != nil {
		fmt.Printf("CHALLENGE ERR: %v\n", err)
	} else {
		dump(dir, "report", chResp.Payload)
	}

	time.Sleep(2 * time.Second)
	if p := getTimes(); p != nil {
		dump(dir, "times_after", p)
	}
	if p := panel(); p != nil {
		dump(dir, "panel_after", p)
	}
	if p := opponents(); p != nil {
		dump(dir, "opps_after", p)
	}
}