//go:build probe

// 目标: 宠物仙域 Module 755 (Mod_PetImmortalDomain_Base) 只读探测
// 探测动作: 755/0 get_state, 755/1 sect_domain_list, 755/3 share_domain_info, 755/14 my_pet_list, 755/2 sect_domain_travel_info
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_pet_immortal_domain_live.go
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
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活")
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(name string, act uint32, payload []byte) []byte {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, err := sess.Call(cCtx, pkt, act)
		if err != nil {
			slog.Info("无回包", "name", name, "action", fmt.Sprintf("755/%d", act&0xFFFF), "err", err)
			return nil
		}
		head := resp.Payload
		if len(head) > 64 {
			head = head[:64]
		}
		slog.Info("回包", "name", name, "action", fmt.Sprintf("755/%d", act&0xFFFF),
			"len", len(resp.Payload), "head_hex", hex.EncodeToString(head))
		return resp.Payload
	}

	// 755 = 0x02F3
	const mod755 uint32 = 755 << 16
	stateBytes := call("get_state", mod755|0, nil)
	if len(stateBytes) >= 1 {
		slog.Info("755/0 get_state", "state", stateBytes[0], "is_open", stateBytes[0] == 0)
	}

	listBytes := call("sect_domain_list", mod755|1, nil)
	var firstMyDomainID int32
	if len(listBytes) > 0 {
		r := protocol.NewReader(listBytes)
		res, _ := r.ReadUint8()
		mySectID, _ := r.ReadInt32()
		domCnt, _ := r.ReadUint16()
		slog.Info("755/1 sect_domain_list 头", "result", res, "my_sect_id", mySectID, "domain_count", domCnt)
		for i := 0; i < int(domCnt); i++ {
			id, _ := r.ReadInt32()
			domainID, _ := r.ReadInt32()
			protectTS, _ := r.ReadInt32()
			sectID, _ := r.ReadInt32()
			attackNum, _ := r.ReadInt32()
			defenseNum, _ := r.ReadInt32()
			scorePro, _ := r.ReadInt32()
			luckPro, _ := r.ReadInt32()
			extraScorePro, _ := r.ReadInt32()
			extraLuckPro, _ := r.ReadInt32()
			isTravel, _ := r.ReadUint8()
			nowTravelNum, _ := r.ReadInt32()
			specialTS, _ := r.ReadInt32()
			isAlliance, _ := r.ReadInt32()
			if sectID == mySectID || isTravel == 1 || i < 3 {
				slog.Info("  domain", "id", id, "domain_id", domainID, "sect_id", sectID,
					"my_sect", sectID == mySectID, "score_pro", scorePro, "luck_pro", luckPro,
					"extra_score", extraScorePro, "extra_luck", extraLuckPro,
					"is_travel", isTravel, "now_travel_num", nowTravelNum,
					"protect_ts", protectTS, "attack", attackNum, "defense", defenseNum,
					"special_ts", specialTS, "is_alliance", isAlliance)
			}
			if sectID == mySectID && firstMyDomainID == 0 {
				firstMyDomainID = id
			}
		}
	}

	petBytes := call("my_pet_list", mod755|14, nil)
	if len(petBytes) > 0 {
		r := protocol.NewReader(petBytes)
		cnt, _ := r.ReadUint16()
		slog.Info("755/14 my_pet_list", "count", cnt)
		for i := 0; i < int(cnt); i++ {
			petID, _ := r.ReadInt32()
			travelTS, _ := r.ReadInt32()
			isTravel, _ := r.ReadUint8()
			luckTimes, _ := r.ReadInt32()
			luckLimit, _ := r.ReadInt32()
			isHave, _ := r.ReadInt32()
			if isHave == 1 || isTravel == 1 || travelTS > 0 {
				slog.Info("  pet", "pet_id", petID, "travel_ts", travelTS,
					"travel_hours", fmt.Sprintf("%.2f", float64(travelTS)/3600.0),
					"is_travel", isTravel, "luck", fmt.Sprintf("%d/%d", luckTimes, luckLimit), "is_have", isHave)
			}
		}
		isUnread, _ := r.ReadUint8()
		slog.Info("755/14 tail", "is_unread", isUnread, "remaining", r.Remaining())
	}

	shareBytes := call("share_domain_info", mod755|3, nil)
	if len(shareBytes) > 0 {
		r := protocol.NewReader(shareBytes)
		res, _ := r.ReadUint8()
		myAppID, _ := r.ReadInt32()
		scorePro, _ := r.ReadInt32()
		luckPro, _ := r.ReadInt32()
		petCnt, _ := r.ReadUint16()
		slog.Info("755/3 share_domain_info", "result", res, "my_app_id", myAppID,
			"score_pro", scorePro, "luck_pro", luckPro, "pets_cnt", petCnt)
	}

	if firstMyDomainID > 0 {
		w := protocol.NewWriter()
		w.WriteInt32(firstMyDomainID)
		infoBytes := call("sect_domain_travel_info", mod755|2, w.Bytes())
		if len(infoBytes) > 0 {
			r := protocol.NewReader(infoBytes)
			res, _ := r.ReadUint8()
			myAppID, _ := r.ReadInt32()
			nowSectID, _ := r.ReadInt32()
			petCnt, _ := r.ReadUint16()
			for i := 0; i < int(petCnt); i++ {
				petID, _ := r.ReadInt32()
				protectTS, _ := r.ReadInt32()
				travelTS, _ := r.ReadInt32()
				nick, _ := r.ReadString()
				srv, _ := r.ReadString()
				appID, _ := r.ReadInt32()
				sectID, _ := r.ReadInt32()
				posID, _ := r.ReadInt32()
				warPower, _ := r.ReadInt64()
				sectName, _ := r.ReadString()
				slog.Info("  domain_pet", "pos_id", posID, "pet_id", petID, "nick", nick, "srv", srv,
					"app_id", appID, "is_mine", appID == myAppID, "travel_ts", travelTS,
					"protect_ts", protectTS, "sect_id", sectID, "sect_name", sectName, "war_power", warPower)
			}
			canOther, _ := r.ReadUint8()
			mySectID, _ := r.ReadInt32()
			expelTS, _ := r.ReadInt32()
			expelLv, _ := r.ReadInt32()
			specialTS, _ := r.ReadInt32()
			isAlliance, _ := r.ReadInt32()
			posNum, _ := r.ReadInt32()
			allianceRank, _ := r.ReadInt32()
			slog.Info("755/2 sect_domain_travel_info", "domain_id", firstMyDomainID,
				"result", res, "my_app_id", myAppID, "now_sect_id", nowSectID, "my_sect_id", mySectID,
				"occupied", petCnt, "pos_num", posNum, "can_travel_other_sect", canOther,
				"expel_lower_ts", expelTS, "expel_lower_lv", expelLv, "special_ts", specialTS,
				"is_alliance", isAlliance, "alliance_rank", allianceRank, "remaining", r.Remaining())
		}
	}
}
