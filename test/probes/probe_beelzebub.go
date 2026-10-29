//go:build probe

// Package main 提供魔王试炼 Boss 列表与等级难度逆向诊断探针。
//
// 目标基准事实: 官方 AS3 Mod_BeelzebubTrials_Base
// 执行方式: go run -tags=probe test/probes/probe_beelzebub.go
// 遵循规范: docs/adr/0013-recon-probe-isolation-and-test-harness.md (严禁硬编码敏感凭据)
//
// @author Ateng
// @since 2026-10-29
package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在预期时间内激活", "state", sess.State())
		return
	}

	slog.Info("会话已激活，开始诊断魔王试炼")

	// 1. 检查今日挑战资格
	canChallenge, err := sess.BeelzebubTrialsCheckCanChallenge(ctx)
	slog.Info("BeelzebubTrialsCheckCanChallenge", "canChallenge", canChallenge, "err", err)

	// 2. 拉取主面板
	openData, err := sess.BeelzebubTrialsOpen(ctx)
	slog.Info("BeelzebubTrialsOpen", "openData", fmt.Sprintf("%+v", openData), "err", err)

	// 3. 拉取可挑战魔王列表
	canList, err := sess.BeelzebubTrialsGetCanChallengeList(ctx)
	slog.Info("BeelzebubTrialsGetCanChallengeList", "count", len(canList), "list", canList, "err", err)

	type BossItem struct {
		ID    int32
		Name  string
		Level int32
	}
	var bossItems []BossItem
	for _, bID := range canList {
		bossItems = append(bossItems, BossItem{
			ID:    bID,
			Name:  protocol.BeelzebubTrialsBossName(bID),
			Level: protocol.BeelzebubTrialsBossLevel(bID),
		})
	}

	sort.Slice(bossItems, func(i, j int) bool {
		if bossItems[i].Level != bossItems[j].Level {
			return bossItems[i].Level < bossItems[j].Level
		}
		return bossItems[i].ID < bossItems[j].ID
	})

	// 挨个确认是否可以挑战
	for _, item := range bossItems {
		cRes, err := sess.BeelzebubTrialsCreateTeam(ctx, item.ID, protocol.BeelzebubTrialsNotLimit)
		if err != nil {
			continue
		}
		if cRes == protocol.BeelzebubTrialsSuccess {
			slog.Info("已锁定可挑战魔王并成功建队，开始挑战", "boss_id", item.ID, "boss_name", item.Name, "level", item.Level)
			// 招募助战
			helpers, _ := sess.BeelzebubTrialsGetHelperRoles(ctx)
			slot := uint8(1)
			for _, h := range helpers {
				if slot > 2 {
					break
				}
				if !h.InBody {
					if _, err := sess.BeelzebubTrialsChooseHelper(ctx, h.RoleID, slot); err == nil {
						slot++
					}
				}
			}
			// 发起战斗
			fRes, err := sess.BeelzebubTrialsFight(ctx)
			slog.Info("发起战斗", "fRes", fRes, "err", err)
			time.Sleep(500 * time.Millisecond)
			warRes, err := sess.BeelzebubTrialsGetWarResult(ctx)
			slog.Info("结算战报", "warRes", warRes, "err", err)
			_ = sess.BeelzebubTrialsLeaveWar(ctx, 0)
			_ = sess.BeelzebubTrialsExitTeam(ctx)
			slog.Info("挑战完成！")
			break
		}
	}
}
