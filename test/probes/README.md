# 协议逆向探针目录 (Recon Probes)

本目录归档在逆向与重构过程中编写的脱机协议验证脚本，用于以最小闭环复现真实协议交互与原版对齐，便于后续直接复用。

详见架构决策记录 [docs/adr/0013-recon-probe-isolation-and-test-harness.md](../../docs/adr/0013-recon-probe-isolation-and-test-harness.md)。

## 核心规约

1. **构建标签隔离**：所有探针源码首行必须声明 `//go:build probe`，确保默认的 `go build ./...` 和 `go test ./...` 零污染。
2. **零凭据泄露**：严禁硬编码真实 Token、账号或私密 IP。通过环境变量（如 `SXD_PROBE_SESSION`、`SXD_PROBE_SERVER`）或未纳管本地配置注入。
3. **结构化头注释**：每个探针文件顶部必须包含元数据注释（目标协议、原版对齐基准、执行命令、预期表现）。

## 探针模板示例

```go
//go:build probe

// 目标协议: [Module 0, Action 0] player_login (玩家登录验证)
// 基准对齐: 对齐原版 pie.exe 登录握手与角色列表同步
// 运行命令: go run -tags=probe test/probes/login/main.go
// 预期表现: 输出服务端下发的状态码 0，并打印角色名称与 ID
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	server := os.Getenv("SXD_PROBE_SERVER")
	sessionToken := os.Getenv("SXD_PROBE_SESSION")
	if server == "" || sessionToken == "" {
		fmt.Println("请设置环境变量 SXD_PROBE_SERVER 与 SXD_PROBE_SESSION")
		return
	}
	// 执行探索验证发包...
}
```

## CI 编译防腐化检查

本地与 CI 仅执行静态语法与依赖编译检查（零网络开销）：

```bash
go test -run=^$ -tags=probe ./test/probes/...
```
