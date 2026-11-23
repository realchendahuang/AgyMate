# AgyMate（Antigravity 原生插件）

将 **AgyMate** 的本地实时遥测与多开管理能力直接赋予 Antigravity Agent。安装至 `~/.gemini/config/plugins/agymate` 后，支持：

- **Antigravity 原生 MCP App 交互界面（`agymate_hub`）**：
  - 左侧边栏 **Plugins > AgyMate** 全页交互式看板；
  - 右侧辅助面板 **`+` > AgyMate** 侧边栏看板；
  - 对话内直接调用 `get_status` / `open_dashboard` 渲染中文实时配额与账单卡片；
  - Antigravity CLI / IDE 中自动降级为清晰纯文本输出。
- **技能（`skills/agymate/SKILL.md`）**：指导 Agent 调用 MCP 工具或 `~/.local/bin/agymate` 各子命令获取权威实时数据。
- **配合组件**：通过 `agymate setup`（或仓库内 `make install`）可一键自包含部署后台守护服务、无损生命周期 Hooks、原生 Sidecar 与本插件；通过 `agymate uninstall` 可一键干净卸载。
