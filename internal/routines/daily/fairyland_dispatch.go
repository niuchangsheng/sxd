// 本文件实现宠物仙域「游历」(Mod_PetImmortalDomain_Base, Module 755)。
//
// 流程:
//  1. 755/0 get_state 确认宠物仙域已开放 (0=OPEN)
//  2. 755/14 my_pet_list 获取已拥有宠物、当前游历数量(上限 2 只)及本周已游历时长(上限 20 小时)
//  3. 755/1 sect_domain_list 获取宗门仙域列表，按收益加成优先筛选本宗门空余仙域
//  4. 755/2 sect_domain_travel_info 查找空槽位 (pet_id==0) 并调用 755/15 pet_travel 派遣
//  5. 若宗门仙域均满位则回退至共享仙域 (domain_id=0, pos_id=0)
package daily

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// maxConcurrentPetTravel 同时游历宠物上限 (TRAVEL_TOTAL_LIMIT = 2)。
	maxConcurrentPetTravel = 2
	// maxPetTravelSeconds 单只宠物本周游历时长上限 (20 小时)。
	maxPetTravelSeconds = 20 * 3600
	// maxDomainInfoProbes 单轮最多查询的仙域详情次数，避免无谓请求。
	maxDomainInfoProbes = 12
)

// NewFairylandDispatchRoutine 构造宠物仙域游历任务 (Module 755)。
func NewFairylandDispatchRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"fairyland_dispatch",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.Call(pkt, act)
				if err != nil {
					slog.Info("宠物仙域协议无回包", "role_id", session.RoleID(),
						"module", act>>16, "act", act&0xFFFF, "err", err)
					return nil
				}
				return resp
			}

			// 1. 查询宠物仙域开放状态 (755/0)
			stateResp := call(protocol.BuildPetImmortalGetStatePacket(), protocol.ActionIDPetImmortalGetState)
			if stateResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				return scheduler.NewCooldown(30*time.Minute, "宠物仙域开放状态无回包")
			}
			state, err := protocol.ParseActionFirstByte(stateResp.Payload, "宠物仙域开放状态")
			if err != nil {
				slog.Warn("宠物仙域开放状态解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "宠物仙域开放状态解析失败")
			}
			if state == protocol.PetImmortalClose {
				slog.Info("宠物仙域当前未开放，2 小时后再查", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "宠物仙域未开放")
			}
			if state != protocol.PetImmortalOpen {
				slog.Info("宠物仙域开放状态异常，2 小时后再试", "role_id", session.RoleID(),
					"state", state, "state_text", protocol.PetImmortalDomainResultText(state))
				return scheduler.NewCooldown(2*time.Hour, "宠物仙域开放状态异常")
			}

			// 2. 查询我的宠物游历状态 (755/14)
			petResp := call(protocol.BuildPetImmortalMyPetListPacket(), protocol.ActionIDPetImmortalMyPetList)
			if petResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				return scheduler.NewCooldown(30*time.Minute, "宠物仙域宠物列表无回包")
			}
			petList, err := protocol.ParsePetImmortalMyPetList(petResp.Payload)
			if err != nil {
				slog.Warn("宠物仙域宠物列表解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "宠物仙域宠物列表解析失败")
			}

			ownedCount := 0
			travelingCount := 0
			var candidates []protocol.PetImmortalMyPet
			for _, p := range petList.Pets {
				if !p.IsHave {
					continue
				}
				ownedCount++
				if p.IsTravel {
					travelingCount++
					continue
				}
				if p.TravelTS < maxPetTravelSeconds {
					candidates = append(candidates, p)
				}
			}

			if travelingCount >= maxConcurrentPetTravel {
				slog.Info("宠物仙域已有 2 只宠物游历中，2 小时后复查",
					"role_id", session.RoleID(), "traveling", travelingCount, "owned", ownedCount)
				return scheduler.NewCooldown(2*time.Hour, "宠物仙域已有2只宠物游历中")
			}
			if ownedCount == 0 {
				slog.Info("宠物仙域暂无已拥有宠物，跳过游历", "role_id", session.RoleID())
				return scheduler.NewCooldown(6*time.Hour, "宠物仙域暂无已拥有宠物")
			}
			if len(candidates) == 0 {
				if travelingCount > 0 {
					slog.Info("宠物仙域其余宠物均已达本周上限，等待当前游历结束",
						"role_id", session.RoleID(), "traveling", travelingCount)
					return scheduler.NewCooldown(2*time.Hour, "宠物仙域等待当前游历结束")
				}
				wakeAt := nextFairylandWeeklyReset(time.Now())
				slog.Info("宠物仙域全部宠物本周游历时间已达 20 小时上限，等待下次重置探测",
					"role_id", session.RoleID(), "owned", ownedCount, "wake_at", wakeAt.Format(time.RFC3339))
				return scheduler.NewCooldownUntil(wakeAt, "宠物本周游历时间已达20小时上限")
			}

			// 优先派遣幸运上限高、已游历时间短的宠物
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].LuckLimit != candidates[j].LuckLimit {
					return candidates[i].LuckLimit > candidates[j].LuckLimit
				}
				if candidates[i].TravelTS != candidates[j].TravelTS {
					return candidates[i].TravelTS < candidates[j].TravelTS
				}
				return candidates[i].PetID < candidates[j].PetID
			})

			// 3. 查询宗门仙域列表 (755/1)
			domResp := call(protocol.BuildPetImmortalSectDomainListPacket(), protocol.ActionIDPetImmortalSectDomainList)
			if domResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				return scheduler.NewCooldown(30*time.Minute, "宠物仙域列表无回包")
			}
			domList, err := protocol.ParsePetImmortalSectDomainList(domResp.Payload)
			if err != nil {
				slog.Warn("宠物仙域列表解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "宠物仙域列表解析失败")
			}
			if domList.Result != protocol.PetImmortalSuccess {
				reason := protocol.PetImmortalDomainResultText(domList.Result)
				slog.Info("宠物仙域列表返回非成功码", "role_id", session.RoleID(),
					"result", domList.Result, "result_text", reason)
				switch domList.Result {
				case protocol.PetImmortalFunNotOpen, protocol.PetImmortalNoSect, protocol.PetImmortalSectNotInvolved:
					return scheduler.NewCooldown(6*time.Hour, "宠物仙域不可用: "+reason)
				default:
					return scheduler.NewCooldown(2*time.Hour, "宠物仙域列表失败: "+reason)
				}
			}

			var mySectDomains, otherSectDomains []protocol.PetImmortalSectDomain
			for _, d := range domList.Domains {
				if d.IsTravel {
					continue // 每个仙域最多同时游历 1 只己方宠物
				}
				if d.SectID == domList.MySectID {
					mySectDomains = append(mySectDomains, d)
				} else {
					otherSectDomains = append(otherSectDomains, d)
				}
			}
			sortDomains := func(ds []protocol.PetImmortalSectDomain) {
				sort.Slice(ds, func(i, j int) bool {
					bi := ds[i].ScorePro + ds[i].ExtraScorePro + ds[i].LuckPro + ds[i].ExtraLuckPro
					bj := ds[j].ScorePro + ds[j].ExtraScorePro + ds[j].LuckPro + ds[j].ExtraLuckPro
					if bi != bj {
						return bi > bj
					}
					li := ds[i].LuckPro + ds[i].ExtraLuckPro
					lj := ds[j].LuckPro + ds[j].ExtraLuckPro
					if li != lj {
						return li > lj
					}
					if ds[i].NowTravelNum != ds[j].NowTravelNum {
						return ds[i].NowTravelNum < ds[j].NowTravelNum
					}
					return ds[i].ID < ds[j].ID
				})
			}
			sortDomains(mySectDomains)
			sortDomains(otherSectDomains)

			// 4. 遍历本宗门仙域（若允许跨宗门游历，再补充其他宗门仙域）寻找空槽位派遣
			dispatched := 0
			petIdx := 0
			probed := 0
			canTravelOtherSect := false
			allHitTimeLimit := true

			tryDispatch := func(domainID, posID int32) (domainDone bool, stopAll bool, abortErr error) {
				for petIdx < len(candidates) && travelingCount < maxConcurrentPetTravel {
					if ctx.Err() != nil {
						return true, true, nil
					}
					pet := candidates[petIdx]
					tResp := call(protocol.BuildPetImmortalPetTravelPacket(domainID, posID, pet.PetID),
						protocol.ActionIDPetImmortalPetTravel)
					if tResp == nil {
						if abort := caller.AbortErr(); abort != nil {
							return true, true, abort
						}
						return true, true, scheduler.HandleSilentDropCtx(ctx, session, "fairyland_dispatch", 3,
							30*time.Minute, "宠物仙域游历派遣无回包")
					}
					scheduler.ResetSilentDropsCtx(ctx, session, "fairyland_dispatch")
					tCode, tErr := protocol.ParseActionFirstByte(tResp.Payload, "宠物仙域游历派遣")
					if tErr != nil {
						slog.Warn("宠物仙域游历派遣回包解析失败", "role_id", session.RoleID(), "err", tErr)
						return true, true, scheduler.NewCooldown(10*time.Minute, "宠物仙域游历派遣回包解析失败")
					}
					switch tCode {
					case protocol.PetImmortalSuccess:
						dispatched++
						travelingCount++
						petIdx++
						allHitTimeLimit = false
						slog.Info("宠物仙域游历派遣成功", "role_id", session.RoleID(),
							"domain_id", domainID, "pos_id", posID, "pet_id", pet.PetID,
							"traveling", travelingCount)
						return true, travelingCount >= maxConcurrentPetTravel, nil
					case protocol.PetImmortalTravelTotalLimit:
						slog.Info("宠物仙域同时游历数量已达上限", "role_id", session.RoleID())
						travelingCount = maxConcurrentPetTravel
						allHitTimeLimit = false
						return true, true, nil
					case protocol.PetImmortalTravelTimeLimit:
						slog.Info("宠物本周游历时长已达上限，跳过该宠物", "role_id", session.RoleID(), "pet_id", pet.PetID)
						petIdx++
					case protocol.PetImmortalTraveling, protocol.PetImmortalPetError, protocol.PetImmortalNoPet:
						slog.Info("宠物当前不可派遣，尝试下一只", "role_id", session.RoleID(),
							"pet_id", pet.PetID, "result", tCode,
							"result_text", protocol.PetImmortalDomainResultText(tCode))
						allHitTimeLimit = false
						petIdx++
					case protocol.PetImmortalPosNotEmpty, protocol.PetImmortalPosIDError:
						allHitTimeLimit = false
						return false, false, nil // 换同一仙域的下一个空槽位
					case protocol.PetImmortalDomainTravelLimit, protocol.PetImmortalNotMyDomain,
						protocol.PetImmortalNotMySpecialDomain, protocol.PetImmortalDomainSectChange,
						protocol.PetImmortalDidError, protocol.PetImmortalWaring:
						allHitTimeLimit = false
						return true, false, nil // 换下一个仙域
					case protocol.PetImmortalFunNotOpen, protocol.PetImmortalNoSect, protocol.PetImmortalSectNotInvolved:
						reason := protocol.PetImmortalDomainResultText(tCode)
						slog.Info("宠物仙域不可用，停止派遣", "role_id", session.RoleID(),
							"result", tCode, "result_text", reason)
						return true, true, scheduler.NewCooldown(6*time.Hour, "宠物仙域不可用: "+reason)
					default:
						allHitTimeLimit = false
						slog.Info("宠物仙域游历派遣未成功", "role_id", session.RoleID(),
							"domain_id", domainID, "pos_id", posID, "pet_id", pet.PetID,
							"result", tCode, "result_text", protocol.PetImmortalDomainResultText(tCode))
						return true, false, nil
					}
				}
				return true, true, nil
			}

			probeAndDispatch := func(domains []protocol.PetImmortalSectDomain) error {
				for _, d := range domains {
					if ctx.Err() != nil || travelingCount >= maxConcurrentPetTravel || petIdx >= len(candidates) {
						return nil
					}
					if probed >= maxDomainInfoProbes {
						return nil
					}
					probed++
					infoResp := call(protocol.BuildPetImmortalSectDomainTravelInfoPacket(d.ID),
						protocol.ActionIDPetImmortalSectDomainTravelInfo)
					if infoResp == nil {
						if abort := caller.AbortErr(); abort != nil {
							return abort
						}
						continue
					}
					info, pErr := protocol.ParsePetImmortalSectDomainTravelInfo(infoResp.Payload)
					if pErr != nil {
						slog.Warn("宠物仙域槽位详情解析失败", "role_id", session.RoleID(),
							"domain_id", d.ID, "err", pErr)
						continue
					}
					if info.Result != protocol.PetImmortalSuccess {
						continue
					}
					if info.CanTravelOtherSect {
						canTravelOtherSect = true
					}
					if info.NowSectID != info.MySectID && !info.CanTravelOtherSect {
						continue
					}
					hasMyPet := false
					for _, dp := range info.Pets {
						if dp.PetID > 0 && dp.AppID == info.MyAppID {
							hasMyPet = true
							break
						}
					}
					if hasMyPet {
						continue
					}
					for _, dp := range info.Pets {
						if dp.PetID != 0 || dp.PosID <= 0 {
							continue
						}
						domainDone, stopAll, abortErr := tryDispatch(d.ID, dp.PosID)
						if abortErr != nil {
							return abortErr
						}
						if stopAll {
							return nil
						}
						if domainDone {
							break
						}
					}
				}
				return nil
			}

			if err := probeAndDispatch(mySectDomains); err != nil {
				return err
			}
			if travelingCount < maxConcurrentPetTravel && petIdx < len(candidates) && canTravelOtherSect {
				if err := probeAndDispatch(otherSectDomains); err != nil {
					return err
				}
			}

			// 5. 若宗门仙域均满位或不可用，回退至共享仙域 (domain_id=0, pos_id=0)
			if travelingCount < maxConcurrentPetTravel && petIdx < len(candidates) {
				_, _, abortErr := tryDispatch(0, 0)
				if abortErr != nil {
					return abortErr
				}
			}

			if dispatched > 0 {
				slog.Info("宠物仙域游历派遣完成", "role_id", session.RoleID(),
					"dispatched", dispatched, "traveling", travelingCount)
				return scheduler.NewCooldown(2*time.Hour, "宠物仙域游历派遣完成，2小时后复查")
			}
			if allHitTimeLimit && petIdx >= len(candidates) && travelingCount == 0 {
				wakeAt := nextFairylandWeeklyReset(time.Now())
				slog.Info("宠物仙域候选宠物均达本周时长上限，等待下次重置探测",
					"role_id", session.RoleID(), "wake_at", wakeAt.Format(time.RFC3339))
				return scheduler.NewCooldownUntil(wakeAt, "宠物本周游历时间已达20小时上限")
			}
			slog.Info("宠物仙域本轮未派遣新宠物，2 小时后再查", "role_id", session.RoleID(),
				"traveling", travelingCount, "probed", probed)
			return scheduler.NewCooldown(2*time.Hour, "宠物仙域暂无可派遣槽位")
		},
	)
}

// nextFairylandWeeklyReset 返回下一次可重试时刻（每天 05:05，游戏侧日常重置点）。
//
// 宠物仙域本周累积游历时间上限为 20 小时；周一 05:05 重置后自动恢复。
func nextFairylandWeeklyReset(now time.Time) time.Time {
	reset := time.Date(now.Year(), now.Month(), now.Day(), 5, 5, 0, 0, now.Location())
	if !now.Before(reset) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset
}
