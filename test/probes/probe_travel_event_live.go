//go:build probe

// 目标: 仙旅奇缘答题链路 (Mod_TravelEvent_Base, Module 22?/见 protocol)
//
//	get_activity_info -> get_event_and_answer -> answer_travel_event
//	本探针对比 answer 请求的两种编码:
//	  A. 8 字节 [Int event_id, Int answer_id]
//	  B. 12 字节 [Int event_id, Int answer_id, Uint32 prev_act]
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_travel_event_live.go
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	roleIdx := 0
	if v := os.Getenv("SXD_ROLE_INDEX"); v != "" {
		fmt.Sscanf(v, "%d", &roleIdx)
	}
	if roleIdx < 0 || roleIdx >= len(acc.Roles) {
		roleIdx = 0
	}
	role := acc.Roles[roleIdx]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	infoPkt, _ := protocol.BuildFateGetInfoPacket(protocol.FateGetInfoRequest{})
	if r := call(infoPkt, protocol.ActionIDFateGetInfo, 8*time.Second); r != nil {
		slog.Info("活动信息", "hex", hex.EncodeToString(r.Payload))
	}
	qPkt, _ := protocol.BuildFateQuestionPacket(protocol.FateQuestionRequest{})
	qResp := call(qPkt, protocol.ActionIDFateGetQuestion, 8*time.Second)
	if qResp == nil {
		slog.Error("获取题目失败")
		return
	}
	slog.Info("题目回包", "hex", hex.EncodeToString(qResp.Payload))
	qRes, perr := protocol.ParseFateQuestionResult(qResp.Payload)
	if perr != nil || qRes == nil || len(qRes.Options) == 0 {
		slog.Error("解析题目失败", "err", perr)
		return
	}
	chosen := qRes.Options[0]
	slog.Info("选择选项", "event_id", qRes.EventID, "answer_id", chosen.AnswerID, "text", chosen.Text,
		"cur_answer_times", qRes.CurAnswerTimes, "tol_answer_times", qRes.TolAnswerTimes, "join_count", qRes.JoinCount)
	if qRes.CurAnswerTimes >= qRes.TolAnswerTimes && qRes.TolAnswerTimes > 0 {
		// 面板认为本轮答题已满, 先开宝箱 (Action 4) 再重新拉题.
		drawPkt := protocol.NewPacket(0x00150004, []byte{0, 0, 0, 0})
		drawPkt.HasPrevAct = true
		if r := call(drawPkt, 0x00150004, 8*time.Second); r != nil {
			slog.Info("开宝箱回包", "hex", hex.EncodeToString(r.Payload))
		}
		if qResp2 := call(qPkt, protocol.ActionIDFateGetQuestion, 8*time.Second); qResp2 != nil {
			if q2, perr := protocol.ParseFateQuestionResult(qResp2.Payload); perr == nil && q2 != nil {
				slog.Info("重拉题目", "event_id", q2.EventID, "cur", q2.CurAnswerTimes, "tol", q2.TolAnswerTimes,
					"join", q2.JoinCount, "msg", q2.Msg, "options", len(q2.Options))
				if len(q2.Options) > 0 {
					qRes = q2
					chosen = q2.Options[0]
				}
			}
		}
	}

	// A: 8 字节
	w := protocol.NewWriter()
	w.WriteInt32(qRes.EventID)
	w.WriteInt32(chosen.AnswerID)
	pktA := protocol.NewPacket(protocol.ActionIDFateAnswer, w.Bytes())
	if r := call(pktA, protocol.ActionIDFateAnswer, 10*time.Second); r != nil {
		slog.Info("答案回包A(8字节)", "hex", hex.EncodeToString(r.Payload))
	} else {
		// 是否被服务端静默接受？查询活动状态 joinCount 是否 +1。
		time.Sleep(1 * time.Second)
		if r2 := call(infoPkt, protocol.ActionIDFateGetInfo, 8*time.Second); r2 != nil {
			slog.Info("答题后活动信息", "hex", hex.EncodeToString(r2.Payload))
		}
	}
	return
	// B: 12 字节带 prevAct
	w2 := protocol.NewWriter()
	w2.WriteInt32(qRes.EventID)
	w2.WriteInt32(chosen.AnswerID)
	w2.WriteUint32(sess.LastActionID())
	pktB := protocol.NewPacket(protocol.ActionIDFateAnswer, w2.Bytes())
	if r := call(pktB, protocol.ActionIDFateAnswer, 10*time.Second); r != nil {
		slog.Info("答案回包B(12字节)", "hex", hex.EncodeToString(r.Payload))
	}
}
