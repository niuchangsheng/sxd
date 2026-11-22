// Package farming 提供药园、取经护送/劫镖、仙界矿山、粗铁淬炼池与仙葫芦等资源养成自动化。
//
// 本文件实现粗铁淬炼池 (Mod_RefinePool_Base) 自动一键淬炼与登录触发。
package farming

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// SpiritPoolRefineTarget 粗铁淬炼池一键操作的产出目标。
type SpiritPoolRefineTarget int

const (
	// SpiritPoolRefineXuanTie 淬炼玄铁 (协议 REFINE_1)。
	SpiritPoolRefineXuanTie SpiritPoolRefineTarget = iota + 1
	// SpiritPoolRefineLingCai 提炼灵材 (协议 REFINE_2)。
	SpiritPoolRefineLingCai
)

// SpiritPoolConfig 粗铁淬炼池策略配置。
//
// 无论选择玄铁还是灵材, 均固定使用铜钱消耗, 永不消耗元宝。
type SpiritPoolConfig struct {
	// Target 一键操作的产出目标: 玄铁 (默认) 或灵材。
	Target SpiritPoolRefineTarget
}

// DefaultSpiritPoolConfig 返回默认淬炼池策略: 一键淬炼玄铁。
func DefaultSpiritPoolConfig() SpiritPoolConfig {
	return SpiritPoolConfig{Target: SpiritPoolRefineXuanTie}
}

// ParseSpiritPoolRefineTarget 解析配置中的淬炼目标文案。
// 支持 xuantie / xuan_tie / 玄铁 / iron 与 lingcai / ling_cai / 灵材;
// 空值按默认玄铁处理, 无法识别时返回 false。
func ParseSpiritPoolRefineTarget(raw string) (SpiritPoolRefineTarget, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "xuantie", "xuan_tie", "玄铁", "iron":
		return SpiritPoolRefineXuanTie, true
	case "lingcai", "ling_cai", "灵材":
		return SpiritPoolRefineLingCai, true
	default:
		return 0, false
	}
}

// normalizeSpiritPoolConfig 将淬炼池策略收敛到合法取值。
func normalizeSpiritPoolConfig(cfg SpiritPoolConfig) SpiritPoolConfig {
	if cfg.Target != SpiritPoolRefineXuanTie && cfg.Target != SpiritPoolRefineLingCai {
		cfg.Target = SpiritPoolRefineXuanTie
	}
	return cfg
}

// NewSpiritPoolRoutine 构造粗铁淬炼池与异兽提炼任务 (Feature 5: 淬炼粗铁)。
//
// 交互序列与实机抓包 (haha.pcapng) 对齐:
//  1. Module 282, Action 0 打开淬炼池面板, 回包携带全部铁槽生产状态;
//  2. Module 534, Action 5 查询淬炼池资源面板;
//  3. Module 282, Action 1 一键执行淬炼 (玄铁) 或提炼 (灵材)。
//
// 请求第 1 字节为产出目标 (5 玄铁 / 6 灵材), 第 2 字节为消耗类型 (固定铜钱 7),
// 第 3 字节为操作方式 (一键 10); 服务端自动处理全部已完成槽位, 请求不携带槽位数量。
// 协议基准为原版 Mod_RefinePool_Base 反编译常量, 追踪 issue #1。
// spiritPoolTarget 返回淬炼目标与名称。
func spiritPoolTarget(cfg SpiritPoolConfig) (uint8, string) {
	if cfg.Target == SpiritPoolRefineLingCai {
		return protocol.RefinePoolTargetLingCai, "灵材"
	}
	return protocol.RefinePoolTargetXuanTie, "玄铁"
}

// spiritPoolLoginAttempted 标记本进程内已在登录后执行过淬炼池操作。
var spiritPoolLoginAttempted sync.Map

// RunSpiritPoolOnLogin 在会话激活后、调度器启动前执行一次淬炼池一键操作。
//
// 服务端对 282/1 的 PrevAct 上下文敏感：登录后第一时间执行可稳定成功，
// 长会话中再执行可能被静默丢弃（2026-11-11 实测）。
func RunSpiritPoolOnLogin(ctx context.Context, session *client.RoleSession, cfg SpiritPoolConfig) {
	if session == nil {
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if session.State() != client.StateActive {
		return
	}
	if _, loaded := spiritPoolLoginAttempted.LoadOrStore(session, true); loaded {
		return
	}
	cfg = normalizeSpiritPoolConfig(cfg)
	target, targetName := spiritPoolTarget(cfg)
	slog.Info("登录后粗铁淬炼池一键操作", "role_id", session.RoleID(), "target", targetName)
	if err := runSpiritPoolCore(ctx, session, cfg, target, targetName); err != nil {
		slog.Info("登录后粗铁淬炼池操作未完成", "role_id", session.RoleID(), "err", err)
	}
}

// NewSpiritPoolRoutine 构造粗铁淬炼池任务。
func NewSpiritPoolRoutine(cfgs ...SpiritPoolConfig) scheduler.ActivityRoutine {
	cfg := DefaultSpiritPoolConfig()
	if len(cfgs) > 0 {
		cfg = normalizeSpiritPoolConfig(cfgs[0])
	}
	target, targetName := spiritPoolTarget(cfg)
	return scheduler.NewBaseRoutine(
		"spirit_pool",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行资源任务: 粗铁淬炼池一键操作",
				"role_id", session.RoleID(),
				"target", targetName,
			)
			return runSpiritPoolCore(ctx, session, cfg, target, targetName)
		},
	)
}

// runSpiritPoolCore 刷新 PrevAct 链、读取面板并对已完成槽位执行一键淬炼。
func runSpiritPoolCore(
	ctx context.Context,
	session *client.RoleSession,
	cfg SpiritPoolConfig,
	target uint8,
	targetName string,
) error {
	const callTimeout = 8 * time.Second
	// 一键淬炼是一次性结算全部已完成槽位的重操作（线上 done_slots=10 时
	// 8s 内未能返回），给结算请求更长的等待窗口，避免把慢回包误判为丢包。
	const executeTimeout = 20 * time.Second

	// 1. 功能开放门禁：未开放角色 (如 ncs s1 等级 94) 发送 282/0 会被静默丢弃，
	// 先用 Action 6 开放列表判定，避免每轮空等 8 秒。
	if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDRefinePool); fErr != nil {
		slog.Info("粗铁淬炼池功能开放状态查询失败，本轮跳过",
			"role_id", session.RoleID(), "err", fErr)
		return nil
	} else if !open {
		slog.Info("粗铁淬炼池功能未开放，跳过",
			"role_id", session.RoleID(), "function_id", protocol.FunctionIDRefinePool)
		// 保持既有语义：正常跳过，由 30 分钟 Loop 在功能开放后自动重试。
		return nil
	}

	// 2. 打开淬炼池面板 (Mod 282, Act 0), 获取各铁槽生产状态。
	//    2026-11-14 实机：282/0 仅接受空载荷，追加 4 字节 PrevAct 会被静默丢弃；
	//    282/1 同样只接受裸 3 字节业务字段。因此不再刷新 PrevAct 链锚点。
	openPkt := protocol.BuildRefinePoolOpenPacket(0)
	oCtx, oCancel := context.WithTimeout(ctx, callTimeout)
	openResp, err := session.Call(oCtx, openPkt, protocol.ActionIDRefinePoolOpen)
	oCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Info("打开粗铁淬炼池面板无回包，稍后重试",
				"role_id", session.RoleID(),
				"target", targetName,
			)
			return scheduler.NewCooldown(10*time.Minute, "粗铁淬炼池面板无回包")
		} else {
			slog.Warn("打开粗铁淬炼池面板失败", "role_id", session.RoleID(), "err", err)
		}
		return nil
	}
	panel, err := protocol.ParseRefinePoolOpenResponse(openResp.Payload)
	if err != nil {
		slog.Warn("解析粗铁淬炼池面板失败", "role_id", session.RoleID(), "err", err)
		return nil
	}
	// 诊断：输出每个槽位的生产状态，定位 DoneCount=0 的根因。
	{
		statusCounts := make(map[uint8]int, 5)
		for _, iron := range panel.Irons {
			statusCounts[iron.ProduceStatus]++
		}
		slog.Info("粗铁淬炼池面板状态",
			"role_id", session.RoleID(),
			"irons", len(panel.Irons),
			"status_counts", statusCounts,
			"level", panel.Level,
			"lian_qi_jing_hua", panel.LianQiJingHua,
			"coins_need", panel.CoinsNeed,
			"one_key_lock_flag", panel.OneKeyLockFlag,
		)
	}

	// 3. 线上实测（2026-11-07 22:00，s0-niuchangsheng）：534 资源面板
	// 在本服可被省略，且该请求经常被静默丢弃。open_panel 之后直接发送
	// 一键淬炼 (282/1, PrevAct=282/0) 可正常返回 result=11 SUCCESS，
	// 因此不再把 534 作为必选链路。保留 protocol 层构造器供探针使用。
	done := panel.DoneCount()
	if done == 0 {
		statusCounts := make(map[uint8]int, 5)
		type ironInfo struct {
			Seq     int8
			Status  uint8
			Need    int32
			Remain  int32
			Quality int8
		}
		irons := make([]ironInfo, 0, len(panel.Irons))
		for _, iron := range panel.Irons {
			statusCounts[iron.ProduceStatus]++
			irons = append(irons, ironInfo{
				Seq: iron.Seq, Status: iron.ProduceStatus,
				Need: iron.ProduceNeedTime, Remain: iron.ProduceRemainTime,
				Quality: iron.Quality,
			})
		}
		slog.Info("淬炼池暂无已完成的粗铁, 跳过一键操作",
			"role_id", session.RoleID(),
			"target", targetName,
			"iron_count", len(panel.Irons),
			"status_counts", statusCounts,
			"irons", irons,
			"level", panel.Level,
			"lian_qi_jing_hua", panel.LianQiJingHua,
		)
		return nil
	}

	// 4. 一键操作所有已完成槽位 (固定铜钱消耗, 永不扣除元宝)
	execPkt := protocol.BuildRefineExecutePacket(
		target,
		protocol.RefinePoolCostCoins,
		protocol.RefinePoolTypeOneKey,
		protocol.ActionIDRefinePoolOpen,
	)
	eCtx, eCancel := context.WithTimeout(ctx, executeTimeout)
	resp, err := session.Call(eCtx, execPkt, protocol.ActionIDRefineExecute)
	eCancel()
	if err != nil {
		slog.Warn("粗铁淬炼执行无回包，稍后重试",
			"role_id", session.RoleID(), "target", targetName, "done_slots", done, "err", err)
		return scheduler.NewCooldown(10*time.Minute, "粗铁淬炼执行无回包")
	}

	result, err := protocol.ParseRefineExecuteResponse(resp.Payload)
	if err != nil {
		slog.Warn("解析粗铁淬炼回包失败", "role_id", session.RoleID(), "err", err)
		return nil
	}
	var essence, xuantie, lingcai, awardItems int32
	for _, award := range result.Awards {
		for _, item := range award.Items {
			awardItems += item.Count
			switch item.ItemID {
			case protocol.ItemRefineEssence:
				essence += item.Count
			case protocol.ItemXuanTie:
				xuantie += item.Count
			}
		}
		for _, item := range award.LingCaiItems {
			lingcai += item.Count
		}
	}
	attrs := []any{
		"role_id", session.RoleID(),
		"target", targetName,
		"result", result.Result,
		"refined_slots", len(result.Awards),
		"xuantie_gained", xuantie,
		"essence_gained", essence,
		"lingcai_gained", lingcai,
		"lian_qi_jing_hua", result.LianQiJingHua,
		"level", result.Level,
	}

	switch result.Result {
	case protocol.RefinePoolResultSuccess:
		produced := awardItems > 0 || lingcai > 0 || result.XuanTie > panel.XuanTie
		if cfg.Target == SpiritPoolRefineXuanTie {
			produced = xuantie > 0 || essence > 0 || result.XuanTie > panel.XuanTie
		}
		if produced {
			slog.Info("粗铁一键淬炼完成", attrs...)
			return nil
		}
		slog.Warn("粗铁一键淬炼返回成功码但未产出目标奖励", attrs...)
		return fmt.Errorf("粗铁%s成功码无产出", targetName)
	case protocol.RefinePoolResultLackOfCoins:
		slog.Warn("粗铁淬炼铜钱不足, 本轮跳过", attrs...)
		return scheduler.ErrConditionNotMet
	case protocol.RefinePoolResultLackOfIngot:
		// 本任务固定使用铜钱, 出现元宝不足通常意味着协议字段错位。
		slog.Warn("粗铁淬炼返回元宝不足, 疑似协议字段错位", attrs...)
		return scheduler.ErrConditionNotMet
	case protocol.RefinePoolResultFailed:
		slog.Warn("粗铁淬炼失败", attrs...)
		return fmt.Errorf("粗铁%s失败 (result=%d)", targetName, result.Result)
	default:
		slog.Warn("粗铁淬炼返回未知结果码", attrs...)
		return fmt.Errorf("粗铁%s返回未知结果码 %d", targetName, result.Result)
	}
}
