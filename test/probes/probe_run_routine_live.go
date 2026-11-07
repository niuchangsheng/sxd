//go:build probe

// 目标: 按名称执行单个例程并测量每日活跃度增量
//
//	运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_ROUTINE=five_blessings \
//	          go run -tags=probe test/probes/probe_run_routine_live.go
//
// 支持: five_blessings / caravan_hijack / crystal_mine / qimen_dunjia /
//
//	duobao_mountain / find_beast / daily_gifts / jieyuan_box
//
// 预期表现: 打印活跃度 before/after/delta 与元宝 delta；元宝 delta 必须为 0。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/routines/pvp"
	"sxd-pie-ng/internal/routines/social"
	"sxd-pie-ng/internal/scheduler"
)

func dailyActivity(sess *client.RoleSession, ctx context.Context) (int, int, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	if err != nil || resp == nil {
		return 0, 0, false
	}
	r := protocol.NewReader(resp.Payload)
	for i := 0; i < 33; i++ {
		if i == 1 || i == 3 {
			_, _ = r.ReadInt64()
		} else {
			_, _ = r.ReadInt32()
		}
	}
	readList := func() [][4]int32 {
		n, _ := r.ReadUint16()
		out := make([][4]int32, 0, n)
		for i := 0; i < int(n); i++ {
			var row [4]int32
			for j := 0; j < 4; j++ {
				row[j], _ = r.ReadInt32()
			}
			out = append(out, row)
		}
		return out
	}
	_ = readList()
	active := readList()
	earned := 0
	task46 := 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
		if row[0] == 46 {
			task46 = int(row[2] - row[1])
		}
	}
	return earned, task46, true
}

func buildRoutine(name string) scheduler.ActivityRoutine {
	switch name {
	case "five_blessings":
		return minigame.NewFiveBlessingsRoutine()
	case "caravan_hijack":
		return pvp.NewCaravanHijackRoutine()
	case "crystal_mine":
		return farming.NewCrystalMineRoutine()
	case "qimen_dunjia":
		return dungeon.NewQiMenRoutine()
	case "duobao_mountain":
		return minigame.NewDuobaoMountainRoutine()
	case "find_beast":
		return minigame.NewFindBeastRoutine()
	case "daily_gifts":
		return daily.NewDailyGiftsRoutine()
	case "fate_hunt":
		return daily.NewFateHuntRoutine()
	case "forbidden_book":
		return minigame.NewForbiddenBookRoutine()
	case "holy_arena":
		return pvp.NewHolyArenaRoutine()
	case "children_daily":
		return daily.NewChildrenDailyRoutine()
	case "activity_report":
		return daily.NewDailyActivityReportRoutine()
	case "jieyuan_box":
		return social.NewJieYuanBoxRoutine(social.DefaultJieYuanBoxConfig())
	case "spirit_pool":
		return farming.NewSpiritPoolRoutine(farming.DefaultSpiritPoolConfig())
	case "refine_pot":
		return farming.NewRefinePotRoutine()
	case "tong_tian_tower":
		return dungeon.NewTongTianTowerRoutine()
	case "find_immortal":
		return minigame.NewFindImmortalRoutine()
	case "wufa_field":
		return minigame.NewWuFaRoutine()
	case "pilgrimage":
		pcfg := farming.DefaultPilgrimageConfig()
		if os.Getenv("SXD_PROBE_FORCE_SA") == "1" {
			pcfg.SaintAreaFallback = farming.SaintAreaForceOnly
		}
		return farming.NewPilgrimageRoutine(pcfg)
	case "demon_tower":
		return dungeon.NewDemonTowerRoutine()
	case "online_reward":
		return daily.NewOnlineRewardRoutine()
	case "rebirth_land":
		return daily.NewRebirthLandRoutine()
	case "star_picture":
		return minigame.NewStarPictureRoutine()
	case "get_peach":
		return minigame.NewGetPeachRoutine()
	case "courtyard_pet":
		return daily.NewCourtyardPetRoutine()
	case "weekend_event":
		return daily.NewWeekendEventRoutine()
	case "daoyuan_vip_shop":
		return daily.NewDaoyuanVipShopRoutine()
	case "void_worship":
		return social.NewVoidWorshipRoutine()
	case "sa_union_worship":
		return social.NewSaUnionWorshipRoutine()
	case "marry_home":
		return social.NewMarryHomeRoutine()
	case "era_treasure":
		return minigame.NewEraTreasureRoutine()
	case "bailian":
		return minigame.NewBaiLianRoutine()
	case "soul_hunt":
		return minigame.NewSoulHuntRoutine()
	case "era_creation":
		return minigame.NewEraCreationRoutine()
	case "partner_practice":
		return daily.NewPartnerPracticeRoutine()
	case "cultivation_card":
		return minigame.NewCultivationCardRoutine()
	default:
		return nil
	}
}

func main() {
	if os.Getenv("SXD_PROBE_DEBUG") == "1" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	} else {
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}

	name := os.Getenv("SXD_PROBE_ROUTINE")
	if name == "" {
		slog.Error("请设置 SXD_PROBE_ROUTINE")
		return
	}
	rt := buildRoutine(name)
	if rt == nil {
		slog.Error("未知例程", "name", name)
		return
	}

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	before := sess.GetPlayerState()
	beforeAct, beforeTask46, _ := dailyActivity(sess, ctx)
	slog.Info("例程执行前", "routine", name, "daily_activity", beforeAct, "task46_remain", beforeTask46,
		"coins", before.Coins, "ingots", before.Ingots)

	if err := rt.Execute(ctx, sess, nil); err != nil {
		slog.Warn("例程返回错误", "routine", name, "err", err)
	}

	after := sess.GetPlayerState()
	afterAct, afterTask46, _ := dailyActivity(sess, ctx)
	slog.Info("例程执行后",
		"routine", name,
		"daily_activity", afterAct,
		"activity_delta", afterAct-beforeAct,
		"task46_remain", afterTask46,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}
