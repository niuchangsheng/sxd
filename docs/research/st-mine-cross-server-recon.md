# 仙界矿山（Mod_StMine_Base, Module 264）逆向与跨服会话验证

日期：2026-11-14

## 结论

- 矿山位于**仙界跨服节点**：主服会话 `264/0` 恒返回 `FAILED(13)`（裸载荷、`HasPrevAct`、
  显式 `LastAckActionID` 三种形态结果一致，11 字节 `0d` + 全 0）。
- 使用 `StcLogin(96/0) + StTown(95/0)` 建立仙界跨服会话（角色 ID 带 `-st` 后缀）后，
  `264/0` 返回 `SUCCESS(9)`，`264/1` 可进入矿脉并返回矿洞列表。
- 功能编号来自 `FunctionTypeData.List[157] = StMine`（通关天庭女儿国(1)开启）。

## 协议（来源：`/tmp/mine_out/scripts/com/protocols/Mod_StMine_Base.as`）

动作号按 `Actions` 数组：

| Act | 名称 | 请求 | 说明 |
|---|---|---|---|
| 0 | get_mine_mountain_info | 空 | 矿山概览 + 各矿脉容量 |
| 1 | enter_mine_mountain | Byte mountain | 进入矿脉，返回玩家矿洞与矿洞列表 |
| 2 | leave_mine_mountain | 空 | 退出矿山面板（不放弃矿洞） |
| 3 | rob_mine_hole | Short hole_id, Int robbed_player_id | 占领/抢夺矿洞 |
| 4 | leave_mine_hole | 空 | 放弃当前矿洞 |
| 5 | recapture_mine_hole | 空 | 抢回被夺矿洞 |
| 7 | receive_award | 空 | 领取累计收益 |

### 264/0 回包

`UByte result, Byte level, Int total_skill, Byte cur_mountain,
[Byte level, Byte used_count, Byte total_count] mountains,
UByte lucky_open_flag, Byte lucky_flag`

`cur_mountain=0` 表示尚未选择；矿脉列表全部 `used_count==total_count` 时表示满员。

### 264/1 回包

`UByte result, Byte skill_level, Int skill_value, Short remain_rob_count,
Byte buy_need_ingot, Int next_rob_timestamp,
16×玩家矿洞字段, [矿洞列表], Int feedback_mult`

玩家矿洞 16 字段：`status, hole_id, rob_over_ts, next_award_ts, protect_over_ts,
item_id, item_count, need_minutes, receive_count, ingot_rob_flag, daoyuan_rob_flag,
recapture_ts, be_recapture_ts, enemy_role_id, enemy_nickname, enemy_servername`。

矿洞列表项：`hole_id, lucky_flag, type, status, player_id, nick_name, player_level,
server_name, protect_over_ts, rob_over_ts, receive_count`。

### 矿洞状态

`EMPTY=0, ROB=1, BE_ROBBED=2, BE_INVITED=3, CAN_BE_ROB=4`（`StMineData` 与
`StMineHolesView`）。

## 自动化流程

1. 主服检查功能开放 `FunctionIDStMine=157`；
2. 建立仙界跨服会话；
3. `264/0` → 选择矿脉：优先有空位的最高等级矿脉，全部满员时选与角色矿山等级一致的矿脉；
4. `264/1` 进入并解析玩家矿洞 + 矿洞列表；
5. 被夺矿洞在期限内 → `264/5` 抢回；
6. `receive_count>0` → `264/7` 领取；
7. 已有矿洞则保持；否则 `remain_rob_count>0` 且 `next_rob_timestamp<=now` 时：
   优先空矿洞，其次等级最低的可抢矿洞 → `264/3`，成功后立即 `264/7`；
8. `264/2` 退出面板（不放弃矿洞）。

结果码分流：`ROB_CD(18)` 按 `next_rob_timestamp` 冷却；`ROB_COUNT_LIMIT(19)` 冷却 2 小时；
`AWARD_HAS_NOT_RECEIVE(20)` 先领取；`PLAYER_*` 稍后重试。全程不调用
`buy_rob_count(10)`、`clean_rob_cd_time(11)`、`increase_protect_time(12)`，不消耗元宝。

## 实机验证（ncs s0，2026-11-14 23:50 / 23:57）

首次（23:50）：

```
仙界矿山状态 role_id=s0-niuchangsheng-st result=9 level=5 cur_mountain=0
进入仙界矿山 mountain=5 skill_level=3 remain_rob_count=3 candidates=5
仙界矿山抢夺矿洞 hole_id=25 owner_player_id=84640 owner_level=300 result=9 result_text=成功
仙界矿山占领后领取收益 result=26 result_text=暂无可领取收益
Routine 执行完成
```

第二次（23:57，含每日活跃度任务 51「抢夺仙界矿山」）：

```
抢夺仙界矿山活跃度 stage=before cur=1 max=3 per=5
仙界矿山抢夺矿洞 hole_id=25 owner_player_id=84640 owner_level=300 result=9 result_text=成功
抢夺仙界矿山活跃度 stage=after  cur=2 max=3 per=5
```

说明抢夺成功后活跃度任务按预期 +5（每轮 5、上限 15）。

主服对照：`264/0` 三种载荷形态均返回 `0d` + 全 0（FAILED 13）。

## 未决观察

- 两次进入均显示 `player_hole=0`，即 `rob_mine_hole` 成功计入活跃度但未观察到矿洞归属
  变为自己；矿山各矿脉 `used_count==total_count`（满员），空矿洞出现后应可真正占领。
- 后续可在有玩家占据矿洞且列表出现空位时复查 `player_hole` 是否保持。