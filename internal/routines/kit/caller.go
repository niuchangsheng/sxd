// Package kit 提供各玩法领域包复用的统一协议调用、错误分类与上下文感知休眠原语，
// 取代散落在业务玩法中的局部 call 闭包与裸 time.Sleep。
package kit

import (
	"context"
	"errors"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

const (
	fallbackCallTimeout = 8 * time.Second
	// reconnectCallWait 首次调用因「会话重连中」失败后，等待状态机恢复的上限。
	// 重连退避最长 5 分钟；这里只覆盖一次恢复窗口，超时后按 ErrNotConnected
	// 交给调度器 90 秒短重试，避免调用方无限挂起。
	reconnectCallWait = 60 * time.Second
)

type timeoutOverrideKey struct{}

// WithTimeoutOverride 在上下文中注入单次协议调用超时上限，供单元测试把 8~30 秒
// 业务超时压缩到毫秒级，避免测试真实干等超时。
func WithTimeoutOverride(ctx context.Context, maxTimeout time.Duration) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxTimeout <= 0 {
		return ctx
	}
	return context.WithValue(ctx, timeoutOverrideKey{}, maxTimeout)
}

func resolveTimeout(ctx context.Context, requested time.Duration) time.Duration {
	if requested <= 0 {
		requested = fallbackCallTimeout
	}
	if ctx != nil {
		if capTimeout, ok := ctx.Value(timeoutOverrideKey{}).(time.Duration); ok && capTimeout > 0 && requested > capTimeout {
			return capTimeout
		}
	}
	return requested
}

// IsAbortErr 判断协议调用错误是否属于必须立即中止本轮执行的会话/上下文终止事件。
//   - client.ErrNotConnected / client.ErrSessionClosed 统一归一化为 client.ErrNotConnected
//     （由调度器按 5 分钟断线短重试处理，绝不当作成功或静默丢包）；
//   - context.Canceled 透传 context.Canceled 以便优雅退出。
func IsAbortErr(err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	if errors.Is(err, client.ErrNotConnected) || errors.Is(err, client.ErrSessionClosed) {
		return client.ErrNotConnected, true
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled, true
	}
	return nil, false
}

// Call 使用受控超时发起一次协议请求并返回完整错误，绝不吞错。
//
// 若首次调用因会话正在重连（ErrNotConnected 且状态可自愈）失败，先等待状态机
// 恢复再重试一次，避免把「重连窗口」当成业务失败暴露给 Routine。
//
// 重试不会产生重复业务动作：Call 只在发包前（Send 发现 conn 不可用）返回
// ErrNotConnected；一旦请求已写入连接，失败口径是超时，不做重发。
func Call(ctx context.Context, session *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout time.Duration) (*protocol.Packet, error) {
	if session == nil {
		return nil, client.ErrNotConnected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := callOnce(ctx, session, pkt, respActionID, timeout, false)
	if !shouldWaitForReconnect(ctx, session, err) {
		return resp, err
	}
	if waitErr := awaitReconnect(ctx, session); waitErr != nil {
		return resp, err
	}
	return callOnce(ctx, session, pkt, respActionID, timeout, false)
}

// CallResync 使用受控超时发起带 PrevAct 重同步重试的协议请求，绝不吞错。
// 断线语义与 Call 一致：等待重连后重试一次。
func CallResync(ctx context.Context, session *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout time.Duration) (*protocol.Packet, error) {
	if session == nil {
		return nil, client.ErrNotConnected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := callOnce(ctx, session, pkt, respActionID, timeout, true)
	if !shouldWaitForReconnect(ctx, session, err) {
		return resp, err
	}
	if waitErr := awaitReconnect(ctx, session); waitErr != nil {
		return resp, err
	}
	return callOnce(ctx, session, pkt, respActionID, timeout, true)
}

// callOnce 执行一次受控超时的协议请求。
func callOnce(ctx context.Context, session *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout time.Duration, resync bool) (*protocol.Packet, error) {
	cCtx, cancel := context.WithTimeout(ctx, resolveTimeout(ctx, timeout))
	defer cancel()
	if resync {
		return session.CallWithPrevActResync(cCtx, resolveTimeout(ctx, timeout), pkt, respActionID)
	}
	return session.Call(cCtx, pkt, respActionID)
}

// shouldWaitForReconnect 判断本次失败是否属于「会话正在自愈」的断线：
// 只有 ErrNotConnected 且状态机仍在建连/认证/重连，且调用上下文未取消时才值得等待。
func shouldWaitForReconnect(ctx context.Context, session *client.RoleSession, err error) bool {
	if !errors.Is(err, client.ErrNotConnected) {
		return false
	}
	if ctx.Err() != nil || session == nil {
		return false
	}
	return session.State().IsRecovering()
}

// awaitReconnect 阻塞等待会话重新进入激活态。
func awaitReconnect(ctx context.Context, session *client.RoleSession) error {
	waitCtx, cancel := context.WithTimeout(ctx, reconnectCallWait)
	defer cancel()
	return session.AwaitActive(waitCtx)
}

// Sleep 在尊重 ctx.Done() 的前提下等待指定时长，取代无法中断的裸 time.Sleep。
// 返回 true 表示完整等待结束；返回 false 表示上下文已取消或超时。
func Sleep(ctx context.Context, d time.Duration) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return true
	}
	if ctx == nil {
		time.Sleep(d)
		return true
	}
	timer := time.NewTimer(d)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Caller 绑定单轮 Routine 执行上下文、默认会话与默认超时的统一调用器。
//
// 相比散落在各玩法中的局部 `call := func(...) *protocol.Packet` 闭包，Caller 始终保留
// 最近一次调用的原始错误，支持通过 AbortErr() 在断线/取消时立即中止并正确向上透传。
type Caller struct {
	ctx            context.Context
	session        *client.RoleSession
	defaultTimeout time.Duration
	lastErr        error
}

// NewCaller 创建统一协议调用器。defaultTimeout <= 0 时默认使用 8 秒。
func NewCaller(ctx context.Context, session *client.RoleSession, defaultTimeout time.Duration) *Caller {
	if ctx == nil {
		ctx = context.Background()
	}
	if defaultTimeout <= 0 {
		defaultTimeout = fallbackCallTimeout
	}
	return &Caller{
		ctx:            ctx,
		session:        session,
		defaultTimeout: defaultTimeout,
	}
}

func (c *Caller) pickTimeout(override []time.Duration) time.Duration {
	if len(override) > 0 && override[0] > 0 {
		return override[0]
	}
	return c.defaultTimeout
}

// Call 向默认会话发起请求并返回完整应答与错误，同时记录 LastErr()。
func (c *Caller) Call(pkt *protocol.Packet, respActionID uint32, timeout ...time.Duration) (*protocol.Packet, error) {
	return c.CallOn(c.session, pkt, respActionID, timeout...)
}

// CallOn 向指定会话（如跨服/圣域/仙盟城镇子会话）发起请求并返回完整应答与错误。
func (c *Caller) CallOn(session *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout ...time.Duration) (*protocol.Packet, error) {
	resp, err := Call(c.ctx, session, pkt, respActionID, c.pickTimeout(timeout))
	c.lastErr = err
	return resp, err
}

// CallResync 向默认会话发起带 PrevAct 重同步重试的请求并返回完整应答与错误。
func (c *Caller) CallResync(pkt *protocol.Packet, respActionID uint32, timeout ...time.Duration) (*protocol.Packet, error) {
	resp, err := CallResync(c.ctx, c.session, pkt, respActionID, c.pickTimeout(timeout))
	c.lastErr = err
	return resp, err
}

// TryCall 发起请求并在失败时返回 nil，同时记录 LastErr() 供 AbortErr() 区分断线/取消与静默丢包。
func (c *Caller) TryCall(pkt *protocol.Packet, respActionID uint32, timeout ...time.Duration) *protocol.Packet {
	return c.TryCallOn(c.session, pkt, respActionID, timeout...)
}

// TryCallOn 向指定会话发起请求并在失败时返回 nil，同时记录 LastErr()。
func (c *Caller) TryCallOn(session *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout ...time.Duration) *protocol.Packet {
	resp, err := c.CallOn(session, pkt, respActionID, timeout...)
	if err != nil {
		return nil
	}
	return resp
}

// TryCallResync 发起带 PrevAct 重同步重试的请求并在失败时返回 nil，同时记录 LastErr()。
func (c *Caller) TryCallResync(pkt *protocol.Packet, respActionID uint32, timeout ...time.Duration) *protocol.Packet {
	resp, err := c.CallResync(pkt, respActionID, timeout...)
	if err != nil {
		return nil
	}
	return resp
}

// LastErr 返回最近一次调用产生的原始错误。
func (c *Caller) LastErr() error {
	return c.lastErr
}

// AbortErr 若最近一次调用因断线、会话关闭或上下文取消而失败，返回归一化后的终止错误；
// 若最近一次调用成功或仅超时（静默丢包），返回 nil。
func (c *Caller) AbortErr() error {
	err, _ := IsAbortErr(c.lastErr)
	return err
}
