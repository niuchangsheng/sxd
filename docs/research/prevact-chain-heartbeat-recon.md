# PrevAct 尾链与心跳 (0/75) 实测结论

日期: 2026-11-22
探针: `test/probes/probe_prevact_anchor_live.go`、`test/probes/probe_levitate_diag_live.go`
账号: qq `s0-tongchen`（同尘）

## 结论

服务端业务请求包尾的 4 字节 PrevAct **必须是上一条业务请求的 `[Module,Action]`**。
心跳 `0/75` (`0x0000004B`) 不参与该业务尾链：携带 `0x4B` 的 357/0 会被静默丢弃。

官方抓包 `/tmp/wuliangjiehai.pcapng` 同样显示尾链等于上一条 C2S 请求：

```
855/1 tail=0x03520000
853/0 tail=0x03570001   (855/1)
853/1 tail=0x03550000   (853/0)
853/2 tail=0x03550001   (853/1)
854/1 tail=0x03560027   (854/39)
853/3 tail=0x03560001   (854/1)
853/4 tail=0x03550003   (853/3)
```

## 对照实验（357/0，同一连接）

| 变体 | 尾链 | 结果 |
| --- | --- | --- |
| A | 上一条业务动作（含登录期 `0x00000002`） | 192 字节正常回包 |
| B | 最后业务回包动作 `0x01650000` | 正常回包 |
| C | 心跳 `0x0000004B` | 超时，无回包 |
| D | `0x00000000` | 超时，无回包 |
| E | 再次使用 `0x01650000` | 正常回包 |

结论：只有「上一条业务请求动作」是合法锚点；心跳与 0 均非法。
被丢弃的请求仍会推进服务端链（E 使用被丢弃的 D 的动作仍然成功）。

Mod 4 `get_sections` 对 `0x4B` 与业务动作两种锚点都接受，因此统一改用
`lastBusinessActionID` 不会影响推图；旧提交 `40a117d` 中的 `0x17`
实际是 `get_buy_power_data`，不是心跳。

## 代码落点

- `internal/client/session.go`
  - `lastBusinessActionID`：最后一次发送的非心跳动作。
  - `Send` 的 `PrevActFromLastSent` / `HasPrevAct` 回退均使用该字段。
- 需要显式锚点的玩法使用 `LastBusinessActionID()`，不要使用 `LastActionID()`
  （后者含心跳）或裸 `LastRxActionID()`（可能是心跳回包）。

## 复现

```
SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
  go run -tags=probe test/probes/probe_prevact_anchor_live.go
```

只读查询，不进入战斗、不消耗元宝。