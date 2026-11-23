---
name: agymate
description: >-
  通过内置 MCP App（agymate_hub）或本机 AgyMate CLI（~/.local/bin/agymate）与本地遥测 Hub（127.0.0.1:3778）
  查询和管理 Antigravity 多账号实时配额余量、重置倒计时、上下文水位、SQLite Token 消耗与等效 API 账单、
  历史会话全文检索、隔离多开实例沙盒、多账号 Google Colab 云算力池、旧缓存清理及环境健康体检。
  当用户询问剩余额度/配额、Token 花费/回本率、打开 AgyMate 看板、多开 Profile 管理、Colab CCU 算力状态或体检排障时激活。
---

# AgyMate 本地遥测与多开管理技能

本机已部署 **AgyMate** 插件与统一守护服务：
- **原生 MCP App（`agymate_hub`）**：可在 Antigravity 2.34+ 中调用 `get_status`（内联卡片）、`open_dashboard`（右侧面板 / 全页看板）、`get_tokens`、`search_sessions`、`run_doctor`；用户也可从左侧边栏 **Plugins > AgyMate** 或右侧辅助面板 **`+` > AgyMate** 直接打开。
- **可执行文件**：`~/.local/bin/agymate`（若 `PATH` 未包含 `~/.local/bin`，请直接调用 `~/.local/bin/agymate`）
- **本地控制台与 API**：`http://127.0.0.1:3778`（后台守护服务 macOS `io.agymate.hub` / Linux `agymate.service`）
- **配置文件**：`~/.config/agymate/config.json`

当用户询问与配额、账单、上下文、多开实例、Colab 算力或客户端健康相关的信息时，**优先调用 `agymate_hub` MCP 工具（需要展示面板或内联卡片时）或运行 `agymate` 对应子命令获取权威实时数据**，不要凭空估算或手动解析底层 SQLite 二进制文件。

---

## 核心命令速查

### 1. 账号总览、配额余量与重置倒计时

```bash
# 查看按 Google 账号聚合的在线实例、套餐、配额百分比、上下文水位、今日账单及 Colab 概览
~/.local/bin/agymate status
~/.local/bin/agymate status --json

# 查看各在线实例的分桶配额明细、重置倒计时与燃烧耗尽预警（Runway）
~/.local/bin/agymate quota
~/.local/bin/agymate quota --json
```

### 2. SQLite Token 消耗与等效 API 账单统计

支持时间预设 `--preset`：`today`（默认）、`yesterday`、`last_5h`、`last_24h`、`since_monday`、`last_7d`、`last_30d`、`this_month`、`all`。
支持计价口径 `--mode`：`std`（标准价）、`promo`（活动价）、`nc_std`（无缓存标准价）、`nc_promo`（无缓存活动价）。

```bash
# 统计今日 / 最近 7 天 / 本月 Token 消耗、缓存命中率、缓存节省金额、订阅回本倍数与分模型明细
~/.local/bin/agymate tokens --preset today
~/.local/bin/agymate tokens --preset last_7d
~/.local/bin/agymate tokens --preset this_month --json

# 按指定实例过滤（all | main | ide | <profile>）或导出 CSV
~/.local/bin/agymate tokens --preset last_30d --instance main --csv
```

### 3. 跨实例历史会话全文检索（FTS5）

```bash
# 跨全部发现的实例检索历史对话标题、工作区与摘要片段
~/.local/bin/agymate search "<关键词>" --limit 20
~/.local/bin/agymate search "<关键词>" --instance main --json
```

### 4. 多账号 Google Colab 云算力池管理

```bash
# 查询多账号 CCU 余额、每小时燃烧速率、可用 GPU/TPU 池及活跃/孤儿 VM 列表
~/.local/bin/agymate colab
~/.local/bin/agymate colab --json

# 通过 Google OAuth2 绑定新的 Colab 算力账号（需本地 Hub 在线接收回调）
~/.local/bin/agymate colab login --hint user@gmail.com

# 解绑或隐藏指定 Colab 账号
~/.local/bin/agymate colab remove user@gmail.com

# 强制销毁云端活跃/孤儿 Colab VM（高危操作：执行前务必先展示当前活跃 VM 并征得用户确认）
~/.local/bin/agymate colab nuke
~/.local/bin/agymate colab nuke --account user@gmail.com
```

### 5. 隔离多开实例沙盒生命周期（`profile`）

> **安全红线**：`main`（官方主客户端）与 `ide`（Antigravity IDE）受严格只读保护，禁止对其执行 `stop` 或 `delete`。

```bash
# 列出全部主客户端、IDE 与隔离沙盒实例状态（PID、账号、登录态、断链数）
~/.local/bin/agymate profile list

# 创建新的隔离多开沙盒（可选 --desc 备注、--launch 创建后立即启动、--workspace 指定目录）
~/.local/bin/agymate profile create <name> --desc "工作账号" --launch

# 启动 / 停止 / 自愈修复 / 删除指定隔离实例
~/.local/bin/agymate profile launch <name> --workspace /path/to/repo
~/.local/bin/agymate profile stop <name>
~/.local/bin/agymate profile heal <name>        # 无参数或 --all 可批量修复全部隔离实例断链与过期锁
~/.local/bin/agymate profile delete <name>      # 删除前务必向用户二次确认
```

### 6. 环境健康体检、窗口浮窗（HUD）与后台服务

```bash
# 执行全量环境健康检查（客户端版本、断链、OAuth、CDP 目标、Hooks 与 Sidecar 状态）
~/.local/bin/agymate doctor
~/.local/bin/agymate doctor --fix               # 执行一键无损安全修复
~/.local/bin/agymate doctor --redact --json     # 导出脱敏诊断信息（用于提交 Issue）

# 配置查看与修改 / 自包含部署与卸载
~/.local/bin/agymate config show                # 查看当前配置（自动脱敏密钥）
~/.local/bin/agymate config set pricing.currency CNY
~/.local/bin/agymate setup --dry-run            # 预览初始化/部署路径
~/.local/bin/agymate uninstall --dry-run        # 预览卸载影响范围（高危操作：执行前务必确认）

# 检查或开关 Antigravity 客户端中文汉化与右上角 HUD 浮窗
~/.local/bin/agymate launch --install-app       # macOS 创建 ~/Applications/Antigravity (AgyMate).app 快捷启动器
~/.local/bin/agymate launch                     # 以 CDP 增强模式启动官方客户端（开启悬浮窗 + 中文汉化）
~/.local/bin/agymate zh on                      # 启用内置 Antigravity 客户端中文汉化（--builtin 切回内置 / --script 覆盖）
~/.local/bin/agymate zh off                     # 关闭客户端中文汉化
~/.local/bin/agymate zh status
~/.local/bin/agymate zh inject
curl -fsS -X POST -H "X-AgyMate-Token: $(cat ~/.config/agymate/hub.token)" http://127.0.0.1:3778/api/cdp/sync

# 管理后台常驻 Hub 服务（install | restart | status | uninstall）
~/.local/bin/agymate service status
~/.local/bin/agymate service restart
```

### 7. 超龄会话与 Brain 缓存扫描清理

```bash
# 只读扫描可安全回收的旧会话缓存体积（默认每个实例保留最近 20 条活跃/最新会话）
~/.local/bin/agymate cache scan --keep 20

# 执行清理（执行 clean 前必须先跑 scan 向用户汇报可释放空间并获得确认）
~/.local/bin/agymate cache clean --keep 20 --instance all
```

---

## 执行准则

1. **只读优先**：回答状态、配额、账单、缓存体积问题时，直接运行对应只读命令（需要精确结构化字段时加 `--json`），用简洁清晰的简体中文表格或要点呈现给用户。
2. **破坏性操作先确认**：涉及 `colab nuke`、`profile stop`、`profile delete`、`cache clean`、`service uninstall` 时，除非用户在当前指令中已明确要求立即执行，否则先运行只读探测展示影响范围，经用户确认后再执行。
3. **服务离线自愈**：若遇到本地 Hub 未运行（如 `127.0.0.1:3778` 连接失败），先检查 `~/.local/bin/agymate service status`，必要时运行 `~/.local/bin/agymate service install && ~/.local/bin/agymate service restart` 恢复守护服务。
