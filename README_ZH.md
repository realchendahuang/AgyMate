# AgyMate

**专为 Antigravity 打造的本地遥测守护中心（Hub）、命令行工具（CLI）、只读 SQLite 账单分析器、安全多开管理器及原生 MCP App 插件套件。**

[English](README.md) | 简体中文

> **开源声明**：AgyMate 是独立的非官方社区伴侣工具。全部数据仅在本机闭环运行，零远端遥测上报；对 Antigravity 本地 SQLite 数据库严格只读（`mode=ro&query_only=1`），不修改官方客户端程序。

---

## 核心特性

- **按 Google 账号聚合的多实例全景视图**
  自动发现官方 `Antigravity`、可选 `Antigravity IDE`、隔离多开目录（`~/.antigravity-profiles/<name>`）以及临时 `--user-data-dir` 进程；将登录同一账号的多个客户端归并至同一张卡片，并持久化离线配额快照。
- **实时配额余量、重置倒计时与燃尽预警（Runway）**
  通过本机 Connect-RPC 轮询活跃 Language Server，实时采集 5 小时 / 周度配额桶、各模型剩余比例、重置倒计时、小时燃烧耗尽预警以及当前会话上下文构成明细。
- **本地 SQLite 只读 Token 账单与订阅回本分析**
  只读扫描本地对话 WAL 数据库（支持今日、昨日、近 5 小时、近 7 天、本月、全部等区间），自动折算 USD / CNY 等效 API 账单、缓存命中节省金额及订阅回本倍数（自动识别 `Pro` `$20` / `Ultra` `$200` 或自定义月费基准）；支持通过 `~/.config/agymate/pricing.json` 热加载自定义模型名称与单价，无需重新编译。
- **多形态轻量入口（无 Electron/WebView 重壳）**
  - **Antigravity 原生插件 & MCP App（v2.34+）**：安装至 `~/.gemini/config/plugins/agymate`，支持左侧栏 **Plugins > AgyMate** 全页视图、右侧 **`+` > AgyMate** 辅助面板与对话内联卡片（`agymate_hub`），CLI/IDE 自动降级纯文本；
  - **独立 Web 控制台**：直接访问 `http://127.0.0.1:3778`（支持一键中英双语切换）；
  - **内置 Antigravity 客户端动态中文汉化 & 右上角毛玻璃悬浮窗（HUD）**：内置完整的 Antigravity 2.x 界面中文汉化引擎（支持自定义本地 `inject.js` 覆盖）与可拖拽 Shadow-DOM 状态微胶囊。隔离多开实例会自动附加临时 CDP 端口；针对 Antigravity `2.33+` 关闭默认调试口的官方客户端，可通过 `agymate launch`（或 macOS 运行 `agymate launch --install-app` 生成 `Antigravity (AgyMate).app` 快捷启动器）在不修改官方安装包、不破坏签名的前提下立即恢复悬浮窗与客户端汉化；
  - **Antigravity Sidecar & Magpie 页面插件**：支持窄栏自适应 Sidecar 以及可选 [Magpie](https://github.com/yetone/magpie) 主题融合内嵌页。
- **安全隔离的多开沙盒生命周期管理**
  基于官方客户端实现多账号隔离运行（`agymate profile create|launch|stop|heal|delete`），自动清理过期锁与修复断链，并对官方 `main` 与 `ide` 实施严格只读保护。
- **多账号 Google Colab 云算力池与孤儿 VM 清场**
  内置 PKCE OAuth2 多账号绑定（`agymate colab login`），实时监控 CCU 余额、燃烧速率与可用 GPU/TPU，支持一键强制销毁残留孤儿虚拟机（`agymate colab nuke`）。
- **无损 Hooks 与多通道消息提醒（Telegram + 通用 Webhook）**
  仅合并 `~/.gemini/config/hooks.json` 中的 `"agymate"` 键（完整保留用户已有第三方 Hooks，绝不拦截权限确认弹窗）；任务完成、异常中断、卡审批或低配额时，支持通过 **Telegram** 与 **通用 Webhook**（自动识别 Slack、Discord、飞书/Lark、钉钉、企业微信、Bark 及 HMAC 签名标准 JSON）推送告警。

---

## 平台支持矩阵

| 平台 | Hub / CLI / 账单 / MCP App | 后台常驻服务 | 多开沙盒 | 支持等级 |
| :--- | :---: | :---: | :---: | :--- |
| **macOS** (`arm64` / `amd64`) | 支持（100% Pure Go，无需 Xcode） | LaunchAgent (`io.agymate.hub`) | 支持（含 `~/Applications` 启动器） | **Tier 1** |
| **Linux** (`amd64` / `arm64`) | 支持 | `systemd --user` (`agymate.service`) | 支持 | **Tier 1** |
| **Windows** (`amd64` / `arm64`) | 支持（Pure Go） | 手动前台（`agymate serve`） | 实验性支持 | **Beta** |

---

## 快速安装

AgyMate 采用 **单二进制自包含设计**（前端、MCP App UI 与插件资源全部内嵌，零 CGO 依赖），安装完毕执行 `agymate setup` 即可一步到位。

### 方式一：一行在线安装（macOS & Linux 推荐）

```sh
curl -fsSL https://raw.githubusercontent.com/realchendahuang/AgyMate/main/scripts/get.sh | sh
```

自动识别系统与架构 → 下载对应 Release 并校验 SHA256 → 安装到 `~/.local/bin/agymate` → 自动运行 `agymate setup`。

### 方式二：Homebrew（macOS）

```bash
brew install realchendahuang/tap/agymate
agymate setup
```

### 方式三：从源码编译安装

```bash
git clone https://github.com/realchendahuang/AgyMate.git
cd AgyMate
make install
```

### 预览变更与一键无残留卸载

```bash
agymate setup --dry-run      # 只读预览将要写入的每个文件与服务路径，绝不改盘
agymate setup --minimal      # 极简模式：仅装配置与后台 Hub，不改动 Antigravity/Magpie
agymate uninstall            # 一键干净卸载服务、Hooks（仅移除 agymate 键）、Sidecar 与插件（保留配置）
agymate uninstall --purge    # 彻底清理 ~/.config/agymate 与 ~/.local/share/agymate
```

---

## 配置指南（零配置开箱 + 三层渐进）

大多数用户无需手改任何 JSON——`agymate setup` 会自动探测客户端路径并启动本地 Hub（`http://127.0.0.1:3778`）。

### 1. 可视化配置（推荐）

打开 `http://127.0.0.1:3778` 或 Antigravity 左侧栏 **Plugins > AgyMate** → **设置**，可直接修改币种、计价口径、预警阈值、HUD 位置、Telegram 推送、通用 Webhook（支持一键测试）以及绑定 Google Colab 账号。

### 2. 终端快捷配置（`agymate config`）

```bash
agymate config path                              # 查看所有配置/状态/定价文件路径
agymate config show                              # 查看当前配置（默认脱敏密钥）
agymate config set pricing.currency CNY          # 切换人民币显示
agymate config set webhook.enabled true          # 启用 Webhook 推送
agymate config set webhook.url "https://open.feishu.cn/open-apis/bot/v2/hook/..."
```

### 3. 声明式文件与高阶覆盖

- **主配置（`0600`）**：`~/.config/agymate/config.json`（完整 Schema 见 [`config.schema.json`](config.schema.json)，环境变量 `AGYMATE_CONFIG` 可覆盖）。
- **自定义模型映射与单价（热加载）**：`~/.config/agymate/pricing.json`（环境变量 `AGYMATE_PRICING` 可覆盖）：
  ```json
  {
    "enums": { "1700": "Gemini 5 Pro" },
    "models": [
      { "match": "gemini 5", "stdIn": 3.0, "stdCache": 0.15, "stdOut": 15.0 }
    ]
  }
  ```
- **状态与凭据存储（`0600`）**：
  - 14 天配额趋势与离线快照：`~/.local/share/agymate/state.json`
  - 本地防 CSRF 鉴权令牌：`~/.config/agymate/hub.token`
  - Colab 多账号 OAuth 凭证：`~/.config/agymate/colab_accounts.json`
  - 可选自定义 Colab OAuth Client：环境变量 `AGYMATE_COLAB_CLIENT_ID` / `AGYMATE_COLAB_CLIENT_SECRET`

---

## CLI 命令速查

```bash
# 安装部署、卸载、配置与后台服务
agymate setup [--dry-run | --minimal | --no-hooks | --no-sidecar | --no-plugin | --with-magpie]
agymate uninstall [--dry-run | --purge]
agymate config path | show [--unmasked] | set <key> <value> | reset --yes
agymate service status | install | restart | uninstall
agymate serve [--addr 127.0.0.1:3778]

# 账号总览、配额、账单与全文检索
agymate status [--json]
agymate quota [--json]
agymate tokens --preset last_7d [--mode std|promo|nc_std|nc_promo] [--instance main] [--csv | --json]
agymate search <关键词> [--limit 25]

# 隔离多开实例沙盒管理（main 与 ide 严格受只读保护）
agymate profile list
agymate profile create <name> [--desc "工作账号"] [--launch]
agymate profile launch | stop | heal | delete <name>

# Google Colab 多账号云算力池
agymate colab [--json]
agymate colab login [--hint user@gmail.com]
agymate colab remove <email>
agymate colab nuke [--account <email>]

# 客户端增强启动（悬浮窗 + 内置汉化）、健康体检与缓存回收
agymate launch [workspace] [--ide] [--install-app]   # 以 CDP 增强模式启动官方客户端或生成 macOS 快捷启动器
agymate zh on [--builtin | --script <path>] | off    # 开关 Antigravity 客户端中文汉化
agymate zh status | inject
agymate doctor [--fix] [--json] [--redact]
agymate cache scan | clean [--keep 20]
```

---

## 安全与隐私承诺

完整安全模型见 [SECURITY.md](SECURITY.md)：

- **严格本地回环**：仅监听 `127.0.0.1`，强制校验回环 IP、`Host` 白名单、`Origin` 策略，全部写操作接口必须携带 `0600` 权限的 `X-AgyMate-Token`；
- **SQLite 绝对只读**：所有对话数据库均以 `mode=ro&_pragma=query_only(1)` 打开；
- **不触碰 Keychain 明文密钥**：macOS 体检仅读取只读元数据，绝不传递 `-w`；
- **一键脱敏排障**：提交 Issue 时运行 `agymate doctor --redact --json`，自动脱敏用户目录与邮箱前缀。

---

## 开发与验证

```bash
make verify       # 依次执行 go vet ./...、go test -race ./... 与 JS 语法校验
make cross-check  # 验证 macOS / Linux / Windows 全平台 Pure Go (CGO_ENABLED=0) 交叉编译
make check        # 执行 verify + 编译二进制
```

欢迎参与贡献，请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 开源许可

[MIT](LICENSE) —— 第三方开源组件与声明见 [NOTICE](NOTICE)。
