//go:build probe

// 目标协议: [Module 345, Action 2] get_attachment_award 单封邮件附件领取幂等性验证
//
// 基准对齐: docs/research/ingot-server-validation-static-audit.md 第 F3 项
// 与第六节验证计划; 线格式 Int mailID + prevAct 见 docs/protocol-spec.md 4.9 节。
//
// 探针流程 (全程最多发送 3 个附件领取请求, 目标仅为"已领取"邮件):
//  1. get_email_info 建立 PrevAct 链并解析邮件列表;
//  2. 定位一封 type=ATTACHMENT(3) 且 attachment_status=GET(4) 的邮件;
//  3. [B] 刷新 prevAct 逻辑重发领取请求 -> 服务端幂等时应返回 HAD_GET(12);
//  4. [A] 紧跟字节级原样重放 -> 若网关 PrevAct 原子链生效应被静默丢弃(超时);
//  5. [C] 仅当 [A] 超时时, 以最新 prevAct 确认链路语义;
//  6. [D] 仅当 [B] 无回包时, 重新同步面板后以 prevAct+mailID 前置顺序发送一次,
//     甄别带参请求的 PrevAct 位置 (文档线格式为逆向推演, 可能与实服存在偏差)。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_email_replay_live.go
//
// 预期表现: [B]/[C] 返回 12 (HAD_GET) 证明服务端幂等; [A] 超时证明网关
// PrevAct 链阻断字节级重放。任一请求返回 8 (SUCCESS) 即疑似重复领取漏洞,
// 探针会立即停止并打印告警, 不做任何进一步尝试。
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	// 全量入站观察器: 记录服务端在每次附件领取请求后实际下发的动作号,
	// 用于区分"服务端无应答"与"回包动作号与请求不一致"。
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		fmt.Printf("[IN] action=0x%08X len=%d hex=%s\n",
			pkt.ActionID, len(pkt.Payload), hex.EncodeToString(pkt.Payload[:min(len(pkt.Payload), 32)]))
	})

	ingotsBefore := sess.GetPlayerState().Ingots
	fmt.Printf("\n角色 %s (Lv.%d) 区服 %s, 当前元宝 %d\n",
		sess.OriginNickName(), sess.GetPlayerState().Level, role.ServerID, ingotsBefore)

	call := func(pkt *protocol.Packet, tag string) *protocol.Packet {
		fmt.Printf("[%s] action=0x%08X 发送 payload=%s (HasPrevAct=%v)\n",
			tag, pkt.ActionID, hex.EncodeToString(pkt.Payload), pkt.HasPrevAct)
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, pkt.ActionID)
		cCancel()
		if cErr != nil {
			fmt.Printf("[%s] action=0x%08X 无回包: %v\n", tag, pkt.ActionID, cErr)
			return nil
		}
		fmt.Printf("[%s] action=0x%08X 回包 len=%d hex=%s\n",
			tag, pkt.ActionID, len(resp.Payload), hex.EncodeToString(resp.Payload))
		return resp
	}

	// 1. 同步邮箱面板并建立 PrevAct 链
	infoResp := call(protocol.BuildEmailGetInfoPacket(protocol.ActionIDTownEnter), "get_email_info")
	if infoResp == nil || len(infoResp.Payload) == 0 {
		slog.Error("邮箱面板不可用, 终止验证")
		return
	}
	emails, err := parseEmails(infoResp.Payload)
	if err != nil {
		slog.Error("解析邮件列表失败", "err", err,
			"hex", hex.EncodeToString(infoResp.Payload[:min(len(infoResp.Payload), 96)]))
		return
	}
	attachTotal, gotten := 0, 0
	var target *emailItem
	for i := range emails {
		e := &emails[i]
		if e.typ == protocol.EmailTypeAttachment {
			attachTotal++
			if e.attachStatus == protocol.EmailAttachmentGet {
				gotten++
				if target == nil {
					target = e
				}
			}
		}
	}
	fmt.Printf("邮件总数 %d, 带附件 %d, 其中已领取 %d\n", len(emails), attachTotal, gotten)
	if target == nil {
		fmt.Println("未找到已领取附件邮件, 为避免改变账号状态, 终止验证")
		return
	}
	fmt.Printf("目标邮件 id=%d subject=%q attachments=%v\n", target.id, target.subject, target.attachments)

	// 2. [B] 刷新 prevAct 的逻辑重发: 由会话层在发送前以最新链值重写尾部 prevAct
	prevFresh := protocol.ActionIDEmailGetInfo
	pktB := protocol.BuildEmailGetAttachmentPacket(target.id, prevFresh)
	pktB.HasPrevAct = true
	respB := call(pktB, "B 逻辑重发")
	var codeB uint8
	if respB != nil {
		codeB, err = protocol.ParseEmailGetAttachmentResult(respB.Payload)
		if err != nil {
			slog.Error("解析 [B] 结果码失败", "err", err)
		} else {
			fmt.Printf("[B] result=%d %s\n", codeB, protocol.EmailResultText(codeB))
		}
	}

	// 3. [A] 字节级原样重放: 固定 prevAct=0x01590000, 不刷新, 模拟旧包原样重发
	respA := call(protocol.BuildEmailGetAttachmentPacket(target.id, prevFresh), "A 字节原样重放")

	// 4. [C] 仅当 [A] 无回包时, 以最新 prevAct 确认链路语义
	if respA == nil {
		pktC := protocol.BuildEmailGetAttachmentPacket(target.id, protocol.ActionIDEmailGetAttachmentAward)
		pktC.HasPrevAct = true
		respC := call(pktC, "C 链路确认")
		if respC != nil {
			if codeC, cErr := protocol.ParseEmailGetAttachmentResult(respC.Payload); cErr == nil {
				fmt.Printf("[C] result=%d %s\n", codeC, protocol.EmailResultText(codeC))
			}
		}
	}

	// 5. [D] 线格式变体: 当 [B] 仍无回包时, 重新同步面板后以 prevAct+mailID 顺序发送一次,
	// 用于甄别带参请求中 PrevAct 是前置还是后置 (文档为逆向推演, 存在偏差可能)。
	if respB == nil {
		if syncResp := call(protocol.BuildEmailGetInfoPacket(protocol.ActionIDTownEnter), "get_email_info 重新同步"); syncResp != nil {
			w := protocol.NewWriter()
			w.WriteUint32(protocol.ActionIDEmailGetInfo) // prevAct 前置
			w.WriteInt32(target.id)
			alt := protocol.NewPacket(protocol.ActionIDEmailGetAttachmentAward, w.Bytes())
			if respD := call(alt, "D prevAct 前置变体"); respD != nil {
				if codeD, dErr := protocol.ParseEmailGetAttachmentResult(respD.Payload); dErr == nil {
					fmt.Printf("[D] result=%d %s\n", codeD, protocol.EmailResultText(codeD))
				}
			}
		}
	}

	// 6. [E] 一键收取幂等对照: 生产链路格式已知 (仅 prevAct), 当前无可领取附件,
	// 连续两次调用观察服务端是否重复入账。
	e1Pkt := protocol.BuildEmailOneKeyGetAwardPacket(protocol.ActionIDEmailGetInfo)
	respE1 := call(e1Pkt, "E1 一键收取")
	var codeE1, codeE2 uint8
	if respE1 != nil {
		if codeE1, err = protocol.ParseEmailOneKeyGetAwardResult(respE1.Payload); err == nil {
			fmt.Printf("[E1] result=%d %s\n", codeE1, protocol.EmailResultText(codeE1))
		}
	}
	e2Pkt := protocol.BuildEmailOneKeyGetAwardPacket(protocol.ActionIDEmailGetInfo)
	e2Pkt.HasPrevAct = true
	respE2 := call(e2Pkt, "E2 一键收取立即重发")
	if respE2 != nil {
		if parsed, eErr := protocol.ParseEmailOneKeyGetAwardResult(respE2.Payload); eErr == nil {
			codeE2 = parsed
			fmt.Printf("[E2] result=%d %s\n", codeE2, protocol.EmailResultText(codeE2))
		}
	}

	ingotsAfter := sess.GetPlayerState().Ingots
	fmt.Printf("\n元宝 delta: %d -> %d (%+d)\n", ingotsBefore, ingotsAfter, ingotsAfter-ingotsBefore)

	// 7. 结论与安全熔断: 任何 SUCCESS 都立即停止
	reGranted := codeB == protocol.EmailResultSuccess ||
		codeE1 == protocol.EmailResultSuccess ||
		codeE2 == protocol.EmailResultSuccess
	if respA != nil {
		if codeA, aErr := protocol.ParseEmailGetAttachmentResult(respA.Payload); aErr == nil && codeA == protocol.EmailResultSuccess {
			reGranted = true
		}
	}
	if reGranted {
		fmt.Println("\n!!! 告警: 服务端对已领取邮件返回 SUCCESS, 疑似重复领取漏洞。立即停止, 不再发送任何请求。")
		return
	}
	fmt.Println("\n结论: 已领取附件邮件未发现重复入账迹象。")
	if respA == nil {
		fmt.Println("字节级原样重放被静默丢弃, 与网关 PrevAct 原子链行为一致。")
	} else {
		fmt.Println("字节级原样重发仍收到回包, Mod 345 的 PrevAct 链未拦截重放; 幂等由业务校验兜底。")
	}
}

// emailItem 为 get_email_info 单封邮件记录中本探针关心的字段。
type emailItem struct {
	id           int32
	subject      string
	typ          uint8
	attachStatus uint8
	attachments  [][2]int64
}

// parseEmails 解析 get_email_info 回包 (线格式见 docs/protocol-spec.md 4.9 节):
// Uint16 count { Int id, String name, String subject, String content, Int receiptTime,
// UByte status, UByte type, [Int,Long] attachments, UByte attachmentStatus,
// Long validTime, UByte deleteFlag, Byte sendType, Int copyFlag }。
func parseEmails(payload []byte) ([]emailItem, error) {
	r := protocol.NewReader(protocol.DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("读取邮件数量失败: %w", err)
	}
	if count > 2000 {
		return nil, fmt.Errorf("邮件数量异常 (%d), 疑似线格式不符", count)
	}
	out := make([]emailItem, 0, count)
	for i := 0; i < int(count); i++ {
		var e emailItem
		if e.id, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("第 %d 封邮件 id 解析失败: %w", i, err)
		}
		if _, err = r.ReadString(); err != nil { // name
			return nil, fmt.Errorf("第 %d 封邮件 name 解析失败: %w", i, err)
		}
		if e.subject, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("第 %d 封邮件 subject 解析失败: %w", i, err)
		}
		if _, err = r.ReadString(); err != nil { // content
			return nil, fmt.Errorf("第 %d 封邮件 content 解析失败: %w", i, err)
		}
		if _, err = r.ReadInt32(); err != nil { // receipt_time
			return nil, fmt.Errorf("第 %d 封邮件 receipt_time 解析失败: %w", i, err)
		}
		if _, err = r.ReadUint8(); err != nil { // status
			return nil, fmt.Errorf("第 %d 封邮件 status 解析失败: %w", i, err)
		}
		if e.typ, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("第 %d 封邮件 type 解析失败: %w", i, err)
		}
		nAtt, aErr := r.ReadUint16()
		if aErr != nil {
			return nil, fmt.Errorf("第 %d 封邮件附件数量解析失败: %w", i, aErr)
		}
		for j := 0; j < int(nAtt); j++ {
			itemID, iErr := r.ReadInt32()
			if iErr != nil {
				return nil, fmt.Errorf("第 %d 封邮件附件 %d item_id 解析失败: %w", i, j, iErr)
			}
			itemNum, iErr := r.ReadInt64()
			if iErr != nil {
				return nil, fmt.Errorf("第 %d 封邮件附件 %d item_num 解析失败: %w", i, j, iErr)
			}
			e.attachments = append(e.attachments, [2]int64{int64(itemID), itemNum})
		}
		if e.attachStatus, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("第 %d 封邮件 attachment_status 解析失败: %w", i, err)
		}
		if _, err = r.ReadInt64(); err != nil { // valid_time
			return nil, fmt.Errorf("第 %d 封邮件 valid_time 解析失败: %w", i, err)
		}
		if _, err = r.ReadUint8(); err != nil { // delete_flag
			return nil, fmt.Errorf("第 %d 封邮件 delete_flag 解析失败: %w", i, err)
		}
		if _, err = r.ReadInt8(); err != nil { // send_type
			return nil, fmt.Errorf("第 %d 封邮件 send_type 解析失败: %w", i, err)
		}
		if _, err = r.ReadInt32(); err != nil { // copy_flag
			return nil, fmt.Errorf("第 %d 封邮件 copy_flag 解析失败: %w", i, err)
		}
		out = append(out, e)
	}
	return out, nil
}