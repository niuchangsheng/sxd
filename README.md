# sxd-auto (神仙道自动化助手)

<p align="center">
  <strong>基于 Go 语言的高性能、脱机纯协议通信、事件驱动拟人防封调度与内嵌 Web 控制台的现代化神仙道自动化助手</strong>
</p>

<p align="center">
  <a href="https://golang.org/">
    <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go Version" />
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square" alt="License" />
  </a>
  <a href="./CONTRIBUTING.md">
    <img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat-square" alt="PRs Welcome" />
  </a>
</p>

---

## 📖 项目简介

`sxd-auto` 是《神仙道》的现代化 **Go 语言** 脱机自动化助手。无需启动 Flash 或浏览器渲染环境，直接通过原生 TCP 二进制长连接与主服、跨服、圣域、仙盟城镇及独立聊天服交互，覆盖 **7 大领域、113 个自动化玩法例程**，并内置严格的不消耗元宝安全红线、回包结果码解析铁律与官方 SWF 静态字典自动重建工具。

---

## ✨ 核心特性

- ⚡ **轻量高并发多区服**：单进程低内存常驻，支持多账号、多区服并发运行；每角色独占独立 `RoleSession` 与事件驱动 `RoleScheduler`，换票阶段串行防风控。
- 🧩 **精准二进制协议与 PrevAct 链状态机**：完整还原 `[4B Length] + [2B Module] + [2B Action] + Payload` 线格式与 `0x78` 透明 zlib 流解压；由 `PrevActMode` + `PrevActChain` 统一维护业务尾链、链污染回退与超时重同步。
- 🛡️ **事件驱动拟人调度与横切中间件**：
  - 基于 `time.NewTimer` + `wakeCh` 的零忙轮询事件驱动调度器，按 `RoutinePhase` 语义阶段有序执行并注入可配置拟人抖动（默认 1~3 秒）；
  - 内置声明式生命周期中间件（`WithReturnToTown` / `WithFunctionID` / `WithSilentDropPolicy`）与按调度器实例隔离的 `DropTracker` 静默丢包退避；
  - 全部玩法统一使用 `internal/routines/kit`（`kit.Caller` / `kit.Sleep`），断线与取消信号毫秒级响应、绝不吞错。
- 🔒 **元宝安全红线 & 回包解析铁律**：所有默认自动化链路仅消费免费次数/铜钱/道缘等游戏内常规资源，严禁静默消费元宝；所有业务请求必须解析服务端结果码并按失败语义熔断或冷却（ADR-0015）。
- 📚 **官方 SWF 同步字典与智能题库**：通过 `scripts/rebuild_dict.py` 从官方最新客户端 SWF（`2026112001`）从零反编译构建 `data/gamedata.db`（35 张官方静态表、20,126 行），并内置 5,719 题智能问答引擎。
- 🌐 **内嵌 Web 控制台**：基于 `go:embed` 打包零外部依赖单文件，提供角色状态监视、按角色/全局玩法动态开关、预设策略切换与实时日志流。

---

## 📚 文档导航

| 文档 | 说明 |
| :--- | :--- |
| [架构与详细设计文档](docs/design.md) | 系统分层架构、核心状态机（`RoleSession` / `PrevActChain` / 事件驱动调度器）、`kit` 原语与 `gamedata.db` 静态数据层职责 |
| [用户使用手册](docs/user-manual.md) | 快速上手、配置文件详解、多区服覆盖、玩法策略调优、Web 控制台操作与 FAQ |
| [运维部署与排障手册](docs/operations.md) | 生产部署（`systemd`）、日志轮转、监控与 HTTP API、凭据续期、常见故障排查与 CI 校验 |
| [网络协议规范白皮书](docs/protocol-spec.md) | 二进制帧格式、编解码契约、回包解析铁律与黄金报文 |
| [玩法自动化规范白皮书](docs/routine-spec.md) | 任务状态机、语义智能熔断、连续丢包退避与工程原语规范 |
| [数据字典规范](docs/database-dictionary.md) | `data/gamedata.db` 官方静态表结构说明 |
| [架构决策记录 (ADR)](docs/adr/) | 关键工程决策与演进历史 |

---

## 🏛️ 目录结构

```text
sxd-auto/
├── cmd/
│   ├── server/                 # 主服务端入口 (启动编排、多角色舰队、Web 控制台与优雅停机)
│   └── probe/                  # 脱机协议探针 CLI 工具
├── internal/
│   ├── protocol/               # 二进制帧编解码、PrevActMode 与按模块拆分的协议封包库
│   ├── client/                 # RoleSession 长连接状态机、PrevActChain、网关认证、多服子会话与离线 Mock
│   ├── scheduler/              # 事件驱动 RoleScheduler、RoutinePhase 优先级、横切中间件与 DropTracker
│   ├── routines/
│   │   ├── kit/                # 统一协议调用器 (Caller)、AbortErr 透传、可中断 Sleep 与测试超时压缩
│   │   ├── system/             # 会话保活与定时刷新 (session_refresh)
│   │   ├── daily/              # 日常签到、小助手、邮件、打坐、飞升、修真境界等
│   │   ├── dungeon/            # 主线推图、精英/英雄扫荡、背包清理、六道、伏魔、魔王试炼等
│   │   ├── farming/            # 药园、取经、矿山、淬炼池、炼宝炉、壶中界、符文等
│   │   ├── minigame/           # 天帝宝库、宗师修行、吉星高照、钓鱼、五行天宫、法相等
│   │   ├── pvp/                # 本服/仙界/圣域竞技场、神魔大战、劫镖、世界 Boss、跨服天梯
│   │   └── social/             # 帮派、仙盟、圣盟、灵域猎妖、无量劫海、无尽洞天、魔界远征等
│   ├── platform/               # 心动等平台全自治 Web 换票驱动与浏览器辅助登录
│   ├── dictionary/             # data/gamedata.db SQLite 官方静态元数据仓储
│   ├── qa/                     # 仙旅奇缘 / 金榜题名智能题库匹配引擎
│   ├── config/                 # YAML 配置加载、环境变量展开与多区服深度合并覆盖
│   ├── logging/                # 按大小与时间双维度轮转的本地日志组件
│   └── web/                    # go:embed 内嵌 Web 控制台与管理 REST API
├── configs/                    # 配置模板 (config.example.yaml)
├── data/                       # 官方静态数据字典 (gamedata.db，由官方 SWF 从零构建)
├── docs/                       # 设计文档、用户手册、运维手册、协议/玩法规范与 ADR
├── scripts/                    # 官方 SWF 字典重建 (rebuild_dict.py)、提交与发版脚本
├── test/probes/                # 隔离的脱机逆向探针脚本 (强制 //go:build probe)
└── .github/workflows/          # GitHub Actions CI (go vet + go test -race) 与自动化发版
```

---

## 🛠️ 快速开始

### 运行环境
- **Go**: 1.26+（参见 [go.mod](./go.mod)）
- **数据字典**: 仓库内置基于 `2026112001` 从零构建的 `data/gamedata.db`

### 编译与运行

```bash
# 1. 复制并填写配置文件（将账号/密码或 Cookie 填入 configs/config.yaml）
cp configs/config.example.yaml configs/config.yaml

# 2. 编译主程序
go build -o bin/sxd-auto ./cmd/server

# 3. 启动运行（打开 http://127.0.0.1:8080 查看控制台）
./bin/sxd-auto --config configs/config.yaml
```

常用命令行参数：

```bash
./bin/sxd-auto -v                                      # 查看版本与构建信息
./bin/sxd-auto --config configs/config.yaml            # 指定配置文件启动
./bin/sxd-auto --config configs/config.yaml --debug    # 开启 Debug 日志输出
./bin/sxd-auto --config configs/config.yaml --browser-login  # 弹出浏览器捕获/刷新平台 Cookie
```

### 同步最新官方游戏静态字典

```bash
# 1. 查看线上最新 cur_ver 与 diff_urls[cur_ver]
go run -tags=probe test/probes/probe_ticket_extra.go

# 2. 自动拉取官方最新 SWF 并从零构建 data/gamedata.db 全量表
python3 scripts/rebuild_dict.py --ver 2026112001 --diff r395-01
```

### 质量校验

```bash
go vet ./...
go build ./...
go test -race ./...
```

---

## 🤝 贡献与发版

- 提交代码请遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/)，可使用内置交互式脚本 `bash scripts/commit.sh`。
- 发版流程请使用 `bash scripts/release.sh --dry-run` 预演后执行 `bash scripts/release.sh`。
- 协作规范与工程铁律参见 [CONTRIBUTING.md](./CONTRIBUTING.md) 与 [AGENTS.md](./AGENTS.md)。

---

## 📄 开源许可证

本项目基于 [Apache License 2.0](./LICENSE) 协议开源。
