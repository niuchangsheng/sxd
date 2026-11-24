// 本文件实现仙盟上香与仙盟魔神挑战
// (Mod_StUnionActivity_Base 179 / Mod_StUnionTask_Base 337)。
//
// 抓包基准: /tmp/xianmeng.pcapng（2026-11-15，qq s0）。
// 魔神挑战在**仙界跨服会话**执行；上香优先跨服，无回包时回退到**仙盟城镇会话**
// 按 179/0 → 179/10 → 179/11 重试（写操作只在城镇会话被受理）。
//
//	上香: 179/10 面板 → 179/11 [offer_id=1]（铜钱档；禁止元宝档）
//	魔神: 337/0 面板 → 337/1 挑战，直到券不足/次数用完
//
// 严禁调用 337/3 buy_ticket 与任何元宝档上香。
package social

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// stUnionIncenseOfferID 上香档位（1 为白色最底档：铜钱/免费；2、3 为元宝档，禁止）。
const stUnionIncenseOfferID int32 = 1

// stUnionIncenseDropKey 上香静默丢包的独立计数键：与魔神挑战分开统计，
// 避免 337 的成功把上香计数清零，也避免上香失败拖累 337 的退避。
const stUnionIncenseDropKey = "st_union_incense"

// maxStUnionIncenseSectTownTries 跨服无回包后，改用仙盟城镇会话兜底探测的最大次数。
// 2026-11-16 16:42 实测城镇会话连 179/10 也收不到回包，故只保留最初两次探测。
const maxStUnionIncenseSectTownTries = 2

// maxStUnionGodFights 单轮最多魔神挑战次数（每周 3 次）。
const maxStUnionGodFights = 3

// maxStUnionTicketBuys 单轮最多购买挑战券次数（用户确认首次用道源购买）。
const maxStUnionTicketBuys = 2

// stUnionTicketBuyDisabled 一旦检测到购买消耗元宝，本进程内不再自动购买。
var stUnionTicketBuyDisabled atomic.Bool

// stUnionTicketStatePath 记录「买了挑战券仍然提示次数已用完」的日期，
// 跨进程持久化（与 server_chat_room_state.json 同样的做法），避免每次
// 重启后又白买一张券。
const stUnionTicketStatePath = "data/runtime/st_union_ticket_state.json"

// stUnionTicketIneffectiveDay 内存缓存，文件不可用时的兜底。
var stUnionTicketIneffectiveDay atomic.Value // string

func stUnionTicketIneffective(today string) bool {
	if v, _ := stUnionTicketIneffectiveDay.Load().(string); v == today {
		return true
	}
	b, err := os.ReadFile(stUnionTicketStatePath)
	if err != nil {
		return false
	}
	var st struct {
		IneffectiveDate string `json:"ineffective_date"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	return st.IneffectiveDate == today
}

func markStUnionTicketIneffective(today string) {
	stUnionTicketIneffectiveDay.Store(today)
	_ = os.MkdirAll(filepath.Dir(stUnionTicketStatePath), 0o755)
	b, _ := json.Marshal(map[string]string{"ineffective_date": today})
	if err := os.WriteFile(stUnionTicketStatePath, b, 0o644); err != nil {
		slog.Info("写入仙盟挑战券状态失败", "path", stUnionTicketStatePath, "err", err)
	}
}

// openStUnionSession 建立仙界跨服会话；单测可替换为复用 mock 会话。
var openStUnionSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenCrossServerSession(ctx)
}

// openStUnionSectSession 建立仙盟城镇会话；179/11 上香是仙盟场景写操作，
// 客户端从仙盟城镇进入，仙界跨服会话上会被服务端静默丢弃（179/0、179/10
// 只读面板在跨服会话可用，容易误判链路正常）。单测可替换。
var openStUnionSectSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenSectTownSession(ctx)
}

// stUnionSelfNames 返回用于匹配 179/10 记录的本角色显示名。
//
// 179/10 记录里的数字 ID 与会话 PlayerID 可能不是同一命名空间，故同时用
// 登录同步的官方昵称（可能带 .sN 后缀）与配置角色名兜底。
func stUnionSelfNames(session *client.RoleSession) []string {
	names := []string{session.RoleName(), session.OriginNickName()}
	out := names[:0]
	for _, n := range names {
		if n == "" {
			continue
		}
		out = append(out, trimServerSuffix(n))
	}
	return out
}

// trimServerSuffix 去掉官方昵称的 ".sN" 区服后缀（如 "曹植始.s1" → "曹植始"）。
func trimServerSuffix(name string) string {
	i := strings.LastIndex(name, ".s")
	if i <= 0 || i+2 >= len(name) {
		return name
	}
	for _, r := range name[i+2:] {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:i]
}

// logStUnionGodPanel 解析并打印 179/10 神像面板，返回「今日是否已上香」。
// panel 为 nil / 解析失败时第二个返回值 known 为 false，调用方应继续尝试上香。
func logStUnionGodPanel(scope string, session *client.RoleSession, resp *protocol.Packet) (offered, known bool) {
	if resp == nil {
		return false, false
	}
	head := resp.Payload
	if len(head) > 400 {
		head = head[:400]
	}
	slog.Info("仙盟神像面板", "scope", scope, "role_id", session.RoleID(),
		"len", len(resp.Payload), "hex", hex.EncodeToString(head))
	info, err := protocol.ParseStUnionGodInfo(resp.Payload)
	if err != nil {
		slog.Info("仙盟神像面板解析失败", "scope", scope, "role_id", session.RoleID(), "err", err)
		return false, false
	}
	offered = info.OfferedToday(session.PlayerID(), stUnionSelfNames(session), time.Now())
	var lastTime int64
	for _, rec := range info.Records {
		if rec.Time > lastTime {
			lastTime = rec.Time
		}
	}
	slog.Info("仙盟神像面板记录", "scope", scope, "role_id", session.RoleID(),
		"player_id", session.PlayerID(),
		"records", len(info.Records), "offered_today", offered,
		"last_record_at", time.Unix(lastTime, 0).Format(time.DateTime))
	return offered, true
}

// tryStUnionIncenseOnSectSession 在仙盟城镇会话上按 179/0 → 179/10 → 179/11
// 重试上香，返回 179/11 回包；offered=true 表示 179/10 面板证明今日已上香，
// 此时不再发 179/11（服务端会对重复上香静默丢包）。
//
// 179/10 只读面板在仙界跨服会话可用，但上香是仙盟场景写操作：跨服会话上服务端
// 会静默丢弃 179/11（2026-11-16 00:21 起多次实测，均在上香前刚成功拉过 179/10）。
// 仙盟城镇会话（8252）才是官方客户端打开神像面板/点击上香的会话，故作为兜底。
// 仅在上一步拿到回包后才发下一步，避免 PrevAct 链路令牌错位。
func tryStUnionIncenseOnSectSession(ctx context.Context, session *client.RoleSession,
	timeout time.Duration) (resp *protocol.Packet, offered bool) {
	sect, err := openStUnionSectSession(ctx, session)
	if err != nil {
		slog.Info("仙盟城镇会话建立失败，跳过城镇会话上香", "role_id", session.RoleID(), "err", err)
		return nil, false
	}
	if sect != session {
		defer sect.Close()
	}
	sectCaller := kit.NewCaller(ctx, sect, timeout)
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		resp, err := sectCaller.CallResync(pkt, act)
		if err != nil {
			slog.Info("仙盟城镇会话无回包", "role_id", session.RoleID(),
				"action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	if call(protocol.BuildStUnionTreeInfoPacket(), protocol.ActionIDStUnionTreeInfo) == nil {
		return nil, false
	}
	godResp := call(protocol.BuildStUnionGodInfoPacket(), protocol.ActionIDStUnionGodInfo)
	if godResp == nil {
		return nil, false
	}
	if offered, _ := logStUnionGodPanel("sect_town", session, godResp); offered {
		return nil, true
	}
	return call(protocol.BuildStUnionIncensePacket(stUnionIncenseOfferID),
		protocol.ActionIDStUnionGodIncense), false
}

// NewStUnionRitualRoutine 构造仙盟上香 + 魔神挑战任务。
func NewStUnionRitualRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	const fightTimeout = 30 * time.Second
	return scheduler.NewBaseRoutine(
		"st_union_ritual",
		"social",
		scheduler.ScheduleCron,
		// 仙盟上香/魔神在长会话后段实测会被批量静默丢弃，登录后优先执行。
		scheduler.PhasePriority(scheduler.PhaseEarlyBattle, 0),
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			cross, err := openStUnionSession(ctx, session)
			if err != nil {
				slog.Info("仙盟跨服会话建立失败，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙盟跨服会话不可用")
			}
			if cross != session {
				defer cross.Close()
			}
			crossCaller := kit.NewCaller(ctx, cross, callTimeout)
			call := func(pkt *protocol.Packet, act uint32, timeout ...time.Duration) *protocol.Packet {
				resp, err := crossCaller.CallResync(pkt, act, timeout...)
				if err != nil {
					slog.Info("仙盟协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 仙盟上香（仅铜钱档）。抓包顺序：179/0 许愿树 → 179/10 神像 → 179/11 上香。
			_ = call(protocol.BuildStUnionTreeInfoPacket(), protocol.ActionIDStUnionTreeInfo)
			offeredToday, godKnown := logStUnionGodPanel("cross",
				session, call(protocol.BuildStUnionGodInfoPacket(), protocol.ActionIDStUnionGodInfo))
			var incenseResp *protocol.Packet
			variant := "cross"
			drops := scheduler.SilentDropCountCtx(ctx, session, stUnionIncenseDropKey)
			if offeredToday {
				// 179/10 已证明今日上香成功（记录是服务端权威状态），
				// 再发 179/11 只会被静默丢弃，直接跳过并清零丢包计数。
				scheduler.ResetSilentDropsCtx(ctx, session, stUnionIncenseDropKey)
				slog.Info("仙盟神像面板显示今日已上香，跳过 179/11",
					"role_id", session.RoleID())
			} else {
				if !godKnown {
					slog.Info("仙盟神像面板不可用，仍尝试上香", "role_id", session.RoleID())
				}
				incenseResp = call(protocol.BuildStUnionIncensePacket(stUnionIncenseOfferID),
					protocol.ActionIDStUnionGodIncense)
				if incenseResp == nil && drops < maxStUnionIncenseSectTownTries {
					slog.Info("仙盟上香在跨服会话无回包，改在仙盟城镇会话重试",
						"role_id", session.RoleID(), "consecutive", drops)
					variant = "sect_town"
					var sectOffered bool
					incenseResp, sectOffered = tryStUnionIncenseOnSectSession(ctx, session, callTimeout)
					if sectOffered {
						offeredToday = true
						incenseResp = nil
					}
				}
			}
			if offeredToday && incenseResp == nil {
				// 今日已完成，无需计数；继续魔神挑战。
			} else if incenseResp == nil {
				n := scheduler.NoteSilentDropCtx(ctx, session, stUnionIncenseDropKey)
				if n == 1 || n%24 == 0 {
					slog.Warn("仙盟上香无回包，继续尝试魔神挑战", "role_id", session.RoleID(),
						"consecutive", n, "last_variant", variant)
				}
			} else {
				scheduler.ResetSilentDropsCtx(ctx, session, stUnionIncenseDropKey)
				if res, pErr := protocol.ParseStUnionIncenseResult(incenseResp.Payload); pErr != nil {
					slog.Warn("仙盟上香回包解析失败", "role_id", session.RoleID(),
						"variant", variant, "err", pErr,
						"payload_hex", hex.EncodeToString(incenseResp.Payload))
				} else if res.Result == protocol.StUnionIncenseSuccess {
					slog.Info("仙盟上香成功", "role_id", session.RoleID(), "variant", variant,
						"offer_id", stUnionIncenseOfferID, "a", res.A, "b", res.B, "c", res.C)
				} else {
					slog.Info("仙盟上香未成功", "role_id", session.RoleID(), "variant", variant,
						"result", res.Result, "result_text", protocol.StUnionIncenseResultText(res.Result))
				}
			}

			// 2. 仙盟魔神挑战（券不足时可用道源购买；不消耗元宝）。
			if infoResp := call(protocol.BuildStUnionTaskInfoPacket(),
				protocol.ActionIDStUnionTaskInfo); infoResp == nil {
				if abort := crossCaller.AbortErr(); abort != nil {
					return abort
				}
				slog.Info("仙盟魔神挑战面板无回包，稍后再试", "role_id", session.RoleID())
				return scheduler.HandleSilentDropCtx(ctx, session, "st_union_ritual", 3,
					time.Hour, "仙盟魔神挑战面板无回包")
			} else if info, iErr := protocol.ParseStUnionTaskInfo(infoResp.Payload); iErr == nil {
				slog.Info("仙盟魔神挑战面板", "role_id", session.RoleID(),
					"a", info.A, "b", info.B, "c", info.C, "d", info.D)
			}
			fights := 0
			buys := 0
			boughtThisRun := false
			today := time.Now().Format("2006-01-02")
			buyIneffectiveToday := func() bool { return stUnionTicketIneffective(today) }
			for fights < maxStUnionGodFights && ctx.Err() == nil {
				fightResp := call(protocol.BuildStUnionTaskFightPacket(),
					protocol.ActionIDStUnionTaskFight, fightTimeout)
				if fightResp == nil {
					if abort := crossCaller.AbortErr(); abort != nil {
						return abort
					}
					return scheduler.HandleSilentDropCtx(ctx, session, "st_union_ritual", 3,
						time.Hour, "仙盟魔神挑战无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "st_union_ritual")
				code, fErr := protocol.ParseStUnionResult(fightResp.Payload)
				if fErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "仙盟魔神挑战回包解析失败")
				}
				if code == protocol.StUnionTaskNoTicket && buys < maxStUnionTicketBuys &&
					!stUnionTicketBuyDisabled.Load() && !buyIneffectiveToday() {
					before := cross.GetPlayerState().Ingots
					buyResp := call(protocol.BuildStUnionTaskBuyTicketPacket(),
						protocol.ActionIDStUnionTaskBuyTicket)
					after := cross.GetPlayerState().Ingots
					if buyResp == nil {
						slog.Info("仙盟购买挑战券无回包", "role_id", session.RoleID())
						return nil
					}
					buyCode, bErr := protocol.ParseStUnionResult(buyResp.Payload)
					if bErr != nil {
						return nil
					}
					slog.Info("仙盟购买挑战券", "role_id", session.RoleID(),
						"result", buyCode, "result_text", protocol.StUnionTaskResultText(buyCode),
						"ingots_before", before, "ingots_after", after)
					if buyCode != protocol.StUnionTaskSuccess {
						return nil
					}
					if before > 0 && after > 0 && after < before {
						stUnionTicketBuyDisabled.Store(true)
						slog.Error("仙盟挑战券购买消耗了元宝，已停止自动购买",
							"role_id", session.RoleID(), "ingots_before", before, "ingots_after", after)
						return nil
					}
					buys++
					boughtThisRun = true
					continue
				}
				if code != protocol.StUnionTaskSuccess {
					slog.Info("仙盟魔神挑战结束", "role_id", session.RoleID(),
						"fights", fights, "result", code,
						"result_text", protocol.StUnionTaskResultText(code))
					if boughtThisRun && code == protocol.StUnionTaskTimesUsed {
						// 刚用道源买了挑战券仍然次数已用完：当天不再购买。
						markStUnionTicketIneffective(today)
						slog.Info("购买挑战券后仍次数已用完，今日不再自动购买",
							"role_id", session.RoleID())
					}
					return nil
				}
				fights++
				slog.Info("仙盟魔神挑战成功", "role_id", session.RoleID(), "fights", fights)
			}
			slog.Info("仙盟魔神挑战本轮完成", "role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}
