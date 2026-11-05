# s0 服务器元宝产出面侦察 (Ingot Faucet Recon)

- **日期**: 2026-11-05
- **账号**: `configs/s0.yaml` → `s0-niuchangsheng`（角色名：遁一，等级 300，VIP 12）
- **基准事实**: 官方 `Main.swf`（CDN `http://uri.xdcdn.net/sxd/tree/4f160f61e0968bd8568bc9cc38bbea6224ed274e/Main.swf`）
  反编译产物，反编译工具 `~/rea-sxd/tools/ffdec/ffdec.jar`
- **目标**: 在**不消耗元宝**、不使用游戏漏洞的前提下，找出尚未接入自动化的合法元宝产出。

---

## 一、侦察方法

1. **红点列表**（`Mod_RedDot_Base`，Module 558 Act 1，`0x022E0001`）：
   服务端下发的 `[red_dot_type, is_show]` 列表，反映官方客户端认为“有可领取/可参与内容”的功能入口。
   红点类型编号到功能名的映射来自 `com.assist.server.RedDotType`。
2. **活动日历**（`Mod_GodDaily_Base.activity_calendar`，Module 809 Act 13，`0x0329000D`）：
   返回 `[activity_id, start_time, end_time, ...]`，用于判断活动当前是否真正开放。
   `activity_id` 映射来自 `com.assist.server.source.ActivityTemplate`。
3. **逐模块只读探针**：对候选福利模块先调用只读面板/状态接口，再决定是否存在免费领取路径。
   所有探针沉淀在 `test/probes/`：
   - `probe_red_dot_live.go`
   - `probe_activity_open_live.go`
   - `probe_daily_signin_live.go`
   - `probe_vip_gift_live.go`
   - `probe_online_reward_live.go`
   - `probe_fourteen_anniversary_live.go`
   - `probe_anniversary_events_live.go`
   - `probe_cat_gold_live.go`

---

## 二、活动日历实采（2026-11-05 01:32 +08）

s0 返回 13 条活动记录，按时间窗过滤后**当前开放 8 条**：

| activity_id | ActivityTemplate | 时间窗 (UTC+8) | 性质 |
| :---: | :--- | :--- | :--- |
| 333 | `new_dunhuang_treasure` 新敦煌宝藏 | 11-05 ~ 11-07 | 开格消耗元宝，`get_award` 非免费 |
| 334 | `charge_back_special` 充值返利 | 11-05 ~ 11-07 | 充值向 |
| 138 | `DOUBLE_ELEVEN_GIFT` 双十一礼包 | 11-01 ~ 11-11 | `open_gift` 消耗道缘/元宝/礼券 |
| 290 | `wu_fa_field_activity` 悟法领域活动 | 10-25 ~ 11-08 | 已接入 `wufa_field` 自动化 |
| 340 | `mystical_treasure` 神秘宝藏 | 10-25 ~ 11-16 | 任务/挖宝与元宝消费挂钩 |
| 317 | `summer_themed_activity` 夏日主题活动 | 09-13 ~ 11-23 | 主题活动（礼包/通行证向） |
| 215 | `limited_time_charge` 限时充值 | 09-13 ~ 11-22 | 充值向 |
| 377 | `glaze_pavilion` 琉璃阁 | 11-01 ~ 11-09 | 兑换商店，非免费产出 |

已结束但红点仍可能残留：`162 seek_immortal_road`（11-05 00:00 结束）、
`191 new_charge_back`（11-05 00:00 结束，11-07 再开）。

> 结论：**红点亮 ≠ 活动开放**。例如十四周年礼包、周年蛋糕、财源滚滚的红点均亮，
> 但对应模块实机返回 `FUNC_NOT_OPEN` 或无可用次数；必须以活动日历/模块回包为准。

---

## 三、候选模块实测结论

| 模块 | Action | 实机结果 | 是否有免费元宝 | 处理 |
| :--- | :--- | :--- | :---: | :--- |
| 89 签到 | `get_sign_in_status` | `000001`，活动未开放；`player_sign_in` 返回 `13`(SI_FAILED) | 活动期有 | **已修正协议并接入** |
| 18 VIP 礼包 | `get_player_vip_info` | VIP 12，1–12 档已领、13–15 档超等级 | 免费档每日重置 | **已接入免费档** |
| 712 在线奖励 | `main_panel` | 返回 2012 年历史时间窗；`one_key_get` 返回 `1`(FAILED) | 活动期最高档元宝×100 | **已接入并按时间窗跳过** |
| 707 招财猫 | `cat_gold_info` | `is_store=0`、`can_get_ingot=0`，无存款 | 仅返利，需先存元宝 | 不接入 |
| 821 十四周年礼包 | `get_panel_info` | `result=1`(FUNC_NOT_OPEN) | 是（活动期） | 暂不接入 |
| 350 周年蛋糕 | `get_sixth_anniversary_cake_info` | `today_self_num=0`、`has_eat=0`，无可用次数 | 是（活动期） | 暂不接入 |
| 723 财源滚滚 | `open_panel` | `result=1`(FAILED)、`coupon_num=0` | 礼券抽奖含 `get_ingot` | 不接入 |
| 731 新敦煌宝藏 | `get_panel_info` | 开格消耗元宝；`get_award` 需开满格子 | 否（消耗向） | 不接入 |
| 740 神秘宝藏 | `get_panel_info` | 任务与 `cost_ingot/today_ingot` 挂钩 | 否（消耗向） | 不接入 |
| 240 开服福利 | `get_info` | 需充值/特权 | 否 | 不接入 |
| 328 众神福利 | `get_level_award_info` | 等级成长奖励（道具） | 非元宝 | 不接入 |
| 551 点金石 | `get_status` | 产出经验/幸运值 | 非元宝 | 不接入 |
| 875/874/873 周年子活动 | panel | 多为付费/兑换向 | 否 | 不接入 |
| 185 幸运刮刮卡 | `scratch_card_info` | `is_in_activity=1`，但 `card_times=0`、`active_degree=0` | 活动期可刮出元宝 (`INGOT=7`) | **已接入免费次数刮卡** |
| 705 镇妖塔 | `panel_info` | 开放；奖励为服务端下发道具，未确认含元宝 | 未确认 | 暂不接入 |
| 411 妖魔入侵 | `get_state` | `state=3`(CLOSE) | 未确认 | 不接入 |
| 886 纪元镇妖塔 | `panel_info` | `result=2`(FUNCTION_NO_OPEN) | 否 | 不接入 |
| 24 世界Boss | `get_world_boss_list(0)` | 仅 1 个 Boss（赤炎兽 lv280），`state=0`(未开始) | 奖励以铜钱/声望为主 | **已接入自动参战** |
| 393 冰窟探险 | — | 未开放/未验证 | 非元宝 | 不接入 |

---

## 四、落地结果

1. **签到协议修正**：旧实现 `0x00000049` 为错误协议，实服无回包；
   已改为 Module 89 的 5 个真实动作，并补齐 `SI_FAILED` 等失败码分流。
2. **VIP 每日礼包**：新增真实 Module 18 免费档领取，永不调用付费 `buy_send_gift`。
3. **在线奖励**：新增 Module 712 面板查询与一键领取；活动时间窗过期时直接跳过。
4. `configs/s0.yaml` 已启用 `daily_sign_in`、`vip_welfare`、`online_reward`。

## 五、SWF 静态奖励表全量扫描（2026-11-05 第二轮）

对 `com.assist.server.*` / `com.assist.server.source.*` 共 414 个反编译类做 `3345`（元宝道具 ID）扫描，
新增结论：

| 玩法 | 静态奖励 | 实机状态 | 结论 |
| :--- | :--- | :--- | :--- |
| 设备绑定 (Mod 814) | 首绑 200 元宝 + 月度 50 元宝 | `is_bind=0`，需手机验证码 | **已接入领取 Routine**，待用户绑定后自动领 |
| 仙途秘宝 (Mod 624) | 1/5/10/15/20/25 级各 200 元宝 | `cur_level=25`，领取返回 `NOT_BUY(8)` | 元宝档属付费进阶轨，不接入 |
| 海岛钓鱼 (Mod 826) | 按鱼星级 20–80 元宝 | `bait_num=0`，鱼饵需元宝购买 | 非免费，不接入 |
| 累计登录礼 (Mod 542) | 第 4/8/10 天各 100 元宝 | 活动 id 218 未开放 | 活动期可接入 |
| 问卷投票 (Mod 773) | 每题 50 元宝 | 活动 id 60 未开放 | 活动期可接入 |
| 公众号关注礼 | 50 元宝（静态展示） | 无游戏内领取协议，需外部关注 | 不接入 |
| 通用排名奖励 (`CommonRankTypeData`) | 多档含元宝 | — | 世界 Boss/爬塔等排名结算走邮件，已由 `mail_collect` 覆盖 |

| 每日问灵 (Mod 503) | 问灵等级 5/6 随机池含 20/30 元宝 | `max_ask=1, has_ask=1` 今日已问 | **已接入每日自动问灵**，随机奖励优先选元宝 |

> 说明：本侦察仅覆盖**不消耗元宝**的合法路径。充值返利、开格、元宝抽奖等
> 消耗向活动一律未接入；不使用任何游戏漏洞或规避检测手段。