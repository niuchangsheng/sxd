# 系统架构与详细设计文档 (Design Document)

> 本文档面向研发与维护人员，描述 `sxd-auto` 完成 Phase 0~4 工程化重构与 2026 官方静态字典重建后的整体架构、核心状态机、并发模型、协议与调度原语以及扩展规范。

---

## 1. 系统总体架构

`sxd-auto` 采用**单进程多角色隔离架构**：每个配置的角色在进程内拥有独立的网络长连接会话（`RoleSession`）与独立的事件驱动调度器（`RoleScheduler`），共享只读的官方静态数据字典仓储（`data/gamedata.db`）、问答引擎（`qa.Engine`）与内嵌 Web 管理面（`web.Server`）。

![sxd-auto 系统总体架构图](assets/architecture.svg)

### 1.1 核心设计原则
1. **单角色故障隔离**：每个角色独立持有 `RoleSession`、`RoleScheduler` 与 `DropTracker`，任一区服断线、超时或静默丢包不会阻塞或污染其他区服。
2. **零外部运行时依赖**：前端静态资源通过 `go:embed` 编入二进制，SQLite 使用纯 Go 驱动挂载 `data/gamedata.db`，单二进制即可部署。
3. **协议精确对齐与回包解析铁律（ADR-0015）**：严禁以“未超时”判定业务成功，所有业务请求必须解析结果码并按失败语义驱动熔断/冷却。
4. **元宝安全红线**：自动化默认只消耗免费次数、铜钱、道缘等常规资源；所有涉及元宝的入口（清冷却、买次数、高级抽奖、元宝培养等）默认禁止调用。

---

## 2. 分层模块职责

### 2.1 网络会话与传输层 (`internal/client` + `internal/platform`)
经过职责拆分与连接层加固后，`internal/client` 按「传输层 / 会话状态机 / 派生子会话 / 观测指标 / 领域封装」清晰分层：

| 文件 | 核心职责 |
| :--- | :--- |
| [transport.go](file:///home/chang/sxd/internal/client/transport.go) | 单代 TCP 连接传输对象 `connTransport`：独占 `net.Conn`、写锁与 10s 写超时（写错/超时立即断链防帧错乱）、16KB `bufio.Reader` 读循环、本代 `pendingCalls` 路由表与连接级 `ctx/cancel`；断线时整对象销毁并排空等待通道，从结构上杜绝跨连接串包 |
| [session.go](file:///home/chang/sxd/internal/client/session.go) | `RoleSession` 生命周期状态机（`Disconnected`/`Connecting`/`Authenticating`/`Active`/`Reconnecting`/`Closed`）、`AwaitActive` 事件等待、`cfgMu` 并发安全配置快照与串行换票、主动重建（`EnsureFreshConnection` + `1.5s` 网关释放缓冲）、`Close`/`onBeforeClose` 离城时序与 `0/6` 半开僵尸连接活性探针（`StaleProbeInterval`） |
| [child_session.go](file:///home/chang/sxd/internal/client/child_session.go) | 统一派生子会话工厂 `openChildSession(lifeCtx, waitCtx, ...)`：分离生命周期上下文与激活等待超时，统一处理 `AwaitActive`、失败回收与关闭主服半开探针 |
| [metrics.go](file:///home/chang/sxd/internal/client/metrics.go) | 线程安全连接健康度快照 `SessionMetrics`（连接代数、存活/收包间隔、被动断线、主动重建、握手失败、半开探针、跨服保活状态与最近断线原因），供 Web `/api/roles` 透出 |
| [prevact.go](file:///home/chang/sxd/internal/client/prevact.go) | 并发安全的 `PrevActChain` 状态机：统一管理业务尾链锚点、失步标记（`chainDirty`）、最近 32 条收包环形诊断缓冲与载荷覆写 |
| [player_state.go](file:///home/chang/sxd/internal/client/player_state.go) | `PlayerState` 资产快照、下行属性推送解析（`update_player_data`）、`FetchLatestPlayerInfo` 与 `SyncQuests` |
| [gateway_auth.go](file:///home/chang/sxd/internal/client/gateway_auth.go) | `DefaultGatewayAuthenticator` 四步登录握手与全量角色资产初始化；监听连接级 `connCtx` 实现握手 EOF 秒级快速失败 |
| [town_practice.go](file:///home/chang/sxd/internal/client/town_practice.go) | 主城切换（`EnterTown`/`ReturnToTown`）与城镇打坐练功协议封装 |
| [beelzebub_session.go](file:///home/chang/sxd/internal/client/beelzebub_session.go) / [beelzebub_trials.go](file:///home/chang/sxd/internal/client/beelzebub_trials.go) | 魔王试炼（Mod 57）会话封装、组队与自动挑战编排 |
| [tower_actions.go](file:///home/chang/sxd/internal/client/tower_actions.go) | 六道轮回塔（Mod 32）与背包物品出售封装 |
| [cross_server.go](file:///home/chang/sxd/internal/client/cross_server.go) / [saint_area.go](file:///home/chang/sxd/internal/client/saint_area.go) / [sect_town.go](file:///home/chang/sxd/internal/client/sect_town.go) / [server_chat_room.go](file:///home/chang/sxd/internal/client/server_chat_room.go) | 仙界常驻保活辅助连接（`94/0` + `6/12`，不进城，抵御 5 分钟墙钟扫描回收）、仙界城镇（`onBeforeClose` 发送 `95/2 leave_town`，`onAfterClose` 同步短超时恢复常驻辅助连接）、圣域（含 `sa_dragon_area` Module 362）、仙盟城镇与独立聊天服子会话 |
| [mock_server.go](file:///home/chang/sxd/internal/client/mock_server.go) | 供离线调试、沙箱与单元测试使用的虚拟协议服务端 `runMockGameServer` |
| [cache.go](file:///home/chang/sxd/internal/platform/cache.go) | `TicketCache` 按 `platform:username` 维度的账号级换票门闩（`accountLoginGate`）与最小换票间隔（默认 3s）：不同账号并发换票互不阻塞，同账号多区服运行期严格串行防互顶 |

### 2.2 协议编解码与 PrevAct 链状态机 (`internal/protocol` + `internal/client/prevact.go`)
1. **帧格式与流式解压**：
   - 物理帧固定 8 字节大端头：`[4B Length][2B ModuleID][2B ActionID]` + 变长 Payload。
   - 下行 Payload 若首字节为 `0x78`（zlib CMF 魔数），由 `DecompressPayloadIfNeeded` 自动尝试透明解压，解压失败时安全降级为原始字节。
2. **统一 `PrevActMode` 契约**（[packet.go](file:///home/chang/sxd/internal/protocol/packet.go)）：
   - `PrevActNone`：不携带尾链（保留历史 Mod 3/4/32 兼容兜底）。
   - `PrevActAutoBusiness`：发送瞬间由 `PrevActChain.StampPayload` 自动填充最新业务动作锚点（兼容旧 `HasPrevAct` / `PrevActFromLastSent`）。
   - `PrevActExplicit`：使用调用方显式写入的锚点，发送层**绝不覆盖**（用于重同步重试与特定跨模块锚点）。
3. **链污染自愈与重同步**：
   - 心跳包（`0x0000004B`）与服务端异步推送不污染 `lastBusinessActionID`。
   - 当带尾链请求超时（疑似被服务端静默丢弃），`Call()` 标记 `chainDirty = true`，后续请求自动回退到服务端最后确认动作 `lastAckActionID`，直至收到新业务回包清除脏标记。
   - `CallWithPrevActResync` 仅在 `context.DeadlineExceeded` 且上层未取消时，使用 `PrevActExplicit` 以最新锚点重试一次；遇到断线/关闭/取消直接透传错误。
4. **模块化协议文件**：Phase 4 已将原超千行的 `feature_packets.go` 与 `activity_expansion.go` 拆分为按游戏模块独立的协议文件（`online_gift.go`、`jieyuan_box.go`、`caravan.go`、`demon_tower.go`、`treasure_house.go`、`bailian.go`、`partner_practice.go`、`era_creation.go`、`cultivation_card.go` 等），全目录无超千行巨文件。

### 2.3 事件驱动调度器与横切中间件 (`internal/scheduler`)
```mermaid
stateDiagram-v2
    [*] --> Idle: 计算最近到期任务 wait
    Idle --> Picking: Timer 到期 / wakeCh 唤醒
    Picking --> SessionCheck: 选取最高优先级就绪任务
    SessionCheck --> Idle: 会话未激活 (退避等待)
    SessionCheck --> Middleware: 会话 Active
    Middleware --> Execute: 前置 FunctionID 探测 + 注入 DropTracker
    Execute --> PostTown: defer WithReturnToTown()
    PostTown --> Classify: 错误语义分类
    Classify --> Cooling: CooldownError / ErrAttemptsExhausted / ErrBagFull
    Classify --> ShortRetry: ErrNotConnected (90s)
    Classify --> SilentBackoff: DeadlineExceeded (连续 3 次 → min(次日 00:05, 6h))
    Classify --> Done: nil (清零该任务丢包计数)
    Cooling --> Idle
    ShortRetry --> Idle
    SilentBackoff --> Idle
    Done --> Idle
```

- **零忙轮询事件驱动**（[scheduler.go](file:///home/chang/sxd/internal/scheduler/scheduler.go)）：`pickNextReadyOrWait()` 在锁内计算最高优先级可执行项或精确等待时长，通过 `time.NewTimer(wait)` 休眠；会话未激活时直接阻塞在事件驱动的 `session.AwaitActive(ctx)`（零轮询、重连恢复瞬间拉起）；`Register`、`SetRoutineEnabled(..., true)`、`TriggerBatch` 通过非阻塞 `wakeCh` 立即唤醒调度循环。
- **语义化优先级阶段**（[routine.go](file:///home/chang/sxd/internal/scheduler/routine.go)）：禁止魔数优先级，统一使用 `PhasePriority(phase, offset)`：
  - `PhaseSystem (500)`：会话维护（`cross_keepalive` 510 常驻跨服保活）
  - `PhaseTownSensitive (420)`：对城镇上下文极敏感的玩法（天帝宝库、灵域猎妖）
  - `PhaseEarlyBattle (350)`：长会话后段易丢包的战斗/养成（五行天宫、无量劫海、飞升、魔界远征、无尽洞天等）
  - `PhaseCoreLoop (300)`：核心体力与循环链路，严格分层为 **主支线/精英首通推图 `dungeon` (300) > 通天塔 `tong_tian_tower` (290) > 精英副本扫荡 `dungeon_sweep` (280)**
  - `PhaseEarlyWindow (200)`：早间窗口（结缘宝箱等）
  - `PhaseHighPriority (100)` / `PhaseStandard (50)` / `PhaseCleanup (10)`：常规日常（含圣域无尽龙域 `sa_dragon_area`）与收尾清理
- **声明式横切选项**：
  - `WithReturnToTown(timeout...)`：玩法退出时自动回城，避免战斗场景残留污染后续玩法；
  - `WithFunctionID(id)`：执行前原生调用 `check_function_open`（Mod 0, Act 69）；
  - `WithSilentDropPolicy(threshold, retry)`：定制静默丢包退避阈值与短重试周期。
- **实例隔离 `DropTracker`**（[dropbackoff.go](file:///home/chang/sxd/internal/scheduler/dropbackoff.go)）：每个 `RoleScheduler` 独立持有 `drops *DropTracker` 并通过 `WithDropTracker(ctx, drops)` 注入上下文；玩法侧统一使用 `HandleSilentDropCtx` / `ResetSilentDropsCtx` / `NoteSilentDropCtx` / `SilentDropCountCtx`，彻底消除进程级全局状态串扰。

### 2.4 玩法调用原语与 7 大领域包 (`internal/routines`)
- **统一调用原语**（[caller.go](file:///home/chang/sxd/internal/routines/kit/caller.go)）：
  - `kit.NewCaller(ctx, session, defaultTimeout)` 统一管理超时上下文并保留 `LastErr()`；
  - `caller.AbortErr()` 与 `kit.IsAbortErr(err)` 将 `ErrNotConnected`、`ErrSessionClosed`、`context.Canceled` 统一识别为必须立即中止本轮的终止错误，防止断线被误当静默丢包吞掉；
  - `kit.Sleep(ctx, d)` 取代裸 `time.Sleep`，支持 `SIGINT` 毫秒级退出；
  - `kit.WithTimeoutOverride(ctx, d)` 允许单测将 8~45 秒业务超时压缩到毫秒级。
- **7 大领域包与注册工厂**（[registry.go](file:///home/chang/sxd/internal/routines/registry.go)）：
  - `system`（1 项：`cross_keepalive`）、`daily`、`dungeon`（含拆分后的 `dungeon` / `tong_tian_tower` / `dungeon_sweep` 与圣域无尽龙域 `sa_dragon_area`）、`farming`、`minigame`、`pvp`、`social` 共 **123 个例程**，全部按单玩法/单业务域拆分为内聚文件。

### 2.5 官方静态数据层 (`data/gamedata.db`)、自动构建管线与题库 (`internal/qa`)
- `internal/dictionary` 以只读模式挂载 `data/gamedata.db`（带进程级 `Default()` 共享与热点表内存缓存），作为全系统唯一的官方 SWF 静态数据层：为主线推图（任务/NPC/关卡锁/怪物编队）、精英扫荡与背包清理（`item`）、宗师修行（`roletype`）、天帝宝库自选大奖（`wishpoolaward`）、NPC 结交问答（`NPCQuest`）、魔王试炼 Boss 元数据（`beelzebub_boss`）及主角法相祈愿奖励（`faxiang_pray_award`）提供统一查询，彻底消除了代码中散落的 SWF 硬编码补丁。
- [rebuild_dict.py](file:///home/chang/sxd/scripts/rebuild_dict.py) 内建纯 Python AVM2 DoABC 字节码求值器，可按官方 CDN `cur_ver` 自动下载最新 `Index.swf` / `Main.swf` / `templet.swf` 并**从零构建**全部 35 张官方静态表（当前对齐 `2026112001`，共 20,126 行），同时写入 `_rebuild_meta` 追踪版本。
- `internal/qa` 启动时预加载 5,719 道题库，服务仙旅奇缘与金榜题名自动答题。

---

## 3. 工程质量与扩展约定

新增玩法或协议时必须遵守：
1. **协议层**：明确声明 `PrevActMode`，解析器先校验结果码再读业务字段；
2. **玩法层**：使用 `kit.NewCaller` + `kit.Sleep` + `PhasePriority` + `*Ctx` 丢包接口；战斗/跨场景玩法声明 `scheduler.WithReturnToTown()`；
3. **测试层**：覆盖成功路径与至少一个失败码路径，通过 `go vet ./...` 与 `go test -race ./...`；
4. **逆向探针**：统一存放于 `test/probes/`，首行强制声明 `//go:build probe`，禁止硬编码真实凭据（ADR-0013）。
