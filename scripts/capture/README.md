# 抓包分析与协议差分套件 (Traffic Capture & Diff Suite)

本目录归档针对《神仙道》网络通信流量（官方微端 `sxd_wd_*.exe` 与原版辅助 `pie.exe`）的流量嗅探、过滤、实时二进制解码与双端协议差分对比工具。

## 目录结构

```text
scripts/capture/
├── requirements.txt      # 依赖清单 (scapy, colorama)
├── capture_client.py     # 官方微端抓包器 (Web 登录认证 + TCP 8381 游戏封包)
├── run_capture.bat       # 官方微端一键管理员运行脚本
├── capture_pie.py        # 原版辅助 (pie.exe) 抓包器 (长连接感知 + TCP 8381 游戏封包)
├── run_capture_pie.bat   # 原版辅助一键管理员运行脚本
├── diff_captures.py      # 双端抓包差分比对分析器 (Module/Action 序列特征对比)
└── README.md             # 本说明文档
```

## 存储规划与命名规范

为保障代码与测试资产物理分离，抓包生成的数据文件**默认统一存储于项目 [`data/captures/`](../../data/captures/) 目录**，并自动注入精确时间戳，多次抓包绝不覆盖：

| 抓包目标 | 启动入口 | 默认存储路径 |
| :--- | :--- | :--- |
| **官方微端** (`sxd_wd_*.exe`) | `run_capture.bat` | `data/captures/wd_capture_YYYYMMDD_HHMMSS.pcapng` |
| **原版辅助** (`pie.exe`) | `run_capture_pie.bat` | `data/captures/pie_capture_YYYYMMDD_HHMMSS.pcapng` |

## 快速使用

### 步骤一：抓取官方微端数据
直接双击运行：
```cmd
scripts\capture\run_capture.bat
```
*(操作微端进行登录、进入城镇或参与特定玩法，完成后按 `Ctrl+C` 停止)*

### 步骤二：抓取原版辅助数据
直接双击运行：
```cmd
scripts\capture\run_capture_pie.bat
```
*(在辅助中挂机同一玩法，完成后按 `Ctrl+C` 停止)*

### 步骤三：一键双端协议差分对比
在终端执行：
```bash
python scripts/capture/diff_captures.py
```
分析器将自动寻找 `data/captures/` 中最新的一份微端抓包和辅助抓包，输出：
1. **指令分布差分表**：直观展示哪些 Module/Action 是微端独有、辅助独有或完全对齐；
2. **初始时序切片比对**：前 10 帧握手发包时序高亮对照，快速定位辅助协议实现的差异细节。
