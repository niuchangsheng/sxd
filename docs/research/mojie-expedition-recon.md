# 魔界远征（Mod_DevilExpedition_Base, Module 429）单人对战逆向

日期：2026-11-14 / 15

## 来源

- 抓包：`/tmp/mojieyuanzheng.pcapng`（qq s0，2026-11-14 23:03）
- 反编译：`/home/tongchen/rea-sxd/proto_full/scripts/com/protocols/Mod_DevilExpedition_Base.as`
- 数据层：`com/datas/DevilExpeditionData.as`、`DevilExpeditionSignInView.as`、`DevilExpeditionMapView.as`
- 功能编号：`FunctionTypeData.List[201] = DevilExpedition`（周五+周末开放，消耗屠魔令）

## 动作号

线上动作号为 `Actions` 数组下标 + 1：

| 动作 | 线上 Act | 请求 |
|---|---|---|
| activity_status | 429/1 | 空 |
| open_match_panel | 429/2 | 空 |
| close_match_panel | 429/3 | 空 |
| notify_enter_mission（S2C） | 429/12 | Int mission_id, Int town_id, Int |
| enter_town | 429/13 | Int town_id |
| move_to | 429/17 | Short×4（fromX,fromY,toX,toY），**无回包** |
| get_players | 429/19 | 空 |
| get_deploy_info | 429/20 | 空 |
| get_team_status | 429/28 | 空 |
| quit | 429/29 | 空 |
| get_mission_data_m5 / fight_m5 | 429/57 / 58 | 空 / Int team_id |
| start_expedition_single | 429/63 | UByte mission_type |
| reject_buy_box | 429/66 | 空 |
| get_mission_data_m6 / fight_m6 | 429/68 / 69 | 空 / Int team_id |
| get_skip_war_info | 429/73 | 空 |
| get_mission_data_m7 / fight_m7 | 429/74 / 75 | 空 / Int team_id |

三类伙伴（`start_expedition_single` 首字节）：双生=68（默认，阴阳殿）、魔神=67、隐仙=69。
所有请求尾部带 4 字节 PrevAct。

## open_match_panel 字段（已与实机回包对齐）

`result, is_complete, boss_id, max_health(Long), left_health(Long), level_section,
total_hurt(Long), top_hurt(Long), total_times, tumoling_num, left_join_times,
top_bout, total_bout, soul_arr, team_arr, is_unlock, fast_times, fast_need_soul,
qijue_open, total_stone, week_stone, week_stone_max, total_stone2, week_stone2,
week_stone_max2`

- `soul_arr` 条目：`[role_id, total_soul, week_soul, week_max_soul]` → 魔神伙伴魔灵。
- `week_stone / week_stone_max`：阴阳石（双生伙伴）。
- `week_stone2 / week_stone2_max`：隐仙石（隐仙伙伴）。

实机（2026-11-15 00:16，qq s0）：`week_stone=10/20`、`week_stone2=12/20`、
三个魔灵 `0/20`；`tumoling_num=0`（该字段不是背包屠魔令数量）。

## 关卡数据（429/57|68|74）

`result, members[], monster_teams[]([id,monster_id]), records[], refines[],
level_section, team_life, show_buy_tips(Int), boxes[]`

- M5 refines：`[role_id, get_soul, current_bout, need_bout, week_soul, max_week_soul]`
- M6/M7 refines：`[get_stone, current_bout, need_bout, week_stone, max_week_stone]`

双生抓包（M6）：队伍 `[2,1]`，客户端反复对 id=1 战斗；每次战斗前
`move_to(436,413,436,413)`；战斗成功返回 `2` 并推送 429/70（`PLAYER_FIGHT`），
最后一场返回 `3(FAILED)` 后客户端 quit。

## 自动化规则

1. 活动状态必须为 0（开放），否则按下一个周五 00:05 唤醒；
2. 面板预检本周材料：
   - 双生：`week_stone >= week_stone_max` 跳过；
   - 隐仙：`week_stone2 >= week_stone2_max` 跳过；
   - 魔神：`soul_arr` 全部 `week_soul >= week_max_soul` 时跳过；
3. `start_expedition_single` 返回 `10(NOT_ENOUGH_ITEM)` → 屠魔令不足，跳过该类型；
4. 进入关卡后再次检查 refines；每场战斗后再查一次，达上限立即停止；
5. 战斗结果非 `2`（失败/Boss 死亡/次数不足/怪物队伍已灭）即停止；
6. 不购买宝箱（仅发送 reject_buy_box），不消耗元宝。

## 隐仙伙伴抓包（/tmp/mojieyuanzheng2.pcapng，2026-11-15 00:00）

```
429/63 [0x45=69]  start_expedition_single 隐仙
429/12            notify_enter_mission: mission=7, town=0x9a(154)
429/13 [154]      enter_town
429/74            get_mission_data_m7 → teams=[2,1], refines=[got,cur,need,week,week_max(20)]
429/17            move_to (446,408)/(816,410)   # MonsterPos7 + 20px
429/75 [1|2]      fight_monster_m7
429/76            notify_mission_data_change_m7（PLAYER_FIGHT 更新周计数）
429/29            quit
```

- M7 与 M6 结构一致（5 字段 refines），但队伍 1/2 的点击坐标分别为 (446,408)、(816,410)。
- 本次连续战斗 10 场后客户端主动 quit，未达本周上限。
- 客户端会在一支队伍失败/耗尽后切换到另一支队伍，例程已改为按队伍轮转。

## 实机验证（qq s0，2026-11-15 00:24 / 00:54）

```
魔界远征面板 tumoling=0 week_stone=10/20 week_stone2=12/20 souls=3
魔界远征屠魔令不足，跳过该伙伴挑战 mission=双生伙伴
魔界远征屠魔令不足，跳过该伙伴挑战 mission=魔神伙伴
魔界远征屠魔令不足，跳过该伙伴挑战 mission=隐仙伙伴
结束资产 ingots=95747 coins=1839143876（未变化）
```

与用户描述一致：当前屠魔令不足，三类均未触发挑战；抓包中双生伙伴的成功
链路已由单元测试按真实报文覆盖。待屠魔令恢复后可再次实机验证完整战斗。