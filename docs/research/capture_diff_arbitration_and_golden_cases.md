# 官方微端与原版辅助抓包差分仲裁与 Golden Cases 提取报告 (Capture Diff Arbitration & Golden Cases)

- **日期**: 2026-10-11
- **分析人员**: @Ateng
- **关联 Issue**: [#20](https://github.com/atengk/sxd-pie-ng/issues/20)
- **输入样本**:
  - 官方微端抓包: `data/captures/wd_capture_20261011_112551.pcapng` (17.6 MB)
  - 原版辅助抓包: `data/captures/pie_capture_20261011_113023.pcapng` (115 KB)

---

## 一、 双端握手时序差分比对 (Handshake Diff Analysis)

通过运行 `scripts/capture/diff_captures.py` 进行跨端初始会话时序特征分析，前 10 帧交互切片如下：

| 序号 | 官方微端 (A) 交互切片 | 原版辅助 (B) 交互切片 | 差异特征与仲裁判定 |
| :--- | :--- | :--- | :--- |
| **#1** | `C2S M0:A0` (104B) | `C2S M0:A0` (112B) | **字段对齐**：辅助额外填充了版本/来源冗余字符串，官方仅需 104 字节精简载荷 |
| **#2** | `C2S M0:A72` (8B) | `C2S M0:A72` (8B) | **完全一致**：发送 Action 72 参数 `0x00000000` 触发城镇场景初始化 |
| **#3** | `S2C M0:A0` (26B) | `S2C M0:A0` (26B) | **完全一致**：服务端返回登录确认包，第 4 字节为 `ResultCode = 4 (SUCCESS)` |
| **#4** | `C2S M0:A99` (8B) | `C2S M0:A72` (8B) | **⚠️ 协议漂移告警**：官方微端严格按规范在步 1 之后发送 **`Action 99`**（携带参数 `0x00000048`），而原版辅助错误重复发送了一次 `Action 72`！仲裁结论：**以官方 Action 99 为准**！ |
| **#5** | `S2C M558:A2` (14B) | `S2C M0:A72` (18B) | 场景与世界状态广播 |
| **#6~9**| `S2C M558:A2` (14B) | `S2C M558:A2` (14B) | 广播角色场景实例切片 |
| **#10**| `S2C M0:A3` (15B) | `S2C M0:A3` (15B) | **完全一致**：服务端主动推送角色数值属性同步包 (`update_player_data`) |

---

## 二、 协议漂移与失效 Action 仲裁列表 (Drift & Dead Action Arbitration)

依据 **官方基准仲裁原则 (Official Baseline Arbiter)**，剔除并校正以下原版辅助历史遗留问题：

1. **会话步 2 重复发送 Action 72**：
   - *原版行为*：连续发送两次 `Action 72`；
   - *官方行为*：步 1 发送 `Action 72`（城镇拉取），步 2 发送 `Action 99`（确认进场）；
   - *仲裁决策*：**废除原版连续发送两次 Action 72 的做法，按官方 Action 72 ➔ Action 99 标准时序执行**。
2. **心跳保活指令**：
   - *官方行为*：官方微端周期性发送 `Module 0, Action 23`（参数初值 `0x00000030`），服务端应答确认；原版抓包中未见此心跳保活机制（导致长时间空闲容易被网关 RST）；
   - *仲裁决策*：**在 Go 客户端状态机中全面启用 Action 23 心跳保活管道**。

---

## 三、 Golden Cases 二进制测试用例提取与脱敏归档

已从真实微端抓包中提取全套 Module 0 脱机 Golden Packets，并严格完成账号/散列脱敏，落盘至 [`test/testdata/golden/`](file:///d:/sources/my/sxd-pie-ng/test/testdata/golden/)：

- `c2s_m0_a0.bin` (108B): 登录认证请求包（脱敏为占位符）
- `s2c_m0_a0.bin` (30B): 登录认证成功响应包 (`ResultCode=4`)
- `c2s_m0_a72.bin` (12B): 角色场景初始化步 1
- `c2s_m0_a99.bin` (12B): 角色场景初始化步 2
- `c2s_m0_a23.bin` (12B): 官方长连接心跳保活请求包
- `s2c_m0_a23.bin` (12B): 官方心跳保活服务端应答包
- `c2s_m0_a22.bin` (12B): 同步系统时间请求包
- `s2c_m0_a22.bin` (12B): 同步系统时间响应包
- `c2s_m0_a2.bin` (12B): 查询角色属性快照请求包
- `s2c_m0_a3.bin` (19B): 属性主动推送包
- `s2c_m0_a75.bin` (9B): 场景同步确认响应包

以上 Golden Cases 将作为下一工单 [#21](https://github.com/atengk/sxd-pie-ng/issues/21) 离线测试编解码器与自愈解压的确定性基石。
