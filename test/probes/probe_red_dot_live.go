//go:build probe

// 目标协议: [Module 558 / 0x22E] Mod_RedDot_Base 红点列表 (官方 Main.swf 反编译基准)
//
//	get_red_dot_list  Act 1 -> 0x022E0001  空请求
//	  response: [ Int red_dot_type, Int is_show ]
//
// 基准对齐: com.protocols.Mod_RedDot_Base / com.datas.RedDotData /
//
//	com.assist.server.RedDotType (类型编号到功能名的映射见下方表)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_red_dot_live.go
//
// 预期表现: 打印服务端当前所有 is_show=1 的红点类型；这些即是官方客户端认为
//
//	存在可领取/可参与内容的功能入口，可用于发现尚未接入自动化的福利模块。
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
)

const actionRedDotList uint32 = 0x022E0001

// redDotNames 来自 com.assist.server.RedDotType 的反编译常量。
var redDotNames = map[int32]string{
	1:  "xiu_zhen_jiang_li(修真奖励)",
	2:  "xiao_zhu_shou(小助手)",
	3:  "long_zheng_tiao_zhan(龙争挑战)",
	4:  "tong_qian_fu_ben(铜钱副本)",
	5:  "jie_biao(劫镖)",
	6:  "wen_ling(问灵)",
	7:  "hui_yi_zhi_jing(回忆之境)",
	8:  "ri_chang_tiao_zhan(日常挑战)",
	9:  "gong_neng_da_ting(功能大厅)",
	10: "tu_biao_shou_na(图标收纳)",
	11: "dian_jing_shi(点金石)",
	12: "shou_chong_shuang_bei(首充双倍)",
	13: "chao_zhi_yue_ka(超值月卡)",
	14: "cheng_zhang_ji_jin(成长基金)",
	15: "tian_shu_ji_jin(天书基金)",
	16: "chong_zhi_ji_jin(充值基金)",
	17: "zhan_li_ji_jin(战力基金)",
	18: "ji_jin(基金)",
	19: "xin_fu_zhao_cai_shu(新服招财树)",
	20: "zhu_long_sheng_ling(烛龙生灵)",
	21: "chong_zhi_fu_li(充值福利)",
	22: "shang_cheng(商城)",
	23: "shou_ji_zhu_shou(手机助手)",
	24: "she_zhi(设置)",
	25: "new_year_sign_in(新年签到)",
	26: "children_raising(育儿)",
	27: "around_shop(周边商店)",
	28: "rabbit_present(玉兔献礼)",
	29: "celebration_rank(庆典排行)",
	30: "linlang(琳琅)",
	31: "catgold_storage(招财猫存储)",
	32: "catgold_feedback(招财猫返利)",
	33: "repress_demon_tower(镇妖塔)",
	34: "fairy_game(仙灵游戏)",
	35: "first_charge(首充)",
	36: "first_charge2(首充2)",
	37: "yuan_bao_doubling_machine(元宝翻倍机)",
	38: "wan_bao_dian(万宝殿)",
	39: "demon_invasion(妖魔入侵)",
	40: "god_welfare(众神福利)",
	41: "seek_immortal_road(寻仙之路)",
	42: "new_server_login(新服登录)",
	43: "release_welfare(开服福利)",
	44: "year_sign_in(年度签到)",
	45: "card_soul(卡魂)",
	46: "partner_gift(伙伴赠礼)",
	47: "time_fortune(时光福运)",
	55: "xian_tu_mi_bao(仙途秘宝)",
	56: "ying_xin_zeng_li(迎新赠礼)",
	57: "link_kill_monster(连斩)",
	58: "anniversary_tower(周年塔)",
	59: "friends_sent(好友赠送)",
	60: "monoploy(大富翁)",
	61: "fairy_gift(仙灵赠礼)",
	62: "fzq_lottery(封魔录抽奖)",
	63: "anniversary_gift(周年礼)",
	64: "wealth_celebration(财富庆典)",
	65: "anniversary_winner(周年赢家)",
	66: "maritime_trade(海上贸易)",
	67: "island_fishing(海岛钓鱼)",
	68: "gold_road(黄金之路)",
	69: "sxd_tv_active(SXD TV)",
	70: "island_summer(夏日海岛)",
	71: "island_summer_shop(夏日海岛商店)",
	72: "island_summer_bp(夏日海岛通行证)",
	73: "sxd_tv_series_calendar(SXD TV日历)",
	74: "sxd_tv_series_draw(SXD TV抽奖)",
	76: "sxd_tv_series(SXD TV系列)",
	77: "clear_clouds(拨云见日)",
	78: "EGP",
	79: "era_battlepass(纪元通行证)",
	80: "era_gift(纪元礼)",
	81: "era_shop(纪元商店)",
	82: "era_attr(纪元属性)",
	83: "era_auto(纪元自动)",
	84: "dress_plan(换装计划)",
	85: "sxd_tv_series_quiz(SXD TV答题)",
	86: "sect_bless_land(圣盟福地)",
	87: "bihuishanhe(避讳山河)",
	89: "jinchaofuli(金朝福利)",
	90: "zhounianjishi(周年纪事)",
	91: "caiyuangungun(财源滚滚)",
	92: "xuyuanshenmiao(许愿神庙)",
	93: "zhouniandangao(周年蛋糕)",
	94: "feedback(反馈)",
	96: "era_lun_hui_album(纪元轮回图鉴)",
	97: "recharge_voucher(充值券)",
	98: "tomb_artifacts_wish(摸金许愿)",
}

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
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	pkt := protocol.NewPacket(actionRedDotList, nil)
	cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
	resp, err := sess.Call(cCtx, pkt, actionRedDotList)
	cCancel()
	if err != nil {
		slog.Error("查询红点列表失败", "err", err)
		return
	}
	r := protocol.NewReader(resp.Payload)
	count, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析红点数量失败", "err", err, "hex_len", len(resp.Payload))
		return
	}
	shown := make([]string, 0, count)
	hidden := make([]string, 0, count)
	for i := 0; i < int(count); i++ {
		typ, err := r.ReadInt32()
		if err != nil {
			break
		}
		isShow, err := r.ReadInt32()
		if err != nil {
			break
		}
		name := redDotNames[typ]
		if name == "" {
			name = "unknown"
		}
		entry := fmt.Sprintf("type=%d %s", typ, name)
		if isShow != 0 {
			shown = append(shown, entry)
		} else {
			hidden = append(hidden, entry)
		}
	}
	slog.Info("红点列表", "total", count, "shown", len(shown), "hidden", len(hidden), "remaining", r.Remaining())
	for _, e := range shown {
		slog.Info("红点开启", "entry", e)
	}
}
