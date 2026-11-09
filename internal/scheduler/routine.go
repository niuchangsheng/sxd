// Package scheduler 提供拟人随机抖动任务调度引擎，协调各类日常活动任务的执行时机，防止风控异常。
//
// @author Ateng
// @since 2026-10-08
package scheduler

import (
	"context"
	"errors"
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

// BaseRoutine 为所有具体玩法提供标准化基础骨架实现。
type BaseRoutine struct {
	id        string
	domain    string
	schedType ScheduleType
	priority  RoutinePriority
	interval  time.Duration
	fn        func(ctx context.Context, session *client.RoleSession, jitter *Jitter) error
}

// NewBaseRoutine 构造新的标准化 BaseRoutine 实例。
func NewBaseRoutine(
	id string,
	domain string,
	schedType ScheduleType,
	priority RoutinePriority,
	interval time.Duration,
	fn func(ctx context.Context, session *client.RoleSession, jitter *Jitter) error,
) *BaseRoutine {
	return &BaseRoutine{
		id:        id,
		domain:    domain,
		schedType: schedType,
		priority:  priority,
		interval:  interval,
		fn:        fn,
	}
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

// Execute 执行具体业务闭包逻辑。
func (b *BaseRoutine) Execute(ctx context.Context, session *client.RoleSession, jitter *Jitter) error {
	if b.fn == nil {
		return nil
	}
	// 零假动作铁律：调度前置检查与真正执行之间可能发生断线重连，
	// 此处再确认一次会话状态，避免在连接中断窗口内向服务端发送注定超时的业务包。
	if session != nil && session.State() != client.StateActive {
		return nil
	}
	return b.fn(ctx, session, jitter)
}
