# sxd-pie-ng (神仙道助手重构工程)

<p align="center">
  <strong>基于 Go 语言的高性能、脱机协议通信、拟人防封调度与内嵌 Web 控制台的现代化神仙道助手</strong>
</p>

<p align="center">
  <a href="https://github.com/atengk/sxd-pie-ng/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/atengk/sxd-pie-ng/ci.yml?branch=main&label=CI&style=flat-square" alt="CI Status" />
  </a>
  <a href="https://github.com/atengk/sxd-pie-ng/releases">
    <img src="https://img.shields.io/github/v/release/atengk/sxd-pie-ng?style=flat-square" alt="Release" />
  </a>
  <a href="https://golang.org/">
    <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go" alt="Go Version" />
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

`sxd-pie-ng` 是对《神仙道》传统桌面辅助工具（Pie 助手）的全新重构版本（Next-Generation）。针对原老旧架构跨平台困难、资源消耗高、风控识别易封号等痛点，本项目采用现代化 **Go 语言** 全面重写：
- **脱机纯协议通信**：无需启动庞大的 Flash / 浏览器环境，直接与服务端网关建立纯原生二进制 Socket 长连接；
- **透明 zlib 流解压**：基于魔数探测 (`0x78`) 实现下行大数据载荷的无缝透明解压与大端序二进制编解码；
- **拟人化抖动调度**：每个角色拥有独立生命周期 Goroutine，注入 1~3 秒动态随机正态延迟与操作抖动，彻底规避固定频率检测；
- **内嵌 Web 控制台**：借助 `go:embed` 将前后端编译为零外部依赖的单一独立可执行文件，开箱即用。

---

## ✨ 核心特性

- ⚡ **轻量高并发**：单个独立二进制常驻内存低于 30MB，支持多平台账号与数十个角色会话并行调度；
- 🧩 **协议层自适应解密与解压**：精准还原游戏私有协议 `[4B Length] + [2B Action ID]` 封包规范，自适应处理压缩与非压缩封包；
- 🛡️ **智能防封与拟人调度 (Jitter Scheduler)**：任务间具备随机抖动与行为平滑机制，模拟真实玩家操作轨迹；
- 🌐 **现代化 Web 仪表盘**：内嵌式 Web 管理页面，支持实时监视角色在线状态、任务执行流水及动态配置热加载；
- 🤖 **CI/CD 与工程化守护**：具备完整自动化流水线、Conventional Commits 提交助手与全生命周期发版防呆机制。

---

## 🏛️ 系统架构与目录结构

```text
sxd-pie-ng/
├── cmd/
│   └── server/             # 服务端主入口 (启动、参数解析与优雅停机)
├── internal/
│   ├── protocol/           # 二进制协议编解码、大端序序列化与透明 zlib 流引擎
│   ├── client/             # 角色会话长连接、心跳保活与状态机管理
│   ├── scheduler/          # 拟人随机抖动调度引擎与日常活动任务执行器
│   └── web/                # 内嵌 Web 控制台静态资源与 HTTP 管理接口
├── configs/                # 示例配置文件与配置结构定义
├── docs/                   # 架构决策记录 (ADR) 与领域上下文 (CONTEXT.md)
├── scripts/                # 规范化提交助手 (commit.sh) 与发版脚本 (release.sh)
├── .github/workflows/      # 自动化 CI 与发版流水线
├── go.mod                  # Go 模块定义
└── README.md
```

---

## 🛠️ 快速开始

### 运行环境准备
- **Go**: 1.24+
- **Git**

### 本地编译与运行

```bash
# 1. 克隆代码仓库
git clone https://github.com/atengk/sxd-pie-ng.git
cd sxd-pie-ng

# 2. 运行单测与验证
go test -v ./...

# 3. 编译二进制产物
go build -v -o bin/sxd-pie-ng cmd/server/main.go

# 4. 启动服务 (查看版本或指定配置文件)
./bin/sxd-pie-ng -v
./bin/sxd-pie-ng -config configs/config.example.yaml
```

---

## ⚙️ 配置说明

配置文件采用 YAML 格式，参考模板为 [configs/config.example.yaml](./configs/config.example.yaml)：

```yaml
server:
  port: 8080
  host: "127.0.0.1"

accounts:
  - platform: "xindong"
    username: "YOUR_PLATFORM_USERNAME"
    password: "${PLATFORM_PASSWORD}"
    roles:
      - server_id: "s1"
        role_name: "YOUR_ROLE_NAME"
        auto_login: true

scheduler:
  jitter:
    min_seconds: 1.0
    max_seconds: 3.0
  routines:
    herb_garden: true
    lucky_star: true
    pilgrimage: true
    arena: true
```

### 多区服与按区服策略覆盖

在 `accounts[].roles` 中列出多个 `server_id` 即可同时运行多个区服。每个角色会获得独立的
`RoleSession` 与 `RoleScheduler`；换票阶段串行执行并按 `startup.ticket_min_interval_seconds` /
`ticket_max_interval_seconds` 加入随机间隔，避免同一账号短时间多次 Web 登录触发风控。

若需要让不同区服使用不同的玩法开关或策略参数，使用 `scheduler.server_overrides.<server_id>`：

```yaml
scheduler:
  routines:
    treasure_house: true
  server_overrides:
    s0:
      treasure_house:
        selection_mode: auto
    s1:
      treasure_house:
        selection_mode: explicit
        award_ids: [4, 21, 33]
```

覆盖采用深度合并语义：映射逐键合并、标量整体替换、列表整体替换，未列出的字段继承全局配置。
Web 控制台的玩法面板可切换“全部角色 / 指定角色”，按角色隔离开关，互不影响。

---

## 🤝 贡献与规范

本项目欢迎任何形式的贡献！提交代码时请遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/) 规范。

### 交互式提交助手

项目内置了便捷的规范化提交助手：

```bash
bash scripts/commit.sh
```

更多协作指引请查阅 [CONTRIBUTING.md](./CONTRIBUTING.md)。

---

## 🚀 发版机制

本项目采用 GitHub Actions 自动化发版体系：
1. 本地通过 `bash scripts/release.sh --dry-run` 进行安全演练；
2. 执行 `bash scripts/release.sh` 交互式推导版本并推送附注 Tag；
3. GitHub Actions 自动捕获标签、提取提交日志并发布 GitHub Release。

---

## 📄 开源许可证

本项目基于 [Apache License 2.0](./LICENSE) 协议开源。
