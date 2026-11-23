# AgyMate Magpie 页面插件

将 **AgyMate 本地控制台** 原生集成至 [Magpie](https://github.com/yetone/magpie) 侧边栏与 Agent 快捷菜单。

## 特性

- **启动前健康预检**：先通过 `fetch`（1.5 秒超时）探测本地 AgyMate Hub 在线状态，彻底避免 Hub 未启动或重启瞬间跳入浏览器 `ERR_CONNECTION_REFUSED` 报错白屏。
- **主题无缝继承**：自动同步 Magpie 的深浅色状态与 `--magpie-*` 色彩令牌（`embed=magpie`）。
- **离线自愈引导**：当本地服务未运行时，展示中文诊断引导卡片，支持一键复制恢复命令、后台每 2.5 秒自动重连检测及手动「立即重连」。

## 安装与端口配置

```bash
magpie plugin add ./magpie-plugin
magpie plugin on ./magpie-plugin

# 可选：自定义本地 AgyMate Hub 监听端口（默认 3778）
magpie plugin options agymate '{"port": 3778}'
```
