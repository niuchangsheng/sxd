// Package dungeon 提供副本关卡扫荡、六道轮回、伏魔塔与练功房等推图挑战玩法。
//
// 本文件实现仙界练功房 (Mod_StPracticeRoom_Base, Module 117) 的跨服挂机收益维护:
// 领取已累积经验, 并在有空闲席位的高星级房间中维持最佳练功位置。
//
// @author Ateng
// @since 2026-11-04
package dungeon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// TrainingRoomConfig 仙界练功房挂机收益维护策略配置。
type TrainingRoomConfig struct {
	// AllowRobSeat 是否允许通过战斗抢夺已被占用的高星席位 (默认 false，仅使用空闲席位)
	AllowRobSeat bool
	// MaxRobAttempts 单轮巡检最多发起的席位争夺次数 (默认 1，仅 AllowRobSeat 为 true 时生效)
	MaxRobAttempts int
}

// DefaultTrainingRoomConfig 返回仙界练功房默认策略。
//
// 默认只领取经验并占用空闲席位，不主动发起 PVP 争夺，避免在无人值守时
// 消耗争夺次数或引发不必要的玩家冲突。
func DefaultTrainingRoomConfig() TrainingRoomConfig {
	return TrainingRoomConfig{
		AllowRobSeat:   false,
		MaxRobAttempts: 1,
	}
}

// stPracticeRoomAPI 抽象练功房跨服业务调用，便于用假实现覆盖状态机分支。
type stPracticeRoomAPI interface {
	EnterStPracticeRoom(ctx context.Context) (uint8, error)
	ExitStPracticeRoom(ctx context.Context) (uint8, error)
	GetStPracticeRoomList(ctx context.Context) (*protocol.StPracticeRoomList, error)
	GetStPracticeRoomInfo(ctx context.Context, quality uint8) (*protocol.StPracticeRoomInfo, error)
	ClaimStPracticeRoomExp(ctx context.Context) (*protocol.StPracticeRoomExpResult, error)
	LeaveStPracticeRoomSeat(ctx context.Context) (uint8, error)
	GetStPracticeRoomSeatStatus(ctx context.Context, quality, seat uint8) (uint8, error)
	SitDownStPracticeRoom(ctx context.Context, quality, seat uint8) (uint8, error)
	RobStPracticeRoomSeat(ctx context.Context, quality, seat uint8) (uint8, error)
}

// NewTrainingRoomRoutine 构造仙界练功房普通/高级场挂机收益维护任务。
//
// 官方客户端流程为: Mod 96 申请跨服凭据 → Mod 94 登录跨服节点 →
// Mod 95 进入仙界城镇 → Mod 117 enter_room → get_rooms → 领取经验 →
// 选择空闲席位 sit_down。该任务每次调度在独立跨服会话上完成一轮维护，
// 不消耗元宝购买争夺次数，也不消耗元宝清除争夺冷却。
func NewTrainingRoomRoutine(cfg TrainingRoomConfig) scheduler.ActivityRoutine {
	cfg = normalizeTrainingRoomConfig(cfg)
	return scheduler.NewBaseRoutine(
		"training_room",
		"dungeon",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		60*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行副本任务: 仙界练功房普通/高级场挂机收益维护", "role_id", session.RoleID())

			// 单轮跨服维护包含登录、查询、领取与入座，预留 3 分钟预算。
			crossCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()

			credCtx, credCancel := context.WithTimeout(crossCtx, 3*time.Second)
			cred, err := session.FetchCrossServerCredentials(credCtx)
			credCancel()
			if err != nil {
				slog.Debug("仙界练功房不可用，跳过", "role_id", session.RoleID(), "error", err)
				return nil
			}

			cross, err := session.OpenCrossServerSessionWithInfo(crossCtx, cred)
			if err != nil {
				slog.Warn("仙界练功房跨服会话建立失败", "role_id", session.RoleID(), "error", err)
				return nil
			}
			defer cross.Close()

			return runStPracticeRoomMaintenance(crossCtx, cross, cfg)
		},
	)
}

func normalizeTrainingRoomConfig(cfg TrainingRoomConfig) TrainingRoomConfig {
	if cfg.MaxRobAttempts <= 0 {
		cfg.MaxRobAttempts = 1
	}
	return cfg
}

// stPracticeRoomCandidate 代表一个存在可争取席位的星级房间。
//
// freeSeats 在空闲席位候选中表示未占用的席位序号；
// 在争夺候选中表示按对手战力升序排列的已占用席位序号。
type stPracticeRoomCandidate struct {
	quality uint8
	seats   []uint8
	rob     bool
}

// runStPracticeRoomMaintenance 在已激活的仙界跨服会话上完成一轮练功房收益维护。
func runStPracticeRoomMaintenance(ctx context.Context, api stPracticeRoomAPI, cfg TrainingRoomConfig) error {
	// 1. 进入练功房面板。功能未开放时直接熔断本轮。
	enterResult, err := api.EnterStPracticeRoom(ctx)
	if err != nil {
		slog.Warn("进入仙界练功房面板失败", "error", err)
		return nil
	}
	switch enterResult {
	case protocol.StPracticeRoomSuccess:
	case protocol.StPracticeRoomFunctionNoOpen:
		slog.Info("仙界练功房功能未开放")
		return scheduler.ErrConditionNotMet
	default:
		slog.Warn("进入仙界练功房面板返回业务失败", "result", enterResult)
		return nil
	}
	defer func() {
		if result, err := api.ExitStPracticeRoom(ctx); err != nil {
			slog.Debug("退出仙界练功房面板失败", "error", err)
		} else if result != protocol.StPracticeRoomSuccess {
			slog.Debug("退出仙界练功房面板返回非成功状态", "result", result)
		}
	}()

	// 2. 查询房间列表与当前练功状态。
	list, err := api.GetStPracticeRoomList(ctx)
	if err != nil {
		slog.Warn("查询仙界练功房房间列表失败", "error", err)
		return nil
	}

	currentQuality, seated := stPracticeRoomCurrentQuality(list)
	currentRank := 0
	if seated {
		currentRank = protocol.StPracticeRoomQualityRank(currentQuality)
	}
	slog.Info("仙界练功房当前状态",
		"in_practice", seated,
		"quality", currentQuality,
		"rooms", len(list.Rooms),
	)

	// 3. 查找空闲席位; 若允许争夺, 同时查找收益更高且已满员的星级房间。
	emptyCandidate, err := findBestEmptyStPracticeRoom(ctx, api, list)
	if err != nil {
		slog.Warn("查询仙界练功房空闲席位失败", "error", err)
		return nil
	}
	var robCandidate *stPracticeRoomCandidate
	if cfg.AllowRobSeat {
		robCandidate, err = findBestRobStPracticeRoom(ctx, api, list, currentRank)
		if err != nil {
			slog.Debug("查询仙界练功房可争夺席位失败", "error", err)
		}
	}

	emptyRank := 0
	if emptyCandidate != nil {
		emptyRank = protocol.StPracticeRoomQualityRank(emptyCandidate.quality)
	}
	robRank := 0
	if robCandidate != nil {
		robRank = protocol.StPracticeRoomQualityRank(robCandidate.quality)
	}

	// 4. 当前房间收益不低于所有更优候选时原地保持，仅领取经验。
	if seated && currentRank >= emptyRank && currentRank >= robRank {
		claimStPracticeRoomExp(ctx, api, currentQuality)
		slog.Info("仙界练功房当前席位收益已是最佳，保持挂机", "quality", currentQuality)
		return nil
	}

	// 5. 选择目标: 优先收益更高的空闲房间，其次允许时争夺满员高星房间，
	//    最后回落到普通练功场。
	target := &stPracticeRoomCandidate{quality: protocol.StPracticeRoomFreeQuality}
	if emptyCandidate != nil {
		target = emptyCandidate
	}
	if robCandidate != nil && robRank > emptyRank {
		target = robCandidate
	}

	// 6. 离开当前席位并领取累积经验。官方客户端换房顺序为:
	//    player_leave_seat → get_room_practice_exp → sit_down/rob_the_seat。
	if seated {
		if ok := leaveStPracticeRoomSeat(ctx, api, currentQuality); !ok {
			return nil
		}
	}
	claimStPracticeRoomExp(ctx, api, currentQuality)

	// 7. 目标为争夺房间时先发起争夺; 失败后回落到空闲候选或普通练功场。
	if target.rob {
		if tryRobStPracticeRoomSeats(ctx, api, target.quality, target.seats, cfg.MaxRobAttempts) {
			slog.Info("仙界练功房争夺席位成功", "quality", target.quality)
			return nil
		}
		slog.Info("仙界练功房争夺席位未成功，准备回落", "quality", target.quality)
		if emptyCandidate != nil {
			target = emptyCandidate
		} else {
			target = &stPracticeRoomCandidate{quality: protocol.StPracticeRoomFreeQuality}
		}
	}

	if err := sitInStPracticeRoom(ctx, api, target); err != nil {
		slog.Warn("仙界练功房入座失败", "quality", target.quality, "error", err)
		// 星级房间入座异常时兜底回到普通练功场，避免角色长时间离线无收益。
		if target.quality != protocol.StPracticeRoomFreeQuality {
			if fallbackErr := sitDownStPracticeRoomFree(ctx, api); fallbackErr != nil {
				slog.Warn("仙界练功房回落普通练功场失败", "error", fallbackErr)
			}
		}
		return nil
	}

	slog.Info("仙界练功房席位维护完成", "quality", target.quality, "candidate_seats", len(target.seats))
	return nil
}

// leaveStPracticeRoomSeat 离开当前席位, 并在服务端要求先领取经验时补领后重试一次。
func leaveStPracticeRoomSeat(ctx context.Context, api stPracticeRoomAPI, currentQuality uint8) bool {
	result, err := api.LeaveStPracticeRoomSeat(ctx)
	if err != nil {
		slog.Warn("离开仙界练功房当前席位失败", "error", err)
		return false
	}
	switch result {
	case protocol.StPracticeRoomSuccess, protocol.StPracticeRoomNoInPractice:
		return true
	case protocol.StPracticeRoomExpNotGotten:
		if !claimStPracticeRoomExp(ctx, api, currentQuality) {
			slog.Warn("仙界练功房仍有未领取经验，跳过本轮换房", "result", result)
			return false
		}
		retryResult, retryErr := api.LeaveStPracticeRoomSeat(ctx)
		if retryErr != nil {
			slog.Warn("重试离开仙界练功房席位失败", "error", retryErr)
			return false
		}
		if retryResult == protocol.StPracticeRoomSuccess || retryResult == protocol.StPracticeRoomNoInPractice {
			return true
		}
		slog.Warn("重试离开仙界练功房席位返回业务失败", "result", retryResult)
		return false
	default:
		slog.Warn("离开仙界练功房当前席位返回业务失败", "result", result)
		return false
	}
}

// claimStPracticeRoomExp 领取当前累积经验。
//
// 返回 true 表示当前已没有阻塞换房/入座的未领取经验。
func claimStPracticeRoomExp(ctx context.Context, api stPracticeRoomAPI, quality uint8) bool {
	result, err := api.ClaimStPracticeRoomExp(ctx)
	if err != nil {
		slog.Warn("领取仙界练功房经验失败", "quality", quality, "error", err)
		return false
	}
	switch result.Result {
	case protocol.StPracticeRoomSuccess:
		slog.Info("仙界练功房经验领取成功", "quality", quality, "gain_exp", result.Exp)
		return true
	case protocol.StPracticeRoomNoExpToGet:
		return true
	case protocol.StPracticeRoomFunctionNoOpen:
		slog.Info("仙界练功房功能未开放，无法领取经验", "quality", quality)
		return false
	case protocol.StPracticeRoomExpNotGotten:
		slog.Debug("仙界练功房经验尚未结算", "quality", quality, "result", result.Result)
		return false
	default:
		slog.Warn("仙界练功房经验领取返回业务失败", "quality", quality, "result", result.Result)
		return false
	}
}

// tryRobStPracticeRoomSeats 依次争夺候选席位, 每次争夺后重新查询房间状态确认归属。
func tryRobStPracticeRoomSeats(ctx context.Context, api stPracticeRoomAPI, quality uint8, seats []uint8, maxAttempts int) bool {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if len(seats) < maxAttempts {
		maxAttempts = len(seats)
	}
	for i := 0; i < maxAttempts; i++ {
		seat := seats[i]
		result, err := api.RobStPracticeRoomSeat(ctx, quality, seat)
		if err != nil {
			slog.Warn("争夺仙界练功房席位失败", "quality", quality, "seat", seat, "error", err)
			return false
		}
		switch result {
		case protocol.StPracticeRoomSuccess:
			// 争夺回包携带完整战报, 首字节只表示战斗已受理;
			// 必须重新查询房间状态才能确认席位是否真正到手。
			if stPracticeRoomSeatAcquired(ctx, api, quality) {
				return true
			}
			slog.Info("仙界练功房争夺战未取得席位", "quality", quality, "seat", seat, "attempt", i+1)
		case protocol.StPracticeRoomNoWar:
			// 目标席位在争夺前已空出, 直接入座。
			sitResult, sitErr := api.SitDownStPracticeRoom(ctx, quality, seat)
			if sitErr == nil && (sitResult == protocol.StPracticeRoomSuccess || sitResult == protocol.StPracticeRoomAlreadySatDown) {
				return true
			}
			slog.Info("仙界练功房目标席位已空出但入座失败", "quality", quality, "seat", seat, "result", sitResult)
		case protocol.StPracticeRoomRobTimeUsedUp:
			slog.Info("仙界练功房今日争夺次数已用完", "quality", quality, "seat", seat)
			return false
		case protocol.StPracticeRoomCDTime:
			slog.Info("仙界练功房争夺冷却中", "quality", quality, "seat", seat)
			return false
		case protocol.StPracticeRoomNoEnoughIngot:
			slog.Info("仙界练功房争夺需要元宝，自动化不主动消耗", "quality", quality, "seat", seat)
			return false
		case protocol.StPracticeRoomFunctionNoOpen:
			slog.Info("仙界练功房功能未开放", "quality", quality, "seat", seat)
			return false
		case protocol.StPracticeRoomExpNotGotten:
			slog.Warn("仙界练功房仍有未领取经验，无法争夺席位", "quality", quality, "seat", seat)
			return false
		default:
			slog.Warn("争夺仙界练功房席位返回业务失败", "quality", quality, "seat", seat, "result", result)
			return false
		}
	}
	return false
}

// stPracticeRoomSeatAcquired 重新查询房间列表, 判断角色是否已进入目标品质房间。
func stPracticeRoomSeatAcquired(ctx context.Context, api stPracticeRoomAPI, quality uint8) bool {
	list, err := api.GetStPracticeRoomList(ctx)
	if err != nil {
		slog.Debug("复核仙界练功房席位归属失败", "quality", quality, "error", err)
		return false
	}
	currentQuality, seated := stPracticeRoomCurrentQuality(list)
	return seated && currentQuality == quality
}

// sitInStPracticeRoom 执行入座计划。
func sitInStPracticeRoom(ctx context.Context, api stPracticeRoomAPI, target *stPracticeRoomCandidate) error {
	if target == nil {
		return nil
	}
	if target.quality == protocol.StPracticeRoomFreeQuality {
		return sitDownStPracticeRoomFree(ctx, api)
	}
	return sitDownStPracticeRoomStar(ctx, api, target.quality, target.seats)
}

// sitDownStPracticeRoomFree 进入普通练功场 (品质 100)，由服务端自动分配席位。
func sitDownStPracticeRoomFree(ctx context.Context, api stPracticeRoomAPI) error {
	result, err := api.SitDownStPracticeRoom(ctx, protocol.StPracticeRoomFreeQuality, 0)
	if err != nil {
		return err
	}
	switch result {
	case protocol.StPracticeRoomSuccess, protocol.StPracticeRoomAlreadySatDown:
		slog.Info("已进入普通练功场挂机", "quality", protocol.StPracticeRoomFreeQuality)
		return nil
	case protocol.StPracticeRoomFunctionNoOpen:
		return scheduler.ErrConditionNotMet
	default:
		return &stPracticeRoomSitError{quality: protocol.StPracticeRoomFreeQuality, result: result}
	}
}

// sitDownStPracticeRoomStar 在星级房间的空闲席位中依次尝试入座。
func sitDownStPracticeRoomStar(ctx context.Context, api stPracticeRoomAPI, quality uint8, freeSeats []uint8) error {
	for _, seat := range freeSeats {
		status, err := api.GetStPracticeRoomSeatStatus(ctx, quality, seat)
		if err != nil {
			return err
		}
		if status == protocol.StPracticeRoomSomebody {
			continue
		}
		if status != protocol.StPracticeRoomNobody {
			slog.Warn("仙界练功房席位状态未知", "quality", quality, "seat", seat, "status", status)
			continue
		}

		result, err := api.SitDownStPracticeRoom(ctx, quality, seat)
		if err != nil {
			return err
		}
		switch result {
		case protocol.StPracticeRoomSuccess:
			slog.Info("已坐上仙界练功房高星席位", "quality", quality, "seat", seat)
			return nil
		case protocol.StPracticeRoomAlreadySatDown:
			slog.Info("角色已在练功席位中", "quality", quality, "seat", seat)
			return nil
		case protocol.StPracticeRoomAlreadyInUsed, protocol.StPracticeRoomRoomSeatError:
			// 席位在查询与入座之间被抢走，继续尝试下一个空闲席位。
			continue
		case protocol.StPracticeRoomFunctionNoOpen:
			return scheduler.ErrConditionNotMet
		default:
			return &stPracticeRoomSitError{quality: quality, seat: seat, result: result}
		}
	}

	// 所有候选席位都在竞态中被占用，回落到普通练功场。
	slog.Info("仙界练功房星级席位已满，回落普通练功场", "quality", quality)
	return sitDownStPracticeRoomFree(ctx, api)
}

// stPracticeRoomSitError 描述入座失败的业务结果码。
type stPracticeRoomSitError struct {
	quality uint8
	seat    uint8
	result  uint8
}

func (e *stPracticeRoomSitError) Error() string {
	return fmt.Sprintf("仙界练功房入座返回业务失败: quality=%d, seat=%d, result=%d", e.quality, e.seat, e.result)
}

// stPracticeRoomCurrentQuality 提取当前房间品质与是否真正处于席位中。
func stPracticeRoomCurrentQuality(list *protocol.StPracticeRoomList) (uint8, bool) {
	if list == nil || list.InPracticeFlag != protocol.StPracticeRoomYes {
		return 0, false
	}
	if list.Quality <= 0 {
		return 0, false
	}
	return uint8(list.Quality), true
}

// findBestEmptyStPracticeRoom 从高到低查找存在空闲席位的星级房间。
//
// get_rooms 下发的 count 为房间内玩家数量；当 count 已达到该品质固定席位数时
// 直接跳过，避免无意义的 get_room_info 调用。
func findBestEmptyStPracticeRoom(ctx context.Context, api stPracticeRoomAPI, list *protocol.StPracticeRoomList) (*stPracticeRoomCandidate, error) {
	if list == nil {
		return nil, nil
	}
	roomByQuality := stPracticeRoomMap(list.Rooms)

	for quality := 5; quality >= 1; quality-- {
		q := uint8(quality)
		entry, ok := roomByQuality[q]
		if !ok {
			continue
		}
		seatCount := protocol.StPracticeRoomSeatCount(q)
		if seatCount <= 0 || int(entry.Count) >= seatCount {
			continue
		}

		info, err := api.GetStPracticeRoomInfo(ctx, q)
		if err != nil {
			// 单个房间查询失败不应阻断其他候选房间。
			slog.Debug("查询仙界练功房星级房间失败", "quality", q, "error", err)
			continue
		}
		freeSeats := stPracticeRoomFreeSeats(info.Seats, seatCount)
		if len(freeSeats) == 0 {
			continue
		}
		return &stPracticeRoomCandidate{quality: q, seats: freeSeats}, nil
	}
	return nil, nil
}

// findBestRobStPracticeRoom 从高到低查找收益高于当前品质且已满员的星级房间。
//
// 返回的席位按对手战力升序排列, 自动化优先挑战最弱的守位玩家。
func findBestRobStPracticeRoom(ctx context.Context, api stPracticeRoomAPI, list *protocol.StPracticeRoomList, currentRank int) (*stPracticeRoomCandidate, error) {
	if list == nil {
		return nil, nil
	}
	roomByQuality := stPracticeRoomMap(list.Rooms)

	for quality := 5; quality >= 1; quality-- {
		q := uint8(quality)
		if protocol.StPracticeRoomQualityRank(q) <= currentRank {
			continue
		}
		entry, ok := roomByQuality[q]
		if !ok {
			continue
		}
		seatCount := protocol.StPracticeRoomSeatCount(q)
		if seatCount <= 0 || int(entry.Count) < seatCount {
			// 尚未满员, 优先走空闲席位流程。
			continue
		}

		info, err := api.GetStPracticeRoomInfo(ctx, q)
		if err != nil {
			slog.Debug("查询仙界练功房可争夺房间失败", "quality", q, "error", err)
			continue
		}
		occupied := stPracticeRoomOccupiedSeats(info.Seats)
		if len(occupied) == 0 {
			continue
		}
		return &stPracticeRoomCandidate{quality: q, seats: occupied, rob: true}, nil
	}
	return nil, nil
}

func stPracticeRoomMap(rooms []protocol.StPracticeRoomEntry) map[uint8]protocol.StPracticeRoomEntry {
	out := make(map[uint8]protocol.StPracticeRoomEntry, len(rooms))
	for _, room := range rooms {
		out[room.Quality] = room
	}
	return out
}

// stPracticeRoomFreeSeats 返回指定房间中未被占用的席位序号 (升序)。
func stPracticeRoomFreeSeats(seats []protocol.StPracticeRoomSeat, seatCount int) []uint8 {
	if seatCount <= 0 {
		return nil
	}
	occupied := make([]bool, seatCount)
	for _, seat := range seats {
		if seat.SeatSeq < 0 {
			continue
		}
		if int(seat.SeatSeq) < seatCount {
			occupied[seat.SeatSeq] = true
		}
	}
	free := make([]uint8, 0, seatCount)
	for i := 0; i < seatCount; i++ {
		if !occupied[i] {
			free = append(free, uint8(i))
		}
	}
	return free
}

// stPracticeRoomOccupiedSeats 返回已占用席位序号, 按对手战力/等级升序排列。
func stPracticeRoomOccupiedSeats(seats []protocol.StPracticeRoomSeat) []uint8 {
	ordered := make([]protocol.StPracticeRoomSeat, 0, len(seats))
	for _, seat := range seats {
		if seat.SeatSeq >= 0 {
			ordered = append(ordered, seat)
		}
	}
	// 小型插入排序, 避免为最多 12 条记录引入额外依赖。
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && stPracticeRoomSeatLess(ordered[j], ordered[j-1]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	out := make([]uint8, 0, len(ordered))
	for _, seat := range ordered {
		out = append(out, uint8(seat.SeatSeq))
	}
	return out
}

func stPracticeRoomSeatLess(a, b protocol.StPracticeRoomSeat) bool {
	if a.AttackPower != b.AttackPower {
		return a.AttackPower < b.AttackPower
	}
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	return a.SeatSeq < b.SeatSeq
}
