// Package farming 提供药园、取经护送/劫镖、仙界矿山、粗铁淬炼池与仙葫芦等资源养成自动化。
//
// 本文件实现仙界/圣域取经护送与劫镖 (Mod_StTakeBible_Base / Mod_SaTakeBible_Base)。
package farming

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// crossTakeBibleCallFunc 跨服取经协议调用函数签名。
type crossTakeBibleCallFunc func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error)

// moduleSaTakeBibleBase 圣域取经模块基址 (Mod_SaTakeBible_Base, Module 395 / 0x018B)。
//
// 圣域取经与跨服取经 (Mod_StTakeBible_Base, Module 114 / 0x0072) 的报文结构完全一致，
// 仅模块号不同；因此可复用同一套请求构造与回包解析，只把 ActionID 的模块段替换为 0x018B。
const moduleSaTakeBibleBase uint32 = 0x018B0000

// saTakeBibleAction 将跨服取经 ActionID 映射为圣域取经 ActionID (低 16 位动作号不变)。
func saTakeBibleAction(stAction uint32) uint32 {
	return moduleSaTakeBibleBase | (stAction & 0xFFFF)
}

// normalizeSaTakeBibleMsg 将圣域取经结果码映射为跨服取经结果码。
//
// 两套协议的报文结构一致，但 Mod_SaTakeBible_Base 的业务结果码整体后移：
// 例如 SUCCESS 在圣域是 11、仙界是 9；FULL_TAKE_BIBLE_TIMES 是 20/18。
// 复用仙界解析器后，需要在业务判断前统一归一化，避免把成功误判为失败。
func normalizeSaTakeBibleMsg(msg uint8) uint8 {
	switch msg {
	case 11:
		return 9 // SUCCESS
	case 12:
		return 10 // INVALID_PLAYER_ID
	case 13:
		return 11 // FULL_PROTECT_TIMES
	case 14:
		return 12 // NOT_ONLINE
	case 15:
		return 13 // TAKE_BIBLE_STATUS
	case 16:
		return 14 // CANCEL_APPLY
	case 17:
		return 15 // PROTECT_STATUS
	case 18:
		return 16 // NOT_ENOUGH_INGOT
	case 19:
		return 17 // REQUEST_VIP
	case 20:
		return 18 // FULL_TAKE_BIBLE_TIMES
	case 21:
		return 19 // REROB
	case 22:
		return 20 // PROTECT_TIME
	case 23:
		return 21 // FULL_ROB_TIMES
	case 24:
		return 22 // HAVE_CD_TIME
	case 25:
		return 23 // BE_ROB_ALL
	case 26:
		return 24 // INVALID_SEQUENCE_ID
	case 40:
		return 38 // FAILED
	default:
		return msg
	}
}

const (
	// crossTakeBibleRobMaxProbes 单轮打劫最多探测的目标数量，避免遍历整条取经路。
	crossTakeBibleRobMaxProbes = 8
	// crossTakeBibleRobMaxAttempts 单轮打劫最多尝试次数 (与游戏每日上限 5 次保持一致)。
	crossTakeBibleRobMaxAttempts = 5
	// crossTakeBibleRobProbeHardLimit 探测数量硬上限，防止异常配置造成大量请求。
	crossTakeBibleRobProbeHardLimit = 50
	// crossTakeBibleRobInterval 连续打劫之间的拟人化间隔。
	crossTakeBibleRobInterval = 500 * time.Millisecond
	// crossTakeBibleRobTimeout 单次打劫等待结算回包的超时。
	//
	// rob 回包携带完整战报 (实机约 8KB，随目标战力与战斗回合数增长)，
	// 跨服节点需要先完成战斗计算再下发；4s 预算曾把高战力/拥塞场景误判为无应答，
	// 因此与仙界竞技场 challenge 对齐，给足战报结算时间。
	crossTakeBibleRobTimeout = 15 * time.Second
	// crossTakeBibleRobCDTimeout 打劫冷却查询超时 (也用于超时后的受理复核)。
	crossTakeBibleRobCDTimeout = 3 * time.Second
)

// SaintAreaFallbackMode 圣域取经通道不可用时的回退策略。
type SaintAreaFallbackMode int

const (
	// SaintAreaAuto 自动判定 (默认):
	//   - 圣域功能未开放的角色 (例如尚未解锁圣域的新区角色) 说明取经仍在仙界/主服，
	//     圣域通道不可用时回退仙界跨服/主服链路；
	//   - 圣域功能已开放的角色说明取经已迁入圣域，圣域关闭/维护时跳过本轮，
	//     不再向可能已下线的旧链路发送请求。
	SaintAreaAuto SaintAreaFallbackMode = iota
	// SaintAreaForceOnly 固定圣域：圣域不可用时一律跳过，绝不回退。
	SaintAreaForceOnly
	// SaintAreaAllowFallback 总是允许回退：圣域不可用时回退仙界跨服/主服链路。
	SaintAreaAllowFallback
)

// PilgrimageConfig 西天取经护送与打劫自动化策略配置。
// pilgrimageRobLevelGap 打劫目标的等级上限 = 自身等级 + 该值。
// 跨服对手等级高太多时战斗必败：失败虽不扣每日次数，但会触发 10 分钟冷却，
// 并让例程反复空转（2026-11-14 s1：95 级反复挑战 188/214 级目标）。
const pilgrimageRobLevelGap int16 = 10

// pilgrimageRobMaxLosses 单轮连续失败上限，达到后延长退避时间。
const pilgrimageRobMaxLosses = 2

// pilgrimageRobLossStreak 记录跨轮累计的连续打劫失败次数。
// 同等级目标仍可能战败 (2026-11-14 s1 95 级 vs 93 级战败)；每 10 分钟服务端
// 冷却结束后立刻再打只会反复失败，累计 3 次后延长退避到 30 分钟。
var pilgrimageRobLossStreak sync.Map // *client.RoleSession -> int

// pilgrimageRobStreakLimit 连续打劫失败上限：达到后直接挂起到次日重置。
//
// 2026-11-17 s0 实测：全天每个 30 分钟窗口都去打等级 300 的对手并战败，
// 每次都要建立跨服会话、查询路况与目标详情，整晚空转。失败不消耗次数，
// 但战力差距当天不会改变，因此 N 次（6 次 ≈ 3 小时）后挂起到次日。
const pilgrimageRobStreakLimit = 6

// pilgrimageRobBackoff 根据连续失败次数计算退避时长。
//
// 返回 (wait, capped)：capped=true 表示已达上限、应挂起到次日重置；
// 否则 wait 为延长后的退避（可能等于入参 retryAfter）。
func pilgrimageRobBackoff(streak int, retryAfter time.Duration, now time.Time) (time.Duration, bool) {
	if streak >= pilgrimageRobStreakLimit {
		wait := scheduler.NextDailyReset(now).Sub(now)
		if wait < 30*time.Minute {
			wait = 30 * time.Minute
		}
		return wait, true
	}
	if streak >= 3 && retryAfter < 30*time.Minute {
		return 30 * time.Minute, false
	}
	return retryAfter, false
}

type PilgrimageConfig struct {
	// AutoRob 是否在启程护送后自动打劫取经路上其他玩家队伍。
	AutoRob bool
	// MaxRobAttempts 单轮最多尝试打劫次数 (上限 5)。
	MaxRobAttempts int
	// MaxProbeTargets 单轮最多探测的目标队伍数量 (上限 50)。
	MaxProbeTargets int
	// SaintAreaFallback 圣域取经通道不可用时的回退策略 (默认 SaintAreaAuto)。
	SaintAreaFallback SaintAreaFallbackMode
}

// DefaultPilgrimageConfig 返回默认取经策略：自动打劫、最多尝试 5 次、探测 8 支队伍。
func DefaultPilgrimageConfig() PilgrimageConfig {
	return PilgrimageConfig{
		AutoRob:         true,
		MaxRobAttempts:  crossTakeBibleRobMaxAttempts,
		MaxProbeTargets: crossTakeBibleRobMaxProbes,
	}
}

// normalizePilgrimageConfig 将取经策略参数收敛到安全范围。
func normalizePilgrimageConfig(cfg PilgrimageConfig) PilgrimageConfig {
	if cfg.MaxRobAttempts <= 0 {
		cfg.MaxRobAttempts = crossTakeBibleRobMaxAttempts
	}
	if cfg.MaxRobAttempts > crossTakeBibleRobMaxAttempts {
		cfg.MaxRobAttempts = crossTakeBibleRobMaxAttempts
	}
	if cfg.MaxProbeTargets <= 0 {
		cfg.MaxProbeTargets = crossTakeBibleRobMaxProbes
	}
	if cfg.MaxProbeTargets > crossTakeBibleRobProbeHardLimit {
		cfg.MaxProbeTargets = crossTakeBibleRobProbeHardLimit
	}
	return cfg
}

// String 返回圣域回退策略的可读名称，便于日志排查。
func (m SaintAreaFallbackMode) String() string {
	switch m {
	case SaintAreaForceOnly:
		return "force_saint_area"
	case SaintAreaAllowFallback:
		return "allow_fallback"
	default:
		return "auto"
	}
}

// saintAreaFallbackAllowed 报告圣域通道不可用时是否应回退仙界跨服/主服取经。
func (cfg PilgrimageConfig) saintAreaFallbackAllowed(ctx context.Context, session *client.RoleSession) bool {
	switch cfg.SaintAreaFallback {
	case SaintAreaForceOnly:
		return false
	case SaintAreaAllowFallback:
		return true
	}

	// 自动模式: 以圣域功能是否开放判定取经链路归属。
	// 已开放 = 该角色取经已迁入圣域，圣域关闭/维护时等待下一轮，不回退旧链路；
	// 未开放 = 取经仍在仙界跨服或主服，允许回退。
	open, err := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea)
	if err != nil {
		// 无法确认功能开放状态时保守跳过本轮，避免向可能已被服务端静默丢弃的
		// 旧链路发送请求；下一轮调度会重新探测。
		slog.Warn("取经: 圣域功能开放状态查询失败, 跳过本轮",
			"role_id", session.RoleID(), "error", err)
		return false
	}
	if !open {
		slog.Info("取经: 圣域功能未开放, 回退仙界跨服/主服取经",
			"role_id", session.RoleID(), "function_id", protocol.FunctionIDSaintArea)
		return true
	}
	slog.Info("取经: 圣域功能已开放但当前未开启, 跳过本轮",
		"role_id", session.RoleID(), "function_id", protocol.FunctionIDSaintArea)
	return false
}

// handleCrossPilgrimage 在官方仙界跨服连接 (Mod 114) 上执行一次取经巡检、启程与打劫。
//
// 官方客户端流程为：Mod 96 取跨服凭据 → Mod 94 登录跨服节点 → Mod 95 进入仙界城镇
// → Mod 114 get_take_bible_info → (可选 refresh) → start_take_bible
// → open_take_bible → get_rob_take_bible_cd_time → rob。
// 返回 true 表示本轮已处理完毕（跨服链路完成，或按圣域回退策略主动跳过）；
// 返回 false 表示当前环境不支持仙界跨服连接（凭据缺失或登录失败），
// 调用方应回退到主服传统取经协议。
func handleCrossPilgrimage(ctx context.Context, session *client.RoleSession, cfg PilgrimageConfig) (handled bool, retryAfter time.Duration) {
	cfg = normalizePilgrimageConfig(cfg)
	// 一次巡检最多包含护送、刷新、路况查询、目标探测与多次打劫，放宽整体超时。
	// rob 回包需等待跨服节点完成战斗计算 (单次预算 15s)，整体预算相应放宽。
	crossCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var (
		cross  *client.RoleSession
		isSa   bool
		origin string
	)

	// 1. 优先走圣域取经 (Mod_SaTakeBible_Base, Module 395)。
	// 当前区服已将西天取经迁移至圣域，跨服取经入口可能关闭。
	// 注意: 圣域状态查询线上实测 ~0.6s, 原 1 秒超时余量过小, 高负载时会把
	// 已开启的圣域误判为不可用, 从而整轮跳过取经/打劫 (自动/固定圣域模式均会跳过)。
	// 这里放宽到 3 秒 (mock 服务器会立即返回关闭状态, 不影响单测)。
	statusCtx, statusCancel := context.WithTimeout(crossCtx, 3*time.Second)
	statusResp, statusErr := session.Call(statusCtx, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus)
	statusCancel()
	if statusErr == nil && statusResp != nil {
		if st, pErr := protocol.ParseSaintAreaStatusResult(statusResp.Payload); pErr == nil && st.Status == protocol.SaintAreaStatusOpen {
			// 注意: 不能给 OpenSaintAreaSession 传入带 cancel 的子 context，
			// 该 context 会作为圣域会话的内部生命周期 context，提前 cancel 会立刻关闭连接。
			sa, openErr := session.OpenSaintAreaSession(crossCtx)
			if openErr == nil {
				cross = sa
				isSa = true
				origin = "圣域"
				defer cross.Close()
			} else if errors.Is(openErr, client.ErrSaintAreaCredentialMissing) {
				// 圣域未对当前角色开放（如 s1 区服）：属永久能力限制，
				// 回退跨服取经即可，不再每轮打 WARN。
				slog.Info("圣域功能未对当前角色开放，回退跨服取经",
					"role_id", session.RoleID())
			} else {
				slog.Warn("圣域会话建立失败，尝试跨服取经入口",
					"role_id", session.RoleID(), "error", openErr)
			}
		}
	}

	// 2. 圣域通道不可用：按回退策略决定跳过本轮还是回退仙界跨服/主服。
	if cross == nil && !cfg.saintAreaFallbackAllowed(crossCtx, session) {
		slog.Info("取经圣域通道不可用，按策略跳过本轮且不回退仙界",
			"role_id", session.RoleID(), "fallback_mode", cfg.SaintAreaFallback)
		return true, 0
	}

	// 3. 回退仙界跨服取经 (Mod 114)，兼容仍开放跨服取经的区服。
	if cross == nil {
		credCtx, credCancel := context.WithTimeout(crossCtx, 2*time.Second)
		cred, err := session.FetchCrossServerCredentials(credCtx)
		credCancel()
		if err != nil {
			slog.Debug("仙界跨服取经不可用，回退传统取经协议", "role_id", session.RoleID(), "error", err)
			return false, 0
		}
		st, err := session.OpenCrossServerSessionWithInfo(crossCtx, cred)
		if err != nil {
			slog.Warn("仙界跨服会话建立失败，回退传统取经协议", "role_id", session.RoleID(), "error", err)
			return false, 0
		}
		cross = st
		origin = "仙界"
		defer cross.Close()
	}

	crossCall := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
		act := expectedAct
		if isSa {
			act = saTakeBibleAction(expectedAct)
			pkt = protocol.NewPacket(act, pkt.Payload)
		}
		cCtx, cCancel := context.WithTimeout(crossCtx, d)
		defer cCancel()
		return cross.Call(cCtx, pkt, act)
	}

	slog.Info("取经链路已建立", "role_id", session.RoleID(), "channel", origin)
	normalizeMsg := func(msg uint8) uint8 { return msg }
	if isSa {
		normalizeMsg = normalizeSaTakeBibleMsg
	}
	crossPilgrimageEscort(ctx, session, crossCall, normalizeMsg)
	if cfg.AutoRob {
		// 路况队伍与战报中的玩家 ID 均为跨服 ID 空间，必须使用跨服会话的身份
		// 判断“自己发起的队伍”与打劫胜负，不能混用主服 PlayerID。
		selfID := cross.PlayerID()
		if selfID <= 0 {
			selfID = session.PlayerID()
		}
		if isSa {
			retryAfter = crossPilgrimageRob(ctx, session, selfID, crossCall, normalizeMsg, cfg, protocol.ParseSaTakeBibleRoadResult)
		} else {
			retryAfter = crossPilgrimageRob(ctx, session, selfID, crossCall, normalizeMsg, cfg)
		}
	} else {
		slog.Debug("取经自动打劫已关闭，仅执行护送巡检", "role_id", session.RoleID())
	}
	return true, retryAfter
}

// crossPilgrimageEscort 在圣域/仙界取经连接上巡检使者并启程护送。
//
// 线上实测圣域取经有 3 个独立队列，可一次连续发起 3 支护送队伍；
// 循环重新拉取准备详情，按剩余总次数最多启程 3 次。
func crossPilgrimageEscort(ctx context.Context, session *client.RoleSession, crossCall crossTakeBibleCallFunc, normalize func(uint8) uint8) {
	const maxStartsPerRun = 3
	for started := 0; started < maxStartsPerRun; started++ {
		infoResp, err := crossCall(protocol.BuildStTakeBibleCrossGetInfoPacket(), protocol.ActionIDStTakeBibleCrossGetInfo, 3*time.Second)
		if err != nil {
			slog.Info("跨服取经详情无应答", "role_id", session.RoleID(), "error", err)
			return
		}
		info, err := protocol.ParseStTakeBibleCrossInfoResult(infoResp.Payload)
		if err != nil {
			slog.Warn("解析跨服取经详情失败", "role_id", session.RoleID(), "error", err)
			return
		}
		slog.Info("跨服取经准备详情",
			"role_id", session.RoleID(),
			"started", started,
			"take_times", info.TakeBibleTimes,
			"total_times", info.TotalTakeBibleTimes,
			"refresh_price", info.RefreshPrice,
			"status", info.TakeBibleStatus,
			"protection", protocol.StTakeBibleCrossProtectionName(info.CanProtection),
			"protect_player", info.ProtectPlayer,
			"protection_options", len(info.ProtectionList),
			"protect_players", len(info.ProtectPlayerList),
		)

		if info.TotalTakeBibleTimes <= 0 || info.TakeBibleTimes >= uint8(info.TotalTakeBibleTimes) {
			slog.Info("跨服取经今日护送次数已用满",
				"role_id", session.RoleID(),
				"started", started,
				"take_times", info.TakeBibleTimes,
				"total_times", info.TotalTakeBibleTimes,
			)
			return
		}

		// 使者非最高品质时尝试刷新一次，仅在本次刷新免费时执行。
		protection := info.CanProtection
		if protection < protocol.StTakeBibleCrossProtectionTangSeng {
			if info.RefreshPrice > 0 {
				slog.Info("取经使者刷新需要元宝，跳过刷新",
					"role_id", session.RoleID(), "refresh_price", info.RefreshPrice)
			} else {
				refreshResp, rErr := crossCall(protocol.BuildStTakeBibleCrossRefreshPacket(), protocol.ActionIDStTakeBibleCrossRefresh, 2*time.Second)
				if rErr == nil && refreshResp != nil {
					msg, newProtection, nextPrice, pErr := protocol.ParseStTakeBibleCrossRefreshResult(refreshResp.Payload)
					if pErr == nil {
						msg = normalize(msg)
					}
					if pErr == nil && msg == protocol.StTakeBibleCrossMsgSuccess {
						slog.Info("跨服取经使者刷新成功",
							"role_id", session.RoleID(),
							"old_protection", protocol.StTakeBibleCrossProtectionName(protection),
							"new_protection", protocol.StTakeBibleCrossProtectionName(newProtection),
							"next_price", nextPrice,
						)
						protection = newProtection
					}
				}
			}
		}

		startResp, err := crossCall(protocol.BuildStTakeBibleCrossStartPacket(), protocol.ActionIDStTakeBibleCrossStart, 3*time.Second)
		if err != nil {
			slog.Info("跨服取经启程无应答", "role_id", session.RoleID(), "error", err)
			return
		}
		startMsg, itemID, err := protocol.ParseStTakeBibleCrossStartResult(startResp.Payload)
		if err != nil {
			slog.Warn("解析跨服取经启程回包失败", "role_id", session.RoleID(), "error", err)
			return
		}
		startMsg = normalize(startMsg)

		switch startMsg {
		case protocol.StTakeBibleCrossMsgSuccess:
			slog.Info("跨服取经队伍已顺利启程护送",
				"role_id", session.RoleID(),
				"queue", started+1,
				"protection", protocol.StTakeBibleCrossProtectionName(protection),
				"item_id", itemID,
			)
			// 继续下一队列，重新拉取详情计算剩余次数。
		case protocol.StTakeBibleCrossMsgTakeBibleStatus:
			slog.Info("跨服取经队伍正在护送途中，当前无可启程队列",
				"role_id", session.RoleID(), "started", started)
			return
		case protocol.StTakeBibleCrossMsgFullTakeBibleTimes:
			slog.Info("跨服取经今日护送次数已用满", "role_id", session.RoleID(), "total_times", info.TotalTakeBibleTimes)
			return
		case protocol.StTakeBibleCrossMsgProtectTime:
			slog.Info("跨服取经当前处于护送保护时间", "role_id", session.RoleID())
			return
		case protocol.StTakeBibleCrossMsgNotEnoughIngot:
			slog.Info("跨服取经刷新元宝不足", "role_id", session.RoleID())
			return
		default:
			slog.Info("跨服取经完成巡检", "role_id", session.RoleID(), "status_code", startMsg)
			return
		}
	}
}

// crossPilgrimageRob 在跨服取经路上执行一轮打劫。
//
// 官方客户端流程：open_take_bible 取得路况队伍列表 → get_rob_take_bible_cd_time
// 确认冷却 → get_player_take_bible 校验目标是否可劫 → rob(player_id, sequence_id)。
// 单次调度的探测与尝试上限由 PilgrimageConfig 控制 (默认 8 支/5 次)；
// 每次打劫后重新查询冷却与剩余次数，冷却中立即收手。
// 本流程仅使用免费次数，不消耗元宝清冷却或购买额外打劫次数。
//
// selfID 为跨服会话玩家 ID：路况队伍、战报胜负位与 stageServer 均使用跨服 ID 空间。
func crossPilgrimageRob(ctx context.Context, session *client.RoleSession, selfID int32, crossCall crossTakeBibleCallFunc, normalize func(uint8) uint8, cfg PilgrimageConfig, roadParsers ...func([]byte) (*protocol.StTakeBibleCrossRoadInfo, error)) (retryAfter time.Duration) {
	parseRoad := protocol.ParseStTakeBibleCrossRoadResult
	if len(roadParsers) > 0 && roadParsers[0] != nil {
		parseRoad = roadParsers[0]
	}
	openResp, err := crossCall(protocol.BuildStTakeBibleCrossOpenPacket(), protocol.ActionIDStTakeBibleCrossOpen, 3*time.Second)
	if err != nil {
		slog.Info("跨服取经路况查询无应答", "role_id", session.RoleID(), "error", err)
		return
	}
	road, err := parseRoad(openResp.Payload)
	if err != nil {
		slog.Warn("解析跨服取经路况失败", "role_id", session.RoleID(), "error", err)
		return
	}
	defer func() {
		if _, cErr := crossCall(protocol.BuildStTakeBibleCrossClosePacket(), protocol.ActionIDStTakeBibleCrossClose, time.Second); cErr != nil {
			slog.Debug("关闭跨服取经路况面板失败", "role_id", session.RoleID(), "error", cErr)
		}
	}()

	slog.Info("跨服取经路况同步完成",
		"role_id", session.RoleID(),
		"road_teams", len(road.Teams),
		"can_rob_times", road.CanRobTimes,
		"last_robs", len(road.LastRobs),
	)
	if road.CanRobTimes <= 0 {
		slog.Info("跨服取经今日打劫次数已用尽", "role_id", session.RoleID())
		return
	}

	cd, ok := crossTakeBibleRobCD(session, crossCall)
	if !ok {
		return
	}
	if cd.CDSeconds > 0 {
		slog.Info("跨服取经当前处于打劫冷却中", "role_id", session.RoleID(), "cooldown_sec", cd.CDSeconds)
		retryAfter = time.Duration(cd.CDSeconds+2) * time.Second
		return
	}

	candidates := make([]protocol.StTakeBibleCrossRoadTeam, 0, len(road.Teams))
	for _, team := range road.Teams {
		// 跳过自己发起的队伍，以及自己正在保护的队伍：官方客户端同样禁止打劫。
		if team.PlayerID <= 0 || team.PlayerID == selfID || team.ProtectPlayerID == selfID {
			continue
		}
		candidates = append(candidates, team)
	}
	if len(candidates) == 0 {
		slog.Info("跨服取经路上暂无可打劫队伍", "role_id", session.RoleID())
		return
	}

	// 优先探测高品质使者队伍：收益更高，同时把请求量控制在固定上限内。
	// 注意这里只决定“探测谁”，最终出手顺序由目标详情中的等级决定 (软柿子优先)：
	// 打劫失败的战斗不扣除每日次数，因此先打等级最低的目标才能实际消耗次数。
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Protection != candidates[j].Protection {
			return candidates[i].Protection > candidates[j].Protection
		}
		if candidates[i].Distance != candidates[j].Distance {
			return candidates[i].Distance < candidates[j].Distance
		}
		return candidates[i].PlayerID < candidates[j].PlayerID
	})
	probeCount := len(candidates)
	if probeCount > cfg.MaxProbeTargets {
		probeCount = cfg.MaxProbeTargets
	}

	type robTarget struct {
		team  protocol.StTakeBibleCrossRoadTeam
		info  protocol.StTakeBibleCrossPlayerInfo
		score int64
	}
	level := int16(session.GetPlayerState().Level)
	targets := make([]robTarget, 0, probeCount)
	probeFailed := 0
	for _, team := range candidates[:probeCount] {
		infoResp, cErr := crossCall(
			protocol.BuildStTakeBibleCrossGetPlayerPacket(team.PlayerID, level, 0, team.SequenceID),
			protocol.ActionIDStTakeBibleCrossGetPlayer,
			2*time.Second,
		)
		if cErr != nil {
			probeFailed++
			slog.Debug("跨服取经目标详情无应答", "role_id", session.RoleID(), "player_id", team.PlayerID, "error", cErr)
			continue
		}
		info, pErr := protocol.ParseStTakeBibleCrossPlayerInfoResult(infoResp.Payload)
		if pErr != nil {
			slog.Debug("解析跨服取经目标详情失败", "role_id", session.RoleID(), "player_id", team.PlayerID, "error", pErr)
			continue
		}
		// 已打劫过、目标已被抢光或处于保护时间时跳过。
		if info.IsRobbed != 0 ||
			(info.TotalCanRobTimes > 0 && info.BeRobTimes >= info.TotalCanRobTimes) ||
			info.ProtectTime > 0 {
			continue
		}
		if info.RobCoin <= 0 && info.RobFame <= 0 && info.RobStExp <= 0 {
			continue
		}
		if level > 0 && info.Level > level+pilgrimageRobLevelGap {
			slog.Debug("跨服取经目标等级过高，跳过",
				"role_id", session.RoleID(), "player_id", team.PlayerID,
				"target_level", info.Level, "self_level", level)
			continue
		}
		score := int64(info.RobCoin)*1000 + int64(info.RobFame)*10 + int64(info.RobStExp)
		targets = append(targets, robTarget{team: team, info: *info, score: score})
	}
	// 软柿子优先：服务端只按战斗胜负扣减每日打劫次数，攻击打不过的高战目标
	// 会白白浪费一次 10 分钟冷却而不消耗次数。等级是 get_player_take_bible
	// 下发的唯一实力代理指标 (与仙界竞技场软柿子算法一致)，等级越低越先打；
	// 等级相同时再按预期收益从高到低选择。等级未知 (0) 的目标排到最后。
	sort.SliceStable(targets, func(i, j int) bool {
		li, lj := targets[i].info.Level, targets[j].info.Level
		if (li > 0) != (lj > 0) {
			return li > 0
		}
		if li != lj {
			return li < lj
		}
		if targets[i].score != targets[j].score {
			return targets[i].score > targets[j].score
		}
		return targets[i].team.PlayerID < targets[j].team.PlayerID
	})
	// 探测超时会被静默跳过；聚合到一条 Info 日志，便于区分“路上没人可劫”
	// 与“跨服节点繁忙/丢包”，避免线上只能看到打劫无应答而缺少上下文。
	if probeFailed > 0 {
		slog.Info("跨服取经目标探测存在无应答",
			"role_id", session.RoleID(),
			"probes", probeCount,
			"failed", probeFailed,
			"robbable", len(targets),
		)
	}

	attempts := int(road.CanRobTimes)
	if attempts > cfg.MaxRobAttempts {
		attempts = cfg.MaxRobAttempts
	}
	if attempts > len(targets) {
		attempts = len(targets)
	}
	if attempts <= 0 {
		slog.Info("跨服取经路上暂无可打劫目标", "role_id", session.RoleID(), "candidates", len(candidates))
		return
	}

	var (
		robbed     int
		losses     int
		totalCoin  int64
		totalFame  int64
		totalStExp int64
	)
	for i := 0; i < attempts; i++ {
		target := targets[i]
		// 官方客户端使用 get_player_take_bible 回包的 sequence_id 发起打劫；
		// 路况包中的 sequence_id 仅用于探测。两者不一致时以目标详情为准。
		sequenceID := target.info.SequenceID
		if sequenceID == 0 {
			sequenceID = target.team.SequenceID
		}
		slog.Info("跨服取经发起打劫",
			"role_id", session.RoleID(),
			"target_player_id", target.team.PlayerID,
			"target_nickname", target.info.NickName,
			"target_level", target.info.Level,
			"protection", protocol.StTakeBibleCrossProtectionName(target.team.Protection),
			"protect_player_id", target.team.ProtectPlayerID,
			"is_deeds_so", target.team.IsDeedsSo,
			"distance", target.team.Distance,
			"sequence_id", sequenceID,
			"be_rob_times", target.info.BeRobTimes,
			"total_can_rob_times", target.info.TotalCanRobTimes,
			"expect_coin", target.info.RobCoin,
			"expect_fame", target.info.RobFame,
		)

		robResp, cErr := crossCall(
			protocol.BuildStTakeBibleCrossRobPacket(target.team.PlayerID, sequenceID),
			protocol.ActionIDStTakeBibleCrossRob,
			crossTakeBibleRobTimeout,
		)
		if cErr != nil {
			// 回包超时不等于打劫未被受理：rob 回包要先由服务端完成战斗计算，
			// 高战力目标或跨服节点拥塞时可能超过任何固定预算。
			// 立即补查冷却：已进入冷却说明服务端已结算，绝不能重复发起；
			// 冷却仍为 0 才说明请求确实未被受理，可安全尝试下一个目标。
			accepted, known := crossTakeBibleRobReconcile(session, crossCall)
			if !known {
				slog.Info("跨服取经打劫无应答且冷却复核失败，保守结束本轮打劫",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"error", cErr,
				)
				break
			}
			if accepted {
				slog.Info("跨服取经打劫无应答但服务端已进入冷却，按已受理结束本轮打劫",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"error", cErr,
				)
				break
			}
			slog.Info("跨服取经打劫无应答且服务端未进入冷却，跳过该目标",
				"role_id", session.RoleID(),
				"target_player_id", target.team.PlayerID,
				"error", cErr,
			)
			if i+1 < attempts {
				select {
				case <-ctx.Done():
					return
				case <-time.After(crossTakeBibleRobInterval):
				}
			}
			continue
		}
		result, pErr := protocol.ParseStTakeBibleCrossRobResult(robResp.Payload)
		if pErr == nil {
			result.Msg = normalize(result.Msg)
		}
		won := false
		switch {
		case pErr != nil:
			slog.Warn("解析跨服取经打劫回包失败", "role_id", session.RoleID(), "target_player_id", target.team.PlayerID, "error", pErr)
		case result.Msg == protocol.StTakeBibleCrossMsgSuccess:
			// msg=SUCCESS 只代表战斗已完成结算，不代表打劫获胜。服务端只有在
			// 战胜目标时才会扣减每日打劫次数并发放奖励；战败时 rob_coin/fame/exp
			// 均为 0，次数保持不变。必须依据战报胜负位如实记录，不能记成成功。
			won = result.IsWin(selfID)
			if !result.WarParsed {
				// 兼容不回传战报的节点：以是否发放奖励粗判胜负。
				won = result.RobCoin > 0 || result.RobFame > 0 || result.RobStExp > 0
			}
			if won {
				robbed++
				losses = 0
				pilgrimageRobLossStreak.Store(session, 0)
				totalCoin += int64(result.RobCoin)
				totalFame += int64(result.RobFame)
				totalStExp += int64(result.RobStExp)
				session.AddRewards(int64(result.RobStExp), int64(result.RobCoin))
				slog.Info("跨服取经打劫获胜",
					"role_id", session.RoleID(),
					"target_player_id", result.TargetPlayerID,
					"target_nickname", result.TargetNickName,
					"target_level", target.info.Level,
					"rob_coin", result.RobCoin,
					"rob_fame", result.RobFame,
					"rob_st_exp", result.RobStExp,
					"win_player_id", result.WinPlayerID,
					"deploy_type", result.DeployType,
					"war_report_id", result.WarReportID,
				)
			} else {
				slog.Warn("跨服取经打劫战斗失败，未消耗每日打劫次数",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"target_nickname", target.info.NickName,
					"target_level", target.info.Level,
					"self_level", level,
					"win_player_id", result.WinPlayerID,
					"war_parsed", result.WarParsed,
					"war_report_id", result.WarReportID,
				)
				losses++
				streak := 1
				if prev, ok := pilgrimageRobLossStreak.Load(session); ok {
					if n, _ := prev.(int); n > 0 {
						streak = n + 1
					}
				}
				pilgrimageRobLossStreak.Store(session, streak)
				if wait, capped := pilgrimageRobBackoff(streak, retryAfter, time.Now()); capped {
					// 连续失败达到上限：战力差距当天不会改变，挂起到次日重置，
					// 避免整晚每 30 分钟重复建立跨服会话并战败。
					slog.Info("跨服取经连续打劫失败达上限，挂起至次日重置",
						"role_id", session.RoleID(), "loss_streak", streak,
						"retry_after", wait.Round(time.Minute))
					return wait
				} else if wait > retryAfter {
					slog.Info("跨服取经累计打劫失败，延长退避 30 分钟",
						"role_id", session.RoleID(), "loss_streak", streak)
					// 立即返回，避免随后查询服务端 10 分钟冷却时把退避覆盖回 10 分钟。
					return wait
				}
				if losses >= pilgrimageRobMaxLosses {
					slog.Info("跨服取经连续打劫失败，延长退避",
						"role_id", session.RoleID(), "losses", losses)
					if retryAfter < 30*time.Minute {
						retryAfter = 30 * time.Minute
					}
					return retryAfter
				}
			}
		default:
			slog.Info("跨服取经打劫未成功",
				"role_id", session.RoleID(),
				"target_player_id", target.team.PlayerID,
				"msg", result.Msg,
				"reason", protocol.StTakeBibleCrossRobMsgName(result.Msg),
			)
		}

		// 官方客户端在 rob 回包后无论成功还是失败都会重新查询冷却。
		// 打劫失败同样可能触发冷却，必须立即收手，避免继续发送无效请求。
		cd, ok = crossTakeBibleRobCD(session, crossCall)
		if !ok {
			break
		}
		if cd.CDSeconds > 0 {
			slog.Info("跨服取经打劫进入冷却，结束本轮打劫", "role_id", session.RoleID(), "cooldown_sec", cd.CDSeconds)
			retryAfter = time.Duration(cd.CDSeconds+2) * time.Second
			break
		}
		if pErr != nil {
			// 回包无法解析时保守结束本轮，避免在打劫状态未知时继续操作。
			break
		}
		if result.Msg == protocol.StTakeBibleCrossMsgFullRobTimes ||
			result.Msg == protocol.StTakeBibleCrossMsgHaveCDTime {
			break
		}
		// 官方客户端每次 rob 回包后都会同步 get_take_bible_updata_data。
		// 打劫失败不会扣减每日次数，以服务端下发的剩余次数为准决定是否继续，
		// 避免仅凭路况快照把“未消耗次数”误当成“还有次数”。
		if update, uErr := crossTakeBibleRobTimes(session, crossCall); uErr == nil {
			slog.Info("跨服取经打劫后剩余次数同步",
				"role_id", session.RoleID(),
				"remain_rob_times", update.CanRobTimes,
				"remain_take_times", update.CanTakeBibleTimes,
			)
			if update.CanRobTimes <= 0 {
				slog.Info("跨服取经今日打劫次数已用尽", "role_id", session.RoleID())
				break
			}
		}
		if i+1 < attempts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(crossTakeBibleRobInterval):
			}
		}
	}

	slog.Info("跨服取经打劫巡检完成",
		"role_id", session.RoleID(),
		"robbed", robbed,
		"rob_coin", totalCoin,
		"rob_fame", totalFame,
		"rob_st_exp", totalStExp,
	)
	return retryAfter
}

// crossTakeBibleRobCD 查询跨服取经打劫冷却。
func crossTakeBibleRobCD(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (*protocol.StTakeBibleCrossRobCD, bool) {
	resp, err := crossCall(protocol.BuildStTakeBibleCrossGetRobCDPacket(), protocol.ActionIDStTakeBibleCrossGetRobCD, crossTakeBibleRobCDTimeout)
	if err != nil {
		slog.Info("跨服取经打劫冷却查询无应答", "role_id", session.RoleID(), "error", err)
		return nil, false
	}
	cd, err := protocol.ParseStTakeBibleCrossRobCDResult(resp.Payload)
	if err != nil {
		slog.Warn("解析跨服取经打劫冷却失败", "role_id", session.RoleID(), "error", err)
		return nil, false
	}
	return cd, true
}

// crossTakeBibleRobTimes 查询跨服取经今日剩余护送/打劫次数。
func crossTakeBibleRobTimes(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (*protocol.StTakeBibleCrossUpdateData, error) {
	resp, err := crossCall(protocol.BuildStTakeBibleCrossUpdateDataPacket(), protocol.ActionIDStTakeBibleCrossUpdateData, crossTakeBibleRobCDTimeout)
	if err != nil {
		slog.Info("跨服取经剩余次数查询无应答", "role_id", session.RoleID(), "error", err)
		return nil, err
	}
	update, err := protocol.ParseStTakeBibleCrossUpdateDataResult(resp.Payload)
	if err != nil {
		slog.Warn("解析跨服取经剩余次数失败", "role_id", session.RoleID(), "error", err)
		return nil, err
	}
	return update, nil
}

// crossTakeBibleRobReconcile 在 rob 回包超时后核对本次打劫是否已被服务端受理。
//
// 官方客户端每次打劫后都会重新查询打劫冷却；服务端一旦受理 (无论胜负) 都会
// 设置冷却。因此回包缺失时以冷却状态作为唯一可靠的受理凭证：
//   - accepted=true：冷却已触发，按已结算处理，绝不能重复发起；
//   - accepted=false 且 known=true：冷却仍为 0，确认请求未被受理，可安全尝试下一目标；
//   - known=false：冷却查询本身失败，状态未知，调用方必须保守收手。
func crossTakeBibleRobReconcile(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (accepted, known bool) {
	cd, ok := crossTakeBibleRobCD(session, crossCall)
	if !ok {
		return false, false
	}
	return cd.CDSeconds > 0, true
}

// NewPilgrimageRoutine 构造西天取经护送与打劫任务。
// 可传入 PilgrimageConfig 覆盖默认策略；不传时使用 DefaultPilgrimageConfig。
func NewPilgrimageRoutine(configs ...PilgrimageConfig) scheduler.ActivityRoutine {
	cfg := DefaultPilgrimageConfig()
	if len(configs) > 0 {
		cfg = normalizePilgrimageConfig(configs[0])
	}
	return scheduler.NewBaseRoutine(
		"pilgrimage",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		20*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行资源任务: 西天取经巡检与护送", "role_id", session.RoleID())

			callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, d)
				defer cancel()
				return session.Call(cCtx, pkt, expectedAct)
			}

			// 1. 官方仙界跨服取经 (Module 114, Mod_StTakeBible_Base)
			// 取经模块只在 Mod 94 登录的仙界跨服连接上生效，主服网关不会响应。
			if handled, retryAfter := handleCrossPilgrimage(ctx, session, cfg); handled {
				if retryAfter > 0 {
					slog.Info("取经打劫冷却中，按冷却结束时间唤醒",
						"role_id", session.RoleID(), "retry_after", retryAfter.Round(time.Second))
					return scheduler.NewCooldown(retryAfter, "跨服取经打劫冷却")
				}
				return nil
			}

			// 2. 传统 Web 协议兜底回退 (Module 30, Mod_TakeBible_Base)
			openPkt := protocol.BuildTakeBibleOpenPacket(session.LastActionID())
			if openResp, err := callWithTimeout(openPkt, protocol.ActionIDTakeBibleOpen, 2*time.Second); err == nil && openResp != nil {
				openRes, _ := protocol.ParseTakeBibleOpenResult(openResp.Payload)
				if openRes != nil && openRes.CanTakeTimes <= 0 {
					slog.Info("西天取经今日护送次数已用尽", "role_id", session.RoleID(), "remain_times", openRes.CanTakeTimes)
					closePkt := protocol.BuildTakeBibleClosePacket(session.LastActionID())
					_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
					return nil
				}
			}

			// 2.2 拉取当前取经准备状态与使者数据 (0x001E0002)
			infoPkt := protocol.BuildTakeBibleGetInfoPacket(session.LastActionID())
			infoResp, err := callWithTimeout(infoPkt, protocol.ActionIDTakeBibleGetInfo, 2*time.Second)
			if err != nil {
				slog.Info("西天取经准备详情无应答，当前角色可能尚未解锁护送权限或该服取经已迁移",
					"role_id", session.RoleID(),
				)
				return nil
			}

			infoRes, err := protocol.ParseTakeBibleGetInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("farming: 解析取经详情失败: %w", err)
			}

			prevAct := protocol.ActionIDTakeBibleGetInfo

			// 检查是否已经在护送中
			if infoRes.IsBusy {
				slog.Info("当前取经队伍已在途中，无需重复启程", "role_id", session.RoleID(), "angel", protocol.AngelName(infoRes.CurrentAngel))
				closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
				_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
				return nil
			}

			// 检查今日总取经次数是否已达到上限 (每日上限 3 次)
			if infoRes.TotalTakeTimes >= 3 {
				slog.Info("西天取经今日总护送次数已达上限 (3次)", "role_id", session.RoleID(), "total_times", infoRes.TotalTakeTimes)
				closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
				_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
				return nil
			}

			slog.Info("当前取经使者就绪",
				"role_id", session.RoleID(),
				"angel", protocol.AngelName(infoRes.CurrentAngel),
				"protect_player_id", infoRes.ProtectPlayerID,
				"remain_rob_times", infoRes.RobTimes,
			)

			// 2.3 使者品质刷新策略: 若不是最高品质使者 (唐僧)，尝试刷新提升护送收益
			if infoRes.CurrentAngel < protocol.TakeBibleTangSeng {
				refreshPkt := protocol.BuildTakeBibleRefreshPacket(prevAct)
				if refResp, rErr := callWithTimeout(refreshPkt, protocol.ActionIDTakeBibleRefresh, 2*time.Second); rErr == nil && refResp != nil {
					prevAct = protocol.ActionIDTakeBibleRefresh
					if refRes, prErr := protocol.ParseTakeBibleRefreshResult(refResp.Payload); prErr == nil && refRes.ResultCode == protocol.TakeBibleSuccess {
						slog.Info("西天取经使者刷新成功",
							"role_id", session.RoleID(),
							"old_angel", protocol.AngelName(infoRes.CurrentAngel),
							"new_angel", protocol.AngelName(refRes.NewAngel),
							"next_cost", refRes.NextRefreshCost,
						)
						infoRes.CurrentAngel = refRes.NewAngel
					}
				}
			}

			// 2.4 正式启程开始护送 (0x001E000A)
			startPkt := protocol.BuildTakeBibleStartPacket(prevAct)
			startResp, err := callWithTimeout(startPkt, protocol.ActionIDTakeBibleStart, 2*time.Second)
			if err != nil {
				return fmt.Errorf("farming: 启程西天取经失败: %w", err)
			}
			prevAct = protocol.ActionIDTakeBibleStart

			startCode, _ := protocol.ParseTakeBibleStartResult(startResp.Payload)
			if startCode == protocol.TakeBibleSuccess {
				slog.Info("西天取经队伍已顺利启程护送",
					"role_id", session.RoleID(),
					"angel", protocol.AngelName(infoRes.CurrentAngel),
				)
			} else {
				slog.Info("启程西天取经返回状态", "role_id", session.RoleID(), "status_code", startCode)
			}

			// 2.5 安全关闭取经主界面 (0x001E0001)
			closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
			_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
			return nil
		},
	)
}
