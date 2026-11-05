//go:build probe

// 目标协议: 十四周年庆活动中的免费/券类产出 (官方 Main.swf 反编译基准)
//
//	[Module 350] Mod_SixthAnniversaryCake_Base 周年蛋糕
//	  get_sixth_anniversary_cake_info Act 0 -> 0x015E0000
//	    response: Int all_num, Int all_max_num, Int today_self_num, Int today_max_num, Int has_eat
//	  eat_cake                         Act 1 -> 0x015E0001  空请求
//	    response: UByte result, [Int item_id, Int item_num] award_list
//
//	[Module 723] Mod_MoneyRolling_Base 财源滚滚
//	  open_panel Act 1 -> 0x02D30001 空请求
//	    response: UByte result, Int total_charge_ingot, Int total_cost_ingot, Int coupon_num, Int qing_yu_step_value
//	  lottery    Act 2 -> 0x02D30002  Int times
//	    response: UByte result, Int get_ingot, Int coupon_num, Int get_qing_yu_num, Int qing_yu_step_value
//
// 基准对齐: Mod_SixthAnniversaryCake_Base / Mod_MoneyRolling_Base /
//
//	SixthAnniversaryCakeData / MoneyRollingData
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_anniversary_events_live.go [-execute]
//
// 预期表现: 默认只读打印蛋糕与财源滚滚面板；-execute 时：
//   - 蛋糕今天有可用次数且未吃过 -> 吃蛋糕领奖；
//   - 财源滚滚有礼券 -> 用 1 张券抽奖 (礼券为活动道具，不消耗元宝)。
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
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
	actionCakeInfo   uint32 = 0x015E0000
	actionCakeEat    uint32 = 0x015E0001
	actionMoneyPanel uint32 = 0x02D30001
	actionMoneyDraw  uint32 = 0x02D30002
)

func main() {
	execute := flag.Bool("execute", false, "实际吃蛋糕/用礼券抽奖")
	flag.Parse()
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
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	before := sess.GetPlayerState().Ingots
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID(), "ingots", before)

	call := func(payload []byte, act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("调用失败: "+name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		slog.Info("回包: "+name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		return resp
	}

	// ---- 周年蛋糕 ----
	cakeOK, cakeCanEat := false, false
	if resp := call(nil, actionCakeInfo, "cake_info"); resp != nil && len(resp.Payload) >= 20 {
		r := protocol.NewReader(resp.Payload)
		allNum, _ := r.ReadInt32()
		allMax, _ := r.ReadInt32()
		todaySelf, _ := r.ReadInt32()
		todayMax, _ := r.ReadInt32()
		hasEat, _ := r.ReadInt32()
		cakeOK = true
		cakeCanEat = hasEat == 0 && todaySelf > 0
		slog.Info("周年蛋糕面板",
			"all_num", allNum, "all_max_num", allMax,
			"today_self_num", todaySelf, "today_max_num", todayMax,
			"has_eat", hasEat,
		)
	}

	// ---- 财源滚滚 ----
	couponNum := int32(0)
	if resp := call(nil, actionMoneyPanel, "money_rolling_open"); resp != nil && len(resp.Payload) >= 17 {
		r := protocol.NewReader(resp.Payload)
		result, _ := r.ReadUint8()
		totalCharge, _ := r.ReadInt32()
		totalCost, _ := r.ReadInt32()
		couponNum, _ = r.ReadInt32()
		qingYuStep, _ := r.ReadInt32()
		slog.Info("财源滚滚面板",
			"result", result,
			"total_charge_ingot", totalCharge,
			"total_cost_ingot", totalCost,
			"coupon_num", couponNum,
			"qing_yu_step_value", qingYuStep,
		)
	}

	if !*execute {
		slog.Info("只读模式结束", "cake_can_eat", cakeCanEat, "coupons", couponNum)
		return
	}

	if cakeOK && cakeCanEat {
		if resp := call(nil, actionCakeEat, "cake_eat"); resp != nil && len(resp.Payload) >= 3 {
			r := protocol.NewReader(resp.Payload)
			result, _ := r.ReadUint8()
			count, _ := r.ReadUint16()
			type award struct {
				ItemID int32
				Num    int32
			}
			awards := make([]award, 0, count)
			for i := 0; i < int(count); i++ {
				id, err := r.ReadInt32()
				if err != nil {
					break
				}
				num, err := r.ReadInt32()
				if err != nil {
					break
				}
				awards = append(awards, award{ItemID: id, Num: num})
			}
			slog.Info("吃周年蛋糕结果", "result", result, "awards", awards)
		}
	}

	if couponNum > 0 {
		payload := make([]byte, 4)
		binary.BigEndian.PutUint32(payload, 1)
		if resp := call(payload, actionMoneyDraw, "money_rolling_lottery"); resp != nil && len(resp.Payload) >= 17 {
			r := protocol.NewReader(resp.Payload)
			result, _ := r.ReadUint8()
			getIngot, _ := r.ReadInt32()
			leftCoupon, _ := r.ReadInt32()
			getQingYu, _ := r.ReadInt32()
			step, _ := r.ReadInt32()
			slog.Info("财源滚滚抽奖结果",
				"result", result, "get_ingot", getIngot, "left_coupon", leftCoupon,
				"get_qing_yu", getQingYu, "qing_yu_step_value", step,
			)
		}
	} else {
		slog.Info("财源滚滚无可用礼券，跳过抽奖")
	}

	slog.Info("资产变化", "ingots_before", before, "ingots_after", sess.GetPlayerState().Ingots,
		"delta", sess.GetPlayerState().Ingots-before)
}
