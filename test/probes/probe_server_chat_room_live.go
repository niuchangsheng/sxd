//go:build probe

// 目标: 全网聊天室 (Mod_ServerChatRoom_Base, Module 336) 协议探针。
//
// 链路 (官方客户端 View.as):
//  1. 主服 336/0  get_recent_room_list  request=[] response=[UByte result, [Byte room_id]]
//  2. 主服 336/11 get_chat_room_logincode request=[Byte room_id]
//     response=[UByte result, String ip, String node, String servername, Int port,
//     String stagename, String login_code, Int timestamp]  (字段序待实测)
//  3. 聊天服 336/12 login_chat_room
//     request=[String node, Int player_id, String servername, String stagename, Int timestamp, String login_code]
//     response=[UByte result, Int ?]
//  4. 聊天服 336/20 chat_with_players
//     request=[String message, String eip_num, String eip_index] response=[Int ?, UByte result]
//
// 本探针先只读打印 336/0、336/11 的回包 hex；-send 时再连接聊天服并尝试发一条表情。
// 不写死任何凭据，凭据来自 SXD_CONFIG 指定的配置。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_server_chat_room_live.go -role 0
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const (
	actionRecentRooms uint32 = 0x01500000 // 336/0
	actionRoomStatus  uint32 = 0x01500001 // 336/1
	actionLoginCode   uint32 = 0x0150000B // 336/11
	actionChatLogin   uint32 = 0x0150000C // 336/12
	actionChatSpeak   uint32 = 0x01500014 // 336/20
)

func hexDump(tag string, payload []byte) {
	limit := len(payload)
	if limit > 200 {
		limit = 200
	}
	slog.Info(tag, "len", len(payload), "hex", hex.EncodeToString(payload[:limit]))
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
	send := flag.Bool("send", false, "连接聊天服并尝试发送一条表情")
	eipNum := flag.String("eip-num", "1", "表情包编号 (eip_num)")
	eipIndex := flag.String("eip-index", "16", "表情编号 (eip_index)")
	text := flag.String("text", "", "随表情发送的文本")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

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
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	callMain := func(act uint32, payload []byte, timeout time.Duration) []byte {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, protocol.NewPacket(act, payload), act)
		if cErr != nil || resp == nil {
			slog.Warn("主服请求无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		hexDump(fmt.Sprintf("主服 %d/%d 回包", act>>16, act&0xFFFF), resp.Payload)
		return resp.Payload
	}

	// 1. 最近聊天室列表
	payload := callMain(actionRecentRooms, nil, 8*time.Second)
	if payload == nil {
		slog.Info("探针结束: 336/0 无回包")
		return
	}

	roomIDs := parseRoomIDs(payload)
	slog.Info("聊天室列表解析", "result", firstByte(payload), "room_ids", roomIDs)

	// 2. 状态与登录码
	callMain(actionRoomStatus, nil, 8*time.Second)
	if len(roomIDs) == 0 {
		slog.Warn("没有可用的聊天室，结束")
		return
	}
	roomID := roomIDs[0]
	codePayload := callMain(actionLoginCode, []byte{byte(int8(roomID))}, 8*time.Second)
	if codePayload == nil {
		slog.Info("探针结束: 336/11 无回包")
		return
	}
	info := parseLoginCode(codePayload)
	slog.Info("登录码解析", "result", info.Result, "ip", info.IP, "node", info.Node,
		"servername", info.ServerName, "port", info.Port, "stagename", info.StageName,
		"login_code", info.LoginCode, "timestamp", info.Timestamp)
	if !*send {
		slog.Info("探针结束 (未指定 -send)")
		return
	}
	if info.IP == "" || info.Port <= 0 {
		slog.Warn("登录码未解析出 IP/端口，无法连接聊天服")
		return
	}

	// 3. 连接聊天服并登录
	addr := net.JoinHostPort(info.IP, fmt.Sprintf("%d", info.Port))
	conn, dErr := net.DialTimeout("tcp", addr, 10*time.Second)
	if dErr != nil {
		slog.Error("连接聊天服失败", "addr", addr, "err", dErr)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	loginPkt := protocol.NewPacket(actionChatLogin, buildLoginPayload(info, sess.PlayerID()))
	if wErr := protocol.WritePacket(conn, loginPkt); wErr != nil {
		slog.Error("发送聊天服登录失败", "err", wErr)
		return
	}
	loginResp, rErr := protocol.ReadPacket(conn)
	if rErr != nil {
		slog.Error("读取聊天服登录回包失败", "err", rErr)
		return
	}
	hexDump("聊天服 336/12 回包", loginResp.Payload)
	if firstByte(loginResp.Payload) != 9 { // SUCCESS=9
		slog.Warn("聊天服登录未成功，结束", "result", firstByte(loginResp.Payload))
		return
	}

	// 4. 发送表情
	speakPkt := protocol.NewPacket(actionChatSpeak, buildSpeakPayload(*text, *eipNum, *eipIndex))
	if wErr := protocol.WritePacket(conn, speakPkt); wErr != nil {
		slog.Error("发送表情失败", "err", wErr)
		return
	}
	speakResp, sErr := protocol.ReadPacket(conn)
	if sErr != nil {
		slog.Error("读取表情回包失败", "err", sErr)
		return
	}
	hexDump("聊天服 336/20 回包", speakResp.Payload)
	slog.Info("探针结束")
}

func firstByte(b []byte) uint8 {
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// parseRoomIDs 按 [UByte result, [Byte room_id]] 解析；数组长度前缀尝试 uint16/uint8。
func parseRoomIDs(payload []byte) []int8 {
	if len(payload) < 2 {
		return nil
	}
	// 尝试 uint16 长度前缀: result + uint16 count + ids
	if len(payload) >= 3 {
		cnt := int(binary.BigEndian.Uint16(payload[1:3]))
		if cnt >= 0 && 3+cnt <= len(payload) {
			ids := make([]int8, 0, cnt)
			for i := 0; i < cnt; i++ {
				ids = append(ids, int8(payload[3+i]))
			}
			return ids
		}
	}
	// 退化为 uint8 长度前缀
	cnt := int(payload[1])
	if 2+cnt <= len(payload) {
		ids := make([]int8, 0, cnt)
		for i := 0; i < cnt; i++ {
			ids = append(ids, int8(payload[2+i]))
		}
		return ids
	}
	return nil
}

type loginCodeInfo struct {
	Result     uint8
	IP         string
	Node       string
	ServerName string
	Port       int32
	StageName  string
	LoginCode  string
	Timestamp  int32
}

// parseLoginCode 解析 336/11 回包。2026-11-15 实测字段序:
// [UByte flag, String node("s0@host"), String servername, String stagename,
//
//	Int timestamp, String login_code, String host, Int port]。
func parseLoginCode(payload []byte) loginCodeInfo {
	var out loginCodeInfo
	r := protocol.NewReader(payload)
	out.Result, _ = r.ReadUint8()
	out.Node, _ = r.ReadString()
	out.ServerName, _ = r.ReadString()
	out.StageName, _ = r.ReadString()
	out.Timestamp, _ = r.ReadInt32()
	out.LoginCode, _ = r.ReadString()
	out.IP, _ = r.ReadString()
	out.Port, _ = r.ReadInt32()
	return out
}

func buildLoginPayload(info loginCodeInfo, playerID int32) []byte {
	w := protocol.NewWriter()
	w.WriteString(info.Node)
	w.WriteInt32(playerID)
	w.WriteString(info.ServerName)
	w.WriteString(info.StageName)
	w.WriteInt32(info.Timestamp)
	w.WriteString(info.LoginCode)
	return w.Bytes()
}

func buildSpeakPayload(text, eipNum, eipIndex string) []byte {
	w := protocol.NewWriter()
	w.WriteString(text)
	w.WriteString(eipNum)
	w.WriteString(eipIndex)
	return w.Bytes()
}
