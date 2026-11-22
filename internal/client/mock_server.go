// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件承载离线调试、沙箱模式与单元测试使用的轻量级虚拟游戏协议服务端 (runMockGameServer)。
package client

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// runMockGameServer 为离线调试、沙箱环境与单元测试提供轻量级虚拟游戏协议服务端。
// writeFindImmortalAwardMock 写入一份随品阶增长的龙鱼奖励 mock 数据。
func writeFindImmortalAwardMock(w *protocol.Writer, state int8) {
	w.WriteInt16(int16(100 + int(state)*20))  // fame
	w.WriteInt32(int32(500 + int(state)*100)) // skill
	w.WriteInt16(int16(10 + int(state)*5))    // cultivation
	w.WriteInt16(int16(5 + int(state)*2))     // state_point
	w.WriteInt16(1)                           // yu_pai
}

// writeFiveBlessingsAwardsMock 写入一张固定的五福奖励卡 mock 数据。
func writeFiveBlessingsAwardsMock(w *protocol.Writer) {
	w.WriteUint16(1)
	w.WriteInt8(1)    // image_id
	w.WriteInt32(100) // skill
	w.WriteInt16(50)  // xian_ling
	w.WriteInt16(5)   // star_soul
	w.WriteInt16(1)   // jewel_scrap
}

// mockFunctionEndAward 是 mock 服务端为功能终结礼包摇出的固定奖励。
type mockFunctionEndAward struct {
	statePoint int16
	skill      int32
	xianLing   int32
	fame       int32
	nimbus     int32
	expBuff    int32
	ingot      int32
	juLing     int32
}

func mockFunctionEndAwardFor(giftID int16) mockFunctionEndAward {
	switch giftID {
	case 1:
		// 免费礼包: ingot=0, 可正常领取。
		return mockFunctionEndAward{statePoint: 20, skill: 85000}
	case 4:
		return mockFunctionEndAward{skill: 80000, xianLing: 28, fame: 320, ingot: 50}
	case 7:
		return mockFunctionEndAward{nimbus: 43, ingot: 60}
	case 13:
		return mockFunctionEndAward{fame: 10, expBuff: 120, ingot: 10}
	case 16:
		return mockFunctionEndAward{statePoint: 13, ingot: 60}
	case 17:
		return mockFunctionEndAward{ingot: 100, juLing: 5500}
	default:
		return mockFunctionEndAward{skill: 1000, ingot: 10}
	}
}

func writeMockFunctionEndRandomAward(w *protocol.Writer, giftID int16) {
	award := mockFunctionEndAwardFor(giftID)
	w.WriteInt16(award.statePoint)
	w.WriteInt32(award.skill)
	w.WriteInt32(award.xianLing)
	w.WriteInt32(award.fame)
	w.WriteInt32(award.nimbus)
	w.WriteInt32(award.expBuff)
	w.WriteInt32(award.ingot)
	w.WriteInt32(award.juLing)
}

func writeMockFunctionEndClaimRewards(w *protocol.Writer, giftID int16, isHaveNext bool) {
	award := mockFunctionEndAwardFor(giftID)
	w.WriteInt16(award.statePoint)
	w.WriteInt32(award.skill)
	w.WriteInt32(award.xianLing)
	w.WriteInt32(award.fame)
	w.WriteInt32(award.nimbus)
	w.WriteInt16(0) // coin_buff
	w.WriteInt16(int16(award.expBuff))
	if isHaveNext {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	w.WriteInt32(award.juLing)
}

func runMockGameServer(conn net.Conn) {
	defer conn.Close()

	// 帮派吉星高照 mock 状态: 今日剩余 2 次投掷机会
	rollCakeRemain := int8(2)
	rollCakeTodayScore := int32(0)

	// 仙旅奇缘 mock 状态: 每日 3 次，已参与 0 次。
	// 每轮答题 1 次即产出宝箱，用于覆盖 routine 的开箱门禁分支。
	fateJoinCount := int32(0)
	fateTotalCount := int32(3)
	fateCurAnswerTimes := int32(0)
	fateTolAnswerTimes := int32(1)

	// 帮派祭神 mock 状态: 每日 3 次免费上香机会
	factionIncenseCount := int32(0)
	factionIncenseTotal := int32(3)
	factionBlessingCount := int32(0)

	// 帮派转盘 mock 状态: 持有 2 枚青龙令
	luckyWheelRosefinch := int32(2)

	// 个人吉星高照 mock 状态: 今日剩余 2 次投掷、2 次免费改运
	luckyStarRemain := int16(2)
	luckyStarFreeRerolls := int8(2)
	luckyStarRollCount := int32(0)
	luckyStarPending := false
	luckyStarDice := []int8{1, 2, 3, 4, 4, 4}
	luckyStarType := int8(3)

	// 功能终结礼包 mock 状态: 每个礼包的剩余可领取次数与元宝消耗。
	// 1 = 免费礼包(可连续领取 2 次), 4 = 吉星高照(消耗 50 元宝), 16 = 免费礼包。
	// functionEndRolled 记录已通过 random_award 摇出的结果，仅用于列表展示；
	// 普通领取 get_game_function_end_gift 不依赖摇奖状态，也不扣除元宝。
	functionEndGiftIDs := []int16{1, 4, 16}
	functionEndRemaining := map[int16]int{1: 2, 4: 1, 16: 1}
	functionEndCost := map[int16]int32{1: 0, 4: 50, 16: 0}
	functionEndRolled := map[int16]bool{}

	// 喜从天降 mock 状态: 持有 2 枚龙鱼令、1 次五福祈福机会
	findImmortalTokens := int16(2)
	// 对齐线上抓包: start 回包的 remain_number 是“剩余吹鱼次数”(随鱼随机),
	// 与龙鱼令数量无关，因此固定返回 3，防止再次把它误当龙鱼令余量。
	findImmortalGoadRemain := int16(3)
	// 初始存在一条上一轮遗留的未领取龙鱼，用于覆盖召唤回包 GOADING 的恢复分支。
	findImmortalPending := true
	findImmortalState := int8(0)
	findImmortalCultivation := int32(1000)

	// 悟法领域 mock 状态: 秘馆 2 有 1 次免费十连，抽卡后归零。
	wuFaFreeTenCount := int32(1)
	// 法力星图 mock 状态: 2 次免费占星
	starPictureMockFree := int32(2)
	findImmortalTotalCount := int32(5)
	findBlessNumber := int16(1)
	findBlessStage := int8(0)
	findBlessTotalCount := int8(0)

	// 天帝宝库 mock 状态: 持有 2 枚免费钥匙、1 次可领免费，三档各开放 3 个候选。
	treasureFreeItemNum := int32(2)
	treasureLeftCanGetFree := int32(1)
	treasureUseFreeFlag := int32(0)
	treasureTotalTimes := int32(0)
	treasureWishList := map[int32]int32{1: 50, 2: 25, 3: 42}
	treasureOffered := map[int32][]int32{
		1: {50, 12, 9},
		2: {25, 21, 30},
		3: {42, 33, 31},
	}

	for {
		pkt, err := protocol.ReadPacket(conn)
		if err != nil {
			return
		}

		switch pkt.ActionID {
		case protocol.ActionIDPlayerLogin:
			// 响应主服登录成功 Golden Case 2 (ResultCode=4)
			respPayload := []byte{
				0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerLogin, respPayload))

		case protocol.ActionIDPlayerInitStep1:
			// 响应 Golden Case 3 (TownID=4879, TownLine=2, SceneID=4879, TargetID=4882)
			initResp := protocol.NewPacket(protocol.ActionIDPlayerInitStep1, []byte{
				0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12,
			})
			_ = protocol.WritePacket(conn, initResp)

		case protocol.ActionIDPlayerInitStep2:
			// 响应场景初始化步 2 确认
			initResp2 := protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00})
			_ = protocol.WritePacket(conn, initResp2)

		case protocol.ActionIDPlayerInitStep3:
			// 响应扩展模块初始化步 3 确认
			initResp3 := protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8))
			_ = protocol.WritePacket(conn, initResp3)

		case protocol.ActionIDPlayerGetInfo:
			// 响应全量角色资产快照 (201 体力、39409 元宝、362 亿铜钱)
			var rawBuf bytes.Buffer
			binary.Write(&rawBuf, binary.BigEndian, int16(0)) // Result
			binary.Write(&rawBuf, binary.BigEndian, int16(2)) // RoleID
			name := "梦一场"
			binary.Write(&rawBuf, binary.BigEndian, uint16(len(name)))
			rawBuf.WriteString(name)
			binary.Write(&rawBuf, binary.BigEndian, int32(300))         // Level
			binary.Write(&rawBuf, binary.BigEndian, int32(39409))       // Ingots
			binary.Write(&rawBuf, binary.BigEndian, int64(36200000000)) // Coins
			rawBuf.Write(make([]byte, 16))                              // 16B 占位
			binary.Write(&rawBuf, binary.BigEndian, int32(201))         // Stamina 201 点
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, rawBuf.Bytes()))

		case protocol.ActionIDStLogin:
			// 响应跨服登录成功，并推送 0x0300 初始体力
			res := protocol.StLoginResult{
				Result:     0,
				PlayerID:   65536,
				ServerTime: 1791469358,
			}
			resPkt, _ := protocol.BuildStLoginResultPacket(res)
			_ = protocol.WritePacket(conn, resPkt)

			// 推送体力 201 点 (Mod_Player_Base 0x0300)
			upw := protocol.NewWriter()
			upw.WriteUint8(protocol.PlayerPropPower)
			upw.WriteInt32(201)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerUpdateData, upw.Bytes()))

		case protocol.ActionIDPlayerGetFunctions:
			// 参与自动化功能门槛的玩法在 Mock 下一律视为已开放，避免单测因
			// 功能开放查询无回包而整轮跳过。
			ids := []int32{
				protocol.FunctionIDZodiac,
				protocol.FunctionIDBeelzebubTrials,
				protocol.FunctionIDMiracleFighters,
				protocol.FunctionIDRepressDemonTower,
				protocol.FunctionIDShanhaiWorld,
				protocol.FunctionIDEudemons,
				protocol.FunctionIDDestinySword,
				protocol.FunctionIDLinkFate,
				protocol.FunctionIDPartnerLink,
				protocol.FunctionIDSaintArea,
				protocol.FunctionIDFaction,
				protocol.FunctionIDTreasureHouse,
				protocol.FunctionIDStTrials,
				protocol.FunctionIDStPracticeRoom,
				protocol.FunctionIDTravelEvent,
				protocol.FunctionIDRollCake,
				protocol.FunctionIDFindImmortal,
				protocol.FunctionIDFishing,
				protocol.FunctionIDWuFaField,
				protocol.FunctionIDWorshipMars,
				protocol.FunctionIDRefinePool,
				protocol.FunctionIDMarryHome,
				protocol.FunctionIDAwakeForbiddenBook,
				protocol.FunctionIDPartnerPractice,
				protocol.FunctionIDPartnerExpedition,
				protocol.FunctionIDEraCreationRealm,
				protocol.FunctionIDBaiLianQianKun,
				protocol.FunctionIDEraYanJie,
				protocol.FunctionIDDragonball,
				protocol.FunctionIDFiveBlessings,
				protocol.FunctionIDLunHuiJingDi,
			}
			w := protocol.NewWriter()
			w.WriteUint16(uint16(len(ids)))
			for _, id := range ids {
				w.WriteInt32(id)
				w.WriteUint8(1)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerGetFunctions, w.Bytes()))

		case protocol.ActionHeartbeat:
			// 响应心跳包 (Mod_Player_Base.heart response=[Byte])
			hbPkt := protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x01})
			_ = protocol.WritePacket(conn, hbPkt)

		case protocol.ActionIDPracticeGetStatus:
			// 响应扫荡状态查询 (HeaderTag: "FP", 0 missions, isPracticing: 0)
			w := protocol.NewWriter()
			w.WriteBytes([]byte{'F', 'P'})
			w.WriteUint16(0)
			w.WriteUint8(0)
			w.WriteUint32(0)
			w.WriteUint32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPracticeGetStatus, w.Bytes()))

		case protocol.ActionIDPracticeStart:
			// 响应关卡扫荡请求 (兼容 StartPracticeRequest 与 SweepRequest)
			if req, err := protocol.ParseStartPracticeRequest(pkt.Payload); err == nil {
				res := protocol.StartPracticeResult{
					Result:    protocol.PracticeResultSuccess,
					MissionID: req.MissionID,
					Count:     req.Count,
				}
				resPkt, _ := protocol.BuildStartPracticeResultPacket(res)
				_ = protocol.WritePacket(conn, resPkt)
			} else if sReq, err := protocol.ParseSweepRequest(pkt.Payload); err == nil {
				res := protocol.SweepResult{
					Success:   true,
					MissionID: sReq.MissionID,
					Times:     sReq.Times,
					CostPower: int(sReq.Times) * 5,
					GainExp:   int64(sReq.Times) * 2500,
					GainCoins: int64(sReq.Times) * 12000,
					Message:   "扫荡完成",
				}
				resPkt, _ := protocol.BuildSweepResultPacket(res)
				_ = protocol.WritePacket(conn, resPkt)
			}

		case protocol.ActionIDPracticeQuickly:
			// 响应 Mod_MissionPractice_Base 加速完成包
			req, err := protocol.ParseQuicklyRequest(pkt.Payload)
			if err == nil {
				res := protocol.QuicklyResult{
					Result: protocol.PracticeResultSuccess,
					Count:  req.Count,
				}
				resPkt, _ := protocol.BuildQuicklyResultPacket(res)
				_ = protocol.WritePacket(conn, resPkt)
			}

		case protocol.ActionIDSignInStatus:
			// 响应签到活动状态: 活动开启、有奖、状态正常
			_ = protocol.WritePacket(conn, protocol.BuildSignInStatusResultPacket(1, 1, 1))

		case protocol.ActionIDSignInInfo:
			// 响应签到详情: 今日未签到、无连签奖励、5 个累计档位均可领取
			_ = protocol.WritePacket(conn, protocol.BuildSignInInfoResultPacket(protocol.SignInInfo{
				ContinueState: protocol.SignInStateAlreadyGet,
				ContinueDay:   1,
				TotalInfo: []protocol.SignInTier{
					{Tier: 2, State: protocol.SignInStateCanGet},
					{Tier: 3, State: protocol.SignInStateCanGet},
				},
				TotalDay:  0,
				SignState: protocol.SignInStateNoSignIn,
				CurDay:    1,
			}))

		case protocol.ActionIDSignInPlayer:
			// 响应签到成功 (SI_SUCCESS)
			_ = protocol.WritePacket(conn, protocol.BuildSignInActionResultPacket(protocol.SignInActionResult{
				Result:   protocol.SignInResultOK,
				TotalDay: 1,
			}, false))

		case protocol.ActionIDSignInContinue:
			_ = protocol.WritePacket(conn, protocol.BuildSignInContinueResultPacket(protocol.SignInResultSuccess, 1))

		case protocol.ActionIDSignInTotalAward:
			_ = protocol.WritePacket(conn, protocol.BuildSignInTotalAwardResultPacket(protocol.SignInActionResult{
				Result:   protocol.SignInResultSuccess,
				TotalDay: 1,
			}))

		case protocol.ActionIDVIPInfo:
			// 响应 VIP 信息: VIP 2, 等级 1 已领取、等级 2 可领取
			_ = protocol.WritePacket(conn, protocol.BuildVIPInfoResultPacket(protocol.VIPInfo{
				VIPLevel:         2,
				MoneyToNextLevel: 1000,
				Gifts: []protocol.VIPGiftState{
					{Level: 1, SendGiftGot: 1},
					{Level: 2, SendGiftGot: 0},
					{Level: 3, SendGiftGot: 0},
				},
			}))

		case protocol.ActionIDVIPSendGift:
			_ = protocol.WritePacket(conn, protocol.BuildVIPGiftResultPacket(protocol.VIPResultSuccess))

		case protocol.ActionIDWorldBossList:
			// 响应世界 Boss 列表: 1 个 Boss 处于开战中
			_ = protocol.WritePacket(conn, protocol.BuildWorldBossListResultPacket([]protocol.WorldBossEntry{
				{BossID: 1, MapID: 11, Level: 280, State: protocol.WorldBossStateOpen},
			}))

		case protocol.ActionIDWorldBossFight:
			_ = protocol.WritePacket(conn, protocol.BuildWorldBossFightResultPacket(protocol.WorldBossResultSuccess))

		case protocol.ActionIDScratchCardInfo:
			_ = protocol.WritePacket(conn, protocol.BuildScratchCardInfoResultPacket(protocol.ScratchCardInfo{
				InActivity:   1,
				ActiveDegree: 20,
				CardTimes:    1,
			}))

		case protocol.ActionIDScratchCardShave:
			_ = protocol.WritePacket(conn, protocol.BuildScratchCardShaveResultPacket(protocol.ScratchCardResultIngot, 3345))

		case protocol.ActionIDRuneBatchList:
			_ = protocol.WritePacket(conn, protocol.BuildRuneBatchListResultPacket(protocol.RuneBatchInfo{
				Field1: 600, Field2: 10, Field3: 98, Field4: 24050000,
			}))

		case protocol.ActionIDImperialExamStatus:
			_ = protocol.WritePacket(conn, protocol.BuildImperialExamStatusResultPacket(protocol.ImperialExamStatus{
				Status: protocol.ImperialExamActivityStart,
			}))

		case protocol.ActionIDGuessPartnerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerInfoResultPacket(1))

		case protocol.ActionIDGuessPartnerStart:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerStartResultPacket(protocol.GuessPartnerRound{
				Result: protocol.GuessPartnerResultSuccess, PictureID: 1, RemainTime: 60,
				RemainScrapeTimes: 1, RemainGuessTimes: 1, RoleIDs: []int32{1, 2},
			}))

		case protocol.ActionIDGuessPartnerScrape:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerScrapeResultPacket(protocol.GuessPartnerResultSuccess, 0))

		case protocol.ActionIDGuessPartnerGuess:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerGuessResultPacket(protocol.GuessPartnerResultRight, 0, 5715, 1))

		case protocol.ActionIDStMineInfo:
			_ = protocol.WritePacket(conn, protocol.BuildStMineInfoResultPacket(protocol.StMineInfo{
				Result: protocol.StMineResultSuccess, Level: 1, CurMountain: 1,
			}))

		case protocol.ActionIDStMineEnter:
			_ = protocol.WritePacket(conn, protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineEnter, protocol.StMineResultSuccess))

		case protocol.ActionIDStMineReceiveAward:
			_ = protocol.WritePacket(conn, protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineReceiveAward, protocol.StMineResultSuccess))

		case protocol.ActionIDStMineLeave:
			_ = protocol.WritePacket(conn, protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineLeave, protocol.StMineResultSuccess))

		case protocol.ActionIDStTrialsModeStatus:
			_ = protocol.WritePacket(conn, protocol.BuildStTrialsModeStatusResultPacket([]protocol.StTrialsMode{
				{ModeType: 1, RemainChallengeTimes: 1, ModeStatus: 1},
			}))

		case protocol.ActionIDStTrialsModeInfo:
			_ = protocol.WritePacket(conn, protocol.BuildStTrialsModeInfoResultPacket(protocol.StTrialsModeInfo{Floor: 1}))

		case protocol.ActionIDStTrialsGetAward:
			_ = protocol.WritePacket(conn, protocol.BuildStTrialsAwardResultPacket(protocol.StTrialsResultSuccess))

		case protocol.ActionIDStTrialsChallenge:
			_ = protocol.WritePacket(conn, protocol.BuildStTrialsChallengeResultPacket(protocol.StTrialsResultSuccess))

		case protocol.ActionIDWorldPkStatus:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkStatusResultPacket(protocol.WorldPkStatusOn))

		case protocol.ActionIDWorldPkJoin:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkJoinResultPacket(protocol.WorldPkResultSuccess))

		case protocol.ActionIDGodWarStatus:
			_ = protocol.WritePacket(conn, protocol.BuildGodWarStatusResultPacket(protocol.GodWarStatusSignUp, 0))

		case protocol.ActionIDGodWarSignUp:
			_ = protocol.WritePacket(conn, protocol.BuildGodWarSignUpResultPacket(protocol.GodWarResultSuccess))

		case protocol.ActionIDSectOpenStatus:
			_ = protocol.WritePacket(conn, protocol.BuildSectOpenStatusResultPacket(1))

		case protocol.ActionIDSectPanelInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDSectPanelInfo, []byte{0, 0, 0, 0}))

		case protocol.ActionIDSectAllianceSeason:
			_ = protocol.WritePacket(conn, protocol.BuildSectAllianceSeasonResultPacket(1, 0))

		case protocol.ActionIDMarryHomeSelfInfo:
			_ = protocol.WritePacket(conn, protocol.BuildMarryHomeSelfInfoResultPacket(0))

		case protocol.ActionIDFriendsSentPanel:
			_ = protocol.WritePacket(conn, protocol.BuildFriendsSentPanelResultPacket([]protocol.FriendsSentLetter{
				{ID: 1},
			}))

		case protocol.ActionIDFriendsSentOpen:
			_ = protocol.WritePacket(conn, protocol.BuildFriendsSentOpenResultPacket(protocol.FriendsSentOpenResult{
				IsRead: 0, IsGetAward: 0, ID: 1, Awards: [][2]int32{{5715, 1}},
			}))

		case protocol.ActionIDFriendsSentRead:
			_ = protocol.WritePacket(conn, protocol.BuildFriendsSentActionResultPacket(protocol.ActionIDFriendsSentRead, protocol.FriendsSentResultSuccess))

		case protocol.ActionIDFriendsSentAward:
			_ = protocol.WritePacket(conn, protocol.BuildFriendsSentActionResultPacket(protocol.ActionIDFriendsSentAward, protocol.FriendsSentResultSuccess))

		case protocol.ActionIDIceCaveInfo:
			_ = protocol.WritePacket(conn, protocol.BuildIceCaveInfoResultPacket(protocol.IceCaveInfo{
				Slots:       []protocol.IceCaveSlot{{Ice: 1, State: 0, IsIron: 0}},
				ActiveNum:   100,
				ActiveLimit: 10,
				HammerNum:   1,
			}))

		case protocol.ActionIDIceCaveHit:
			_ = protocol.WritePacket(conn, protocol.BuildIceCaveHitResultPacket(protocol.IceCaveResultSuccess))

		case protocol.ActionIDIceCaveGetGift:
			_ = protocol.WritePacket(conn, protocol.BuildIceCaveGiftResultPacket(protocol.IceCaveResultSuccess))

		case protocol.ActionIDFairyGourdDate:
			now := time.Now().Unix()
			_ = protocol.WritePacket(conn, protocol.BuildFairyGourdDateResultPacket(protocol.FairyGourdDate{
				StartTime: int32(now - 100), EndTime: int32(now + 3600),
			}))

		case protocol.ActionIDFairyGourdList:
			_ = protocol.WritePacket(conn, protocol.BuildFairyGourdListResultPacket([]protocol.FairyGourdEntry{
				{Coin: 100000, Exp: 500, Date: 20261105},
			}))

		case protocol.ActionIDFairyGourdClaim:
			_ = protocol.WritePacket(conn, protocol.BuildFairyGourdClaimResultPacket(protocol.FairyGourdResultSuccess))

		case protocol.ActionIDZodiacInfo:
			_ = protocol.WritePacket(conn, protocol.BuildZodiacInfoResultPacket(protocol.ZodiacInfo{
				NowZodiacLevel: 1, NowBarrier: 3, NextZodiacLevel: 1, NextBarrier: 4,
				RequireLevel: 100, GoldOilID: 0, Heaven: 1,
			}))

		case protocol.ActionIDZodiacChallenge:
			_ = protocol.WritePacket(conn, protocol.BuildZodiacChallengeResultPacket(
				protocol.ZodiacResultSuccess, 1, 4, 0))

		case protocol.ActionIDWorshipMarsInfo:
			_ = protocol.WritePacket(conn, protocol.BuildWorshipMarsInfoResultPacket(protocol.WorshipMarsInfo{
				MarsLevel: 1, IncenseCount: 0, TotalCount: 3,
			}))

		case protocol.ActionIDWorshipMarsRemain:
			_ = protocol.WritePacket(conn, protocol.BuildWorshipMarsRemainResultPacket(1))

		case protocol.ActionIDWorshipMarsIncense:
			_ = protocol.WritePacket(conn, protocol.BuildWorshipMarsIncenseResultPacket(protocol.WorshipMarsResultSuccess))

		case protocol.ActionIDFishHooks:
			_ = protocol.WritePacket(conn, protocol.BuildFishHooksResultPacket(2))

		case protocol.ActionIDFishDo:
			_ = protocol.WritePacket(conn, protocol.BuildFishDoResultPacket(protocol.FishResultSuccess, 5715, 1))

		case protocol.ActionIDForbiddenBookInfo:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookInfoResultPacket(protocol.ForbiddenBookInfo{
				Areas: []protocol.ForbiddenBookArea{{
					ForbiddenType: protocol.ForbiddenBookRegulus, ExploreType: protocol.ForbiddenBookOnce,
					CostCoin: 1000, CostIngot: 0, CurNumber: 1, TotalNumber: 5,
				}},
			}))

		case protocol.ActionIDForbiddenBookExplore:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExploreResultPacket(protocol.AwakeResultSuccess))

		case protocol.ActionIDForbiddenBookExchange:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExchangeResultPacket(protocol.AwakeResultSuccess))

		case protocol.ActionIDGoldenTouchStoneStatus:
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneStatusResultPacket(protocol.GoldenTouchStoneStatusTouch))

		case protocol.ActionIDGoldenTouchStoneTouchPanel:
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneTouchPanelResultPacket(protocol.GoldenTouchStonePanel{
				Level: 1, Exp: 10, ThisWeekLuckValue: 5, TodayTouchNum: 1,
			}))

		case protocol.ActionIDGoldenTouchStoneInit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDGoldenTouchStoneInit, []byte{0}))

		case protocol.ActionIDGoldenTouchStoneTouch:
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneTouchResultPacket(protocol.GoldenTouchStoneResultSuccess, 10, 5))

		case protocol.ActionIDAskFairyInfo:
			_ = protocol.WritePacket(conn, protocol.BuildAskFairyInfoResultPacket(protocol.AskFairyInfo{
				TotalLoginDay: 100,
				Level:         300,
				HasGainAward:  1,
				MaxAskNum:     1,
				HasAskNum:     0,
				LeftDay:       10,
				CurLevel:      6,
				RandomAwards: []protocol.AskFairyRandomAward{
					{AwardID: 1, ItemID: 3345, ItemCount: 20},
					{AwardID: 2, ItemID: 1747, ItemCount: 1000000},
				},
			}))

		case protocol.ActionIDAskFairyDo:
			_ = protocol.WritePacket(conn, protocol.BuildAskFairyActionResultPacket(protocol.ActionIDAskFairyDo, protocol.AskFairyResultSuccess))

		case protocol.ActionIDAskFairyGain:
			_ = protocol.WritePacket(conn, protocol.BuildAskFairyActionResultPacket(protocol.ActionIDAskFairyGain, protocol.AskFairyResultSuccess))

		case protocol.ActionIDAskFairyLevelAward:
			_ = protocol.WritePacket(conn, protocol.BuildAskFairyActionResultPacket(protocol.ActionIDAskFairyLevelAward, protocol.AskFairyResultNotEnoughDay))

		case protocol.ActionIDAskFairyRecallAward:
			_ = protocol.WritePacket(conn, protocol.BuildAskFairyActionResultPacket(protocol.ActionIDAskFairyRecallAward, protocol.AskFairyResultHasGainRecall))

		case protocol.ActionIDDeviceBindInfo:
			// 响应设备绑定状态: 已绑定且已验证，首绑/月度奖励均可领取
			_ = protocol.WritePacket(conn, protocol.BuildDeviceBindInfoResultPacket(protocol.DeviceBindInfo{
				Result:         protocol.DeviceBindResultSuccess,
				IsBind:         1,
				PhoneNum:       "138****0000",
				FirstReward:    0,
				MonthlyReward:  0,
				NeedVerifyBind: 0,
			}))

		case protocol.ActionIDDeviceBindFirstReward, protocol.ActionIDDeviceBindMonthlyReward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDDeviceBindFirstReward, []byte{protocol.DeviceBindResultSuccess}))

		case protocol.ActionIDEmailGetInfo:
			// 响应邮箱面板查询 (Mod 345, Act 0)，业务侧仅用于建立 prevAct 链。
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDEmailGetInfo, []byte{0}))

		case protocol.ActionIDEmailIsHaveNew:
			// 响应存在未读/新邮件 (Mod 345, Act 5)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDEmailIsHaveNew, []byte{protocol.EmailStateUnread}))

		case protocol.ActionIDEmailOneKeyGetAward:
			// 响应邮箱附件一键收取成功 (Mod 345, Act 6)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDEmailOneKeyGetAward, []byte{protocol.EmailResultSuccess}))

		case protocol.ActionIDFateGetInfo:
			// 响应仙旅奇缘状态查询
			resPkt := protocol.BuildFateGetInfoResultPacket(protocol.FateGetInfoResult{
				JoinCount:  fateJoinCount,
				TotalCount: fateTotalCount,
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDFateGetQuestion:
			// 响应仙旅奇缘事件题目 (对齐 answers.txt 真实故事)
			resPkt := protocol.BuildFateQuestionResultPacket(protocol.FateQuestionResult{
				EventID: 26,
				Story:   "此地出现女娲大神造人剩下的七彩神土，传闻使用神土可创造第三种人类。",
				Msg:     protocol.FateResultSuccess,
				Options: []protocol.FateOption{
					{AnswerID: 7, Sign: "A", Text: "创造第三人类，自诩为第三人类始祖。"},
					{AnswerID: 8, Sign: "B", Text: "遵守天地法则，封印神土，以免人间大乱。"},
				},
				CurAnswerTimes: fateCurAnswerTimes,
				TolAnswerTimes: fateTolAnswerTimes,
				JoinCount:      fateJoinCount,
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDFateLotteryDraw:
			// 响应仙旅奇缘答题宝箱领取: 宝箱开启后本轮答题计数归零
			fateCurAnswerTimes = 0
			_ = protocol.WritePacket(conn, protocol.BuildFateLotteryDrawResultPacket(protocol.FateLotteryDrawResult{
				Msg: protocol.FateResultSuccess,
			}))

		case protocol.ActionIDFateAnswer:
			// 响应仙旅奇缘答题结算
			if fateJoinCount < fateTotalCount {
				fateJoinCount++
			}
			if fateCurAnswerTimes < fateTolAnswerTimes {
				fateCurAnswerTimes++
			}
			resPkt := protocol.BuildFateAnswerResultPacket(protocol.FateAnswerResult{
				Message: "你获得50声望,30阅历！",
				Awards: []protocol.FateAward{
					{Type: protocol.FateAwardFame, Value: 50},
					{Type: protocol.FateAwardSkill, Value: 30},
				},
				Info: protocol.FateGetInfoResult{
					JoinCount:  fateJoinCount,
					TotalCount: fateTotalCount,
				},
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDFactionGodInfo:
			// 响应帮派祭神神像信息
			w := protocol.NewWriter()
			w.WriteInt32(5)    // 神像等级
			w.WriteInt32(1200) // 当前神像灵气
			w.WriteInt32(3000) // 升级所需神像灵气
			w.WriteInt32(factionIncenseCount)
			w.WriteInt32(factionIncenseTotal)
			w.WriteInt8(0)   // is_cover
			w.WriteUint16(0) // 上香记录条数
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFactionGodInfo, w.Bytes()))

		case protocol.ActionIDFactionGetBlessingCount:
			// 响应帮派祭神当前祝福次数
			w := protocol.NewWriter()
			w.WriteInt32(factionBlessingCount)
			w.WriteInt32(300) // 祝福金币加成
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFactionGetBlessingCount, w.Bytes()))

		case protocol.ActionIDFactionIncense:
			// 响应帮派祭神上香，按客户端提交的供品编号累加祝福次数
			offeringID := protocol.FactionOfferingWhiteSandalwood
			if len(pkt.Payload) >= 4 {
				offeringID = int32(binary.BigEndian.Uint32(pkt.Payload[:4]))
			}
			w := protocol.NewWriter()
			if factionIncenseCount >= factionIncenseTotal {
				w.WriteUint8(protocol.FactionIncenseResultCountUsed)
			} else {
				factionIncenseCount++
				if offering, ok := protocol.GetFactionOffering(offeringID); ok {
					factionBlessingCount = offering.BlessingCount
				}
				w.WriteUint8(protocol.FactionIncenseResultSuccess)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFactionIncense, w.Bytes()))

		case protocol.ActionIDLuckyWheelOpen:
			// 响应帮派转盘状态查询
			w := protocol.NewWriter()
			w.WriteUint8(protocol.LuckyWheelResultOK)
			w.WriteInt32(5000) // 今日帮派贡献
			w.WriteInt32(luckyWheelRosefinch)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDLuckyWheelOpen, w.Bytes()))

		case protocol.ActionIDLuckyWheelStart:
			// 响应帮派转盘抽奖，扣减一枚青龙令
			w := protocol.NewWriter()
			if luckyWheelRosefinch <= 0 {
				w.WriteUint8(protocol.LuckyWheelResultNotEnoughNumber)
				w.WriteInt8(0)
				w.WriteInt32(0)
				w.WriteInt32(0)
			} else {
				luckyWheelRosefinch--
				w.WriteUint8(protocol.LuckyWheelResultSuccess)
				w.WriteInt8(2) // 奖励类型: 阅历
				w.WriteInt32(luckyWheelRosefinch)
				w.WriteInt32(25000) // 奖励道具/阅历数值
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDLuckyWheelStart, w.Bytes()))

		case protocol.ActionIDFactionRollCakeInfo:
			// 响应帮派吉星高照战绩面板
			w := protocol.NewWriter()
			w.WriteInt32(980) // 本周最佳成绩
			w.WriteInt32(rollCakeTodayScore)
			w.WriteInt8(rollCakeRemain)
			w.WriteInt32(5000) // 帮派累计成绩
			w.WriteUint16(1)   // 成员条数
			w.WriteInt32(2)    // 成员角色编号 (与登录回包 RoleID 一致)
			w.WriteString("梦一场")
			w.WriteInt32(980)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFactionRollCakeInfo, w.Bytes()))

		case protocol.ActionIDFactionRollCakeRemainTimes:
			// 响应吉星高照剩余投掷次数
			w := protocol.NewWriter()
			w.WriteInt8(rollCakeRemain)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFactionRollCakeRemainTimes, w.Bytes()))

		case protocol.ActionIDFactionRollCake:
			// 响应摇骰结算: 6+6+6 豹子，扣减一次投掷机会
			if rollCakeRemain > 0 {
				rollCakeRemain--
			}
			rollCakeTodayScore += 66
			w := protocol.NewWriter()
			w.WriteUint8(protocol.RollCakeResultSuccess)
			w.WriteInt8(3) // 奖励类型
			w.WriteInt32(66)
			w.WriteInt32(rollCakeTodayScore)
			w.WriteUint16(3) // 骰子数量
			w.WriteInt8(6).WriteInt8(6).WriteInt8(6)
			w.WriteInt8(rollCakeRemain)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFactionRollCake, w.Bytes()))

		case protocol.ActionIDFindImmortalGetXunXianLing:
			// 响应喜从天降龙鱼令数量
			w := protocol.NewWriter()
			w.WriteInt16(findImmortalTokens)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalGetXunXianLing, w.Bytes()))

		case protocol.ActionIDFindImmortalExchangeXianLing:
			// 响应声望兑换龙鱼令: 读取 int32 兑换数量并成功入账
			if req, err := protocol.NewReader(pkt.Payload).ReadInt32(); err == nil && req > 0 {
				findImmortalTokens += int16(req)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalExchangeXianLing, []byte{protocol.FindImmortalResultSuccess}))

		case protocol.ActionIDFindImmortalOpen:
			// 响应喜从天降面板
			w := protocol.NewWriter()
			w.WriteInt16(findImmortalTokens) // can_buy_number
			writeFindImmortalAwardMock(w, findImmortalState)
			w.WriteInt8(findImmortalState)
			w.WriteInt8(1) // god_id
			w.WriteInt32(findImmortalCultivation)
			w.WriteInt32(findImmortalTotalCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalOpen, w.Bytes()))

		case protocol.ActionIDFindImmortalStart:
			// 响应召唤龙鱼: 存在未领取龙鱼时返回 GOADING，否则消耗一枚龙鱼令
			result := protocol.FindImmortalResultSuccess
			if findImmortalPending {
				result = protocol.FindImmortalResultGoading
			} else if findImmortalTokens > 0 {
				findImmortalTokens--
				findImmortalPending = true
				findImmortalState = 0
			} else {
				result = protocol.FindImmortalResultHaveNotNumber
			}
			w := protocol.NewWriter()
			w.WriteUint8(result)
			writeFindImmortalAwardMock(w, findImmortalState)
			w.WriteInt8(findImmortalState)
			w.WriteInt8(1) // god_id
			w.WriteInt16(findImmortalGoadRemain)
			w.WriteInt32(findImmortalTotalCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalStart, w.Bytes()))

		case protocol.ActionIDFindImmortalGoad:
			// 响应吹龙鱼: 品阶 +1，最高 5 阶
			if findImmortalState < 5 {
				findImmortalState++
			}
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			writeFindImmortalAwardMock(w, findImmortalState)
			w.WriteInt8(findImmortalState)
			w.WriteInt8(1) // god_id
			w.WriteInt32(findImmortalTotalCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalGoad, w.Bytes()))

		case protocol.ActionIDFindImmortalPickupAward:
			// 响应收取龙鱼奖励: 累计仙令增加、清空待领取龙鱼并重置品阶
			findImmortalPending = false
			findImmortalCultivation += 100
			findImmortalTotalCount++
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			writeFindImmortalAwardMock(w, findImmortalState)
			w.WriteInt32(findImmortalCultivation)
			findImmortalState = 0
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalPickupAward, w.Bytes()))

		case protocol.ActionIDFindImmortalIsFiveBlessingsOpen:
			// 响应五福祈福活动已开启
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalIsFiveBlessingsOpen, []byte{protocol.FindImmortalResultYes}))

		case protocol.ActionIDFindImmortalOpenFiveBlessings:
			// 响应五福祈福面板
			w := protocol.NewWriter()
			w.WriteInt16(findBlessNumber)
			w.WriteInt8(findBlessStage)
			w.WriteUint16(0) // 暂无待领取奖励
			w.WriteInt8(findBlessTotalCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalOpenFiveBlessings, w.Bytes()))

		case protocol.ActionIDFindImmortalStartBless:
			// 响应开始五福祈福: 消耗一次次数并翻出一张奖励卡
			result := protocol.FindImmortalResultSuccess
			if findBlessNumber > 0 {
				findBlessNumber--
				findBlessStage = 1
				findBlessTotalCount++
			} else {
				result = protocol.FindImmortalResultNotEnoughNumber
			}
			w := protocol.NewWriter()
			w.WriteUint8(result)
			w.WriteInt8(findBlessStage)
			writeFiveBlessingsAwardsMock(w)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalStartBless, w.Bytes()))

		case protocol.ActionIDFindImmortalContinueBless:
			// 响应继续五福祈福: 阶段 +1
			if findBlessStage < 5 {
				findBlessStage++
			}
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			w.WriteInt8(findBlessStage)
			writeFiveBlessingsAwardsMock(w)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalContinueBless, w.Bytes()))

		case protocol.ActionIDFindImmortalEndBless:
			// 响应结束祈福并领取奖励
			findBlessStage = 0
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			w.WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalEndBless, w.Bytes()))

		case protocol.ActionIDFindImmortalChangeLucky:
			// 响应一键变金
			w := protocol.NewWriter()
			w.WriteUint8(protocol.FindImmortalResultSuccess)
			w.WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFindImmortalChangeLucky, w.Bytes()))

		case protocol.ActionIDTreasureHouseMainPanel:
			// 响应天帝宝库主面板: result + wish_list + 免费钥匙/累计字段
			w := protocol.NewWriter()
			w.WriteUint8(protocol.TreasureHouseResultSuccess)
			w.WriteUint16(3)
			for _, quality := range []int32{3, 2, 1} {
				w.WriteInt32(quality)
				w.WriteInt32(treasureWishList[quality])
			}
			w.WriteInt32(treasureFreeItemNum)
			w.WriteInt32(treasureUseFreeFlag)
			w.WriteInt32(5) // free_need_times
			w.WriteInt32(0) // free_item_accu
			w.WriteInt32(0) // use_ingot_times
			w.WriteUint16(1)
			w.WriteInt32(1)  // min
			w.WriteInt32(10) // max
			w.WriteInt32(20) // cost ingot
			w.WriteInt32(treasureLeftCanGetFree)
			w.WriteInt32(treasureTotalTimes)
			w.WriteUint8(0) // is_new_around_shop_privilege_flag
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseMainPanel, w.Bytes()))

		case protocol.ActionIDTreasureHouseSelectPanel:
			// 响应天帝宝库自选面板: 三档候选及其 open_flag
			w := protocol.NewWriter()
			w.WriteUint8(protocol.TreasureHouseResultSuccess)
			w.WriteUint16(3)
			for _, quality := range []int32{3, 2, 1} {
				w.WriteInt32(quality)
				w.WriteInt32(0) // refresh_times
				w.WriteInt32(10)
				w.WriteInt32(3) // refresh_limit
				items := treasureOffered[quality]
				w.WriteUint16(uint16(len(items)))
				for _, itemID := range items {
					w.WriteInt32(itemID)
					w.WriteInt32(1) // open_flag
				}
			}
			w.WriteInt32(0) // notify_flag
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseSelectPanel, w.Bytes()))

		case protocol.ActionIDTreasureHouseSetAward:
			// 响应设定自选大奖: 校验三档候选必须位于当前开放池
			result := uint8(protocol.TreasureHouseResultSuccess)
			r := protocol.NewReader(pkt.Payload)
			count, cErr := r.ReadUint16()
			if cErr != nil || int(count) != 3 {
				result = protocol.TreasureHouseResultFailed
			} else {
				chosen := make(map[int32]int32, 3)
				for i := 0; i < int(count); i++ {
					quality, e1 := r.ReadInt32()
					awardID, e2 := r.ReadInt32()
					if e1 != nil || e2 != nil {
						result = protocol.TreasureHouseResultFailed
						break
					}
					chosen[quality] = awardID
				}
				if result == protocol.TreasureHouseResultSuccess {
					for quality, awardID := range chosen {
						valid := false
						for _, offeredID := range treasureOffered[quality] {
							if offeredID == awardID {
								valid = true
								break
							}
						}
						if !valid {
							result = protocol.TreasureHouseResultFailed
							break
						}
					}
				}
				if result == protocol.TreasureHouseResultSuccess {
					treasureWishList = chosen
				}
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseSetAward, []byte{result}))

		case protocol.ActionIDTreasureHouseSetUseFreeFlag:
			// 响应设置优先免费次数
			if v, rErr := protocol.NewReader(pkt.Payload).ReadInt32(); rErr == nil {
				treasureUseFreeFlag = v
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseSetUseFreeFlag,
				[]byte{protocol.TreasureHouseResultSuccess}))

		case protocol.ActionIDTreasureHouseGetFreeItem:
			// 响应领取免费钥匙
			if treasureLeftCanGetFree > 0 {
				treasureLeftCanGetFree--
				treasureFreeItemNum++
				_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseGetFreeItem,
					[]byte{protocol.TreasureHouseResultSuccess}))
			} else {
				_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseGetFreeItem,
					[]byte{protocol.TreasureHouseResultNoFreeItem}))
			}

		case protocol.ActionIDTreasureHouseLottery:
			// 响应抽奖: 优先消耗免费钥匙，耗尽后返回 NO_ITEM
			w := protocol.NewWriter()
			if treasureFreeItemNum <= 0 {
				w.WriteUint8(protocol.TreasureHouseResultNoItem)
				w.WriteInt32(0)
			} else {
				treasureFreeItemNum--
				treasureTotalTimes++
				w.WriteUint8(protocol.TreasureHouseResultSuccess)
				w.WriteInt32(42)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseLottery, w.Bytes()))

		case protocol.ActionIDTreasureHouseGetAwardList:
			// 响应全量候选大奖列表: 45 项 [award_id, is_full]
			w := protocol.NewWriter()
			w.WriteUint16(45)
			for id := int32(1); id <= 45; id++ {
				w.WriteInt32(id)
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTreasureHouseGetAwardList, w.Bytes()))

		case protocol.ActionIDRollCakeGetState:
			// 响应个人吉星高照未领取记录
			w := protocol.NewWriter()
			if luckyStarPending {
				w.WriteUint8(protocol.RollCakeCodeHaveRecord)
			} else {
				w.WriteUint8(protocol.RollCakeCodeNoRecord)
			}
			w.WriteInt8(luckyStarType)
			w.WriteInt32(10) // 逆天改运所需元宝
			for _, num := range luckyStarDice {
				w.WriteInt8(num)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeGetState, w.Bytes()))

		case protocol.ActionIDRollCakeGetCount:
			// 响应个人吉星高照剩余次数与免费改运次数
			w := protocol.NewWriter()
			w.WriteInt16(luckyStarRemain)
			w.WriteInt8(luckyStarFreeRerolls)
			w.WriteInt8(5) // 今日可购买改运次数
			w.WriteInt32(luckyStarRollCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeGetCount, w.Bytes()))

		case protocol.ActionIDRollCakeRoll:
			// 响应个人吉星高照投掷: 消耗一次次数并产生未领取记录
			w := protocol.NewWriter()
			if luckyStarRemain > 0 {
				luckyStarRemain--
				luckyStarRollCount++
				luckyStarPending = true
				luckyStarDice = []int8{1, 2, 3, 4, 4, 4}
				luckyStarType = 3
				w.WriteUint8(protocol.RollCakeCodeSuccess)
			} else {
				w.WriteUint8(protocol.RollCakeCodeFailure)
			}
			w.WriteInt8(luckyStarType)
			w.WriteInt32(10) // 逆天改运所需元宝
			for _, num := range luckyStarDice {
				w.WriteInt8(num)
			}
			w.WriteInt32(luckyStarRollCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeRoll, w.Bytes()))

		case protocol.ActionIDRollCakeReroll:
			// 响应个人吉星高照逆天改运: 优先消耗免费次数，改运后六吉
			w := protocol.NewWriter()
			if !luckyStarPending {
				w.WriteUint8(protocol.RollCakeCodeNoRecord)
			} else if luckyStarFreeRerolls > 0 {
				luckyStarFreeRerolls--
				luckyStarDice = []int8{4, 4, 4, 4, 4, 4}
				luckyStarType = 11
				w.WriteUint8(protocol.RollCakeCodeSuccess)
			} else {
				w.WriteUint8(protocol.RollCakeCodeNoRerollTimes)
			}
			w.WriteInt8(luckyStarType)
			w.WriteInt8(luckyStarFreeRerolls)
			w.WriteInt32(20) // 下次逆天改运所需元宝
			for _, num := range luckyStarDice {
				w.WriteInt8(num)
			}
			w.WriteInt32(luckyStarRollCount)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeReroll, w.Bytes()))

		case protocol.ActionIDRollCakeGetAward:
			// 响应个人吉星高照领取奖励
			w := protocol.NewWriter()
			if luckyStarPending {
				luckyStarPending = false
				w.WriteUint8(protocol.RollCakeCodeSuccess)
			} else {
				w.WriteUint8(protocol.RollCakeCodeNoRecord)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeGetAward, w.Bytes()))

		case protocol.ActionIDRollCakeIsAuto:
			// 响应自动逆天改运开关 (默认开启)
			w := protocol.NewWriter()
			w.WriteInt8(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeIsAuto, w.Bytes()))

		case protocol.ActionIDRollCakeSetAuto:
			// 响应设置自动逆天改运开关
			w := protocol.NewWriter()
			w.WriteUint8(protocol.RollCakeCodeSuccess)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDRollCakeSetAuto, w.Bytes()))

		case protocol.ActionIDArenaGetTimes:
			// 响应竞技场剩余挑战次数 (10 次)
			resPkt, _ := protocol.BuildArenaGetTimesResultPacket(10)
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDArenaOpenPanel:
			// 响应竞技场面板 (当前第 10 名, 含 1 条胜利战报)
			panel := protocol.ArenaPanelResult{
				MyRank:               10,
				Fame:                 18000000,
				FameLevel:            42,
				RemainChallengeTimes: 10,
				RemainBuyTimes:       100,
				NextBuyPrice:         12,
				Opponents: []protocol.ArenaOpponent{
					{Rank: 5, PlayerID: 1001, RoleID: 1, Name: "初级剑仙", Level: 30, AwardCoin: 40000, AwardFame: 10},
					{Rank: 4, PlayerID: 1002, RoleID: 2, Name: "天罗神将", Level: 40, AwardCoin: 45000, AwardFame: 10},
					{Rank: 3, PlayerID: 1003, RoleID: 5, Name: "万妖统帅", Level: 50, AwardCoin: 50000, AwardFame: 10},
				},
				WarReports: []protocol.ArenaWarReport{
					{
						WarReportID:       340247349,
						AttackPlayerID:    65536,
						AttackPlayerName:  "梦一场",
						DefensePlayerID:   1003,
						DefensePlayerName: "万妖统帅",
						WinPlayerID:       2, // 与 PlayerGetInfo 回包的 RoleID 一致，模拟本方获胜
						OldRank:           20,
						NewRank:           10,
						Version:           "2026110201",
						ReportTimer:       1793549704,
					},
				},
			}
			panelPkt, _ := protocol.BuildArenaPanelResultPacket(panel)
			_ = protocol.WritePacket(conn, panelPkt)

		case protocol.ActionIDArenaGetOpponents:
			// 响应竞技场可挑战对手列表
			opponents := []protocol.ArenaOpponent{
				{Rank: 5, PlayerID: 1001, RoleID: 1, Name: "初级剑仙", Level: 30, AwardCoin: 1000},
				{Rank: 4, PlayerID: 1002, RoleID: 2, Name: "天罗神将", Level: 40, AwardCoin: 1000},
				{Rank: 3, PlayerID: 1003, RoleID: 5, Name: "万妖统帅", Level: 50, AwardCoin: 1000},
			}
			resPkt, _ := protocol.BuildArenaGetOpponentsResultPacket(opponents)
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDArenaChallenge:
			// 响应竞技场挑战结算: 本方 (RoleID=2) 获胜, 战报编号与面板一致
			resPkt, _ := protocol.BuildArenaChallengeResultPacket(protocol.ArenaChallengeResult{
				Result:      protocol.ArenaResultSuccess,
				Success:     true,
				RemainTimes: 9,
				AwardFame:   90,
				AwardCoin:   900000,
				ReportID:    340247349,
				DeployType:  3,
				WinPlayerID: 2,
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDPlayerStartPractice:
			// 响应城镇打坐开始
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerStartPractice, []byte{}))

		case protocol.ActionIDPlayerGetPracticeData:
			// 响应城镇打坐练功数据查询
			data := protocol.TownPracticeData{
				PracticeTime:         1800,
				PracticeExp:          250000,
				PracticeSpeedNum:     24,
				PracticeExpOne:       10000,
				SuperTownPracticeExp: 2000,
			}
			resPkt, _ := protocol.BuildTownPracticeDataResponsePacket(data)
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDPlayerCancelPractice:
			// 响应城镇打坐取消
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerCancelPractice, []byte{}))

		case protocol.ActionIDPlayerSignFinishedPractice:
			// 响应城镇打坐结束并结算经验
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerSignFinishedPractice, []byte{}))

		case protocol.ActionIDPlayerCheckFunctionOpen:
			// Mock 环境默认所有功能均已开放；未开放分支由测试自定义服务端覆盖。
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerCheckFunctionOpen, []byte{protocol.PlayerFunctionOpenTrue}))

		case protocol.ActionIDIsGameFunctionEnd:
			// 响应已终结功能列表 (两个已终结功能)
			fw := protocol.NewWriter()
			fw.WriteUint16(2)
			fw.WriteInt16(1)
			fw.WriteUint8(1)
			fw.WriteInt16(4)
			fw.WriteUint8(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDIsGameFunctionEnd, fw.Bytes()))

		case protocol.ActionIDGameFunctionEndGift:
			// 响应待领取功能终结礼包列表 (仅返回仍有剩余次数的礼包)
			available := make([]int16, 0, len(functionEndGiftIDs))
			for _, id := range functionEndGiftIDs {
				if functionEndRemaining[id] > 0 {
					available = append(available, id)
				}
			}
			gw := protocol.NewWriter()
			gw.WriteUint16(uint16(len(available)))
			for _, id := range available {
				gw.WriteInt16(id)
				if functionEndRolled[id] {
					// 已摇奖: 回填本次奖励与领取价格，与线上服务端一致。
					award := mockFunctionEndAwardFor(id)
					gw.WriteInt16(award.statePoint)
					gw.WriteInt32(award.skill)
					gw.WriteInt32(award.xianLing)
					gw.WriteInt32(award.fame)
					gw.WriteInt32(award.nimbus)
					gw.WriteInt16(0) // coin_buff
					gw.WriteInt16(int16(award.expBuff))
					gw.WriteInt32(award.ingot)
					gw.WriteUint8(2) // bei
					gw.WriteInt32(award.juLing)
				} else {
					gw.WriteInt16(0)                   // state_point
					gw.WriteInt32(0)                   // skill
					gw.WriteInt32(0)                   // xian_ling
					gw.WriteInt32(0)                   // fame
					gw.WriteInt32(0)                   // nimbus
					gw.WriteInt16(0)                   // coin_buff
					gw.WriteInt16(0)                   // exp_buff
					gw.WriteInt32(functionEndCost[id]) // ingot cost
					gw.WriteUint8(2)                   // bei
					gw.WriteInt32(0)                   // juling
				}
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDGameFunctionEndGift, gw.Bytes()))

		case protocol.ActionIDGetGameFunctionEndGift:
			// 响应领取功能终结礼包。
			// 真实服务端要求先 random_award(127/3) 摇奖；未摇奖时返回
			// SUCCESS + 全 0 奖励 + is_have_next=1（会造成空转）。此处对齐该语义。
			gr := protocol.NewReader(pkt.Payload)
			giftID, gerr := gr.ReadInt16()
			rolled := functionEndRolled[giftID]
			if rolled {
				delete(functionEndRolled, giftID)
			}
			result := protocol.FunctionEndSuccess
			isHaveNext := false
			switch {
			case gerr != nil:
				result = protocol.FunctionEndNoIngot
			case os.Getenv("SXD_MOCK_FUNCTION_END_PACK_FULL") == "1":
				result = protocol.FunctionEndItemPackFull
			case functionEndRemaining[giftID] <= 0:
				// 无剩余次数: 成功但无后续
			default:
				functionEndRemaining[giftID]--
				isHaveNext = functionEndRemaining[giftID] > 0
			}
			cw := protocol.NewWriter()
			cw.WriteUint8(result)
			if result != protocol.FunctionEndSuccess {
				// 失败回包保持字段长度一致。
				cw.WriteInt16(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt16(0)
				cw.WriteInt16(0)
				cw.WriteUint8(0)
				cw.WriteInt32(0)
			} else if !rolled {
				// 未摇奖：全 0 奖励 + 还有下一份，模拟真实服务端。
				cw.WriteInt16(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt32(0)
				cw.WriteInt16(0)
				cw.WriteInt16(0)
				cw.WriteUint8(1)
				cw.WriteInt32(0)
			} else {
				writeMockFunctionEndClaimRewards(cw, giftID, isHaveNext)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDGetGameFunctionEndGift, cw.Bytes()))

		case protocol.ActionIDFunctionEndRandomAward:
			// 响应功能终结礼包奖励预览，并记录该礼包已完成摇奖。
			rr := protocol.NewReader(pkt.Payload)
			giftID, _ := rr.ReadInt16()
			functionEndRolled[giftID] = true
			rw := protocol.NewWriter()
			writeMockFunctionEndRandomAward(rw, giftID)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFunctionEndRandomAward, rw.Bytes()))

		case protocol.ActionIDNewPracticeGetInfo:
			// 响应现代新版打坐练功数据查询
			w := protocol.NewWriter()
			w.WriteUint8(protocol.NewPracticeSuccess) // Result
			w.WriteUint16(0)                          // AirList count = 0
			w.WriteUint32(3600)                       // GatherTime
			w.WriteUint32(259200)                     // MaxGatherTime
			w.WriteInt64(300000)                      // PracticeExp (待领取打坐经验)
			w.WriteInt32(1)                           // RouteLevel
			w.WriteInt32(0)                           // AirAccRate
			w.WriteInt32(0)                           // ExpAccRate
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDNewPracticeGetInfo, w.Bytes()))

		case protocol.ActionIDNewPracticeCollectAll:
			// 响应现代新版打坐练功一键领取
			w := protocol.NewWriter()
			w.WriteUint8(protocol.NewPracticeSuccess) // Result
			w.WriteUint16(0)                          // AirList count = 0
			w.WriteInt64(300000)                      // GainExp (实际增加经验)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDNewPracticeCollectAll, w.Bytes()))

		case protocol.ActionIDTowerGetInfo:
			// 响应六道轮回塔面板查询 (SequenceMax=25, Layer=1, SequenceWar=1, BagNum=2, ResetNum=1, ResetIngot=0, HideID=0, HighStone=50)
			tw := protocol.NewWriter()
			tw.WriteUint8(25) // SequenceMax
			tw.WriteUint8(1)  // Layer
			tw.WriteUint8(1)  // SequenceWar
			tw.WriteUint8(2)  // BagNum
			tw.WriteUint8(1)  // ResetNum
			tw.WriteUint16(0) // ResetIngot
			tw.WriteUint8(0)  // HideID
			tw.WriteInt32(50) // HighStone
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerGetInfo, tw.Bytes()))

		case protocol.ActionIDTowerStartChallenge:
			// 响应六道轮回塔单关挑战成功
			cw := protocol.NewWriter()
			cw.WriteUint8(protocol.TowerResultSuccess) // Result: SUCCESS
			cw.WriteInt32(1001)                        // WarSoulID
			cw.WriteInt32(300000)                      // WarExp
			cw.WriteInt32(300)                         // WarFame
			cw.WriteInt32(50000)                       // WarCoin
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerStartChallenge, cw.Bytes()))

		case protocol.ActionIDTowerResetLayer:
			// 响应六道轮回重置
			rw := protocol.NewWriter()
			rw.WriteUint8(protocol.TowerResultSuccess) // Result
			rw.WriteUint8(1)                           // Layer
			rw.WriteUint16(50)                         // ResetIngot
			rw.WriteUint8(0)                           // ResetNum
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerResetLayer, rw.Bytes()))

		case protocol.ActionIDTowerStartAutoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerStartAutoChallenge, []byte{protocol.TowerResultSuccess}))

		case protocol.ActionIDTowerQuicklyAutoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerQuicklyAutoChallenge, []byte{protocol.TowerResultSuccess}))

		case protocol.ActionIDTowerStopAutoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerStopAutoChallenge, []byte{}))

		case protocol.ActionIDBeelzebubTrialsGetOpenTime:
			// 响应魔王试炼开启时间：当前处于开启窗口内，避免自动化等待。
			now := time.Now().Unix()
			tow := protocol.NewWriter()
			tow.WriteUint8(1)  // state
			tow.WriteUint16(1) // 1 window
			tow.WriteInt32(int32(now - 3600))
			tow.WriteInt32(int32(now + 3600))
			tow.WriteInt32(0) // all death = false
			tow.WriteUint8(protocol.BeelzebubTrialsNonActivity)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetOpenTime, tow.Bytes()))

		case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
			// 响应魔王试炼可挑战检查
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCheckCanChallenge, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsGetCanChallengeList:
			// 响应可挑战魔王列表: 3个魔王 (1, 71, 72: 魔掌门神识)
			bw := protocol.NewWriter()
			bw.WriteUint16(3)
			bw.WriteInt32(1)
			bw.WriteInt32(71)
			bw.WriteInt32(72)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetCanChallengeList, bw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsOpen:
			// 响应魔王试炼主面板
			ow := protocol.NewWriter()
			ow.WriteUint16(0)   // 0 teams
			ow.WriteInt32(0)    // int32
			ow.WriteUint16(0)   // 0 bosses in list
			ow.WriteInt32(5000) // moralExp
			ow.WriteInt32(3)    // moralLv
			ow.WriteInt32(1)    // bGet (canClaim)
			ow.WriteUint8(protocol.BeelzebubTrialsOnActivity)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsOpen, ow.Bytes()))

		case protocol.ActionIDBeelzebubTrialsCreateTeam:
			// 响应创建队伍成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsJoinTeam:
			// 响应加入队伍成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsJoinTeam, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo:
			// 响应队伍成员信息 (队长1人)
			tiw := protocol.NewWriter()
			tiw.WriteInt32(1001) // leader ID
			tiw.WriteUint16(1)   // 1 member
			tiw.WriteInt32(1001) // playerID
			tiw.WriteString("大侠")
			tiw.WriteString("主角")
			tiw.WriteInt16(120)
			tiw.WriteInt32(0)
			tiw.WriteInt32(0)
			tiw.WriteInt32(0)
			tiw.WriteString("")
			tiw.WriteInt16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, tiw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsGetHelperRoleList:
			// 响应助战伙伴列表 (2个助战镜像)
			hw := protocol.NewWriter()
			hw.WriteUint16(2)
			hw.WriteInt32(501)
			hw.WriteString("巨灵神镜像")
			hw.WriteInt32(110)
			hw.WriteInt32(1)
			hw.WriteUint8(protocol.BeelzebubTrialsNoDeploy)
			hw.WriteInt16(0)

			hw.WriteInt32(502)
			hw.WriteString("杨戬镜像")
			hw.WriteInt32(115)
			hw.WriteInt32(2)
			hw.WriteUint8(protocol.BeelzebubTrialsNoDeploy)
			hw.WriteInt16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetHelperRoleList, hw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsChooseHelperRole:
			// 响应选择助战成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsChooseHelperRole, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsFight:
			// 响应发起战斗成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsFight, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsGetWarResult:
			// 响应战斗结算 (击败魔王，奖励声望2000，铜钱100万，道行500，经验20万)
			ww := protocol.NewWriter()
			ww.WriteUint8(protocol.BeelzebubTrialsSuccess) // warResult
			ww.WriteInt32(0)                               // placeholder
			ww.WriteInt32(0)                               // placeholder
			ww.WriteUint16(0)                              // 0 war frames
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetWarResult, ww.Bytes()))

		case protocol.ActionIDBeelzebubTrialsLeaveWar:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsLeaveWar, []byte{}))

		case protocol.ActionIDBeelzebubTrialsExitTeam:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsExitTeam, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsOpenMoral:
			mw := protocol.NewWriter()
			mw.WriteInt32(6000) // exp
			mw.WriteInt32(2)    // lv
			mw.WriteInt32(1)    // bGet
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsOpenMoral, mw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsGetMoralAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetMoralAward, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDWuFaFieldInfo:
			// 悟法领域面板: 2 个秘馆；秘馆 2 有 1 次免费十连，抽卡后剩余次数归零。
			w := protocol.NewWriter()
			w.WriteUint16(2)
			// 秘馆 1
			w.WriteInt32(1) // field_type
			w.WriteInt32(0) // today_count
			w.WriteInt32(0) // total_count
			w.WriteInt32(0) // curr_count_id
			w.WriteInt32(0) // left_free_one_count
			w.WriteInt32(0) // left_free_ten_count
			w.WriteInt32(0) // wish_gongfa
			w.WriteInt32(0) // wish_gongfa_prob
			w.WriteInt32(0) // left_total_ten_count
			// 秘馆 2
			w.WriteInt32(2)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(wuFaFreeTenCount) // left_free_ten_count
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			// 资源列表
			w.WriteUint16(1)
			w.WriteInt32(8364)
			w.WriteInt32(10)
			w.WriteInt32(0)
			// 公共字段 11 个 Int + is_skip
			w.WriteInt32(0) // gift_start_time
			w.WriteInt32(0) // gift_end_time
			w.WriteInt32(0) // have_privilege
			w.WriteInt32(0) // book_store_is_open
			w.WriteInt32(1) // ru_men_field_active
			w.WriteInt32(0) // psychic_tbld_level
			w.WriteInt32(0) // psychic_res_num
			w.WriteInt32(0) // is_lucky_day
			w.WriteInt32(2) // rest_times
			w.WriteInt32(0) // discount_num
			w.WriteInt32(0) // indexTime
			w.WriteInt8(1)  // is_skip
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWuFaFieldInfo, w.Bytes()))

		case protocol.ActionIDStarPictureInfo:
			// 法力星图面板: 2 次免费占星 + 2 次元宝占星。
			_ = protocol.WritePacket(conn, protocol.BuildStarPictureInfoResultPacket(protocol.StarPictureInfo{
				FreeDivination:  starPictureMockFree,
				IngotDivination: 2,
				ChipCount:       30,
				CurDeployLevel:  1,
			}))

		case protocol.ActionIDStarPictureDivination:
			// 免费占星: 只有 free 次数 > 0 才成功，绝不回落到元宝档。
			code := protocol.StarPictureResultSuccess
			if starPictureMockFree > 0 {
				starPictureMockFree--
			} else {
				code = 1
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDStarPictureDivination, []byte{code}))

		case protocol.ActionIDEraYanjiePanel:
			// 纪元湮劫主面板: 已通关关卡 0, 纪元等级 1。
			_ = protocol.WritePacket(conn, protocol.BuildEraYanjiePanelResultPacket(protocol.EraYanjiePanel{
				EraLevel:     1,
				MissionLevel: 0,
				EraZone:      1,
			}))

		case protocol.ActionIDEraYanjieMonsterTeam:
			// 关卡可战。
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDEraYanjieMonsterTeam, []byte{protocol.EraYanjieResultSuccess}))

		case protocol.ActionIDEraYanjieFight:
			// 战斗胜利。
			_ = protocol.WritePacket(conn, protocol.BuildEraYanjieFightResultPacket(protocol.EraYanjieFightResult{
				Result: protocol.EraYanjieResultSuccess,
				Battles: []protocol.EraYanjieBattle{
					{Seq: 1, IsWin: 1, WarReportID: 1, ReportVersion: "2026110501"},
				},
				IsWin: 1,
			}))

		case protocol.ActionIDMysticHuntEntranceOpen:
			// 仙幽猎境入口: mock 中视为开放。
			_ = protocol.WritePacket(conn, protocol.BuildMysticHuntEntranceResultPacket(1))

		case protocol.ActionIDMysticHuntEnter:
			// 进入猎境成功。
			_ = protocol.WritePacket(conn, protocol.BuildMysticHuntEnterResultPacket(protocol.MysticHuntResultSuccess))

		case protocol.ActionIDMarryHomeGetCake:
			// 家园小蛋糕: 领取成功并返回 1 件奖励。
			_ = protocol.WritePacket(conn, protocol.BuildMarryHomeCakeResultPacket(protocol.MarryHomeCakeResult{
				Result: protocol.MarryHomeResultSuccess,
				Awards: []protocol.MarryHomeCakeAward{{ItemID: 2001, ItemNum: 1}},
			}))

		case protocol.ActionIDGameAssistantInfo:
			// 每日活跃度面板: 仅返回「悟法领域冥想」任务，剩余 20 次。
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: 69, CurTimes: 0, MaxTimes: 20, PerActiveAdd: 10},
				{TaskID: 71, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
				{TaskID: 73, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))

		case protocol.ActionIDWuFaDrawCard:
			// 悟法感悟抽卡成功，返回 1 项奖励与剩余次数。
			if wuFaFreeTenCount > 0 {
				wuFaFreeTenCount--
			}
			w := protocol.NewWriter()
			w.WriteUint8(protocol.WuFaResultSuccess)
			w.WriteUint16(1)
			w.WriteInt32(8364) // item_id
			w.WriteInt32(1)    // item_num
			w.WriteInt32(0)    // change_item_id
			w.WriteInt32(0)    // change_item_num
			w.WriteInt32(0)    // is_synthesize
			w.WriteInt32(0)    // is_up_level
			w.WriteInt32(0)    // psychic_add_item_num
			w.WriteInt32(0)    // lucky_item_num
			w.WriteInt32(1)    // rest_times
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDWuFaDrawCard, w.Bytes()))
		}
	}
}
