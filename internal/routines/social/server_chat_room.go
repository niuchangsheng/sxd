// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// 本文件实现全网聊天室每日发言 (Mod_ServerChatRoom_Base, Module 336)：
//   - 主服 336/0 取最近聊天室列表；
//   - 主服 336/11 换取聊天服地址与登录码；
//   - 连接独立聊天服 336/12 登录；
//   - 336/20 发送一条表情（默认 eip_num=1 / eip_index=16）。
//
// 每天最多成功发送一次，跨进程通过 data/runtime/server_chat_room_state.json 记账。
package social

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// ServerChatRoomConfig 全网聊天室发言配置。
type ServerChatRoomConfig struct {
	// EipNum 表情包编号 (聊天室 336/20 的 eip_num)。
	EipNum string
	// EipIndex 表情编号 (聊天室 336/20 的 eip_index，客户端渲染 eip<index>)。
	EipIndex string
	// Message 随表情附带的文本，默认空。
	Message string
	// RoomID 指定房间编号；0 表示使用列表中的第一个房间。
	RoomID int8
}

// DefaultServerChatRoomConfig 返回默认配置：每天发一次 1 号表情包第 16 个表情。
func DefaultServerChatRoomConfig() ServerChatRoomConfig {
	return ServerChatRoomConfig{EipNum: "1", EipIndex: "16"}
}

// serverChatRoomStatePath 每日发送记账文件；测试可通过变量替换为临时路径。
var serverChatRoomStatePath = filepath.Join("data", "runtime", "server_chat_room_state.json")

var serverChatRoomStateMu sync.Mutex

type serverChatRoomState struct {
	// LastSentDate 按 role_id 记录最后一次成功发言的本地日期 (YYYY-MM-DD)。
	LastSentDate map[string]string `json:"last_sent_date"`
}

func loadServerChatRoomState() serverChatRoomState {
	state := serverChatRoomState{LastSentDate: make(map[string]string)}
	data, err := os.ReadFile(serverChatRoomStatePath)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	if state.LastSentDate == nil {
		state.LastSentDate = make(map[string]string)
	}
	return state
}

// serverChatRoomSentToday 判断指定角色今天是否已成功发言。
func serverChatRoomSentToday(roleID string) bool {
	serverChatRoomStateMu.Lock()
	defer serverChatRoomStateMu.Unlock()
	state := loadServerChatRoomState()
	return state.LastSentDate[roleID] == time.Now().Format("2006-01-02")
}

// markServerChatRoomSent 记录指定角色今日已发言。
func markServerChatRoomSent(roleID string) {
	serverChatRoomStateMu.Lock()
	defer serverChatRoomStateMu.Unlock()
	state := loadServerChatRoomState()
	state.LastSentDate[roleID] = time.Now().Format("2006-01-02")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		slog.Warn("全网聊天室发送记账序列化失败", "role_id", roleID, "err", err)
		return
	}
	if dir := filepath.Dir(serverChatRoomStatePath); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(serverChatRoomStatePath, data, 0o644); err != nil {
		slog.Warn("全网聊天室发送记账写入失败", "role_id", roleID, "err", err)
	}
}

// NewServerChatRoomRoutine 构造全网聊天室每日表情发言任务。
//
// 流程对齐官方客户端：336/0 房间列表 → 336/11 登录码 → 336/12 聊天服登录
// → 336/20 发表情。所有回包结果码均参与分支判定，不把“有回包”当成功。
func NewServerChatRoomRoutine(cfgs ...ServerChatRoomConfig) scheduler.ActivityRoutine {
	cfg := DefaultServerChatRoomConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.EipNum == "" {
		cfg.EipNum = "1"
	}
	if cfg.EipIndex == "" {
		cfg.EipIndex = "16"
	}
	return scheduler.NewBaseRoutine(
		"server_chat_room",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			if session.State() != client.StateActive {
				return nil
			}
			roleID := session.RoleID()
			if serverChatRoomSentToday(roleID) {
				slog.Debug("全网聊天室今日已发送，跳过", "role_id", roleID)
				return scheduler.ErrAttemptsExhausted
			}

			callMain := func(pkt *protocol.Packet, act uint32, timeout time.Duration) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil || resp == nil {
					return resp, err
				}
				return resp, nil
			}

			// 1. 最近聊天室列表
			listResp, listErr := callMain(protocol.BuildServerChatRoomRecentRoomsPacket(),
				protocol.ActionIDServerChatRoomRecentRooms, 8*time.Second)
			if listErr != nil || listResp == nil {
				slog.Info("全网聊天室列表无回包，稍后重试", "role_id", roleID, "err", listErr)
				return scheduler.NewCooldown(30*time.Minute, "全网聊天室列表无回包")
			}
			result, rooms, pErr := protocol.ParseServerChatRoomRecentRoomsResult(listResp.Payload)
			if pErr != nil {
				slog.Warn("解析全网聊天室列表失败", "role_id", roleID, "err", pErr)
				return nil
			}
			if result != protocol.ServerChatRoomResultSuccess {
				slog.Info("全网聊天室列表返回失败结果码",
					"role_id", roleID, "result", result, "result_text", protocol.ServerChatRoomResultText(result))
				return scheduler.NewCooldown(30*time.Minute, "全网聊天室列表返回失败结果码")
			}
			if len(rooms) == 0 {
				slog.Info("全网聊天室暂无可用房间，稍后重试", "role_id", roleID)
				return scheduler.NewCooldown(30*time.Minute, "全网聊天室无可用房间")
			}
			roomID := rooms[0]
			if cfg.RoomID != 0 {
				found := false
				for _, id := range rooms {
					if id == cfg.RoomID {
						found = true
						break
					}
				}
				if found {
					roomID = cfg.RoomID
				} else {
					slog.Info("全网聊天室配置房间不在列表中，使用首个房间",
						"role_id", roleID, "configured_room", cfg.RoomID, "room_id", roomID)
				}
			}

			// 2. 换取聊天服地址与登录码
			codeResp, codeErr := callMain(protocol.BuildServerChatRoomLoginCodePacket(roomID),
				protocol.ActionIDServerChatRoomLoginCode, 8*time.Second)
			if codeErr != nil || codeResp == nil {
				slog.Info("全网聊天室登录码无回包，稍后重试", "role_id", roleID, "room_id", roomID, "err", codeErr)
				return scheduler.NewCooldown(30*time.Minute, "全网聊天室登录码无回包")
			}
			info, infoErr := protocol.ParseServerChatRoomLoginCodeResult(codeResp.Payload)
			if infoErr != nil {
				slog.Warn("解析全网聊天室登录码失败", "role_id", roleID, "err", infoErr)
				return nil
			}
			if info.Host == "" || info.Port <= 0 || info.LoginCode == "" {
				slog.Info("全网聊天室登录码缺少地址/登录码，稍后重试",
					"role_id", roleID, "host", info.Host, "port", info.Port)
				return scheduler.NewCooldown(30*time.Minute, "全网聊天室登录码不完整")
			}

			// 3. 登录独立聊天服
			// 会话生命周期绑定该 ctx，发言完成后再取消，避免登录后立即被关闭。
			chatCtx, chatCancel := context.WithTimeout(ctx, 60*time.Second)
			chat, chatErr := session.OpenServerChatRoomSession(chatCtx, info)
			if chatErr != nil {
				chatCancel()
				slog.Info("全网聊天服登录失败，稍后重试", "role_id", roleID, "err", chatErr)
				return scheduler.NewCooldown(30*time.Minute, "全网聊天服登录失败")
			}
			defer chatCancel()
			defer chat.Close()

			// 4. 发送表情
			speakPkt := protocol.BuildServerChatRoomSpeakPacket(cfg.Message, cfg.EipNum, cfg.EipIndex)
			speakCtx, speakCancel := context.WithTimeout(ctx, 10*time.Second)
			speakResp, speakErr := chat.Call(speakCtx, speakPkt, protocol.ActionIDServerChatRoomSpeak)
			speakCancel()
			if speakErr != nil || speakResp == nil {
				slog.Info("全网聊天室发言无回包，稍后重试", "role_id", roleID, "err", speakErr)
				return scheduler.NewCooldown(30*time.Minute, "全网聊天室发言无回包")
			}
			speakResult, timeDiff, sErr := protocol.ParseServerChatRoomSpeakResult(speakResp.Payload)
			if sErr != nil {
				slog.Warn("解析全网聊天室发言回包失败", "role_id", roleID, "err", sErr)
				return nil
			}
			switch speakResult {
			case protocol.ServerChatRoomResultSuccess:
				markServerChatRoomSent(roleID)
				slog.Info("全网聊天室表情发言成功",
					"role_id", roleID, "room_id", roomID,
					"eip_num", cfg.EipNum, "eip_index", cfg.EipIndex)
				return scheduler.ErrAttemptsExhausted
			case protocol.ServerChatRoomResultTimeDiff:
				// 服务端认为发言间隔未到（通常表示今日已经发过），记账后今日不再尝试。
				markServerChatRoomSent(roleID)
				slog.Info("全网聊天室发言间隔未到，今日不再尝试",
					"role_id", roleID, "time_diff", timeDiff)
				return scheduler.ErrAttemptsExhausted
			case protocol.ServerChatRoomResultDisable, protocol.ServerChatRoomResultForbid:
				slog.Warn("全网聊天室发言被禁止，今日不再尝试",
					"role_id", roleID, "result", speakResult,
					"result_text", protocol.ServerChatRoomResultText(speakResult))
				return scheduler.ErrAttemptsExhausted
			default:
				slog.Info("全网聊天室发言未成功",
					"role_id", roleID, "result", speakResult,
					"result_text", protocol.ServerChatRoomResultText(speakResult))
				return scheduler.NewCooldown(time.Hour, "全网聊天室发言未成功")
			}
		},
	)
}
