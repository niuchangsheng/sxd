# 工程 Agent 协作规范 (Engineering Agent Guidelines)

## Core specifications

开发具体业务模块前，按触发条件查阅对应规范：

- **网络协议与编解码**：实现二进制封包、流式解压或报文单测时，查阅 [docs/protocol-spec.md](./docs/protocol-spec.md)。
- **元数据与数据字典**：操作 `data/Pieb.db` 静态表结构或 SQLite 仓储时，查阅 [docs/database-dictionary.md](./docs/database-dictionary.md)。
- **自动化玩法与调度**：实现 Routine 任务生命周期、卫语句、智能熔断与拟人抖动时，查阅 [docs/routine-spec.md](./docs/routine-spec.md)。
- **逆向探针与交互验证**：编写或复用脱机协议验证代码时，统一存放于 `test/probes/`，强制声明 `//go:build probe` 与结构化头注释，严禁硬编码敏感凭据。参见 [docs/adr/0013-recon-probe-isolation-and-test-harness.md](./docs/adr/0013-recon-probe-isolation-and-test-harness.md)。

## Agent skills

### Issue tracker

操作任务、缺陷或运行 `gh` 命令时，统一通过 GitHub Issues 驱动。参见 [docs/agents/issue-tracker.md](./docs/agents/issue-tracker.md)。

### Triage labels

分诊或流转 Issue/PR 状态时，遵循标准五大角色标签体系。参见 [docs/agents/triage-labels.md](./docs/agents/triage-labels.md)。

### Domain docs

命名领域实体或评估方案冲突时，遵循单上下文（single-context）规范，查阅 [CONTEXT.md](./CONTEXT.md) 与 [docs/adr/](./docs/adr/)。流程参见 [docs/agents/domain.md](./docs/agents/domain.md)。

### Architectural recon

行为异常、协议未明或实现存疑时，以原版 `C:\software\疯玩神仙道\pie.exe` 为基准事实（Ground Truth），使用 REA MCP 工具或 [reverse-engineer-anything](./.agents/skills/reverse-engineer-anything/SKILL.md) 技能进行逆向分析与行为对齐；期间编写的验证代码必须沉淀至 `test/probes/` 探针以便直接复用，已有成果参见 [docs/research/](./docs/research/)。
