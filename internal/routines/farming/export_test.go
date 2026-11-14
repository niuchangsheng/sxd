package farming

import (
	"context"

	"sxd-pie-ng/internal/client"
)

// SetCrystalMineSessionOpenerForTest 替换仙界跨服会话建立逻辑，返回恢复函数。
func SetCrystalMineSessionOpenerForTest(fn func(context.Context, *client.RoleSession) (*client.RoleSession, error)) func() {
	old := openCrystalMineSession
	openCrystalMineSession = fn
	return func() { openCrystalMineSession = old }
}
