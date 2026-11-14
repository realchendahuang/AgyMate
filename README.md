# AgyMate — Antigravity 五开伴侣

单栈 Go + Wails v3 原生 macOS 桌面应用 + Loopback Hub + CLI，专为 **Main (Ultra) + Antigravity IDE + ParaGravity 多开分身 (`p1..p4`)** 架构设计。

## 核心能力与安全边界

- **Main 与 IDE 严格只读保护**：对 `main` (`/Applications/Antigravity.app`) 与 `ide` (`/Applications/Antigravity IDE.app`) 仅读取只读进程信息、本地 Connect-RPC 与 SQLite WAL，**绝不提取 Keychain 密钥、绝不修改应用签名、不提供任何启停/清理按钮**。
- **全实例配额雷达与耗尽预测**：并行轮询各实例本地 `daemon/hub_*.json` (`GetUserStatus` / `RetrieveUserQuotaSummary`)，按重置周期记录 14 天配额曲线，计算 2 小时滚动燃烧速率并在将在重置前耗尽时预警。
- **SQLite WAL 只读 Token 与 API 账单**：使用 `?mode=ro` + `query_only=1` 增量解析 `steps.metadata` 与 `gen_metadata.data` protobuf，`std / promo / nc_std / nc_promo` 四口径估算 API 等值账单，统计 Main Ultra `$100/月` 回本倍数。
- **上下文窗口 (Context Window) 水位计**：解析当前活跃对话 `GetCascadeTrajectoryGeneratorMetadata` 分层构成，超出阈值自动提示 `/compact`。
- **窗口内 Shadow-DOM HUD + CDP 汉化融合**：3 秒周期发现 `DevToolsActivePort`，幂等注入 `~/.antigravity-zh/inject.js` 并在窗口内挂载隔离 Shadow-DOM 状态微胶囊（支持右键跳转、悬停展开详细配额与上下文）。
- **ParaGravity 沙盒生命周期与六维健康体检**：`p1..pN` 安全启停/断链修复/缓存重建、Antigravity-Box 版本漂移检测与同步、`LSCopyDefaultHandlerForURLScheme` 核验、Hooks 无损合并（保留 `herdr` / `marktoast`）与 Antigravity 原生 `AUX_PANE` Sidecar 注册。
- **专属 Telegram Bot 异步告警**：任务完成、异常中断、权限卡住超时或配额低于阈值时发送 HTML 卡片告警。

## 常用 CLI

```bash
agymate status                      # 五开 + IDE 全景状态
agymate quota                       # 各实例配额、重置倒计时与耗尽预测
agymate tokens --preset last_7d     # SQLite Token 与等值账单（支持 --csv / --json）
agymate search <query>              # 跨实例 FTS5 全文检索历史对话
agymate doctor [--fix]              # 六维环境健康体检（并支持一键安全修复）
agymate profile launch|stop|heal|create <pN>
agymate zh status | inject          # 探测或立即注入中文界面与窗口内 HUD
agymate cache scan|clean [--keep 20]
agymate install-hooks               # 无损合并 hooks.json 并注册 Sidecar
agymate serve                       # 前台运行 127.0.0.1:3778 HTTP Hub
```

## 构建与安装

```bash
VERSION=0.1.0 make install
agymate install-hooks
```
