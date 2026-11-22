// Package scheduler 提供拟人随机抖动任务调度引擎，协调各类日常活动任务的执行时机，防止风控异常。
//
// @author Ateng
// @since 2026-10-08
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
)

// RoutinePriority 定义任务执行优先级。数值越大优先级越高。
type RoutinePriority int

const (
	// PriorityLow 低优先级任务 (如后置整理、冗余上报)
	PriorityLow RoutinePriority = 10
	// PriorityNormal 普通优先级任务 (如常规日常巡检、资源领用)
	PriorityNormal RoutinePriority = 50
	// PriorityHigh 高优先级任务 (如定时准点活动、时效性抢购)
	PriorityHigh RoutinePriority = 100
)

// RoutinePhase 声明玩法的执行阶段，取代散落在各玩法中的魔法优先级数字。
// 调度器按优先级从高到低依次执行各阶段；同阶段内可通过 PhasePriority 附带小幅偏移。
type RoutinePhase RoutinePriority

const (
	// PhaseCleanup 后置整理、低优先级汇总类玩法 (10 档)
	PhaseCleanup RoutinePhase = 10
	// PhaseStandard 常规日常/巡检/非上下文敏感玩法 (50 档)
	PhaseStandard RoutinePhase = 50
	// PhaseHighPriority 高优日常玩法 (100 档，如孩子/摘仙桃/叶公好龙)
	PhaseHighPriority RoutinePhase = 100
	// PhaseEarlyWindow 登录早期敏感面板与道具玩法 (200 档，如结缘宝箱)
	PhaseEarlyWindow RoutinePhase = 200
	// PhaseCoreLoop 核心主线推图与高频活跃玩法 (300 档，如副本推图/虚空膜拜/神龙上供)
	PhaseCoreLoop RoutinePhase = 300
	// PhaseEarlyBattle 需在长会话污染前完成的早期挑战与高敏感日常 (350 档，如无量劫海/魔界远征/飞升/无尽洞天)
	PhaseEarlyBattle RoutinePhase = 350
	// PhaseTownSensitive 对城镇上下文高度敏感、须在所有战斗前执行的玩法 (420 档，如宝库/猎妖/五行天宫)
	PhaseTownSensitive RoutinePhase = 420
	// PhaseSystem 系统级维护任务，最高优先级 (500 档，如会话刷新)
	PhaseSystem RoutinePhase = 500
)

// PhasePriority 计算「执行阶段 + 阶段内偏移」对应的调度优先级。
func PhasePriority(phase RoutinePhase, offset int) RoutinePriority {
	return RoutinePriority(phase) + RoutinePriority(offset)
}

// ScheduleType 标识任务调度触发模式。
type ScheduleType int

const (
	// ScheduleLoop 周期常驻巡检任务 (如药园成熟采摘、练功房挂机、体力恢复扫荡)
	ScheduleLoop ScheduleType = iota
	// ScheduleCron 定点批处理任务 (如 00:00/06:00 每日重置福利、定时活动)
	ScheduleCron
)

func (s ScheduleType) String() string {
	switch s {
	case ScheduleLoop:
		return "Loop"
	case ScheduleCron:
		return "Cron"
	default:
		return "Unknown"
	}
}

// CooldownError 表示任务需要在指定时间点之后再唤醒。
//
// Routine 在收到服务端冷却秒数、活动开启时间或临时不可用状态时返回该错误，
// 调度器会把 nextRun 直接设为 Until，而不是等待下一次 24 小时 Cron 批次。
type CooldownError struct {
	// Until 下次允许执行的时间点
	Until time.Time
	// Reason 可读原因，仅用于日志
	Reason string
}

// Error 实现 error 接口。
func (e *CooldownError) Error() string {
	if e == nil {
		return "scheduler: cooldown"
	}
	if e.Reason == "" {
		return "scheduler: cooldown until " + e.Until.Format(time.RFC3339)
	}
	return "scheduler: cooldown until " + e.Until.Format(time.RFC3339) + ": " + e.Reason
}

// NewCooldown 构造一个相对当前时间的冷却等待错误。
func NewCooldown(d time.Duration, reason string) error {
	if d < time.Second {
		d = time.Second
	}
	return &CooldownError{Until: time.Now().Add(d), Reason: reason}
}

// NewCooldownUntil 构造一个绝对时间点的冷却等待错误。
func NewCooldownUntil(until time.Time, reason string) error {
	if until.Before(time.Now()) {
		until = time.Now()
	}
	return &CooldownError{Until: until, Reason: reason}
}

var (
	// ErrAttemptsExhausted 今日可用次数已耗尽，触发智能熔断挂起至次日重置
	ErrAttemptsExhausted = errors.New("scheduler: daily attempts exhausted")
	// ErrStaminaDepleted 体力耗尽，触发智能熔断挂起至下次体力恢复时钟
	ErrStaminaDepleted = errors.New("scheduler: stamina depleted")
	// ErrBagFull 背包已满，暂停产出类任务执行
	ErrBagFull = errors.New("scheduler: inventory bag full")
	// ErrConditionNotMet 前置等级或功能未解锁条件不满足
	ErrConditionNotMet = errors.New("scheduler: condition not met")
)

// ActivityRoutine 抽象神仙道日常活动任务基础接口。
type ActivityRoutine interface {
	// Name 返回任务唯一名称 (例如 "herb_garden", "lucky_star", "arena", "pilgrimage")
	Name() string
	// Priority 返回任务优先级
	Priority() RoutinePriority
	// Interval 返回任务周期执行间隔。若 <= 0 则表示仅执行一次
	Interval() time.Duration
	// Execute 执行具体业务逻辑，传入角色网络会话与防封抖动引擎
	Execute(ctx context.Context, session *client.RoleSession, jitter *Jitter) error
}

// DetailedRoutine 扩展提供领域归属与调度触发类型。
type DetailedRoutine interface {
	ActivityRoutine
	Domain() string
	ScheduleType() ScheduleType
}

// LifecycleRoutine 扩展声明横切生命周期策略，供调度器统一执行静默丢包退避等治理。
type LifecycleRoutine interface {
	SilentDropPolicy() (threshold int, retry time.Duration)
}

// FuncRoutine 帮助快速构造基于闭包函数的日常活动任务实例。
type FuncRoutine struct {
	name     string
	priority RoutinePriority
	interval time.Duration
	fn       func(ctx context.Context, session *client.RoleSession, jitter *Jitter) error
}

// NewFuncRoutine 创建一个基于函数的 ActivityRoutine 实例。
func NewFuncRoutine(
	name string,
	priority RoutinePriority,
	interval time.Duration,
	fn func(ctx context.Context, session *client.RoleSession, jitter *Jitter) error,
) *FuncRoutine {
	return &FuncRoutine{
		name:     name,
		priority: priority,
		interval: interval,
		fn:       fn,
	}
}

// Name 返回任务名称。
func (r *FuncRoutine) Name() string {
	return r.name
}

// Priority 返回任务优先级。
func (r *FuncRoutine) Priority() RoutinePriority {
	return r.priority
}

// Interval 返回执行间隔。
func (r *FuncRoutine) Interval() time.Duration {
	return r.interval
}

// Execute 调用内部闭包函数。
func (r *FuncRoutine) Execute(ctx context.Context, session *client.RoleSession, jitter *Jitter) error {
	if r.fn == nil {
		return nil
	}
	return r.fn(ctx, session, jitter)
}

const defaultReturnToTownTimeout = 5 * time.Second

// RoutineOption 为 BaseRoutine 附加声明式横切生命周期能力。
type RoutineOption func(*BaseRoutine)

// WithReturnToTown 标记该玩法会进入战斗/活动场景；执行结束后由中间件自动调用
// session.ReturnToTown 恢复城镇上下文，避免污染后续玩法。timeout 可省略（默认 5 秒）。
func WithReturnToTown(timeout ...time.Duration) RoutineOption {
	return func(b *BaseRoutine) {
		b.returnToTown = true
		if len(timeout) > 0 && timeout[0] > 0 {
			b.returnToTownTimeout = timeout[0]
		} else {
			b.returnToTownTimeout = defaultReturnToTownTimeout
		}
	}
}

// WithFunctionID 声明该玩法对应的原生功能开放编号 (Mod_Player_Base.check_function_open)。
// 若指定，中间件在执行业务逻辑前先探测该功能是否开放；未开放时直接返回 ErrConditionNotMet。
func WithFunctionID(funcID int32) RoutineOption {
	return func(b *BaseRoutine) {
		b.functionID = funcID
	}
}

// WithSilentDropPolicy 声明该玩法在返回 context.DeadlineExceeded 时的连续丢包退避策略。
func WithSilentDropPolicy(threshold int, retry time.Duration) RoutineOption {
	return func(b *BaseRoutine) {
		b.dropThreshold = threshold
		b.dropRetry = retry
	}
}

// BaseRoutine 为所有具体玩法提供标准化基础骨架与横切生命周期中间件实现。
type BaseRoutine struct {
	id        string
	domain    string
	schedType ScheduleType
	priority  RoutinePriority
	interval  time.Duration
	fn        func(ctx context.Context, session *client.RoleSession, jitter *Jitter) error

	returnToTown        bool
	returnToTownTimeout time.Duration
	functionID          int32
	dropThreshold       int
	dropRetry           time.Duration
}

// NewBaseRoutine 构造新的标准化 BaseRoutine 实例，可选挂载横切生命周期选项。
func NewBaseRoutine(
	id string,
	domain string,
	schedType ScheduleType,
	priority RoutinePriority,
	interval time.Duration,
	fn func(ctx context.Context, session *client.RoleSession, jitter *Jitter) error,
	opts ...RoutineOption,
) *BaseRoutine {
	b := &BaseRoutine{
		id:        id,
		domain:    domain,
		schedType: schedType,
		priority:  priority,
		interval:  interval,
		fn:        fn,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return b
}

// Name 返回玩法唯一标识符。
func (b *BaseRoutine) Name() string {
	return b.id
}

// Domain 返回玩法所属领域。
func (b *BaseRoutine) Domain() string {
	return b.domain
}

// ScheduleType 返回触发类型（定点批处理或周期巡检）。
func (b *BaseRoutine) ScheduleType() ScheduleType {
	return b.schedType
}

// Priority 返回调度优先级。
func (b *BaseRoutine) Priority() RoutinePriority {
	return b.priority
}

// Interval 返回周期执行间隔。
func (b *BaseRoutine) Interval() time.Duration {
	return b.interval
}

// NeedsReturnToTown 返回该玩法是否启用了结束后自动返回城镇中间件。
func (b *BaseRoutine) NeedsReturnToTown() bool {
	return b.returnToTown
}

// FunctionID 返回声明的功能开放编号（<= 0 表示未启用前置自动探测）。
func (b *BaseRoutine) FunctionID() int32 {
	return b.functionID
}

// SilentDropPolicy 返回声明的连续静默丢包退避策略（零值表示使用调度器默认策略）。
func (b *BaseRoutine) SilentDropPolicy() (threshold int, retry time.Duration) {
	return b.dropThreshold, b.dropRetry
}

// Execute 执行横切前置守卫、业务逻辑闭包与后置场景清理。
func (b *BaseRoutine) Execute(ctx context.Context, session *client.RoleSession, jitter *Jitter) error {
	if b.fn == nil {
		return nil
	}
	// 零假动作铁律：调度前置检查与真正执行之间可能发生断线重连，
	// 此处再确认一次会话状态，避免在连接中断窗口内向服务端发送注定超时的业务包。
	if session != nil && session.State() != client.StateActive {
		// 返回 ErrNotConnected 而不是 nil：nil 会被调度器当成“本轮成功”，
		// Cron/30 分钟循环要等下一个周期才重试；ErrNotConnected 走 5 分钟短重试。
		return client.ErrNotConnected
	}

	// 横切前置守卫：原生功能开放探测 (Mod_Player_Base.check_function_open)
	if b.functionID > 0 && session != nil {
		open, err := session.CheckFunctionOpen(ctx, b.functionID)
		if err != nil {
			if errors.Is(err, client.ErrNotConnected) || errors.Is(err, client.ErrSessionClosed) {
				return client.ErrNotConnected
			}
			if errors.Is(err, context.Canceled) {
				return err
			}
			slog.Warn("功能开放前置探测失败，本轮跳过",
				"role_id", session.RoleID(),
				"routine", b.id,
				"function_id", b.functionID,
				"err", err,
			)
			return err
		}
		if !open {
			slog.Info("功能尚未开放，本轮跳过",
				"role_id", session.RoleID(),
				"routine", b.id,
				"function_id", b.functionID,
			)
			return ErrConditionNotMet
		}
	}

	// 横切后置清理：战斗/活动场景结束后统一返回城镇，防止污染后续玩法上下文。
	if b.returnToTown && session != nil {
		timeout := b.returnToTownTimeout
		if timeout <= 0 {
			timeout = defaultReturnToTownTimeout
		}
		defer func() {
			if ctx.Err() == nil && session.State() == client.StateActive && session.ReturnToTown(ctx, timeout) {
				slog.Info("玩法结束，已自动返回城镇场景",
					"role_id", session.RoleID(),
					"routine", b.id,
				)
			}
		}()
	}

	return b.fn(ctx, session, jitter)
}
