# 13. 逆向协议探针隔离归档与脚手架规范

日期: 2026-10-11

## 状态

已通过 (Accepted)

## 上下文 (Context)

在对《神仙道》客户端进行逆向与脱机协议重构过程中，需要频繁编写针对具体模块发包与状态验证的试探性代码。若缺乏统一管理，这些代码容易在会话结束后丢失、硬编码真实账号凭据引发泄漏风险，或者因侵入生产目录污染主干编译与日常单元测试；同时，长期缺乏维护的探针易因底层接口重构而发生编译腐化（Bit-Rot）。

## 架构决策 (Decision)

我们确立以下 6 项探针工程化治理与复用规范：

1. **类型归属与物理隔离 (Taxonomy & Isolation)**：
   - 严格区分生产回归测试与逆向探针：离线无副作用的编解码校验直接编写为标准单测（`*_test.go`）；
   - 涉及实机通信、协议试探与特定业务联调的探索代码统一定名为“Recon Probe (协议逆向探针)”，统一归档于 `test/probes/` 目录；
   - 所有探针源码首行强制声明 `//go:build probe`，彻底与默认的 `go build ./...` 和 `go test ./...` 隔离。

2. **与既有脚本目录正交分工 (Directory Harmonization)**：
   - `scripts/probes/`：专项承载 Python 编写的离线 PE 脱壳、SQLite 导出与汇编静态分析工具；
   - `test/probes/`：专项承载 Go 语言编写的在线/离线协议验证探针与可复用交互用例。

3. **轻量脚手架复用 (Harness)**：
   - 在 `test/probes/harness.go`（同属 `//go:build probe`）提供轻量基础底座（如 `DialProbeSession`、`DumpHex`、`MustLoadConfig`），消除重复网络样板代码，同时避免强耦合生产复杂状态机。

4. **零凭据泄露防线 (Zero-Secret Invariant)**：
   - 探针严禁硬编码任何真实 Session、Token、密码或私密 IP；
   - 统一通过读取环境变量（如 `SXD_PROBE_SESSION`）或未纳管的本地配置（`configs/probe.local.yaml`，已被 `.gitignore` 忽略）注入凭据；Git 仅提交含占位符的配置模板。

5. **结构化元数据头注释 (Probe Header)**：
   - 每个探针文件顶部强制包含 4 项自解释元数据：目标协议编号与名称、对齐原版 `pie.exe` 模块、单行执行命令示例（如 `go run -tags=probe test/probes/xxx.go`）与预期表现特征。

6. **CI 静态编译门禁防腐化 (Bit-Rot Prevention)**：
   - 在本地检查与 CI 流程中引入轻量编译校验门禁：`go test -run=^$ -tags=probe ./test/probes/...`（纯静态语法与依赖编译，耗时 < 1 秒，无网络调用），确保底层协议变更时探针同步保持编译通过。

## 结果与影响 (Consequences)

### 正向收益
- 逆向探索成果持久化沉淀，杜绝每次重新编写验证脚本的时间浪费；
- 探针与生产发布 100% 隔离，保证构建纯净与单测高速；
- 统一脚手架与凭据注入机制彻底封死敏感凭据进入 Git 的安全隐患。

### 潜在代价
- 新建探针需遵循固定头注释与构建标签规范；
- 底层通用协议签名变更时，CI 编译门禁会提示同步更新探针调用点。
