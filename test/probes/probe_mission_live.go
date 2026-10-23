//go:build probe

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
)

func main() {
	fmt.Println("==================================================")
	fmt.Println("🚀 副本扫荡 + 体力自愈完整实机全链路测试")
	fmt.Println("==================================================")

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

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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

	// 等待握手完成变为 Active
	deadline := time.Now().Add(10 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		fmt.Printf("❌ 会话未能在 10 秒内变为 Active: state=%v\n", sess.State())
		return
	}

	fmt.Printf("👤 角色已就绪: %s (Lv.%d) | 当前体力: %d 点\n", sess.RoleName(), sess.GetPlayerState().Level, sess.GetStamina())

	// 1. 检查角色背包中的道具，输出体力道具
	packPkt, pErr := sess.Call(ctx, protocol.NewPacket(protocol.ActionIDItemGetPackList, nil), protocol.ActionIDItemGetPackList)
	if pErr == nil && packPkt != nil {
		pack, _ := protocol.ParsePackItemListResponse(packPkt.Payload)
		if pack != nil {
			fmt.Printf("🎒 背包容量: %d, 剩余空格: %d, 当前物品数: %d\n", pack.PackCapacity, pack.PackEmptyGrids, len(pack.PackItems))
			for _, it := range pack.PackItems {
				switch it.ItemID {
				case protocol.ItemIDSteamedBun:
					fmt.Printf("   [包子] 格位: %d (ItemID: %d, +5体力)\n", it.GridID, it.ItemID)
				case protocol.ItemIDRiceDumpling:
					fmt.Printf("   [粽子] 格位: %d (ItemID: %d, +10体力)\n", it.GridID, it.ItemID)
				case protocol.ItemIDTeaEgg:
					fmt.Printf("   [茶叶蛋] 格位: %d (ItemID: %d, +50体力)\n", it.GridID, it.ItemID)
				}
			}
		}
	}

	// 2. 执行一次自动补充体力探测 (如果有道具)
	fmt.Println("\n🔍 测试 TryReplenishStamina 优先级判定...")
	ok, item, power, err := dungeon.TryReplenishStamina(ctx, sess)
	if err != nil {
		fmt.Printf("⚠️ TryReplenishStamina 错误: %v\n", err)
	} else if ok {
		fmt.Printf("🎉 成功自动吃道具: %s (+%d体力), 当前体力: %d\n", item, power, sess.GetStamina())
	} else {
		fmt.Println("ℹ️ 当前背包中无可用体力道具 (包子/粽子/茶叶蛋)")
	}

	// 3. 执行扫荡
	fmt.Println("\n⚔️ 正在执行 Module 25 秒扫荡 (Target=1)...")
	w0 := protocol.NewWriter()
	w0.WriteInt32(1)
	pkt0 := protocol.NewPacket(protocol.ActionIDPracticeGetStatus, w0.Bytes())
	cCtx0, cancel0 := context.WithTimeout(ctx, 3*time.Second)
	resp0, err0 := sess.Call(cCtx0, pkt0, protocol.ActionIDPracticeGetStatus)
	cancel0()
	if err0 != nil {
		fmt.Printf("❌ get_practice_info 失败: %v\n", err0)
		return
	}
	status, _ := protocol.ParsePracticeStatusResult(resp0.Payload)
	fmt.Printf("✅ get_practice_info 成功, isPracticing=%d\n", status.IsPracticing)

	if status.IsPracticing == 1 {
		wQ := protocol.NewWriter()
		wQ.WriteInt16(0)
		_ = sess.Send(protocol.NewPacket(protocol.ActionIDPracticeQuickly, wQ.Bytes()))
		time.Sleep(50 * time.Millisecond)
	}

	w1 := protocol.NewWriter()
	w1.WriteInt32(1)
	w1.WriteInt16(1)
	w1.WriteInt8(1)
	pkt1 := protocol.NewPacket(protocol.ActionIDPracticeStart, w1.Bytes())
	cCtx1, cancel1 := context.WithTimeout(ctx, 3*time.Second)
	resp1, err1 := sess.Call(cCtx1, pkt1, protocol.ActionIDPracticeStart)
	cancel1()
	if err1 != nil {
		fmt.Printf("❌ start_practice 失败: %v\n", err1)
		return
	}
	startRes, _ := protocol.ParseStartPracticeResult(resp1.Payload)
	fmt.Printf("🎯 start_practice 成功! Result=%d, MissionID=%d, Count=%d\n", startRes.Result, startRes.MissionID, startRes.Count)

	w3 := protocol.NewWriter()
	w3.WriteInt16(0)
	pkt3 := protocol.NewPacket(protocol.ActionIDPracticeQuickly, w3.Bytes())
	cCtx3, cancel3 := context.WithTimeout(ctx, 3*time.Second)
	_, err3 := sess.Call(cCtx3, pkt3, protocol.ActionIDPracticeQuickly)
	cancel3()
	if err3 != nil {
		fmt.Printf("❌ quickly 失败: %v\n", err3)
		return
	}
	fmt.Printf("🏆 quickly 极速秒完成成功!\n")

	remStamina, _ := sess.ConsumeStamina(5)
	fmt.Printf("📊 资产扣减完成: 消耗 5 体力, 剩余体力: %d 点\n", remStamina)
	fmt.Println("==================================================")
	fmt.Println("🎉 全链路测试大获全胜！")
}
