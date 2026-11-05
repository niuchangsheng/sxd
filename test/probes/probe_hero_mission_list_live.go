//go:build probe

// 目标协议: [Module 35] Mod_HeroMission_Base.get_hero_mission_list (0x00230000)
// 基准对齐: 原版客户端 com.protocols.Mod_HeroMission_Base
//
//	request=[ShortUtil(town_id)]
//	response=[[IntUtil,ByteUtil,UByteUtil,ByteUtil],ByteUtil,ByteUtil,ShortUtil,IntUtil]
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_hero_mission_list_live.go
// 预期表现: 只读探测精英副本城镇列表，打印原始 Hex 与官方字段解析结果，不发起任何扫荡/消耗体力请求。
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

// eliteTowns 来自 Pieb.db: SELECT DISTINCT map FROM Missions WHERE MissionType=1
var eliteTowns = []uint16{4, 5, 7, 8, 9, 13, 14, 15, 21, 22, 23, 24, 26, 27, 29, 33, 35, 39, 40, 43, 54, 55}

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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	st := sess.GetPlayerState()
	slog.Info("会话已激活", "role", sess.RoleName(), "level", st.Level, "stamina", sess.GetStamina())

	for _, townID := range eliteTowns {
		payload := buildTownPayload(townID)
		pkt := protocol.NewPacket(protocol.ActionIDHeroGetList, payload)
		c, cc := context.WithTimeout(ctx, 4*time.Second)
		resp, cErr := sess.Call(c, pkt, protocol.ActionIDHeroGetList)
		cc()
		fmt.Printf("\n=== town=%d payload=%s ===\n", townID, hex.EncodeToString(payload))
		if cErr != nil || resp == nil {
			fmt.Printf("no response: %v\n", cErr)
			continue
		}
		fmt.Printf("resp (%dB): %s\n", len(resp.Payload), hex.EncodeToString(resp.Payload))
		hl, pErr := parseHeroMissionList(resp.Payload)
		if pErr != nil {
			fmt.Printf("parse err: %v\n", pErr)
			continue
		}
		fmt.Printf("missions=%d all_buy_times=%d remain_buy_times=%d buy_price=%d finish_num=%d\n",
			len(hl.Missions), hl.AllBuyTimes, hl.RemainBuyTimes, hl.BuyPrice, hl.FinishMissionNum)
		for _, m := range hl.Missions {
			fmt.Printf("  mission=%d can_challenge=%d rank=%d finished=%d\n",
				m.MissionID, m.CanChallenge, m.Rank, m.Finished)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func buildTownPayload(townID uint16) []byte {
	w := protocol.NewWriter()
	w.WriteUint16(townID)
	return w.Bytes()
}

type heroMissionEntry struct {
	MissionID    int32
	CanChallenge int8
	Rank         uint8
	Finished     int8
}

type heroMissionList struct {
	Missions         []heroMissionEntry
	AllBuyTimes      int8
	RemainBuyTimes   int8
	BuyPrice         int16
	FinishMissionNum int32
}

// parseHeroMissionList 按官方 schema 解析:
// [[IntUtil,ByteUtil,UByteUtil,ByteUtil],ByteUtil,ByteUtil,ShortUtil,IntUtil]
func parseHeroMissionList(payload []byte) (*heroMissionList, error) {
	r := protocol.NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := &heroMissionList{Missions: make([]heroMissionEntry, 0, count)}
	for i := 0; i < int(count); i++ {
		mid, e := r.ReadInt32()
		if e != nil {
			return nil, e
		}
		can, e := r.ReadInt8()
		if e != nil {
			return nil, e
		}
		rank, e := r.ReadUint8()
		if e != nil {
			return nil, e
		}
		fin, e := r.ReadInt8()
		if e != nil {
			return nil, e
		}
		out.Missions = append(out.Missions, heroMissionEntry{MissionID: mid, CanChallenge: can, Rank: rank, Finished: fin})
	}
	if out.AllBuyTimes, err = r.ReadInt8(); err != nil {
		return nil, err
	}
	if out.RemainBuyTimes, err = r.ReadInt8(); err != nil {
		return nil, err
	}
	if out.BuyPrice, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if out.FinishMissionNum, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return out, nil
}
