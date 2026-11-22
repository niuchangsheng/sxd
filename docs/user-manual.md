# 用户使用手册 (User Manual)

> 本手册面向日常使用者，介绍如何从零配置并运行 `sxd-auto`，按区服定制玩法策略，以及使用内嵌 Web 控制台实时查看与控制任务。

---

## 1. 快速上手（5 分钟启动）

### 第一步：准备配置文件

将示例配置复制为本地私有配置（`configs/config.yaml` 已被 `.gitignore` 忽略，不会误提交）：

```bash
cp configs/config.example.yaml configs/config.yaml
```

### 第二步：填写账号与区服

编辑 `configs/config.yaml` 中的 `accounts` 段落：

```yaml
accounts:
  - platform: "xindong"
    username: "你的平台账号"
    password: "${PLATFORM_PASSWORD}"   # 也可直接填明文，推荐用环境变量
    cookie: "A1=xxxxxx; A2=yyyyyy"     # 推荐填写，可绕过图形验证码风控
    roles:
      - server_id: "s0"                # 区服编号
        role_name: "角色昵称或备注"
        auto_login: true
      - server_id: "s1"
        role_name: "二服角色"
        auto_login: true
```

> [!TIP]
> **如何获取 Cookie（推荐）**：
> - **方式 A（自动捕获）**：首次启动时加上 `--browser-login` 参数，在弹出的浏览器窗口登录一次，程序会自动捕获并保存 Cookie。
> - **方式 B（手动复制）**：在 Chrome 登录心动神仙道页面后，打开开发者工具（F12）复制包含 `A1`、`A2` 的 Cookie 填入 `cookie` 字段。

### 第三步：启动助手

```bash
# 源码直接运行
go run ./cmd/server --config configs/config.yaml

# 或运行已编译好的二进制
./bin/sxd-auto --config configs/config.yaml
```

启动成功后，浏览器访问 **`http://127.0.0.1:8080`** 即可打开 Web 控制台。按 `Ctrl+C` 可安全优雅退出。

---

## 2. 命令行参数说明

| 参数 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `--config <path>` | `configs/config.yaml` | 指定 YAML 配置文件路径（若默认文件不存在会自动回退到 `configs/config.example.yaml`） |
| `--browser-login` | `false` | 启动前弹出浏览器进行交互式登录，自动捕获并更新配置中的 Cookie |
| `--debug` | `false` | 开启 Debug 级别详细日志（排查协议问题时使用） |
| `-v` | `false` | 打印版本号、Git Commit 与构建时间后退出 |

---

## 3. 玩法总览与安全承诺

`sxd-auto` 内置 **7 大领域、113 个自动化例程**。所有玩法严格遵守 **“默认不消耗元宝”** 红线：

| 领域 | 代表玩法 | 消耗与安全说明 |
| :--- | :--- | :--- |
| **日常 (`daily`)** | 每日签到、小助手、邮件、在线/终结礼包、VIP 福利、天神赐福、宠物庭院、周末活动、城镇打坐、飞升灌注、修真境界礼包、服用修为丹、兵法圣水、缘魂宝箱等 | 仅领免费福利或使用铜钱/道缘/背包已有道具；付费终结礼包与元宝档位一律跳过 |
| **副本 (`dungeon`)** | 主线剧情推图、精英/英雄扫荡、背包卷轴自动清理、六道轮回塔、伏魔锁妖塔、仙界练功房、生肖挑战、精灵试炼、奇门遁甲、魔王试炼、通天塔、九空无界 | 体力不足时按配置吃背包体力药；不买挑战次数、不花元宝清冷却 |
| **农场 (`farming`)** | 药园采摘/补种、西天取经（护送与免费打劫）、仙界矿山、粗铁淬炼池、灵霄宝殿炼宝炉、壶中界、符文淬炼 | 仅使用免费次数与铜钱；不买矿山次数、不花元宝加速或刷品质 |
| **小游戏 (`minigame`)** | 天帝宝库（自动/稀有度/指定选奖）、宗师修行（自动换阵与切绝技）、个人/帮派吉星高照、仙旅奇缘答题、金榜题名答题、钓鱼、刮刮卡、五行天宫、法相造物佛手/守护魔兽、摘仙桃、叶公好龙、百炼乾坤、纪元造化/湮劫/秘藏等 | 吉星高照仅用免费改运；摘仙桃/叶公好龙仅用免费与铜钱档；法相炼化单次道缘上限默认 1,000 |
| **竞技 (`pvp`)** | 本服竞技场（软柿子挑选）、仙界竞技场、圣域竞技场、神魔大战报名、阵营劫镖、世界 Boss、跨服天梯 | 仅用免费挑战次数，不买次数、不清冷却、不元宝鼓舞 |
| **社交 (`social`)** | 仙盟活动/上香/魔神、圣盟祭祀/膜拜/世界塔、帮派祭神/转盘、结缘宝箱、住宅祝福、好友送花、NPC 结交与伴手礼、灵域猎妖、无量劫海、无尽洞天、魔界远征、万古神墟、灵契之地、轮回图录、全网聊天室 | 帮派上香默认白檀香（免费）；仙盟/圣盟膜拜仅用免费/铜钱档 |
| **系统 (`system`)** | `session_refresh` 会话定时刷新 | 每 5 分钟自动维护长连接健康度，防止长会话静默丢包 |

---

## 4. 常用策略配置详解

完整配置项可对照 [configs/config.example.yaml](../configs/config.example.yaml) 中的逐行中文注释，以下为最常调整的策略项：

### 4.1 开关单个玩法
在 `scheduler.routines` 下将对应玩法设为 `true`（开启）或 `false`（关闭），例如关闭孩子日常与寻仙之路：

```yaml
scheduler:
  routines:
    children_daily: false
    xunxian_road: false
```

### 4.2 副本推图与扫荡 (`scheduler.dungeon`)

```yaml
scheduler:
  dungeon:
    quest_cycle:
      enabled: true                   # 自动接/做/交主线任务
      auto_replenish_stamina: true    # 推图缺体力时自动吃体力药
      max_replenish_stamina: 200      # 单轮最多补充体力
    sweep:
      enabled: true                   # 是否执行精英/英雄副本扫荡
      sell_swept_items: true          # 扫荡后自动卖出产出杂物防爆包
      global_elite: true              # 跨城镇全局扫荡精英副本
```

### 4.3 天帝宝库自选大奖 (`scheduler.treasure_house`)
支持三种选奖模式：
- `auto`（默认）：采集账号各系统养成进度，按需求分动态选出最缺的三档大奖；采集失败时按 `prefer_awards` 兜底。
- `rarity`：按道具稀有度与已开放功能过滤自动选奖。
- `explicit`：严格使用手动指定的 `award_ids`（3 个条目 ID）。

### 4.4 宗师修行 (`scheduler.master_practice`)
- `practice_order: []`：留空表示只练主角当前职业；填入职业 ID 列表（如 `[15, 1, 2, 3, 6, 5]`）会在满星或打不过时自动切换主角神器绝技并调整同职业伙伴布阵。

### 4.5 粗铁淬炼池 (`scheduler.spirit_pool`)
- `refine_target: "xuantie"`（淬炼玄铁）或 `"lingcai"`（提炼灵材），仅消耗铜钱。

### 4.6 多区服差异化覆盖 (`scheduler.server_overrides`)
当同一账号在多个区服运行，希望某区服使用不同开关或策略时，只需写需要覆盖的字段，其余字段自动继承全局配置：

```yaml
scheduler:
  server_overrides:
    s0:
      treasure_house:
        selection_mode: auto
    s1:
      routines:
        children_daily: false
      dungeon:
        sweep:
          enabled: false
      treasure_house:
        selection_mode: explicit
        award_ids: [4, 21, 33]
```

---

## 5. 使用内嵌 Web 控制台

启动后打开 `http://127.0.0.1:8080`：
1. **角色状态栏**：实时查看每个区服角色的连接状态（在线 / 重连中 / 断开）。
2. **玩法控制面板**：
   - 可切换 **“全部角色”** 或 **指定单个角色**，实时启停任意玩法，修改立即生效无需重启；
   - 查看每个玩法当前状态（`Active` 运行就绪、`Cooling` 冷却挂起及下次唤醒时间、`Disabled` 已停用）；
   - 支持一键应用内置预设组合。
3. **实时日志流**：直接浏览最近 200 条关键运行日志；完整历史日志保存在 `logs/sxd.log`。

---

## 6. 常见问题 (FAQ)

**Q1：可以删除 `data/Pieb.db` 吗？如果游戏出新道具/新伙伴了怎么办？**
不可以删除。服务端协议大多只传数字 ID，`Pieb.db` 用于解析主线任务与怪物编队、精英副本城镇、背包卷轴/杂物价格、伙伴职业、天帝宝库道具品质等。目前 `data/Pieb.db` 已通过 `scripts/rebuild_dict.py` 与 2026-11-20 官方客户端 SWF 全量对齐；后续游戏更新时只需再运行一次 `python3 scripts/rebuild_dict.py --ver <最新版本号> --diff <diff标签>` 即可全自动同步。

**Q2：为什么有的玩法日志显示“进入定时等待”或“挂起至次日 00:05”？**
这是**语义智能熔断**机制。当服务端返回“今日次数已用尽”、“冷却中”、“未开放”或连续 3 次静默丢包时，助手会自动计算下次有效窗口并休眠等待，避免无意义反复发包触发风控。

**Q3：确认不会偷偷花掉我的元宝吗？**
所有默认开启的玩法均经过实机抓包与协议常量核对，元宝消费分支（购买次数、清除冷却、元宝培养、高级寻宝、付费终结礼包等）均被显式禁用或跳过。唯独 `faction_blessing.offering_id` 若手动改为 `2`/`3` 会消耗元宝，默认值为 `1`（免费白檀香），保持默认即可。

**Q4：程序挂机久了会掉线卡死吗？**
不会。助手内置心跳保活、每 5 分钟主动刷新长连接防静默丢包、断线指数退避自动重连以及凭据自动刷新机制，适合 7×24 小时无人值守运行。
