package social

import (
	"context"

	"sxd-pie-ng/internal/client"
)

// SetStUnionSessionOpenerForTest 替换仙盟跨服会话建立逻辑，返回恢复函数。
func SetStUnionSessionOpenerForTest(fn func(context.Context, *client.RoleSession) (*client.RoleSession, error)) func() {
	old := openStUnionSession
	openStUnionSession = fn
	return func() { openStUnionSession = old }
}

// SetStUnionSectSessionOpenerForTest 替换仙盟城镇会话建立逻辑，返回恢复函数。
func SetStUnionSectSessionOpenerForTest(fn func(context.Context, *client.RoleSession) (*client.RoleSession, error)) func() {
	old := openStUnionSectSession
	openStUnionSectSession = fn
	return func() { openStUnionSectSession = old }
}
