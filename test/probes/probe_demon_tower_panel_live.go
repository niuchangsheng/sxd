//go:build probe

// 目标协议: 锁妖塔 (镇妖塔) 主面板与基础信息只读查询
//
//	[Module 705, Act 0] Mod_RepressDemonTower_Base.panel_info 0x02C10000 request=[]
//	  response: [UByte result, Int is_first, Int season, Int left_time, Int now_step,
//	             Int now_finish_layer, Int now_total_round, Int next_layer_left_time, Int rank,
//	             [[Int step_up_id, Int layer_need, Int round_need]],
//	             [[Int layer_id, Int fastest_round]],
//	             Int buy_ticket_num, Int now_ticket_num, Int race_step, Int ranks]
//	[Module 705, Act 8] Mod_RepressDemonTower_Base.get_base_info 0x02C10008 request=[]
//	  response: [Byte isSeasonTime]
//
// 基准对齐: com.datas.RepressDemonTowerData.panel_info 的 oObject.list 字段顺序
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_demon_tower_panel_live.go
// 预期表现: 打印 panel_info 全部字段与 layer_list / step_list，不发起挑战、不消耗挑战次数
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
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置文件缺少账号或角色")
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
		MaxReconnectAttempts: 2,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	query(ctx, sess, 0x02C10000, "repress_demon_tower.panel_info", printPanelInfo)
	query(ctx, sess, 0x02C10008, "repress_demon_tower.get_base_info", func(payload []byte) {
		if len(payload) == 0 {
			slog.Info("get_base_info 回包为空")
			return
		}
		slog.Info("get_base_info", "isSeasonTime", int8(payload[0]))
	})
}

func query(ctx context.Context, sess *client.RoleSession, actionID uint32, name string, parse func([]byte)) {
	cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	resp, err := sess.Call(cCtx, protocol.NewPacket(actionID, nil), actionID)
	cancel()
	if err != nil {
		slog.Error("查询失败: "+name, "action", fmt.Sprintf("0x%08X", actionID), "err", err)
		return
	}
	slog.Info("查询回包: "+name,
		"action", fmt.Sprintf("0x%08X", resp.ActionID),
		"len", len(resp.Payload),
		"hex", hex.EncodeToString(resp.Payload),
	)
	parse(resp.Payload)
}

func printPanelInfo(payload []byte) {
	r := protocol.NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		slog.Error("panel_info 读取 result 失败", "err", err)
		return
	}
	fields := make([]int32, 8)
	for i := range fields {
		if fields[i], err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取主字段失败", "index", i, "err", err)
			return
		}
	}

	stepCount, err := r.ReadUint16()
	if err != nil {
		slog.Error("panel_info 读取 step_list 长度失败", "err", err)
		return
	}
	type step struct{ StepUpID, LayerNeed, RoundNeed int32 }
	steps := make([]step, 0, stepCount)
	for i := 0; i < int(stepCount); i++ {
		var s step
		if s.StepUpID, err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取 step_list 失败", "index", i, "err", err)
			return
		}
		if s.LayerNeed, err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取 step_list 失败", "index", i, "err", err)
			return
		}
		if s.RoundNeed, err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取 step_list 失败", "index", i, "err", err)
			return
		}
		steps = append(steps, s)
	}

	layerCount, err := r.ReadUint16()
	if err != nil {
		slog.Error("panel_info 读取 layer_list 长度失败", "err", err)
		return
	}
	type layer struct{ LayerID, FastestRound int32 }
	layers := make([]layer, 0, layerCount)
	for i := 0; i < int(layerCount); i++ {
		var l layer
		if l.LayerID, err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取 layer_list 失败", "index", i, "err", err)
			return
		}
		if l.FastestRound, err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取 layer_list 失败", "index", i, "err", err)
			return
		}
		layers = append(layers, l)
	}

	tail := make([]int32, 4)
	for i := range tail {
		if tail[i], err = r.ReadInt32(); err != nil {
			slog.Error("panel_info 读取尾部字段失败", "index", i, "err", err)
			return
		}
	}

	slog.Info("panel_info 解析",
		"result", result,
		"is_first", fields[0],
		"season", fields[1],
		"left_time", fields[2],
		"now_step", fields[3],
		"now_finish_layer", fields[4],
		"now_total_round", fields[5],
		"next_layer_left_time", fields[6],
		"rank", fields[7],
		"buy_ticket_num", tail[0],
		"now_ticket_num", tail[1],
		"race_step", tail[2],
		"ranks", tail[3],
		"step_list", fmt.Sprintf("%v", steps),
		"layer_list", fmt.Sprintf("%v", layers),
		"remaining_bytes", r.Remaining(),
	)
}