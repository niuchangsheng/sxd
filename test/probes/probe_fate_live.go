//go:build probe

// 目标协议: 仙旅奇缘 (Module 21 / 官方微端 Mod_TravelEvent_Base)
//   - 0x00150000 get_activity_info 查询今日进度与冷却
//   - 0x00150001 get_event_and_answer 拉取当前事件与两个分支
//   - 0x00150002 answer_travel_event 提交分支答案并结算收益 (严格 8 字节, 不带 PrevAct)
//
// 基准对齐: 2026 官方微端 Main.swf 协议定义与世界地图「仙旅奇缘」事件流程
// 运行命令: go run -tags=probe test/probes/probe_fate_live.go
// 预期表现: 打印状态/题目/答案原始 Hex, 并通过题库权重决策提交一次答案; 会消耗 1 次当日奇遇次数
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/qa"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
		MaxReconnectAttempts: 1,
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
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	qaEngine, err := qa.NewEngine("")
	if err != nil {
		slog.Error("加载题库失败", "err", err)
		return
	}

	// 1. 查询状态
	infoPkt, _ := protocol.BuildFateGetInfoPacket(protocol.FateGetInfoRequest{PrevAct: sess.LastActionID()})
	infoResp, err := call(ctx, sess, infoPkt, protocol.ActionIDFateGetInfo)
	if err != nil {
		slog.Error("查询仙旅奇缘状态失败", "err", err)
		return
	}
	fmt.Printf("\n[STATE] %s\n", hex.EncodeToString(infoResp.Payload))
	info, err := protocol.ParseFateGetInfoResult(infoResp.Payload)
	if err != nil {
		slog.Error("解析状态失败", "err", err)
		return
	}
	fmt.Printf("[STATE] parsed join=%d total=%d remaining=%d cd_timer=%d cd_ingot=%d\n",
		info.JoinCount, info.TotalCount, info.RemainingTimes(), info.CdTimer, info.CdIngot)

	// 2. 拉取题目
	qPkt, _ := protocol.BuildFateQuestionPacket(protocol.FateQuestionRequest{PrevAct: sess.LastActionID()})
	qResp, err := call(ctx, sess, qPkt, protocol.ActionIDFateGetQuestion)
	if err != nil {
		slog.Error("拉取仙旅奇缘题目失败", "err", err)
		return
	}
	fmt.Printf("\n[QUESTION] %s\n", hex.EncodeToString(qResp.Payload))
	q, err := protocol.ParseFateQuestionResult(qResp.Payload)
	if err != nil {
		slog.Error("解析题目失败", "err", err)
		return
	}
	fmt.Printf("[QUESTION] event_id=%d msg=%d story=%q\n", q.EventID, q.Msg, q.Story)
	for i, opt := range q.Options {
		fmt.Printf("[QUESTION] option[%d] answer_id=%d sign=%s text=%q\n", i, opt.AnswerID, opt.Sign, opt.Text)
	}

	// 3. 题库决策
	branch := 1
	if b, entry, ok := qaEngine.MatchFate(q.Story); ok && entry != nil {
		branch = b
		fmt.Printf("[DECIDE] branch=%d p1=%s p2=%s\n", b, entry.P1, entry.P2)
	} else {
		fmt.Printf("[DECIDE] 题库未命中, 默认 branch=1\n")
	}
	if branch < 1 || branch > len(q.Options) {
		branch = 1
	}
	if len(q.Options) == 0 {
		slog.Error("题目无可用分支")
		return
	}
	chosen := q.Options[branch-1]

	// 4. 提交答案 (消耗一次当日次数); 线上仅接受 8 字节 eventId + answerId
	ansPkt, err := protocol.BuildFateAnswerPacket(protocol.FateAnswerRequest{
		EventID:  q.EventID,
		AnswerID: chosen.AnswerID,
	})
	if err != nil {
		slog.Error("构造答案封包失败", "err", err)
		return
	}
	ansResp, err := call(ctx, sess, ansPkt, protocol.ActionIDFateAnswer)
	if err != nil {
		slog.Error("提交仙旅奇缘答案失败", "err", err)
		return
	}
	fmt.Printf("\n[ANSWER] req=%s\n", hex.EncodeToString(ansPkt.Payload))
	fmt.Printf("[ANSWER] resp=%s\n", hex.EncodeToString(ansResp.Payload))
	ans, err := protocol.ParseFateAnswerResult(ansResp.Payload)
	if err != nil {
		slog.Error("解析结算回包失败", "err", err)
		return
	}
	fmt.Printf("[ANSWER] message=%q awards=%+v join=%d total=%d\n",
		ans.Message, ans.Awards, ans.Info.JoinCount, ans.Info.TotalCount)

	// 5. 复查状态，确认次数扣减
	infoPkt2, _ := protocol.BuildFateGetInfoPacket(protocol.FateGetInfoRequest{PrevAct: sess.LastActionID()})
	infoResp2, err := call(ctx, sess, infoPkt2, protocol.ActionIDFateGetInfo)
	if err != nil {
		slog.Error("复查状态失败", "err", err)
		return
	}
	fmt.Printf("\n[STATE-AFTER] %s\n", hex.EncodeToString(infoResp2.Payload))
}

func call(ctx context.Context, sess *client.RoleSession, pkt *protocol.Packet, respAct uint32) (*protocol.Packet, error) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := sess.Call(c, pkt, respAct)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("回包为空")
	}
	return resp, nil
}
