//go:build probe

// 目标协议: 仙界练功房 (Mod_StPracticeRoom_Base, Module 117 / 0x0075) 跨服只读验证
// 基准对齐: 官方 AS3 com.protocols.Mod_StPracticeRoom_Base 描述符 (module 117, action 0..17)
// 运行命令: go run -tags=probe test/probes/probe_st_practice_room_live.go
// 可选开关: SXD_PROBE_NO_PREVACT=1 时所有请求不追加 PrevAct, 用于验证网关是否严格校验前序动作
// 可选开关: SXD_PROBE_ENTER_ROOM=1 时额外调用 enter_room / exit_room 验证面板进入流程
// 预期表现: 建立仙界跨服会话, 打印 get_rooms / get_player_room_quality / get_exp_flag /
//
//	get_free_room_info 的原始回包 Hex 与解码结果
package main

import (
	"context"
	"encoding/binary"
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
	moduleStPracticeRoom uint16 = 117

	actGetRooms              uint32 = 0x00750000
	actGetRoomInfo           uint32 = 0x00750001
	actSitDown               uint32 = 0x00750002
	actGetSeatInfo           uint32 = 0x00750003
	actRobTheSeat            uint32 = 0x00750004
	actBuyRobTime            uint32 = 0x00750005
	actGetRoomPracticeExp    uint32 = 0x00750006
	actGetExpFlag            uint32 = 0x00750007
	actEnterRoom             uint32 = 0x00750008
	actExitRoom              uint32 = 0x00750009
	actCleanRobCDTime        uint32 = 0x0075000A
	actGetRobCDTime          uint32 = 0x0075000B
	actNotifyRefreshExp      uint32 = 0x0075000C
	actPlayerLeaveSeat       uint32 = 0x0075000D
	actGetPlayerRoomQuality  uint32 = 0x0075000E
	actNotifyPracticeRoomMsg uint32 = 0x0075000F
	actGetFreeRoomInfo       uint32 = 0x00750010
	actGetFreeRoomInfoByPage uint32 = 0x00750011
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	noPrevAct := os.Getenv("SXD_PROBE_NO_PREVACT") == "1"

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

	mainCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&mainCfg, ticket)
	mainSess := client.NewRoleSession(mainCfg)
	if err := mainSess.Start(ctx); err != nil {
		slog.Error("主服会话启动失败", "err", err)
		return
	}
	defer mainSess.Close()
	for i := 0; i < 60 && mainSess.State() != client.StateActive; i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if mainSess.State() != client.StateActive {
		slog.Error("主服未激活", "state", mainSess.State())
		return
	}

	credCtx, credCancel := context.WithTimeout(ctx, 5*time.Second)
	cred, err := mainSess.FetchCrossServerCredentials(credCtx)
	credCancel()
	if err != nil {
		slog.Error("拉取仙界凭据失败", "err", err)
		return
	}
	crossSess, err := mainSess.OpenCrossServerSessionWithInfo(ctx, cred)
	if err != nil {
		slog.Error("仙界跨服会话建立失败", "err", err)
		return
	}
	defer crossSess.Close()

	slog.Info("仙界会话已激活",
		"player_id", crossSess.PlayerID(),
		"last_action", fmt.Sprintf("0x%08X", crossSess.LastActionID()),
		"no_prev_act", noPrevAct,
	)

	if diag := os.Getenv("SXD_PROBE_DIAG"); diag != "" {
		runStPracticeRoomDiag(ctx, crossSess, diag)
		return
	}

	newPkt := func(actionID uint32, body []byte) *protocol.Packet {
		if noPrevAct {
			return &protocol.Packet{ActionID: actionID, Payload: body}
		}
		payload := make([]byte, len(body)+4)
		copy(payload, body)
		return &protocol.Packet{ActionID: actionID, Payload: payload, HasPrevAct: true}
	}

	call := func(actionID uint32, body []byte, d time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, d)
		defer cCancel()
		return crossSess.Call(cCtx, newPkt(actionID, body), actionID)
	}

	// callStrict 用于 get_room_info / sit_down / get_seat_info / rob_the_seat
	// 等严格长度业务动作: 追加 PrevAct 会被服务端静默丢弃。
	callStrict := func(actionID uint32, body []byte, d time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(ctx, d)
		defer cCancel()
		return crossSess.Call(cCtx, protocol.NewPacket(actionID, body), actionID)
	}

	// 1. 查询房间列表 (action 0, 请求空载荷)
	resp, err := call(actGetRooms, nil, 6*time.Second)
	if err != nil || resp == nil {
		slog.Error("get_rooms 无回包", "err", err)
		return
	}
	fmt.Printf("\n[get_rooms] %s\n", hex.EncodeToString(resp.Payload))
	if rooms, pErr := parseRooms(resp.Payload); pErr != nil {
		slog.Warn("解析 get_rooms 失败", "err", pErr)
	} else {
		for _, r := range rooms {
			slog.Info("房间", "quality", r.Quality, "count", r.Count)
		}
	}

	// 2. 查询玩家当前所在房间品质 (action 14)
	resp, err = call(actGetPlayerRoomQuality, nil, 6*time.Second)
	if err != nil || resp == nil {
		slog.Warn("get_player_room_quality 无回包", "err", err)
	} else {
		fmt.Printf("\n[get_player_room_quality] %s\n", hex.EncodeToString(resp.Payload))
		if len(resp.Payload) > 0 {
			slog.Info("玩家当前练功房品质", "quality", int8(resp.Payload[0]))
		}
	}

	// 3. 查询待领取经验标记 (action 7)
	resp, err = call(actGetExpFlag, nil, 6*time.Second)
	if err != nil || resp == nil {
		slog.Warn("get_exp_flag 无回包", "err", err)
	} else {
		fmt.Printf("\n[get_exp_flag] %s\n", hex.EncodeToString(resp.Payload))
		if len(resp.Payload) > 0 {
			slog.Info("待领取经验标记", "flag", resp.Payload[0])
		}
	}

	// 4. 查询普通练功场信息 (action 16)
	resp, err = call(actGetFreeRoomInfo, nil, 6*time.Second)
	if err != nil || resp == nil {
		slog.Warn("get_free_room_info 无回包", "err", err)
	} else {
		fmt.Printf("\n[get_free_room_info] %s\n", hex.EncodeToString(resp.Payload))
		if info, pErr := parseFreeRoomInfo(resp.Payload); pErr != nil {
			slog.Warn("解析 get_free_room_info 失败", "err", pErr)
		} else {
			slog.Info("普通练功场", "total_page", info.TotalPage, "total_player", info.TotalPlayer,
				"practice_time", info.PracticeTime, "exp", info.Exp, "rob_time", info.RobTime,
				"player_page", info.PlayerPage, "room_quality", info.RoomQuality,
				"exp_plus_flag", info.ExpPlusFlag, "exp_plus_percent", info.ExpPlusPercent)
		}
	}

	// 5. 可选进入房间面板 (action 8), 并在面板保持打开期间查询座位信息 (action 1)
	if os.Getenv("SXD_PROBE_ENTER_ROOM") == "1" {
		resp, err = call(actEnterRoom, nil, 6*time.Second)
		if err != nil || resp == nil {
			slog.Warn("enter_room 无回包", "err", err)
		} else {
			fmt.Printf("\n[enter_room] %s\n", hex.EncodeToString(resp.Payload))
		}

		// 6. 可选按房间品质查询座位信息 (SXD_PROBE_QUALITY=N)
		if q := os.Getenv("SXD_PROBE_QUALITY"); q != "" {
			var quality int
			fmt.Sscanf(q, "%d", &quality)
			resp, err = callStrict(actGetRoomInfo, []byte{byte(quality)}, 6*time.Second)
			if err != nil || resp == nil {
				slog.Warn("get_room_info 无回包", "quality", quality, "err", err)
			} else {
				fmt.Printf("\n[get_room_info quality=%d] %s\n", quality, hex.EncodeToString(resp.Payload))
				if seats, _, pErr := parseRoomInfo(resp.Payload); pErr != nil {
					slog.Warn("解析 get_room_info 失败", "err", pErr)
				} else {
					slog.Info("房间座位信息", "quality", quality, "occupied_seats", seats)
				}
			}
		}

		resp, err = call(actExitRoom, nil, 6*time.Second)
		if err != nil || resp == nil {
			slog.Warn("exit_room 无回包", "err", err)
		} else {
			fmt.Printf("\n[exit_room] %s\n", hex.EncodeToString(resp.Payload))
		}
	}

	// 7. 查询争夺冷却 (action 11, 只读)
	resp, err = call(actGetRobCDTime, nil, 6*time.Second)
	if err != nil || resp == nil {
		slog.Warn("get_rob_cd_time 无回包", "err", err)
	} else {
		fmt.Printf("\n[get_rob_cd_time] %s\n", hex.EncodeToString(resp.Payload))
		if len(resp.Payload) >= 4 {
			slog.Info("争夺冷却", "cd", int16(binary.BigEndian.Uint16(resp.Payload[0:2])),
				"ignot", int16(binary.BigEndian.Uint16(resp.Payload[2:4])))
		}
	}

	// 8. 可选真实执行维护写操作 (SXD_PROBE_APPLY=1)
	if os.Getenv("SXD_PROBE_APPLY") == "1" {
		applyStPracticeRoomMaintenance(ctx, crossSess)
	}
}

type roomEntry struct {
	Quality uint8
	Count   int16
}

// parseRooms 解析 get_rooms 回包:
// [Uint16 count][count × (Byte quality, Short count)], Uint8 in_practice_flag, Byte quality.
func parseRooms(payload []byte) ([]roomEntry, error) {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	rooms := make([]roomEntry, 0, count)
	for i := 0; i < int(count); i++ {
		q, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		c, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, roomEntry{Quality: q, Count: c})
	}
	inPractice, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	quality, err := r.ReadInt8()
	if err != nil {
		return nil, err
	}
	slog.Info("房间列表尾字段", "in_practice_flag", inPractice, "current_quality", quality)
	return rooms, nil
}

type freeRoomInfo struct {
	TotalPage      int16
	TotalPlayer    int16
	PracticeTime   int32
	Exp            int64
	RobTime        int16
	PlayerPage     int16
	RoomQuality    int8
	ExpPlusFlag    uint8
	ExpPlusPercent int8
}

// parseFreeRoomInfo 解析 get_free_room_info 回包:
// Short total_page, Short total_player, Int practice_time, Long exp,
// Short rob_time, Short player_page, Byte room_quality, UByte exp_plus_flag, Byte exp_plus_percent.
func parseFreeRoomInfo(payload []byte) (*freeRoomInfo, error) {
	r := protocol.NewReader(payload)
	var out freeRoomInfo
	var err error
	if out.TotalPage, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if out.TotalPlayer, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if out.PracticeTime, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.Exp, err = r.ReadInt64(); err != nil {
		return nil, err
	}
	if out.RobTime, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if out.PlayerPage, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if out.RoomQuality, err = r.ReadInt8(); err != nil {
		return nil, err
	}
	if out.ExpPlusFlag, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if out.ExpPlusPercent, err = r.ReadInt8(); err != nil {
		return nil, err
	}
	return &out, nil
}

// parseRoomInfo 解析星级房间 get_room_info 回包, 返回已占用席位与累计经验。
func parseRoomInfo(payload []byte) ([]int8, int64, error) {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, 0, err
	}
	occupied := make([]int8, 0, count)
	for i := 0; i < int(count); i++ {
		seatSeq, err := r.ReadInt8()
		if err != nil {
			return nil, 0, err
		}
		occupied = append(occupied, seatSeq)
		// player_id, role_id, equip_id
		for j := 0; j < 3; j++ {
			if _, err := r.ReadInt32(); err != nil {
				return nil, 0, err
			}
		}
		// nick_name, stage_name, server_name
		for j := 0; j < 3; j++ {
			if _, err := r.ReadString(); err != nil {
				return nil, 0, err
			}
		}
		if _, err := r.ReadInt64(); err != nil { // attack_power
			return nil, 0, err
		}
		if _, err := r.ReadInt32(); err != nil { // level
			return nil, 0, err
		}
		if _, err := r.ReadUint8(); err != nil { // enemy_flag
			return nil, 0, err
		}
		if _, err := r.ReadInt32(); err != nil { // supreme_vip_prefix
			return nil, 0, err
		}
	}
	if _, err := r.ReadInt32(); err != nil { // practice_time
		return nil, 0, err
	}
	exp, err := r.ReadInt64()
	if err != nil {
		return nil, 0, err
	}
	return occupied, exp, nil
}

// applyStPracticeRoomMaintenance 可选真实执行一轮维护 (SXD_PROBE_APPLY=1):
// 领取累积经验并在有空闲席位的高星房间入座, 用于验证写操作线格式。
func applyStPracticeRoomMaintenance(ctx context.Context, sess *client.RoleSession) {
	result, err := sess.EnterStPracticeRoom(ctx)
	if err != nil {
		slog.Error("enter_room 调用失败", "err", err)
		return
	}
	slog.Info("enter_room", "result", result)

	list, err := sess.GetStPracticeRoomList(ctx)
	if err != nil {
		slog.Error("get_rooms 调用失败", "err", err)
		return
	}
	currentQuality := uint8(0)
	seated := list.InPracticeFlag == protocol.StPracticeRoomYes && list.Quality > 0
	if seated {
		currentQuality = uint8(list.Quality)
	}
	slog.Info("维护前状态", "seated", seated, "quality", currentQuality)

	// 查找收益最高的空闲星级房间。
	var targetQuality uint8
	var targetSeat uint8
	found := false
	for q := 5; q >= 1; q-- {
		quality := uint8(q)
		var entry *protocol.StPracticeRoomEntry
		for i := range list.Rooms {
			if list.Rooms[i].Quality == quality {
				entry = &list.Rooms[i]
				break
			}
		}
		seatCount := protocol.StPracticeRoomSeatCount(quality)
		if entry == nil || seatCount <= 0 || int(entry.Count) >= seatCount {
			continue
		}
		info, err := sess.GetStPracticeRoomInfo(ctx, quality)
		if err != nil {
			slog.Warn("get_room_info 调用失败", "quality", quality, "err", err)
			continue
		}
		occupied := make(map[int8]bool, len(info.Seats))
		for _, seat := range info.Seats {
			occupied[seat.SeatSeq] = true
		}
		for seat := 0; seat < seatCount; seat++ {
			if !occupied[int8(seat)] {
				targetQuality = quality
				targetSeat = uint8(seat)
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	slog.Info("目标席位", "found", found, "quality", targetQuality, "seat", targetSeat)

	if seated {
		leaveResult, err := sess.LeaveStPracticeRoomSeat(ctx)
		if err != nil {
			slog.Error("player_leave_seat 调用失败", "err", err)
			return
		}
		slog.Info("player_leave_seat", "result", leaveResult)
	}

	expResult, err := sess.ClaimStPracticeRoomExp(ctx)
	if err != nil {
		slog.Error("get_room_practice_exp 调用失败", "err", err)
		return
	}
	slog.Info("get_room_practice_exp", "result", expResult.Result, "gain_exp", expResult.Exp)

	if !found {
		targetQuality = protocol.StPracticeRoomFreeQuality
		targetSeat = 0
	}
	if targetQuality != protocol.StPracticeRoomFreeQuality {
		status, err := sess.GetStPracticeRoomSeatStatus(ctx, targetQuality, targetSeat)
		if err != nil {
			slog.Error("get_seat_info 调用失败", "err", err)
			return
		}
		slog.Info("get_seat_info", "quality", targetQuality, "seat", targetSeat, "status", status)
	}
	sitResult, err := sess.SitDownStPracticeRoom(ctx, targetQuality, targetSeat)
	if err != nil {
		slog.Error("sit_down 调用失败", "err", err)
		return
	}
	slog.Info("sit_down", "quality", targetQuality, "seat", targetSeat, "result", sitResult)

	finalList, err := sess.GetStPracticeRoomList(ctx)
	if err == nil {
		slog.Info("维护后状态", "in_practice_flag", finalList.InPracticeFlag, "quality", finalList.Quality)
	}

	exitResult, err := sess.ExitStPracticeRoom(ctx)
	if err != nil {
		slog.Warn("exit_room 调用失败", "err", err)
		return
	}
	slog.Info("exit_room", "result", exitResult)
}

// runStPracticeRoomDiag 单动作诊断 (SXD_PROBE_DIAG=<mode>):
// 隔离验证某个 ActionID 是否接受 PrevAct, 以及回包 ActionID 是否与请求一致。
//
// 支持模式:
//
//	state / free_info / free_page            只读查询
//	room_info_prev / room_info_noprev        进入面板后查询星级房间
//	room_info_plain                          不进入面板直接查询星级房间
//	sit_prev / sit_noprev                    普通练功场入座 (带/不带 PrevAct)
func runStPracticeRoomDiag(ctx context.Context, sess *client.RoleSession, mode string) {
	sess.SetPacketObserver(func(p *protocol.Packet) {
		n := len(p.Payload)
		if n > 64 {
			n = 64
		}
		slog.Info("inbound", "action", fmt.Sprintf("0x%08X", p.ActionID),
			"len", len(p.Payload), "hex", hex.EncodeToString(p.Payload[:n]))
	})

	raw := func(actionID uint32, payload []byte, withPrev bool, d time.Duration) (*protocol.Packet, error) {
		var pkt *protocol.Packet
		if withPrev {
			body := make([]byte, len(payload)+4)
			copy(body, payload)
			pkt = &protocol.Packet{ActionID: actionID, Payload: body, HasPrevAct: true}
		} else {
			pkt = protocol.NewPacket(actionID, payload)
		}
		cCtx, cCancel := context.WithTimeout(ctx, d)
		defer cCancel()
		return sess.Call(cCtx, pkt, actionID)
	}

	needEnter := mode == "room_info_prev" || mode == "room_info_noprev" ||
		mode == "room_info_free" || mode == "room_info_int" ||
		mode == "sit_prev" || mode == "sit_q0" || mode == "sit_seat1" ||
		mode == "sit_swapped" || mode == "sit_int" || mode == "free_page" ||
		mode == "claim_seated"
	if needEnter {
		slog.Info("diag: enter_room (PrevAct)")
		resp, err := raw(actEnterRoom, nil, true, 6*time.Second)
		if err != nil || resp == nil {
			slog.Error("diag: enter_room 失败", "err", err)
			return
		}
		fmt.Printf("[diag] enter_room resp=%s\n", hex.EncodeToString(resp.Payload))
	}

	quality := uint8(4)
	if q := os.Getenv("SXD_PROBE_QUALITY"); q != "" {
		var v int
		fmt.Sscanf(q, "%d", &v)
		quality = uint8(v)
	}

	var actionID uint32
	var payload []byte
	var withPrev bool
	switch mode {
	case "state":
		actionID, withPrev = actGetRooms, true
	case "free_info":
		actionID, withPrev = actGetFreeRoomInfo, true
	case "free_page":
		page := int16(1)
		if p := os.Getenv("SXD_PROBE_PAGE"); p != "" {
			var v int
			fmt.Sscanf(p, "%d", &v)
			page = int16(v)
		}
		w := protocol.NewWriter()
		w.WriteInt16(page)
		actionID, payload, withPrev = actGetFreeRoomInfoByPage, w.Bytes(), true
	case "room_info_prev":
		actionID, payload, withPrev = actGetRoomInfo, []byte{quality}, true
	case "room_info_noprev":
		actionID, payload, withPrev = actGetRoomInfo, []byte{quality}, false
	case "room_info_plain":
		actionID, payload, withPrev = actGetRoomInfo, []byte{quality}, true
	case "room_info_free":
		actionID, payload, withPrev = actGetRoomInfo, []byte{protocol.StPracticeRoomFreeQuality}, true
	case "room_info_int":
		w := protocol.NewWriter()
		w.WriteInt32(int32(quality))
		actionID, payload, withPrev = actGetRoomInfo, w.Bytes(), true
	case "sit_prev":
		actionID, payload, withPrev = actSitDown, []byte{protocol.StPracticeRoomFreeQuality, 0}, true
	case "sit_noprev":
		actionID, payload, withPrev = actSitDown, []byte{protocol.StPracticeRoomFreeQuality, 0}, false
	case "sit_q0":
		actionID, payload, withPrev = actSitDown, []byte{0, 0}, true
	case "sit_seat1":
		actionID, payload, withPrev = actSitDown, []byte{protocol.StPracticeRoomFreeQuality, 1}, true
	case "sit_swapped":
		actionID, payload, withPrev = actSitDown, []byte{0, protocol.StPracticeRoomFreeQuality}, true
	case "sit_int":
		w := protocol.NewWriter()
		w.WriteInt32(int32(protocol.StPracticeRoomFreeQuality))
		w.WriteUint8(0)
		actionID, payload, withPrev = actSitDown, w.Bytes(), true
	case "claim_seated":
		// 官方“领取经验”按钮: 不离座直接领取, 验证是否会踢出当前席位。
		actionID, withPrev = actGetRoomPracticeExp, true
	default:
		slog.Error("diag: 未知模式", "mode", mode)
		return
	}

	slog.Info("diag: 发送请求", "mode", mode, "action", fmt.Sprintf("0x%08X", actionID),
		"payload", hex.EncodeToString(payload), "with_prev_act", withPrev)
	resp, err := raw(actionID, payload, withPrev, 8*time.Second)
	if err != nil || resp == nil {
		slog.Error("diag: 请求无回包", "mode", mode, "err", err)
		return
	}
	fmt.Printf("[diag %s] action=0x%08X resp_action=0x%08X resp=%s\n",
		mode, actionID, resp.ActionID, hex.EncodeToString(resp.Payload))

	if mode == "claim_seated" {
		time.Sleep(500 * time.Millisecond)
		stateResp, stateErr := raw(actGetRooms, nil, true, 8*time.Second)
		if stateErr != nil || stateResp == nil {
			slog.Error("diag: 领取后查询状态失败", "err", stateErr)
			return
		}
		fmt.Printf("[diag claim_seated] post_claim_state=%s\n", hex.EncodeToString(stateResp.Payload))
	}
}
