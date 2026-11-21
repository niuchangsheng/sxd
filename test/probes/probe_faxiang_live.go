//go:build probe

// 目标: 实机验证主角法相「造物佛手 / 守护魔兽」(Module 518, Mod_MainRoleFaXiang_Base)
//
// 基准对齐: /tmp/faxiang3.pcapng 官方客户端抓包（2026-11-21）
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_faxiang_live.go
//
// 默认只做只读面板查询 (518/11、518/12)，打印等级/经验/待领取/冷却时间。
// 设置 SXD_PROBE_APPLY=1 后追加一次完整闭环：
//
//	518/17 领取已完成炼化 → 518/13 祈愿 → 518/15 选择首个候选 → 518/16 守护魔兽挑战
//
// 绝不调用 518/14 (元宝重随)，不购买次数，不消耗元宝。
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

func probeCall(ctx context.Context, sess *client.RoleSession, pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
	cCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, pkt, act)
	if err != nil {
		slog.Error("请求无回包", "name", name, "action", act, "err", err)
		return nil
	}
	return resp
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置缺少账号或角色")
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

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
	deadline := time.Now().Add(25 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots", sess.GetPlayerState().Ingots)

	apply := os.Getenv("SXD_PROBE_APPLY") == "1"

	// 518/0 法相主面板（官方客户端先据此判断佛/魔两侧是否开放）
	if panelResp := probeCall(ctx, sess, protocol.BuildFaxiangGetPanelInfoPacket(),
		protocol.ActionIDFaxiangGetPanelInfo, "法相主面板"); panelResp != nil {
		slog.Info("518/0 原始回包", "hex", fmt.Sprintf("%x", panelResp.Payload))
	}

	// 518/11 造物佛手面板
	foResp := probeCall(ctx, sess, protocol.BuildFaxiangExtraFoInfoPacket(),
		protocol.ActionIDFaxiangExtraFoInfo, "造物佛手面板")
	if foResp == nil {
		return
	}
	slog.Info("518/11 原始回包", "hex", fmt.Sprintf("%x", foResp.Payload))
	fo, err := protocol.ParseFaxiangExtraFoInfo(foResp.Payload)
	if err != nil {
		slog.Error("518/11 解析失败", "err", err)
		return
	}
	slog.Info("造物佛手", "level", fo.Level, "exp", fo.CurExp,
		"award_list", fo.AwardList, "cur_award_id", fo.CurPrayAwardID,
		"refine_finish_time", fo.RefineFinishTime)

	// 518/12 守护魔兽面板
	moResp := probeCall(ctx, sess, protocol.BuildFaxiangExtraMoInfoPacket(),
		protocol.ActionIDFaxiangExtraMoInfo, "守护魔兽面板")
	if moResp != nil {
		slog.Info("518/12 原始回包", "hex", fmt.Sprintf("%x", moResp.Payload))
		if mo, mErr := protocol.ParseFaxiangExtraMoInfo(moResp.Payload); mErr != nil {
			slog.Error("518/12 解析失败", "err", mErr)
		} else {
			slog.Info("守护魔兽", "level", mo.Level, "exp", mo.CurExp,
				"next_can_fight_time", mo.NextCanFightTime,
				"stunt", mo.RandomStuntID, "supernatural", mo.RandomSupernaturalID,
				"pattern", mo.RandomSpecialPattern)
		}
	}

	if !apply {
		slog.Info("只读模式结束；设置 SXD_PROBE_APPLY=1 执行一次炼化与挑战")
		return
	}

	// 518/17 领取已完成炼化
	if fo.CurPrayAwardID > 0 {
		if resp := probeCall(ctx, sess, protocol.BuildFaxiangGainPrayAwardPacket(),
			protocol.ActionIDFaxiangGainPrayAward, "领取炼化奖励"); resp != nil {
			code, _ := protocol.ParseFaxiangGainPrayAwardResult(resp.Payload)
			slog.Info("领取炼化奖励", "award_id", fo.CurPrayAwardID,
				"result", code, "result_text", protocol.FaxiangResultText(code))
			if code == protocol.FaxiangSuccess {
				fo.CurPrayAwardID = 0
			}
		}
	}

	// 518/13 祈愿 → 518/15 选择首个候选
	if fo.CurPrayAwardID == 0 {
		awards := fo.AwardList
		if len(awards) == 0 {
			if resp := probeCall(ctx, sess, protocol.BuildFaxiangExtraPrayPacket(),
				protocol.ActionIDFaxiangExtraPray, "祈愿"); resp != nil {
				opts, pErr := protocol.ParseFaxiangExtraPrayResult(resp.Payload)
				if pErr != nil {
					slog.Error("祈愿解析失败", "err", pErr)
				} else {
					slog.Info("祈愿结果", "result", opts.Result,
						"result_text", protocol.FaxiangResultText(opts.Result), "award_ids", opts.AwardIDs)
					awards = opts.AwardIDs
				}
			}
		}
		if len(awards) > 0 {
			if resp := probeCall(ctx, sess, protocol.BuildFaxiangExtraChoosePrayAwardPacket(awards[0]),
				protocol.ActionIDFaxiangExtraChoosePrayAward, "选择炼化奖励"); resp != nil {
				chosen, cErr := protocol.ParseFaxiangChoosePrayAwardResult(resp.Payload)
				if cErr != nil {
					slog.Error("选择炼化奖励解析失败", "err", cErr)
				} else {
					slog.Info("选择炼化奖励", "award_id", awards[0], "result", chosen.Result,
						"result_text", protocol.FaxiangResultText(chosen.Result),
						"refine_finish_time", chosen.RefineFinishTime)
				}
			}
		}
	}

	// 518/16 守护魔兽挑战
	if resp := probeCall(ctx, sess, protocol.BuildFaxiangExtraMoChallengePacket(),
		protocol.ActionIDFaxiangExtraMoChallenge, "守护魔兽挑战"); resp != nil {
		res, rErr := protocol.ParseFaxiangExtraMoChallengeResult(resp.Payload)
		if rErr != nil {
			slog.Error("守护魔兽挑战解析失败", "err", rErr)
		} else {
			slog.Info("守护魔兽挑战", "result", res.Result,
				"result_text", protocol.FaxiangResultText(res.Result), "kill_num", res.KillNum,
				"report_len", len(resp.Payload))
		}
	}

	slog.Info("结束后资产", "ingots", sess.GetPlayerState().Ingots)
}
