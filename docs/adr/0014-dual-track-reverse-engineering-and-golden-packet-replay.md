# ADR 0014: 双轨交叉逆向与黄金报文回放策略 (Dual-Track Reverse Engineering and Golden Packet Replay)

- **状态**: Accepted
- **日期**: 2026-10-11
- **决策者**: @Ateng
- **相关规范**: [CONTEXT.md](../../CONTEXT.md), [docs/protocol-spec.md](../protocol-spec.md), [docs/research/pie_reverse_engineering_master_index.md](../research/pie_reverse_engineering_master_index.md)

## 上下文 (Context)

在重构《神仙道》脱机协议与自动化玩法的过程中，我们面临两个关键挑战：
1. 原版辅助（`pie.exe`）为经过加壳的 Delphi/PE 本地程序，虽然包含全套自动化逻辑，但其内部协议结构体定义被剥离了符号名，完全由本地汇编逆向推导字段语义认知成本高、容易产生歧义；
2. 官方微端（`sxd_wd_*.exe`）基于 Flash 容器封装，内部包含明文命名完整的 ActionScript 3 (AS3) 协议类库，但本身不具备辅助自动化调度的行为时序。
3. 如果每次修改协议解析器都要连接外部真实游戏服务器做测试，不仅受制于网络环境和账号凭据时效，还存在触碰反外挂策略封号的风险。

## 决策 (Decision)

我们决定采取 **“双轨交叉逆向 (Dual-Track Reverse Engineering)”** 与 **“黄金报文回放 (Golden Packet Replay)”** 核心策略：

1. **双轨交叉逆向原则**：
   - **协议数据契约（权威字典）**：以官方微端反编译输出的 AS3 结构体为基准，提取官方结构命名（如 `Mod_Player_Base`）、字段类型与 Action 枚举，并在 Go 代码库 `internal/protocol/` 中按官方符号镜像命名。
   - **自动化状态机与时序**：以原版辅助 `pie.exe` 结合双端真实抓包数据差分比对（`Capture Diff Analysis`）为基准，提取自动化调度的发包先后顺序与参数填充规则。

2. **黄金报文回放测试原则**：
   - 优先从 `data/captures/*.pcapng` 提取真实捕获的高质量 TCP 封包（Golden Packets），构建脱机离线单元测试，验证 Go 协议编解码器与解压缩逻辑。
   - 实机动态交互探针统一收拢在 `test/probes/`，仅在必要时通过脱敏的环境变量或本地 `.env.local` 直注有效凭据，杜绝明文硬编码。

3. **官方基准仲裁原则 (Official Baseline Arbiter & Drift Defense)**：
   - 充分认识到原版辅助（`pie.exe`）为历史沉淀代码，部分玩法与协议随着官方页游版本迭代可能已发生**协议漂移（Protocol Drift）**，导致原版辅助中存在陈旧、失效甚至错误的指令调用。
   - **仲裁底线**：当原版辅助的逆向逻辑与官方微端真实抓包/SWF 源码冲突时，**100% 强制以官方微端流量与代码作为唯一基准事实（Ground Truth）**；原版辅助逻辑仅作为玩法调度的“启发式参考”，严禁盲目迷信原版。

4. **逆向资产隔离归档原则**：
   - 提取的数百兆大型反编译 SWF/源码归入 `data/decompiled/`，通过 `.gitignore` 排除，防止主 Git 代码库膨胀。
   - 所有结构化逆向分析结论与协议白皮书统一沉淀至 `docs/research/` 与 `docs/protocol-spec.md`。

## 影响 (Consequences)

- **正面影响**：
  - 彻底规避了 Delphi 汇编中“盲猜协议字段含义”的试错成本；
  - 协议单元测试 100% 具备确定性、脱机可复现、毫秒级运行；
  - Git 仓库体积保持轻量干净。
- **负面影响/代价**：
  - 前期需要编写或准备针对 Flash SWF 的轻量解包与提取工具链。
