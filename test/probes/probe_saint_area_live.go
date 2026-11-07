//go:build probe

// 目标协议: 圣域玩法链路只读巡检 + 小助手活跃度面板抓取
//
// 覆盖模块:
//
//	圣域状态      Mod_SaintAreaLogin_Base.get_status        Module 293 Action 2  (0x01250002)
//	圣域登录凭据  Mod_SaintAreaLogin_Base.get_login_info    Module 293 Action 1  (0x01250001)
//	圣域修炼房    Mod_SaPracticeRoom_Base.get_free_room_info Module 407 Action 16 (0x01970010)
//	圣域取经      Mod_SaTakeBible_Base.get_info             Module 395 Action 2  (0x018B0002)
//	圣域竞技场    Mod_SaArena_Base.get_info                 Module 298 Action 0  (0x012A0000)
//	小助手信息    Mod_Assistant_Base.info                   Module 52  Action 0  (0x00340000)
//	小助手活跃度  Mod_Assistant_Base.complete_number        Module 52  Action 2  (0x00340002)
//
// 本探针只发送面板查询请求，不执行入座、抢占、启程、领取等状态变更动作。
// 小助手回包仅打印原始 Hex，供后续解析每日活跃度任务清单。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_saint_area_live.go
// 预期表现: 打印圣域开启状态与三类面板解码结果；小助手回包 Hex 落盘 /tmp/assistant_info_hex.txt。
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
	actionAssistantCompleteNumber uint32 = 0x00340002

	// actionGameAssistantInfo Mod_Player_Base.get_game_assistant_info (Module 0, Action 42)。
	// 回包末尾的 active_degree_list 才是每日/每周活跃度任务进度: [任务ID, 已完成次数, 上限, 每次活跃度]。
	actionGameAssistantInfo uint32 = 0x0000002A

	actionSaArenaGetInfo uint32 = 0x012A0000
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
	fmt.Fprintf(os.Stderr, "gateway=%s\n", ticket.GatewayURL)

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
	slog.Info("主服会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		start := time.Now()
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := s.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		if resp == nil {
			slog.Warn("调用回包为空", "call", name, "action", fmt.Sprintf("0x%08X", act))
			return nil
		}
		slog.Info("调用回包", "call", name, "action", fmt.Sprintf("0x%08X", act),
			"len", len(resp.Payload), "elapsed_ms", time.Since(start).Milliseconds())
		return resp
	}

	// ------------------------------------------------------------------
	// 1. 圣域状态
	// ------------------------------------------------------------------
	statusResp := call(sess, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus, "saint_status", 5*time.Second)
	saintOpen := false
	if statusResp != nil {
		slog.Info("圣域状态原始回包", "hex", hex.EncodeToString(statusResp.Payload))
		if st, pErr := protocol.ParseSaintAreaStatusResult(statusResp.Payload); pErr != nil {
			slog.Warn("解析圣域状态失败", "err", pErr)
		} else {
			saintOpen = st.Status == protocol.SaintAreaStatusOpen
			slog.Info("圣域状态", "status", st.Status, "close_windows", len(st.CloseTimes), "open", saintOpen)
		}
	}

	// ------------------------------------------------------------------
	// 2. 小助手活跃度面板 (只读)
	// ------------------------------------------------------------------
	if r := call(sess, protocol.BuildAssistantInfoPacket(), protocol.ActionIDAssistantInfo, "assistant_info", 5*time.Second); r != nil {
		_ = os.WriteFile("/tmp/assistant_info_hex.txt", []byte(hex.EncodeToString(r.Payload)), 0o644)
		slog.Info("小助手 info 回包", "hex", hex.EncodeToString(r.Payload))
	}
	if r := call(sess, protocol.NewPacket(actionAssistantCompleteNumber, nil), actionAssistantCompleteNumber, "assistant_complete_number", 5*time.Second); r != nil {
		_ = os.WriteFile("/tmp/assistant_complete_number_hex.txt", []byte(hex.EncodeToString(r.Payload)), 0o644)
		slog.Info("小助手 complete_number 回包", "hex", hex.EncodeToString(r.Payload))
	}
	if r := call(sess, protocol.NewPacket(actionGameAssistantInfo, nil), actionGameAssistantInfo, "game_assistant_info", 8*time.Second); r != nil {
		_ = os.WriteFile("/tmp/game_assistant_info_hex.txt", []byte(hex.EncodeToString(r.Payload)), 0o644)
		slog.Info("玩家活跃度任务进度回包已落盘", "len", len(r.Payload), "path", "/tmp/game_assistant_info_hex.txt")
	}

	// 2.2 关键活跃度来源的功能开放状态 (只读)。
	openChecks := []struct {
		id   int32
		name string
	}{
		{88, "聚宝盆"},
		{89, "神龙上供"},
		{96, "寻找异兽"},
		{113, "五行天仪"},
		{131, "家园"},
		{140, "结缘宝箱"},
		{154, "兵法"},
		{172, "孩子"},
		{216, "回忆之境"},
		{228, "宗门灵兽"},
		{235, "法力星图"},
		{243, "服用修为丹"},
		{245, "悟法领域"},
		{256, "多宝山"},
		{272, "仙幽猎境"},
		{281, "诸法器冢"},
		{292, "轮回净地"},
		{294, "纪元湮劫"},
		{136, "五福临门"},
		{119, "仙盟"},
	}
	for _, c := range openChecks {
		open, fErr := sess.IsFunctionOpen(ctx, c.id)
		if fErr != nil {
			slog.Warn("查询功能开放失败", "function_id", c.id, "name", c.name, "err", fErr)
			continue
		}
		slog.Info("功能开放状态", "function_id", c.id, "name", c.name, "open", open)
	}

	// 2.1 今日活跃度的两个只读旁证：刮刮卡与冰窟面板。
	if r := call(sess, protocol.BuildScratchCardInfoPacket(), protocol.ActionIDScratchCardInfo, "scratch_card_info", 5*time.Second); r != nil {
		if info, pErr := protocol.ParseScratchCardInfoResult(r.Payload); pErr != nil {
			slog.Warn("解析刮刮卡面板失败", "err", pErr)
		} else {
			slog.Info("刮刮卡面板活跃度",
				"in_activity", info.InActivity,
				"active_degree", info.ActiveDegree,
				"card_times", info.CardTimes,
			)
		}
	}
	if r := call(sess, protocol.BuildIceCaveInfoPacket(), protocol.ActionIDIceCaveInfo, "ice_cave_info", 5*time.Second); r != nil {
		if info, pErr := protocol.ParseIceCaveInfoResult(r.Payload); pErr != nil {
			slog.Warn("解析冰窟面板失败", "err", pErr)
		} else {
			slog.Info("冰窟面板活跃度",
				"active_num", info.ActiveNum,
				"active_limit", info.ActiveLimit,
				"hammer_num", info.HammerNum,
			)
		}
	}

	// ------------------------------------------------------------------
	// 3. 圣域竞技场面板 (主服连接)
	// ------------------------------------------------------------------
	if r := call(sess, protocol.BuildSaArenaGetInfoPacket(), protocol.ActionIDSaArenaGetInfo, "sa_arena_info", 8*time.Second); r != nil {
		slog.Info("圣域竞技场原始回包", "hex", hex.EncodeToString(r.Payload))
		if info, pErr := protocol.ParseSaArenaGetInfoResult(r.Payload); pErr != nil {
			slog.Warn("解析圣域竞技场失败", "err", pErr)
		} else {
			slog.Info("圣域竞技场面板", "seats", len(info.Seats), "available_count", info.AvailableCount)
			for _, seat := range info.Seats {
				slog.Info("圣域竞技场席位", "seq", seat.Seq, "player_id", seat.SaPlayerID, "nickname", seat.Nickname, "war_power", seat.WarPower)
			}
		}
	}

	if !saintOpen {
		slog.Info("圣域未开启，跳过圣域修炼房与圣域取经面板查询")
		return
	}

	// ------------------------------------------------------------------
	// 4. 建立圣域独立会话，查询圣域修炼房与圣域取经面板
	// ------------------------------------------------------------------
	sa, err := sess.OpenSaintAreaSession(ctx)
	if err != nil {
		slog.Error("建立圣域会话失败", "err", err)
		return
	}
	defer sa.Close()
	slog.Info("圣域会话已建立", "player_id", sa.PlayerID())

	// 官方 SaArenaThroneView 将 Mod_SaArena_Base 的 get_info/rob_the_seat
	// 全部路由到 DataBase.SOCKETSAINTAREA。主服调用面板可读，但抢占会返回
	// ACTIVITY_CLOSE(3)。设置 SXD_PROBE_SA_ARENA=1 时在圣域连接上验证抢占。
	if os.Getenv("SXD_PROBE_SA_ARENA") == "1" {
		infoResp := call(sa, protocol.BuildSaArenaGetInfoPacket(), protocol.ActionIDSaArenaGetInfo, "sa_arena_info_on_sa", 8*time.Second)
		if infoResp != nil {
			info, pErr := protocol.ParseSaArenaGetInfoResult(infoResp.Payload)
			if pErr != nil {
				slog.Warn("圣域连接解析竞技场失败", "err", pErr)
			} else {
				slog.Info("圣域连接竞技场面板", "seats", len(info.Seats), "available_count", info.AvailableCount)
				for _, seat := range info.Seats {
					slog.Info("圣域连接竞技场席位", "seq", seat.Seq, "player_id", seat.SaPlayerID, "nickname", seat.Nickname, "war_power", seat.WarPower)
				}
				for _, seat := range info.Seats {
					if seat.SaPlayerID != 0 {
						continue
					}
					slog.Info("尝试在圣域连接抢占空席位", "seq", seat.Seq)
					robResp := call(sa, protocol.BuildSaArenaRobSeatPacket(seat.Seq), protocol.ActionIDSaArenaRobSeat, "sa_arena_rob_on_sa", 15*time.Second)
					if robResp == nil {
						slog.Warn("圣域连接抢占无回包", "seq", seat.Seq)
						break
					}
					slog.Info("圣域连接抢占原始回包", "seq", seat.Seq, "hex", hex.EncodeToString(robResp.Payload))
					res, rErr := protocol.ParseSaArenaRobResult(robResp.Payload)
					if rErr != nil {
						slog.Warn("解析圣域连接抢占结果失败", "err", rErr)
					} else {
						slog.Info("圣域连接抢占结果", "seq", seat.Seq, "result", res.Result,
							"award_score", res.AwardScore, "win_player_id", res.WinPlayerID)
					}
					break
				}
			}
		}
	}

	if r := call(sa, protocol.BuildSaPracticeRoomFreeInfoPacket(), protocol.ActionIDSaPracticeRoomGetFreeInfo, "sa_free_room_info", 8*time.Second); r != nil {
		slog.Info("圣域修炼房原始回包", "hex", hex.EncodeToString(r.Payload))
		if info, pErr := protocol.ParseSaFreeRoomInfoResult(r.Payload); pErr != nil {
			slog.Warn("解析圣域修炼房失败", "err", pErr)
		} else {
			slog.Info("圣域修炼房",
				"room_quality", info.RoomQuality,
				"exp", info.Exp,
				"practice_time", info.PracticeTime,
				"players", info.TotalPlayer,
				"need_ingot", info.NeedIngot,
				"need_daoyuan", info.NeedDaoYuan,
			)
		}
	}

	takeBibleAct := protocol.ActionIDStTakeBibleCrossGetInfo&0xFFFF | 0x018B0000
	takeBiblePkt := protocol.NewPacket(takeBibleAct, protocol.BuildStTakeBibleCrossGetInfoPacket().Payload)
	if r := call(sa, takeBiblePkt, takeBibleAct, "sa_take_bible_get_info", 10*time.Second); r != nil {
		slog.Info("圣域取经原始回包", "hex", hex.EncodeToString(r.Payload))
		if info, pErr := protocol.ParseStTakeBibleCrossInfoResult(r.Payload); pErr != nil {
			slog.Warn("解析圣域取经详情失败", "err", pErr)
		} else {
			slog.Info("圣域取经详情",
				"take_bible_times", info.TakeBibleTimes,
				"total_times", info.TotalTakeBibleTimes,
				"refresh_price", info.RefreshPrice,
			)
		}
	}
}
