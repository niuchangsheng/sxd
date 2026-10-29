//go:build probe

// Package main 提供西天取经、城镇打坐练功与魔王试炼协议闭环的脱机交互诊断探针。
//
// 目标基准事实: 官方 AS3 Mod_TakeBible_Base, Mod_Player_Base, Mod_BeelzebubTrials_Base
// 执行方式: go run -tags=probe test/probes/probe_diag.go
// 遵循规范: docs/adr/0013-recon-probe-isolation-and-test-harness.md (严禁硬编码敏感凭据)
//
// @author Ateng
// @since 2026-10-29
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
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
		MaxReconnectAttempts: 5,
		RefreshCredentials: func(c context.Context, sCfg *client.SessionConfig) error {
			ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
			freshTicket, err := ticketCache.GetOrFetch(c, acc.Platform, acc.Username, acc.Password, role.ServerID)
			if err != nil {
				return err
			}
			client.ApplyTicket(sCfg, freshTicket)
			if freshTicket.GatewayURL != "" {
				sCfg.ServerAddr = freshTicket.GatewayURL
			}
			return nil
		},
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动长连接会话失败", "err", err)
		return
	}
	defer sess.Close()

	// Wait for StateActive (给足自愈重连时间)
	for i := 0; i < 120; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内进入激活状态", "state", sess.State())
		return
	}

	slog.Info("=== 登录成功进入激活态，开始诊断探针 ===")

	// 1. 诊断打坐经验
	slog.Info("--- 诊断 1: 打坐练功经验 ---")
	if err := sess.ClaimTownPracticeExp(ctx); err != nil {
		slog.Warn("ClaimTownPracticeExp 失败", "err", err)
	} else {
		slog.Info("ClaimTownPracticeExp 触发完成")
	}

	// 2. 诊断西天取经
	slog.Info("--- 诊断 2: 西天取经 ---")
	callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32) (*protocol.Packet, error) {
		tCtx, tCancel := context.WithTimeout(ctx, 3*time.Second)
		defer tCancel()
		return sess.Call(tCtx, pkt, expectedAct)
	}

	openPkt := protocol.BuildTakeBibleOpenPacket(sess.LastActionID())
	openResp, err := callWithTimeout(openPkt, protocol.ActionIDTakeBibleOpen)
	if err != nil {
		slog.Warn("TakeBibleOpen 失败", "err", err)
	} else if openResp != nil {
		openRes, _ := protocol.ParseTakeBibleOpenResult(openResp.Payload)
		slog.Info("TakeBibleOpen 成功", "can_take", openRes.CanTakeTimes, "rob_times", openRes.RobTimes)
	}

	infoPkt := protocol.BuildTakeBibleGetInfoPacket(sess.LastActionID())
	infoResp, err := callWithTimeout(infoPkt, protocol.ActionIDTakeBibleGetInfo)
	if err != nil {
		slog.Warn("TakeBibleGetInfo 失败", "err", err)
	} else if infoResp != nil {
		infoRes, err := protocol.ParseTakeBibleGetInfoResult(infoResp.Payload)
		if err != nil {
			slog.Warn("ParseTakeBibleGetInfoResult 失败", "err", err)
		} else {
			slog.Info("TakeBibleGetInfo 成功", "code", infoRes.ResultCode, "angel", protocol.AngelName(infoRes.CurrentAngel), "busy", infoRes.IsBusy)
		}
	}

	// 3. 诊断魔王试炼
	slog.Info("--- 诊断 3: 魔王试炼 ---")
	warRes, err := sess.BeelzebubTrialsAutoChallenge(ctx, 0)
	if err != nil {
		slog.Warn("BeelzebubTrialsAutoChallenge 异常", "err", err)
	} else if warRes != nil {
		slog.Info("BeelzebubTrialsAutoChallenge 成功", "result", warRes.Result, "award_coin", warRes.AwardCoin, "award_fame", warRes.AwardFame, "award_moral", warRes.AwardMoral)
	} else {
		slog.Info("BeelzebubTrialsAutoChallenge 完成 (次数已用或无魔王)")
	}

	slog.Info("=== 诊断探针结束 ===")
}
