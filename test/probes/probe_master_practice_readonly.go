//go:build probe

// 目标协议: 宗师修行 (Module 665) 与 转职 (Module 308) 只读语义核查
//
//	[665, 1]  main_panel          0x02990001  成功时返回全部职业修行进度
//	[665, 2]  get_deploy_info     0x02990002  返回布阵角色与格位
//	[308, 0]  get_job_change_info 0x01340000  response [Int cur_role_id, [Int role_id]]
//	[2, 41]   get_player_pack_item_list 0x00020029 只读背包快照
//	[2, 107]  get_the_item_num    0x0002006B  查询转职符石 5097 持有量
//
// 基准对齐: com.protocols.Mod_MasterPractice_Base / Mod_JobChange_Base /
// com.datas.JobChangeData / com.views.JobChangeView / com.views.CommonDeployView。
// 本探针严格只读: 不发送 deploy(665/4)、change(308/1)、start_challenge(665/5)、
// active_*(665/6,7)、step_up(665/8)，不修改任何服务端状态。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_master_practice_readonly.go
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
	actionJobChangeInfo uint32 = 0x01340000 // Module 308, Action 0
	itemJobChangeRune   int32  = 5097       // 转职符石
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	call := func(name string, pkt *protocol.Packet, act uint32) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Error("查询失败: "+name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		slog.Info("查询回包: "+name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		return resp
	}

	// 1. 宗师修行主面板。
	if resp := call("master_practice.main_panel", protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), protocol.ActionIDMasterPracticePanel); resp != nil {
		if panel, pErr := protocol.ParseMasterPracticePanelResult(resp.Payload); pErr == nil {
			slog.Info("面板解析", "result", panel.Result, "today_times", panel.TodayChallengeTimes, "jobs", len(panel.Jobs))
			for _, j := range panel.Jobs {
				slog.Info("  职业进度", "job_id", j.JobID, "step", j.StepID, "stars", j.TotalStars, "cd", j.StepUpCDTime, "today", j.TodayChallenge)
			}
		} else {
			slog.Error("面板解析失败", "err", pErr)
		}
	}

	// 2. 宗师修行布阵信息。
	if resp := call("master_practice.get_deploy_info", protocol.BuildMasterPracticeDeployInfoPacket(), protocol.ActionIDMasterPracticeDeployInfo); resp != nil {
		if info, pErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload); pErr == nil {
			slog.Info("布阵解析", "result", info.Result, "level", info.DeployLevel, "roles", len(info.Roles), "grids", len(info.Grids))
			for _, r := range info.Roles {
				slog.Info("  角色", "player_role_id", r.PlayerRoleID, "is_main", r.IsMainRole, "role_id", r.RoleID, "name", r.RoleName, "level", r.RoleLevel, "is_deploy", r.IsDeploy)
			}
			for _, g := range info.Grids {
				slog.Info("  格位", "grid_type_id", g.GridTypeID, "player_role_id", g.PlayerRoleID)
			}
		} else {
			slog.Error("布阵解析失败", "err", pErr)
		}
	}

	// 2.1 宗师修行绝技映射 (Mod 85 Act 7): 用于确认职业 -> 绝技的切换链路。
	if resp := call("library.get_master_practice_stunt_info",
		protocol.BuildLibraryMasterPracticeStuntInfoPacket(),
		protocol.ActionIDLibraryMasterPracticeStuntInfo); resp != nil {
		if infos, pErr := protocol.ParseLibraryMasterPracticeStuntInfoResult(resp.Payload); pErr == nil {
			for _, info := range infos {
				slog.Info("宗师修行绝技",
					"job_id", info.RoleJobID,
					"stunt_id", info.RoleStuntID,
					"next_stunt_id", info.NextRoleStuntID,
					"is_active", info.IsActive,
					"is_use", info.IsUse,
				)
			}
		} else {
			slog.Error("宗师修行绝技映射解析失败", "err", pErr, "hex", hex.EncodeToString(resp.Payload))
		}
	}

	// 3. 转职面板信息: [Int cur_role_id, [Int role_id...]]。
	if resp := call("job_change.get_job_change_info", protocol.NewPacket(actionJobChangeInfo, nil), actionJobChangeInfo); resp != nil {
		if cur, ids, pErr := parseJobChangeInfo(resp.Payload); pErr == nil {
			slog.Info("转职信息解析", "cur_role_id", cur, "target_role_ids", ids)
		} else {
			slog.Error("转职信息解析失败", "err", pErr, "hex", hex.EncodeToString(resp.Payload))
		}
	}

	// 4. 背包快照: 定位转职符石 5097 的格位。
	if resp := call("item.get_player_pack_item_list", protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList); resp != nil {
		if list, pErr := protocol.ParsePackItemListResponse(resp.Payload); pErr == nil {
			slog.Info("背包解析", "pack_capacity", list.PackCapacity, "pack_used", len(list.PackItems), "warehouse_used", len(list.WarehouseItems))
			for _, it := range list.PackItems {
				if it.ItemID == itemJobChangeRune {
					slog.Info("  找到转职符石", "grid_id", it.GridID, "player_item_id", it.PlayerItemID,
						"field3", it.Field3, "field4", it.Field4, "field5", it.Field5, "field6", it.Field6,
						"field7", it.Field7, "field8", it.Field8, "field9", it.Field9, "field10", it.Field10,
						"field11", it.Field11, "field12", it.Field12, "field13", it.Field13, "field14", it.Field14,
						"field15", it.Field15, "field16", it.Field16, "field17", it.Field17, "field18", it.Field18,
						"field19", it.Field19, "item_type", it.ItemType)
				}
			}
		} else {
			slog.Error("背包解析失败", "err", pErr)
		}
	}

	// 5. 转职符石持有数量。
	if resp := call("item.get_the_item_num(5097)", protocol.BuildGetItemNumPacket(itemJobChangeRune, protocol.ActionIDItemGetPackList), protocol.ActionIDItemGetTheItemNum); resp != nil {
		if n, pErr := protocol.ParseGetItemNumResponse(resp.Payload); pErr == nil {
			slog.Info("转职符石持有量", "item_id", itemJobChangeRune, "count", n)
		} else {
			slog.Error("转职符石数量解析失败", "err", pErr)
		}
	}
}

func parseJobChangeInfo(payload []byte) (int32, []int32, error) {
	r := protocol.NewReader(payload)
	cur, err := r.ReadInt32()
	if err != nil {
		return 0, nil, err
	}
	n, err := r.ReadUint16()
	if err != nil {
		return 0, nil, err
	}
	ids := make([]int32, 0, n)
	for i := 0; i < int(n); i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return 0, nil, err
		}
		ids = append(ids, v)
	}
	return cur, ids, nil
}
