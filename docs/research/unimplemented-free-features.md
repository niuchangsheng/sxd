# 每日活跃度：已实现与未实现功能清单（2026-11-07）

> 目标：`configs/s0.yaml` 完整执行后每日活跃度 ≥ 1200。
> 当前实测：完整批次执行结束后 s0 `daily_total=1300`（`gap=0`），元宝无净消耗。
> 测量方式：`test/probes/probe_activity_audit_live.go`（只读 `0x0000002A` 面板）。

## 1. 已实现并稳定贡献活跃度的玩法

| 任务 | 玩法 | 实现位置 | 备注 |
| ---: | --- | --- | --- |
| 81 | 法力星图占星 | `internal/routines/minigame/star_picture.go` | 已实现；因面板 free 计数与扣费不同步，**默认关闭**（配置 `star_picture: false`） |
| 46 | 五福临门 | `minigame` 五福例程 | 以面板 `bless_number` 为准，不再用已失效的开放校验 |
| 54 | 兵法圣水 | `daily/tactics_water.go` | 铜钱购买湛蓝圣水 |
| 56 | 命宫碎片 | `daily/fate_hunt.go` | 铜钱兑换 |
| 69 | 悟法领域冥想 | `minigame/minigame.go` (`wufa_field`) | 免费次数耗尽后用冥想书卷 |
| 75 | 消耗体力 | `dungeon` 精英扫荡 | 背包满时先出售非元宝制作卷 |
| 76 | 练功奖励 | `dungeon` 练功房/圣域修炼 | 免费席位 |
| 3/4/9/17/18/19/23/29/34/35/37/39/43/44/48/57/61/65/66/78 | 日常类 | 各日常例程 | 见 `docs/research/s0-daily-activity-1200-audit.md` |
| 12/13/36 | 取经 | `farming/pilgrimage` | `force_saint_area` 固定圣域，不回退仙界；圣域状态实测 `status=4(关闭)` 时按设计跳过（2026-11-07 14:46 实测关闭，开放时段由定时批次自动补上） |
| 59/63 | 孩子日常 | `daily/children_daily.go` | 学堂上学 + 孩子任务奖励 |
| 70 | 轮回净地 | `daily/rebirth_land.go` | 免费 852/6 可领奖，但**不计任务 70** |

## 2. 未实现 / 当前免费不可达

| 任务 | 玩法 | 每次 | 阻塞原因与实证 |
| ---: | --- | ---: | --- |
| 74 | 琳琅阁购买商品 | 10 | **已按用户要求禁用**：代码库中从未存在琳琅阁例程/配置（运行时代码无任何 `100/80`、`2/76` 调用）。排查结论：商品可用铜钱购买（`luckyshop` 大量 `ingot=0`），但请求需当前城镇 NPC id；角色 `town_map_id=153`，本地无 `Towns` 静态表，`enter_town` 也不下发 NPC 列表，所有候选 id 与编码均无回包，故停止尝试 |
| 82 | 领取好礼馈赠 | 20 | `info_assistant (100/78)` 可读但返回 16 字节全 0；`get_award_assistant (100/79)` 索引 0..15 全部超时，入口在机器人会话不可达 |
| 71 | 纪元湮劫战斗 | 20 | **已实现 `era_yanjie`**：`Mod_EraInfiniteMission_Base` (853) `get_panel_info` + `fight`；按 `mission_level+1` 每日推进一关，实测胜利后元宝/铜钱/体力均不变，任务 71 已由 0/1 → 1/1（+20） |
| 77 | 宗门灵兽喂养 | 20 | **已实现 `sect_monster`**：`Mod_SectMonster_Base (576)` 经仙盟城镇会话、**不带 PrevAct** 调用 `get_monster_list (576/1, [sect_id])` + `feed (576/3, [type,0])`；实测免费喂养成功获 2471×10，任务 77 由 0/1 → 1/1（+20），元宝/铜钱不变 |
| 73 | 进入仙幽猎境 | 20 | **已实现 `mystic_hunt`**：`Mod_MysticCapture_Base (762)` `is_open_hunt_entrance (762/5)` + `enter_hunt (762/1)`（空请求、不带 PrevAct）。线上当前入口 `0=关闭`（按时段开放），例程自动跳过；开放时免费进入并 +20，元宝/体力不变 |
| 70 | 轮回净地挂机奖励 | 20 | 免费 852/6 实测 `result=8` 且不计任务；付费 852/5 消耗 10 元宝，永久禁用 |
| 50 | 奇门遁甲 | 5 | 6 个条目以 `NORMAL_PRACTICE(3)` 调用均返回 `LEVEL_UNOPEN(8)`，id 映射未确认 |
| 67 | 回忆之境 | 5 | 主面板 `get_award=1`，但 `get_bind_award (526/3)` 各编码均无回包、`backtrack (526/4)` 返回 `FAILED(1)`；伙伴碎片面板显示 500/500（疑似显示上限，实际不足） |
| 58 | 家园奖励 | 5 | **已实现 `marry_home`**：`get_cake (210/19)` 直接领取自家小蛋糕；线上当前返回 `NON_ACTIVITY_TIME(18)`（非活动时间），活动时段开启后可 +5，元宝不变 |
| 49 | 仙盟上香 | 5 | 角色已在仙盟「闲散道门」，但 `st_union_god_incense (179/11)` 与 `get_st_union_god_info (179/10)` 在仙盟城镇会话/主服、空包/[sect_id]/带 PrevAct 各编码下均无回包，疑似该仙盟未开放神像或需分组 socket |
| 62/60 | 孩子探索/修行 | 10 | 已探测 `get_practice_info (312/29)`：孩子 1812 有三个修行条目，但 `up_deploy (312/16)` 与 `challenge (312/32)` 均返回 `FAILED(11)`（无任何资源变化），疑似孩子处于上学/年龄条件限制 |
| 14 | 使用仙露（余量） | 5 | `观音仙露(5145)` 计数 1 但不在背包格位，客户端中该道具打开「孩子制造」；每日任务的发财树属农场 coin_tree/xianling_tree 体系，仙露免费来源未确认（15 为元宝购买档），暂按付费可达处理 |
| 64 | 召唤仙桃（余量） | 5 | 首次成功，后续 `call_peach` 返回 FAILURE(3) |

## 3. 元宝硬约束与禁区

- `no-ingot-spend`：自动化任何路径都不得消耗元宝，无开关。
- 已确认禁用：禁地之书付费多探（300/次）、`quick_auto_back (852/5)`（10 元宝）、
  天帝宝库付费抽奖、兵法黄金圣水、结缘金兰花、仙露元宝档、阶位赛等。
- **法力星图警告**：面板 `free_divination_count` 与实际扣费不同步；同日第 2、3 次占星
  实测扣除 50 元宝。例程已限制为「`has_divination==0` 时最多 1 次」，且默认关闭。

## 4. 复现与验证入口

- 活跃度审计：`go run -tags=probe test/probes/probe_activity_audit_live.go`
- 仙旅奇缘链路：`probe_travel_event_live.go`、`probe_travel_routine_live.go`
- 天帝宝库 Act4 时序：`probe_treasure_seq_live.go`（`-variant 4` 为修复后顺序）
- 琳琅阁/好礼：`probe_lucky_shop_npc_live.go`、`probe_assistant_gift_live.go`
- 法力星图：`probe_star_picture_live.go`（`-readonly`）、`probe_star_picture_routine_live.go`