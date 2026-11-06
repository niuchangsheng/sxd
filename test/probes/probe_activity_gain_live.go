//go:build probe

// 目标协议: 每日活跃度增量来源验证 (神龙普通上供)
//
// 基准对齐: Mod_Dragonball_Base.sacrifice(NORMAL=4, NOT_AUTO=7) 每次普通上供
// 消耗铜钱、增加每日活跃度，永不使用元宝档 (HIGH_LEVEL=5)。
//
// 探针流程:
//  1. 登录主服，抓取小助手 info (0x00340000) 与刮刮卡 info (0x00B90000) 作为基线；
//  2. 发送一次普通上供并解析 result/remain_times/score；
//  3. 再抓取两个面板，逐字节比较，定位承载“今日活跃度”的字段。
//
// 本探针会真实执行 1 次普通上供 (仅消耗铜钱)，用于确认活跃度字段与增量。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_activity_gain_live.go
// 预期表现: 上供 result=0，活跃度相关字段增加 5 (或服务端配置的固定值)。
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

	call := func(pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "call", name, "err", cErr)
			return nil
		}
		return resp
	}

	dump := func(tag string) {
		if r := call(protocol.BuildAssistantInfoPacket(), protocol.ActionIDAssistantInfo, "assistant_info", 5*time.Second); r != nil {
			slog.Info("小助手 info", "tag", tag, "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
		}
		if r := call(protocol.BuildScratchCardInfoPacket(), protocol.ActionIDScratchCardInfo, "scratch_card_info", 5*time.Second); r != nil {
			if info, pErr := protocol.ParseScratchCardInfoResult(r.Payload); pErr == nil {
				slog.Info("刮刮卡活跃度", "tag", tag, "active_degree", info.ActiveDegree, "card_times", info.CardTimes)
			}
		}
	}

	slog.Info("基线面板采集")
	dump("before")

	state := sess.GetPlayerState()
	slog.Info("上供前资产", "coins", state.Coins, "ingots", state.Ingots)

	resp := call(protocol.BuildDragonballSacrificePacket(protocol.DragonballSacrificeNormal, protocol.DragonballSacrificeNotAuto), protocol.ActionIDDragonballSacrifice, "dragonball_sacrifice", 8*time.Second)
	if resp == nil {
		slog.Error("普通上供无回包，无法验证活跃度增量")
		return
	}
	res, pErr := protocol.ParseDragonballSacrificeResult(resp.Payload)
	if pErr != nil {
		slog.Error("解析普通上供回包失败", "err", pErr)
		return
	}
	if res.Result == protocol.DragonballResultTmpGridFull {
		// 祭坛临时格已满：先查询并收取临时格龙珠，再重试一次，与 dragonball_offer 例程一致。
		slog.Info("祭坛临时格已满，先收取临时格龙珠")
		infoResp := call(protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo, "dragonball_tmp_info", 5*time.Second)
		if infoResp == nil {
			slog.Warn("查询临时格无回包，停止")
			return
		}
		balls, bErr := protocol.ParseDragonballTmpInfoResult(infoResp.Payload)
		if bErr != nil {
			slog.Warn("解析临时格失败", "err", bErr, "hex", hex.EncodeToString(infoResp.Payload))
			return
		}
		slog.Info("临时格原始回包", "hex", hex.EncodeToString(infoResp.Payload))
		for i, b := range balls {
			slog.Info("临时格龙珠明细", "idx", i, "db_id", b.PlayerDBID, "level", b.Level, "quality", b.Quality)
		}
		ids := make([]int32, 0, len(balls))
		for _, b := range balls {
			ids = append(ids, b.PlayerDBID)
		}
		slog.Info("临时格龙珠", "count", len(ids))
		if len(ids) == 0 {
			slog.Warn("临时格无龙珠但返回已满，停止")
			return
		}
		collectResp := call(protocol.BuildDragonballCollectPacket(ids), protocol.ActionIDDragonballCollect, "dragonball_collect", 6*time.Second)
		if collectResp == nil {
			slog.Warn("收取临时格无回包，停止")
			return
		}
		code, collected, cErr := protocol.ParseDragonballCollectResult(collectResp.Payload)
		if cErr != nil {
			slog.Warn("解析收取回包失败", "err", cErr)
			return
		}
		slog.Info("临时格收取", "result", code, "requested", len(ids), "collected", len(collected))
		if code != protocol.DragonballResultSuccess && code != protocol.DragonballResultPackageFull {
			// 诊断: 官方客户端可能使用扁平数组 [Int]，先用扁平格式重试一次。
			flat := protocol.NewWriter()
			flat.WriteUint16(uint16(len(ids)))
			for _, id := range ids {
				flat.WriteInt32(id)
			}
			flatResp := call(protocol.NewPacket(protocol.ActionIDDragonballCollect, flat.Bytes()), protocol.ActionIDDragonballCollect, "dragonball_collect_flat", 6*time.Second)
			if flatResp == nil {
				slog.Warn("扁平数组收取无回包")
				return
			}
			flatCode, flatCollected, flatErr := protocol.ParseDragonballCollectResult(flatResp.Payload)
			if flatErr != nil {
				slog.Warn("解析扁平数组收取回包失败", "err", flatErr, "hex", hex.EncodeToString(flatResp.Payload))
				return
			}
			slog.Info("扁平数组收取", "result", flatCode, "requested", len(ids), "collected", len(flatCollected))
			if flatCode != protocol.DragonballResultSuccess && flatCode != protocol.DragonballResultPackageFull {
				slog.Warn("临时格收取未成功", "nested_result", code, "flat_result", flatCode)
				return
			}
		}

		resp = call(protocol.BuildDragonballSacrificePacket(protocol.DragonballSacrificeNormal, protocol.DragonballSacrificeNotAuto), protocol.ActionIDDragonballSacrifice, "dragonball_sacrifice_retry", 8*time.Second)
		if resp == nil {
			slog.Error("重试普通上供无回包")
			return
		}
		res, pErr = protocol.ParseDragonballSacrificeResult(resp.Payload)
		if pErr != nil {
			slog.Error("解析重试上供回包失败", "err", pErr)
			return
		}
	}
	switch res.Result {
	case protocol.DragonballResultSuccess:
		slog.Info("普通上供成功",
			"remain_times", res.RemainTimes,
			"score", res.Score,
			"total_score", res.TotalScore,
			"alerts", len(res.Alerts),
		)
	case protocol.DragonballResultTimesUseUp:
		slog.Warn("今日普通上供次数已用尽，无法观察增量")
		return
	case protocol.DragonballResultNoEnoughCoin:
		slog.Warn("铜钱不足，无法继续")
		return
	case protocol.DragonballResultFunctionNoOpen:
		slog.Warn("神龙上供功能未开放")
		return
	default:
		slog.Warn("普通上供未成功", "result", res.Result)
		return
	}

	after := sess.GetPlayerState()
	slog.Info("上供后资产", "coins", after.Coins, "ingots", after.Ingots,
		"coin_delta", after.Coins-state.Coins, "ingot_delta", after.Ingots-state.Ingots)

	time.Sleep(500 * time.Millisecond)
	slog.Info("增量面板采集")
	dump("after")
}