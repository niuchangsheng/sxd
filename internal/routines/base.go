// Package routines 提供对齐原版辅助 30+ 自动化玩法的六大业务子域实现矩阵。
//
// @author Ateng
// @since 2026-10-08
package routines

import (
	"sxd-pie-ng/internal/scheduler"
)

// BaseRoutine 为 scheduler.BaseRoutine 的别名，向后兼容存量引用。
type BaseRoutine = scheduler.BaseRoutine

// NewBaseRoutine 构造新的标准化 Routine 实例。
var NewBaseRoutine = scheduler.NewBaseRoutine
