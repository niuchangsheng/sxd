# 神仙道助手重构工程 (sxd-pie-ng) 领域上下文

本项目是对《神仙道》传统桌面挂机辅助（Pie 助手）的现代化重构，采用 Go 语言实现脱机协议通信、自动化日常调度与内嵌 Web 管理。

## 统一术语表 (Language)

**Platform Account (平台账号)**:
游戏运营方平台的通行证凭据（如心动、疯玩等），用于向平台认证网关换取短期有时效性的登录凭据（Token / Session Key）。
_避免使用_: User, Member, LoginID

**Role Session (角色会话)**:
针对特定区服下某个具体游戏角色的在线运行时连接，维护独立的 TCP Socket 长连接、登录态、心跳保活与状态机。
_避免使用_: Game Connection, Player Instance

**Packet (数据封包)**:
客户端与游戏网关服务器之间通过 TCP 传输的二进制协议数据包，由固定包头（长度、消息号）与载荷（Payload）组成。
_避免使用_: Message Frame, Socket Data

**Protocol Codec (协议编解码器)**:
负责封包二进制序列化、反序列化、大端序解析及透明 zlib 流解压缩的底层协议引擎。
_避免使用_: Parser, Converter

**Activity Routine (日常活动任务)**:
游戏中具体某类玩法的自动化执行单元（如药园种植、帮派吉星高照、取经护送、竞技场等）。
_避免使用_: Job, Action, Automation

**Jitter Scheduler (拟人抖动调度器)**:
负责协调各活动任务执行时机、注入 1~3 秒动态随机延迟与操作抖动的核心防封调度引擎。
_避免使用_: Timer, Cron, Task Manager

**Web Console (内嵌 Web 控制台)**:
通过 Go 内嵌文件系统（`go:embed`）与二进制打包在一起的本地 Web 仪表盘，供用户在浏览器中配置账号与监视日志。
_避免使用_: GUI, Client UI, Dashboard

**Pieb SQLite Engine (游戏字典引擎)**:
基于纯 Go SQLite 驱动挂载原版 `Pieb.db`，提供道具、伙伴、怪物、NPC 等游戏静态元数据的统一只读数据字典仓储。
_避免使用_: Game Database, Local Cache

**QA Bank (题库问答引擎)**:
以内存哈希与模糊容错相似度索引加载并检索 `answers.txt` / `scra.ini` 的答题系统，为仙履奇缘、金榜题名等提供毫秒级自动答题决策。
_避免使用_: Quiz Solver, Auto Answer

**Activity Matrix (全量活动玩法矩阵)**:
对齐原版 `01.ini` 与 `pieb.ini` 的 30+ 细分玩法模块化注册表与状态驱动生命周期骨架。
_避免使用_: Plugins, Feature List

**Dual-Mode Config (双模配置体系)**:
同时支持“一键推荐预设”与“专家精细微调”的分层配置模型，兼顾开箱即用与深度定制。
_避免使用_: Simple/Advanced Settings

**Dual-Track Scheduler (双轨复合调度器)**:
结合定时批处理（Cron，对齐 00:00/06:00 等整点重置）与短周期自驱巡检（Loop），并由统一拟人抖动管道驱动的复合任务执行引擎。
_避免使用_: Cron Job, Simple Loop

**Domain Routine Subpackages (六大玩法领域包)**:
将 30+ 玩法按业务内聚性切分的六大子域：日常签到 (`daily`)、农场取经 (`farming`)、帮派社交 (`social`)、副本推图 (`dungeon`)、竞技对抗 (`pvp`) 与益智小游戏 (`minigame`)。
_避免使用_: Feature Modules, Task Folders

**Three-Stage QA Funnel (三级题库漏斗检索)**:
由文本标点归一化查表、实体关键词倒排交集、Levenshtein 编辑距离相似度兜底构成的三级答题检索算法。
_避免使用_: Text Matcher, Regex Solver

**Semantic Cooling-off (语义智能熔断)**:
当玩法执行捕获到服务器确定性的业务状态（如次数耗尽、体力不足）时，自动将该任务挂起至下一个恢复时间窗口（如次日 00:00 或体力恢复点）的非破坏性降级保护机制。
_避免使用_: Circuit Breaker, Error Retry, Task Disable

**Ticket Ingress (凭据直注)**:
从外部配置文件（如 `user.ini`）或自动化换票接口直接加载短期鉴权凭据以建立角色会话的快速接入机制。
_避免使用_: Bypass Auth, Mock Token, Token Injection

**Safety Probe (安全试探模式)**:
以最小单次资源消耗（如 5 点体力）验证协议交互与状态同步闭环的防御性自检机制。
_避免使用_: Dry Run, Mock Test, Test Run

**Dual-Track Ticket (双轨凭据)**:
区分游戏本服主网关会话鉴权与跨服玩法业务鉴权的双层复合数字凭据契约。
_避免使用_: Single Token, Auth Cookie, Login Secret

**Platform Authenticator (平台认证器)**:
封装针对具体运营平台 Web 协议的自动化身份认证、单点登录与票据换取组件。
_避免使用_: Web Scraper, Login Bot, Crawler

**Multi-Gateway Topology (多网关拓扑)**:
区分主服主长连接与跨服业务（仙界、圣域）长连接的分层网络拓扑，主服主干长连优先，跨服按需惰性建立。
_避免使用_: Multiple Sockets, Connection Multiplex

**Gateway Resolution (网关地址解析)**:
结合已知平台区服静态字典映射与用户自定义配置覆盖的真实 TCP Socket 目标寻址策略。
_避免使用_: IP Resolver, Server Routing

**Autonomous Ingress (全自治凭据流水线)**:
纯 Go 语言在内存中执行多阶段平台 Web 认证、跨域重定向与 Cookie 嗅探，彻底脱离外部执行档与文件的自治接入管道。
_避免使用_: Third-Party Bot, External Ingress

**Self-Healing Session (自愈会话状态机)**:
在网络 EOF 闪断或服务端凭证过期时，由状态机自动触发原生换票、地址重解析与无缝重连的长连接韧性架构。
_避免使用_: Auto Reconnect, Simple Retry

**Recon Probe (协议逆向探针)**:
在 `test/probes/` 目录下通过 `//go:build probe` 构建标签物理与逻辑隔离的独立协议验证脚本，用于以最小闭环复现特定协议交互与原版对齐验证，且对主干构建与标准单测零污染。
_避免使用_: Test Script, Debug Code, Scratch Code, Hack Script

**Official Micro-Client (官方微端)**:
官方基于 Flash / ActiveX 封装的桌面独立运行客户端（如 `sxd_wd_*.exe`），由 Web 页面登录选区认证与底层 TCP 8381 游戏长连接双阶段网络通信组成。
_避免使用_: Official Launcher, Web App, Desktop Client

**Capture Diff Analysis (抓包差分对比)**:
通过结构化比对官方微端与原版辅助在同一玩法或生命周期下的 TCP 二进制封包时序（Module/Action/Payload），逆向提取神仙道私有协议字段语义与原版对齐特征的工程化验证方法。
_避免使用_: Packet Compare, Log Diff, Wireshark Comparison

**Dual-Track Reverse Engineering (双轨交叉逆向)**:
以官方微端 ActionScript/SWF 反编译确立权威协议结构字典，并结合原版辅助运行时反编译与抓包差分推导自动化玩法状态机时序的联合逆向方法。
_避免使用_: Hybrid Reversing, Double Reversing

**Golden Packet Replay (黄金报文回放测试)**:
从真实微端与辅助抓包文件（`.pcapng`）中提取确定性业务载荷，并在脱机无网络状态下作为只读单测用例驱动协议编解码器与状态机断言的零风险测试机制。
_避免使用_: Packet Mocking, Offline Test, Wireshark Replay

**Protocol Drift (协议漂移/版本失效)**:
由于页游服务端长期更新迭代，原版辅助内置的旧版发包逻辑、Action 编号或 Payload 字段与当前官方游戏实际运行逻辑不一致、导致报错或失效的现象。
_避免使用_: Outdated Logic, Buggy Code, Broken Action

**Official Baseline Arbiter (官方基准仲裁)**:
当原版辅助行为与官方实际行为冲突或原版辅助失效时，强制以官方微端反编译（AS3 契约）及其实际网络抓包流量作为唯一基准事实（Ground Truth）进行校准和重构的仲裁原则。
_避免使用_: Official Priority, Bug Override, Force Alignment

## 权威规范与研发技术文档索引 (Specifications)

- **[协议规范白皮书 (Protocol Specification)](docs/protocol-spec.md)**:
  定义大端序 `[4B Length] + [2B Module] + [2B Action] + Payload` 物理帧结构、类型编解码规范、透明 zlib 解压契约，并包含 `sxd.pcapng` 抓包真实黄金报文用例。
- **[本地元数据库全量数据字典 (Database Dictionary)](docs/database-dictionary.md)**:
  定义 `data/Pieb.db` 37 张数据表全量分类索引、实体关系图 (ERD) 与纯 Go SQLite 只读接入规范。
- **[玩法子系统与规则白皮书 (Routine Specification)](docs/routine-spec.md)**:
  全面对齐原版 `pieb.ini`（23 个核心子系统）与 `01.ini`，规范六大领域包的任务状态机、卫语句检查与发包时序。
- **[逆向侦察与分析报告合集 (Research & Reconnaissance)](docs/research/pie_reverse_engineering_master_index.md)**:
  包含原版 `pie.exe` 的 PE 架构侦察、双重嵌套加壳脱壳指南、数据模型推导与网络通信逆向成果。
- **[架构决策记录 (ADR)](docs/adr/)**:
  涵盖从 ADR 0001 至 ADR 0014 的完整架构演进决策。
  - [ADR 0014: 双轨交叉逆向与黄金报文回放策略](docs/adr/0014-dual-track-reverse-engineering-and-golden-packet-replay.md)





