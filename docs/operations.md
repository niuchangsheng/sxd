# 运维部署与排障手册 (Operations Guide)

> 本文档面向部署与运维人员，涵盖环境准备、生产部署、日志轮转、监控接口、凭据续期、常见故障定位与发布前检查。

---

## 1. 运行环境与依赖

| 项目 | 要求 |
| :--- | :--- |
| **操作系统** | Linux (amd64/arm64)、macOS、Windows |
| **Go 版本（编译期）** | Go 1.26+（与 [go.mod](../go.mod) 一致） |
| **运行期外部依赖** | 无（单二进制）；需同目录保留 `data/gamedata.db` 与配置文件 |
| **网络出站** | TCP 出站至游戏网关（如 `*.sxdweb.xd.com:8200/8211/8480`）与平台 HTTPS 换票接口 |
| **监听端口** | 默认 `127.0.0.1:8080`（内嵌 Web 控制台，可在 `server.host` / `server.port` 修改） |

---

## 2. 编译与生产部署

### 2.1 编译二进制

```bash
# 本地编译（自动注入版本元数据）
go build -trimpath \
  -ldflags "-s -w -X main.Version=$(git describe --tags --always) -X main.Commit=$(git rev-parse --short HEAD) -X main.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/sxd-auto ./cmd/server
```

部署目录最小集合：

```text
/opt/sxd-auto/
├── bin/sxd-auto
├── configs/config.yaml      # 权限建议 0600，切勿提交到 Git
├── data/gamedata.db         # 官方 SWF 离线静态元数据字典（必须存在）
└── logs/                    # 运行日志目录（程序自动创建）
```

### 2.2 `systemd` 守护进程部署（Linux 推荐）

创建 `/etc/systemd/system/sxd-auto.service`：

```ini
[Unit]
Description=sxd-auto Automation Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/sxd-auto
ExecStart=/opt/sxd-auto/bin/sxd-auto --config /opt/sxd-auto/configs/config.yaml
Environment="PLATFORM_PASSWORD=your_password_if_needed"
Restart=on-failure
RestartSec=10s
KillSignal=SIGINT
TimeoutStopSec=15s

# 安全加固
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/opt/sxd-auto/logs /opt/sxd-auto/configs

[Install]
WantedBy=multi-user.target
```

启用与管理：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now sxd-auto
sudo systemctl status sxd-auto
journalctl -u sxd-auto -f
```

> [!IMPORTANT]
> **优雅停机**：进程监听 `SIGINT` / `SIGTERM`。收到信号后会立即取消所有运行中的 `kit.Caller` / `kit.Sleep`、关闭角色 TCP 会话与 Web 服务并在毫秒级安全退出；请使用 `SIGINT` / `SIGTERM`，避免直接 `kill -9`。

---

## 3. 日志管理与轮转

日志由 `internal/logging` 管理，同时输出到标准输出、内存环形缓冲（供 Web 控制台查看，最近 200 条）以及本地轮转文件：

```yaml
log:
  enabled: true
  dir: "logs"
  file: "sxd.log"
  level: "info"           # debug / info / warn / error
  format: "text"          # text / json（接 ELK/Loki 推荐 json）
  max_size_mb: 10         # 超过 10MB 立即切分
  rotate_interval: "24h"  # 定时切分，"0" 为关闭
  max_backups: 10         # 最多保留历史文件数
  max_age_days: 30        # 最长保留天数
```

- 每次进程启动也会先归档一次旧 `sxd.log`，避免跨次启动日志混杂。
- 排障需要更细粒度报文信息时，可在启动参数追加 `--debug`（同时提升控制台、Web 缓冲与文件日志级别至 `DEBUG`）。

---

## 4. 监控与管理 HTTP API

Web 控制台默认绑定 `127.0.0.1:8080`（**严禁无鉴权直接暴露到公网**；远程访问请使用 SSH 隧道或带 BasicAuth 的反向代理）。

| 端点 | 方法 | 用途 |
| :--- | :--- | :--- |
| `/api/health` | `GET` | 存活探针，返回 `status`、`version`、`time` |
| `/api/roles` | `GET` | 查询所有角色在线状态（`Active` / `Connecting` / `Disconnected` 等） |
| `/api/logs` | `GET` | 获取最近 200 条内存日志 |
| `/api/routines` | `GET` | 查询全部玩法运行态与冷却时间；支持 `?role_id=<id>` 按角色筛选 |
| `/api/routines` | `POST` | 动态启停玩法或应用预设，请求体：`{"role_id":"","id":"arena","enabled":false}` 或 `{"role_id":"","preset":"..."}` |

健康检查示例：

```bash
curl -fsS http://127.0.0.1:8080/api/health
curl -fsS http://127.0.0.1:8080/api/roles
```

---

## 5. 凭据与会话生命周期维护

1. **Cookie 模式（强烈推荐）**：在 `accounts[].cookie` 填入心动平台 `A1=...; A2=...` Cookie，可绕过图形验证码风控，通常有效期数周。
2. **Cookie 过期续期**：运行 `./bin/sxd-auto --config configs/config.yaml --browser-login`，按提示登录即可自动捕获并回写最新 Cookie。
3. **多区服换票节流**：`startup.ticket_min_interval_seconds` / `ticket_max_interval_seconds`（默认 2~5 秒）保证同一账号的多区服串行换票，防止短时间并发登录触发风控。
4. **会话定时自愈**：系统内建 `session_refresh` 例程，每 5 分钟按连接存活时长主动重建主服连接，规避游戏服务端长会话后段的批量静默丢包。
5. **离线字典同步刷新**：游戏大版本更新（新增道具/伙伴/关卡/宝库大奖/魔王 Boss 等）后，执行以下两步从零重建 `data/gamedata.db`：
   ```bash
   go run -tags=probe test/probes/probe_ticket_extra.go       # 获取最新 cur_ver 与 diff_urls[cur_ver]
   python3 scripts/rebuild_dict.py --ver <cur_ver> --diff <diff_tag>
   go test -race -count=1 ./internal/dictionary ./internal/qa
   ```

---

## 6. 常见故障排障手册 (Troubleshooting)

| 日志特征 / 现象 | 常见原因 | 处置方式 |
| :--- | :--- | :--- |
| `角色身份认证失败 ... login handshake timeout` | 网关瞬时抖动或临时票据未就绪 | 调度器自动按指数退避（首次 20s）重连并刷新凭据；若持续失败，检查网络出站或重新执行 `--browser-login` |
| `ErrAuthRejected` / 被顶号 | 官方客户端或其他实例同时登录同一角色 | 关闭其他客户端；进程会在退避后自动重新换票登录 |
| `ErrRoleSelectionRequired (FIRST_TIME)` | 该区服尚未创建角色 | 自动化严禁自动创角；请先在官方客户端进入该区服建角后再启用 |
| `连续静默丢包 3 次，最长挂起 6h0m0s` | 当日次数已耗尽、活动未开启或服务端条件未满足 | 属正常熔断保护，到期或次日 `00:05` 自动恢复，无需人工干预 |
| `PrevAct 重同步重试 ... ok=false` | 特定协议在当前会话状态被服务端静默丢弃 | 属于 `CallWithPrevActResync` 自动自愈尝试；若长期全失败，配合 `--debug` 观察 `recent_rx` 并用探针复现 |
| `背包已满` (`ErrBagFull`) | 角色背包无空位 | 确认 `bag_cleanup` 与 `dungeon.sweep.sell_swept_items: true` 已启用，或手动清理背包 |
| `Web 控制台启动失败: address already in use` | 8080 端口被占用 | 修改配置 `server.port` 或停止冲突进程 |
| 推图/选奖/宗师修行无法识别名称或职业 | 缺少 `data/gamedata.db` | 检查启动日志是否有 `已成功挂载官方游戏数据字典 gamedata.db`，确保工作目录下存在 `data/gamedata.db` |

### 离线协议探针诊断

当需要针对单一协议进行实机抓包对齐或排查时，使用 `test/probes/` 下的隔离探针（带 `//go:build probe` 标签，不影响主程序构建）：

```bash
# 示例：运行网关握手探针
SXD_PROBE_SERVER=127.0.0.1:8381 SXD_PROBE_USER=xxx SXD_PROBE_HASH=xxx \
  go run -tags=probe test/probes/gateway_handshake.go
```

---

## 7. 发布与升级前校验清单

```bash
# 1. 静态检查与构建
go vet ./...
go build ./...

# 2. 全量并发竞态回归测试（CI 同款）
go test -race -count=1 ./...

# 3. 探针隔离检查（确保不破坏常规构建）
git diff --check

# 4. 实机灰度冒烟（观察启动、双区握手、高优玩法与优雅停机）
./bin/sxd-auto --config configs/config.yaml
```
