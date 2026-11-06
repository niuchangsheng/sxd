//go:build probe

// 目标协议: [Module 57] Mod_BeelzebubTrials_Base open(1) / create_team(4) / fight(13) / get_war_result(14) / open_moral(23)
// 基准对齐: 验证 open 面板 Boss 行第三列语义，以及“累计挑战次数达到上限后是否仍有奖励”
//
//	官方 AS3 com.datas.BeelzebubTrialsData 将 Boss 行解释为
//	[bossID, 已挑战次数, 今日已挑战标记]，并以
//	已挑战次数 >= BeelzeBubTrialsBossType.Bosses[bossID][15] 判定该关卡已完成。
//
// 运行命令: go run -tags=probe test/probes/probe_beelzebub_reward_live.go -config configs/s1.yaml -boss 78
// 观察点:
//  1. 挑战前后 open 面板中该 Boss 行的第二列(次数)与第三列(今日标记)如何变化
//  2. 挑战前后道行经验(moral exp)是否增加，用于判定已达到次数上限的关卡是否仍有奖励
//
// 注意: 本探针会真实发起一场魔王试炼战斗（不消耗元宝），仅用于行为对齐。
package main

import (
	"context"
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
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfgPath := flag.String("config", "configs/s1.yaml", "配置文件路径")
	bossID := flag.Int("boss", 78, "要挑战的魔王编号")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ticket, err := xdAuth.Login(ctx, acc.Username, acc.Password, role.ServerID)
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
		MaxReconnectAttempts: 2,
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
		slog.Error("会话未在时限内激活", "state", sess.State())
		return
	}

	snapshot := func(tag string) {
		openData, oErr := sess.BeelzebubTrialsOpen(ctx)
		if oErr != nil {
			slog.Warn("打开面板失败", "tag", tag, "err", oErr)
			return
		}
		var row *protocol.BeelzebubBossStatus
		for i := range openData.Bosses {
			if openData.Bosses[i].BossID == int32(*bossID) {
				row = &openData.Bosses[i]
				break
			}
		}
		moral, mErr := sess.BeelzebubTrialsOpenMoral(ctx)
		slog.Info("面板快照",
			"tag", tag,
			"moral_exp", openData.MoralExp,
			"moral_lv", openData.MoralLv,
			"row", fmt.Sprintf("%+v", row),
			"moral_err", mErr,
			"moral", fmt.Sprintf("%+v", moral),
		)
	}

	snapshot("before")

	if res, err := sess.BeelzebubTrialsCreateTeam(ctx, int32(*bossID), protocol.BeelzebubTrialsNotLimit); err != nil {
		slog.Error("建队失败", "boss", *bossID, "err", err)
		return
	} else {
		slog.Info("建队结果", "boss", *bossID, "result", res)
	}

	if _, err := sess.BeelzebubTrialsFight(ctx); err != nil {
		_ = sess.BeelzebubTrialsExitTeam(ctx)
		slog.Error("发起战斗失败", "err", err)
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(800 * time.Millisecond):
	}

	warRes, wErr := sess.BeelzebubTrialsGetWarResult(ctx)
	slog.Info("战斗结算", "result", fmt.Sprintf("%+v", warRes), "err", wErr)

	_ = sess.BeelzebubTrialsLeaveWar(ctx, 0)
	_ = sess.BeelzebubTrialsExitTeam(ctx)
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
	}

	snapshot("after")
}
