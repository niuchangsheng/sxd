# 悟法领域协议逆向与自动化对齐

日期: 2026-11-03
基准产物: `uri.xdcdn.net/sxd/tree/4f160f61e0968bd8568bc9cc38bbea6224ed274e/Main.swf` (cur_ver `2026102301`)

## 还原来源

反编译 `com.protocols.Mod_WuFaField` / `com.protocols.Mod_WuFaField_Base` /
`com.datas.WuFaFieldData` / `com.views.wu_fa_field.WuFaFieldView`，并与
`com.Protocol.analyze` 的数组 `Uint16 count` 前缀规则交叉验证。

## 关键结论

1. 此前实现把 ActionID 弄反了：
   - `0x02920000` (Action 0) 是 `panel_info`，请求为空；
   - `0x02920001` (Action 1) 是 `draw`，请求为 `Int field_type + UByte draw_type`。
2. 旧代码先向 Action 1 发送 `Int 2 + UByte 0`，实际是一次合法单抽；
   随后向 Action 0 发送 PrevAct，实际是面板查询。因此日志里的“感悟成功”
   只是面板回包到达，并未判断抽卡结果。
3. `panel_info` 回包**没有 result_code**，只能通过完整结构解析判断有效性；
   `draw` 回包首字节才是官方结果码。
4. 官方结果码成功值为 `2`（不是 0），失败码覆盖元宝不足、资源不足、
   次数上限、背包已满、秘馆类型错误等 18 种语义。
5. 免费次数位于每个秘馆的 `left_free_one_count` / `left_free_ten_count`；
   客户端只有在免费次数可用或玩家确认消耗资源后才发抽卡请求。

## 自动化对齐

`wufa_field` routine 现按以下顺序执行：

1. `panel_info` → `ParseWuFaPanel`；
2. 选择免费十连优先、其次免费单抽的秘馆；无免费次数则跳过，不消耗元宝；
3. `draw` → `ParseWuFaDrawResult`；
4. 结果码分流：
   - 次数类 → `scheduler.ErrAttemptsExhausted`（次日熔断）；
   - 背包满 → `scheduler.ErrBagFull`（短冷却）；
   - 资源/功能/元宝不足 → `scheduler.ErrConditionNotMet`；
   - 已领取/已拥有/已满级 → 正常幂等退出；
   - 协议错误或未知码 → 记录结果码后终止本轮，不盲目重发。

## 备注

Module 658 的 Action 0 与灵道初解状态查询共用 ActionID，但请求/回包结构不同，
自动化侧必须按当前业务上下文解释回包，禁止仅凭 ActionID 判定归属。