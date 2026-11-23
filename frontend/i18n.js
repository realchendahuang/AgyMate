// AgyMate lightweight bidirectional i18n engine (Simplified Chinese <-> English).
// Preserves original Chinese source text in WeakMaps so switching languages
// is instant, lossless, and works seamlessly with dynamic DOM rendering.
(function () {
  'use strict';

  const STORAGE_KEY = 'agymate-lang';

  // Exact phrase dictionary (zh-CN -> en)
  const EXACT = {
    // Document & Navigation
    'AgyMate 控制台': 'AgyMate',
    '工作区': 'Workspace',
    '快速跳转': 'Quick Find',
    '跳转视图、账号或快捷操作…': 'Jump to view, account, or action…',
    '无匹配项': 'No matches',
    '页面': 'Pages',
    '操作': 'Actions',
    '视图': 'View',
    '外观': 'Theme',
    '语言': 'Language',
    '总览': 'Overview',
    '账单': 'Billing',
    '会话': 'Sessions',
    '体检': 'Doctor',
    '缓存': 'Storage',
    '设置': 'Settings',
    '视图切换': 'Navigation',
    '展开或收起侧栏': 'Toggle sidebar',
    '币种': 'Currency',
    '计价币种': 'Currency',
    '美元 ($)': 'USD ($)',
    '人民币 (¥)': 'CNY (¥)',
    '美元': 'USD',
    '人民币': 'CNY',
    '浮窗': 'HUD',
    '同步浮窗': 'Sync HUD',
    '同步窗口浮窗': 'Sync Window HUD',
    '同步中…': 'Syncing…',
    '+ 实例': '+ Profile',
    '新建实例': 'New Profile',
    '切换外观': 'Toggle theme',
    '切换深浅外观': 'Toggle theme',
    '切换语言': 'Switch language',
    '切换中 / EN 语言': 'Switch language (中文 / EN)',
    '刷新': 'Refresh',
    '刷新数据': 'Refresh data',
    '连接中…': 'Connecting…',
    '连接中断，重连中…': 'Disconnected, reconnecting…',

    // Section headers & Overview
    '账号与配额': 'Accounts & Quotas',
    '今日消费': 'Today',
    '本月回本': 'Monthly ROI',
    '本月累计': 'This Month',
    '活跃会话': 'Active',
    '缓存命中': 'Cache Hit',
    '总 Tokens': 'Tokens',
    '15m 速率': '15m Rate',
    '进行中': 'Active',
    '上下文': 'Context',
    '历史检索': 'History',
    '自动修复': 'Auto Fix',
    '注入窗口': 'CDP Targets',
    '模型': 'Models',
    '最近调用': 'Recent Calls',
    '26 周活跃': '26-Week Activity',
    'Colab 算力': 'Colab Compute',
    '余额': 'Balance',
    '速率': 'Rate',
    '算力': 'GPUs',
    '巡检': 'Check',
    '+ 绑定': '+ Link',
    '重连': 'Re-auth',
    '隐藏': 'Hide',
    '聚焦': 'Focus',
    '自愈': 'Heal',
    '扫描': 'Scan',
    '清理': 'Clean',
    '重置': 'Reset',
    '保存': 'Save',
    '创建': 'Create',
    '常规与计价': 'General & Pricing',
    '浮窗与汉化': 'HUD & Localization',
    '通知': 'Notifications',
    '界面': 'Interface',
    '计价与阈值': 'Pricing & Alerts',
    '计价口径': 'Pricing Mode',
    '默认币种': 'Currency',
    'USD / CNY 汇率': 'USD / CNY Rate',
    '月订阅基准 ($)': 'Monthly Sub ($)',
    '填 0 自动按套餐估算回本': '0 auto-estimates from plan',
    '低配额预警 (%)': 'Low Quota Alert (%)',
    '上下文预警 (%)': 'Context Alert (%)',
    '客户端注入': 'Window Injection',
    '后台保持与运行窗口连接': 'Stay connected to running windows',
    '窗口监控浮窗': 'Window HUD',
    '停靠位置': 'Corner',
    '客户端中文汉化': 'Chinese UI',
    '内置动态汉化引擎': 'Built-in localization engine',
    '自定义脚本': 'Custom Script',
    '留空使用内置引擎': 'Leave empty for built-in',
    '生效实例': 'Instances',
    '留空默认全部，逗号分隔': 'Empty = all, comma-separated',
    '发送测试': 'Send Test',
    '启用 Telegram': 'Enable Telegram',
    '完成门槛 (s)': 'Min Duration (s)',
    '超时告警 (s)': 'Stuck Timeout (s)',
    '任务完成提醒': 'On Task Stop',
    '异常中断告警': 'On Error',
    '低配额提醒': 'On Low Quota',
    '启用 Webhook': 'Enable Webhook',
    '地址': 'URL',
    '格式': 'Format',
    '签名密钥': 'Secret',
    'HMAC-SHA256，可选': 'Optional HMAC-SHA256',
    '留空不签名': 'Optional',
    '启用算力监控': 'Enable Monitoring',
    '轮询间隔 (s)': 'Poll Interval (s)',
    '月额度基准': 'Monthly CCU',
    '挂机告警 (m)': 'Idle Alert (m)',
    '低余额预警线': 'Low CCU Alert',
    'CLI 路径': 'CLI Path',
    '跟随系统': 'System',
    '右上角': 'Top Right',
    '自动识别': 'Auto',
    '精简隔离': 'Minimal',
    '标识': 'ID',
    '备注': 'Label',
    '工作账号': 'Work',
    '目录隔离': 'Isolation',
    '初始配置': 'Config Source',
    '同步 MCP': 'Sync MCP',
    '创建后启动': 'Launch Now',
    '确认操作': 'Confirm',
    '详情': 'Details',
    '总占用': 'Total',
    '可释放': 'Reclaimable',
    '保留条数': 'Keep',
    '已扫描': 'Scanned',
    '可清理': 'Cleanable',
    '搜索会话…': 'Search sessions…',
    '暂无账号': 'No accounts yet',
    '暂无进行中会话': 'No active sessions',
    '暂无活跃上下文': 'No active context',
    '暂无历史会话': 'No sessions yet',
    '未找到匹配会话': 'No matching sessions',
    '暂无调用记录': 'No calls yet',
    '暂无体检项': 'No checks yet',
    '暂无 CDP 注入窗口': 'No CDP targets',
    '暂无缓存记录': 'No cached sessions',
    '未绑定 Colab 账号': 'No Colab accounts linked',
    '未命名': 'Untitled',
    '运行': 'Running',
    '7天': '7d',
    '5h': '5h',
    '导出 CSV': 'Export CSV',
    '速度': 'Speed',
    '费用': 'Cost',
    '窗口': 'Window',
    '汉化': 'CJK',
    '对话库': 'DB',
    '总计': 'Total',
    '状态': 'Status',
    '在线 · 账单': 'Online · Billing',
    '离线 · 账单': 'Offline · Billing',
    '暂无可清理的缓存': 'No cleanable cache',
    '清理历史缓存': 'Clean Cache',
    '保留活跃及最近会话，仅移除早期闲置缓存，清理后不可恢复。': 'Keeps active and recent sessions; removes older cache permanently.',
    '停止实例': 'Stop Profile',
    '删除实例': 'Delete Profile',
    '将移除该隔离实例的配置与目录，此操作不可撤销。': 'Removes profile config and directory permanently.',
    '关闭该隔离窗口，可随时重启。': 'Closes this sandbox window; restart anytime.',
    '当前仍有活跃任务，停止将直接关闭窗口。': 'Active task running; stopping closes the window immediately.',
    '全池清场': 'Nuke All VMs',
    '终止云端实例': 'Terminate Cloud VMs',
    '将终止所有运行中与孤儿虚拟机，未保存的内存数据将丢失。': 'Terminates active and orphan VMs; unsaved RAM state will be lost.',
    '隐藏账号': 'Hide Account',
    '解绑账号': 'Unbind Account',
    '隐藏该钥匙串账号的 Colab 卡片。': 'Hide this keychain account card from Overview.',
    '移除本地授权凭据，后续可重新绑定。': 'Removes local OAuth token; you can re-link anytime.',

    // Section headers & Overview
    '账号与配额': 'Accounts & Quotas',
    '在线实例': 'Online Instances',
    '今日消耗': 'Today Spend',
    '今日账单': 'Today Spend',
    '今日调用': 'Today Calls',
    '本月回本': 'Monthly ROI',
    '订阅回本': 'Subscription ROI',
    '缓存命中': 'Cache Hit',
    '缓存命中率': 'Cache Hit Rate',
    '缓存节省': 'Cache Saved',
    '近15分钟速率': '15m Burn Rate',
    '活跃会话': 'Live Sessions',
    '活跃上下文': 'Active Context',
    '上下文水位': 'Context Windows',
    '历史会话检索': 'Conversation Search',
    '历史会话缓存': 'Session Cache',
    '环境健康检查': 'Environment Health',
    '窗口注入目标': 'CDP Injection Targets',
    '按模型统计': 'By Model',
    '按账号统计': 'By Account',
    '最近调用明细': 'Recent Calls',
    '近 26 周活跃热力图': '26-Week Activity Heatmap',
    '云算力巡检': 'Colab Compute Pool',
    '云算力池': 'Colab Compute Pool',

    // Time presets & Filters
    '统计区间': 'Time Range',
    '今日': 'Today',
    '昨日': 'Yesterday',
    '近5小时': 'Last 5h',
    '近5h': '5h',
    '近7天': 'Last 7d',
    '本月': 'This Month',
    '全部': 'All Time',
    '全部时间': 'All Time',
    '账号': 'Account',
    '全部账号': 'All Accounts',
    '口径': 'Pricing',
    '标准 + 缓存': 'Std + Cache',
    '优惠 + 缓存': 'Promo + Cache',
    '标准无缓存': 'Std (No Cache)',
    '优惠无缓存': 'Promo (No Cache)',
    '导出账单': 'Export CSV',
    '按账号或客户端实例筛选账单': 'Filter billing by account or instance',
    '切换官方标准或优惠费率计价口径': 'Switch standard or promo pricing mode',
    '导出当前筛选条件下的账单明细 CSV': 'Export filtered telemetry to CSV',
    '按5分钟': '5m buckets',
    '按15分钟': '15m buckets',
    '按小时': 'Hourly',
    '按天': 'Daily',
    '按月': 'Monthly',
    '输入': 'Input',
    '输出': 'Output',

    // Table headers
    '时间': 'Time',
    '模型': 'Model',
    '速度 · 首字': 'Speed · TTFT',
    '等值费用': 'Est. Cost',
    '等效费用': 'Est. Cost',
    '实例': 'Instance',
    '端口': 'Port',
    '窗口标题': 'Window Title',
    '中文汉化': 'Chinese UI',
    '扩展脚本': 'Extra Script',
    '监控浮窗': 'Window HUD',
    '诊断操作': 'Actions',
    '账号 / 实例': 'Account / Instance',
    '会话标题 / 标识': 'Session Title / ID',
    '推理缓存': 'Brain Cache',
    '会话库': 'Session DB',
    '合计占用': 'Total Size',
    '清理策略': 'Policy',

    // Status & Badges
    '运行中': 'Running',
    '待确认': 'Waiting',
    '空闲': 'Idle',
    '在线空闲': 'Online · Idle',
    '离线': 'Offline',
    '离线快照': 'Cached Snapshot',
    '正常': 'Healthy',
    '警告': 'Warning',
    '异常': 'Error',
    '运行异常': 'Error',
    '配额正常': 'Healthy',
    '配额': 'Quota',
    '5小时': '5h',
    '周配额': 'Weekly',
    '日配额': 'Daily',
    '月配额': 'Monthly',
    '即将重置': 'Resetting soon',
    '周期已重置': 'Cycle reset',
    '即将耗尽': 'Depleting soon',
    '当前周期暂不适用': 'Not applicable in current cycle',
    '建议压缩': 'Compact soon',
    '已加载': 'Loaded',
    '已就绪': 'Ready',
    '已挂载就绪': 'HUD Active',
    '已成功注入': 'Injected',
    '待注入': 'Pending',
    '未注入': 'Not Injected',
    '未挂载': 'Not Mounted',
    '未开启': 'Disabled',
    '未运行': 'Stopped',
    '待配置': 'Not Configured',
    '需重新授权': 'Re-auth Required',
    '消耗中': 'Burning',
    '孤儿实例': 'Orphan VM',
    '孤儿残留实例': 'Orphan VM',
    '受保护保留': 'Protected',
    '可安全清理': 'Cleanable',
    '系统钥匙串': 'Keychain',
    '网页授权': 'Web OAuth',
    '宿主': 'Host',
    '宿主实例': 'Host Instance',
    '主窗口': 'Main',
    '隔离': 'Isolated',
    '隔离实例': 'Isolated Profile',
    'Ultra 套餐': 'Ultra Plan',
    'Pro 套餐': 'Pro Plan',
    '团队套餐': 'Team Plan',
    '免费版': 'Free Plan',

    // Actions & Buttons
    '搜索': 'Search',
    '搜索会话标题、路径或对话内容…': 'Search session titles, workspace paths, or content…',
    '按账号筛选会话': 'Filter sessions by account',
    '安全修复': 'Safe Fix',
    '自动修复常见的钩子配置与目录权限问题': 'Auto-fix common hooks, symlinks, and launcher issues',
    '重注浮窗': 'Re-inject HUD',
    '立即重新扫描客户端窗口并注入监控浮窗': 'Rescan windows and re-inject HUD immediately',
    '+ 新建实例': '+ New Profile',
    '+ 新建隔离实例': '+ New Isolated Profile',
    '+ 绑定云算力账号': '+ Link Colab Account',
    '保留': 'Keep',
    '每个实例保留最近会话条数': 'Recent sessions to keep per instance',
    '每个客户端实例保留最近活跃的会话条数': 'Number of recent active sessions preserved per instance',
    '按账号或实例筛选缓存列表': 'Filter cache list by account or instance',
    '重新扫描': 'Rescan',
    '清理缓存': 'Clean Cache',
    '安全清理早期历史会话文件，自动保护正在运行与最近活跃的会话': 'Safely reclaim old session files while protecting active and recent conversations',
    '立即清理': 'Clean Now',
    '立即重新注入': 'Re-inject Now',
    '置顶': 'Focus',
    '启动': 'Launch',
    '停止': 'Stop',
    '修复': 'Heal',
    '删除': 'Delete',
    '详情': 'Inspect',
    '查看详情': 'Inspect',
    '查看异常': 'Inspect Error',
    '清场': 'Nuke VMs',
    '全池清场': 'Nuke All',
    '全部清场': 'Nuke All',
    '解绑': 'Unbind',
    '解绑账号': 'Unbind Account',
    '隐藏卡片': 'Hide Card',
    '刷新': 'Refresh',
    '重新读取': 'Reload',
    '保存设置': 'Save Settings',
    '保存中…': 'Saving…',
    '处理中…': 'Working…',
    '执行中…': 'Running…',
    '正在创建…': 'Creating…',
    '正在停止…': 'Stopping…',
    '正在删除…': 'Deleting…',
    '正在清理…': 'Cleaning…',
    '正在清场…': 'Terminating…',
    '取消': 'Cancel',
    '确认': 'Confirm',
    '知道了': 'Got It',
    '关闭': 'Close',
    '关闭 (Esc)': 'Close (Esc)',
    '关闭弹窗': 'Close dialog',
    '立即创建': 'Create Profile',
    '在新窗口打开': 'Open in New Tab',
    '稍后处理': 'Later',
    '复制': 'Copy',
    '已复制': 'Copied',
    '发送测试消息': 'Send Test',
    '测试 Webhook': 'Test Webhook',
    '将该客户端窗口置于前台': 'Bring window to front',
    '拉起该隔离客户端实例': 'Launch isolated profile',
    '安全关闭该客户端实例': 'Stop isolated profile',
    '检查并修复实例软链与钩子': 'Heal profile locks and symlinks',
    '移除该隔离实例': 'Delete isolated profile',

    // Settings groups & fields
    '界面与语言': 'Interface & Language',
    '界面语言': 'Interface Language',
    '切换控制台、原生插件与窗口浮窗的显示语言': 'Display language for Dashboard, MCP App, and Window HUD',
    '跟随系统（推荐）': 'Auto (System)',
    '简体中文': '简体中文',
    '窗口浮窗与注入': 'Window HUD & CDP Injection',
    '自动注入守护': 'CDP Watcher Daemon',
    '后台持续探测运行中的客户端窗口并自动保持连接': 'Continuously detect running windows and maintain HUD sync',
    '启用窗口监控浮窗': 'Enable Window HUD',
    '在客户端窗口内显示实时配额、上下文水位与今日消耗': 'Show live quota, context usage, and daily spend inside Antigravity windows',
    '浮窗默认位置': 'Default HUD Corner',
    '新窗口首次挂载或归位时的默认停靠角落': 'Default anchor corner for newly attached or reset HUD',
    '右上角（推荐）': 'Top Right (Recommended)',
    '右下角': 'Bottom Right',
    '左上角': 'Top Left',
    '左下角': 'Bottom Left',
    'Antigravity 客户端中文汉化': 'Antigravity Chinese UI Localization',
    '内置 Antigravity 2.x 动态中文汉化引擎，通过 agymate launch 或隔离多开启动窗口后自动生效': 'Built-in dynamic Chinese localization engine for Antigravity 2.x (active when launched via agymate launch or isolated profiles)',
    '自定义汉化脚本覆盖（可选）': 'Custom Localization Override (Optional)',
    '留空默认使用内置汉化引擎；如需覆盖或调试自定义词库，可填本地 inject.js 路径': 'Leave empty for the built-in engine, or point to a local inject.js file to override',
    '留空使用内置汉化引擎（或填本地 inject.js 路径）': 'Leave empty for built-in engine (or local inject.js path)',
    '加载扩展注入脚本': 'Load External Inject Script',
    '向客户端窗口额外注入下方指定的自定义增强脚本': 'Optionally inject a custom local script into CDP targets',
    '扩展脚本路径': 'External Script Path',
    '本地脚本文件路径，开启扩展注入后生效': 'Local JS script path used when external injection is enabled',
    '浮窗实例白名单': 'HUD Instance Allowlist',
    '留空默认对全部实例生效，多个实例用英文逗号分隔': 'Leave empty for all instances, or comma-separate IDs',
    '全部实例': 'All instances',

    '消息通知（Telegram）': 'Notifications (Telegram)',
    '启用消息推送': 'Enable Telegram Alerts',
    '通过机器人实时接收任务完成、卡顿与低配额提醒': 'Receive completion, approval-wait, and low-quota alerts via Telegram Bot',
    '机器人令牌': 'Bot Token',
    '在 @BotFather 创建机器人后获取的访问凭据': 'Token obtained from @BotFather',
    '接收会话标识': 'Chat ID',
    '接收通知的个人或群组数字编号': 'Numeric user or group Chat ID',
    '完成通知耗时门槛（秒）': 'Min Duration for Stop Alert (s)',
    '仅当任务执行时长达到该秒数时才推送完成通知': 'Only notify task completion if duration reaches this threshold',
    '待确认超时告警（秒）': 'Approval Wait Timeout (s)',
    '会话停留在等待人工确认超过该秒数时推送提醒': 'Notify if a session stays waiting for confirmation longer than this',
    '任务完成通知': 'Notify on Task Stop',
    '运行异常中断告警': 'Notify on Task Error',
    '配额低水位预警': 'Notify on Low Quota',

    '通用 Webhook 通知（Slack / Discord / 飞书 / 钉钉 / Bark）': 'Universal Webhook (Slack / Discord / Feishu / DingTalk / Bark)',
    '启用 Webhook 推送': 'Enable Webhook Alerts',
    '与上方通知条件共享触发规则，可按 URL 自动适配主流平台格式': 'Shares alert triggers above and auto-formats payload by URL host',
    'Webhook 地址': 'Webhook URL',
    '支持 Slack、Discord、飞书、钉钉、企业微信、Bark 或自定义 HTTP(S) 接口': 'Supports Slack, Discord, Feishu, DingTalk, WeCom, Bark, or custom HTTP(S)',
    '负载格式': 'Payload Format',
    '默认自动根据域名识别目标平台 JSON 负载结构': 'Auto-detect JSON format from target hostname',
    '自动识别（推荐）': 'Auto-detect (Recommended)',
    '标准 JSON': 'Generic JSON',
    '飞书 / Lark': 'Feishu / Lark',
    '钉钉': 'DingTalk',
    '企业微信': 'WeCom',
    '签名密钥（可选）': 'Signing Secret (Optional)',
    '填写后将在请求头附带 X-AgyMate-Signature (HMAC-SHA256)': 'Adds X-AgyMate-Signature (HMAC-SHA256) header when set',
    '留空则不签名': 'Optional HMAC secret',

    '计价口径与预警阈值': 'Pricing & Alert Thresholds',
    '默认计费口径': 'Default Pricing Mode',
    '用于折算每次模型调用与缓存命中的等效账单金额': 'Rate card used to estimate API value and cache savings',
    '默认展示币种': 'Display Currency',
    '控制台与桌面浮窗默认使用的货币符号': 'Currency used across Dashboard and HUD',
    '美元 ($)': 'USD ($)',
    '人民币 (¥)': 'CNY (¥)',
    '美元兑人民币汇率': 'USD → CNY Exchange Rate',
    '切换为人民币计价时的折算汇率': 'Conversion rate when displaying CNY',
    '月度订阅费基准（美元）': 'Monthly Subscription Baseline (USD)',
    '填 0 则根据在线账号套餐自动估算本月回本倍数': '0 auto-infers Pro ($20) / Ultra ($200) for ROI calculation',
    '配额低水位预警（%）': 'Low Quota Warning (%)',
    '模型配额剩余百分比低于此阈值时高亮提醒': 'Highlight & alert when remaining quota drops below this %',
    '上下文压缩预警（%）': 'Context Compaction Warning (%)',
    '单次会话上下文占用超过此百分比时建议压缩': 'Warn to compact when context window exceeds this %',

    '云算力巡检（Colab）': 'Cloud Compute Pool (Google Colab)',
    '启用云算力监控': 'Enable Colab Monitoring',
    '在总览页实时展示算力点余额、活跃时长与孤儿实例预警': 'Track CCU balance, burn rate, and orphan VMs on Overview',
    '巡检轮询间隔（秒）': 'Poll Interval (s)',
    '后台刷新云端算力状态的周期': 'Background polling period for Colab status',
    '月度算力基准': 'Monthly CCU Allowance',
    '用于展示剩余算力进度条的月度总额度（如 2000）': 'Reference monthly CCU pool for progress display (e.g. 2000)',
    '长时挂机告警（分钟）': 'Long-Running VM Alert (min)',
    '云端虚拟机连续运行超过此时长时高亮提醒': 'Alert when a cloud VM runs continuously past this duration',
    '算力余额预警线': 'Low CCU Warning Threshold',
    '剩余算力点低于该数值时触发低余额提醒': 'Alert when CCU balance falls below this value',
    '本地巡检工具路径': 'Optional Local CLI Path',
    '可选本地命令行工具路径，未安装时仍可通过网页授权绑定': 'Optional local colab-safe path; Web OAuth works standalone',

    // Create Profile & Confirm Modals
    '新建多开隔离实例': 'New Isolated Profile',
    '分配独立配置目录与调试端口，支持多账号并行在线': 'Create a sandboxed data & config directory for parallel accounts',
    '实例标识': 'Profile ID',
    '字母或数字开头，可含点、连字符与下划线': 'Alphanumeric start; allows letters, digits, ., -, _',
    '显示备注': 'Display Label',
    '展示在卡片与筛选菜单中的易读名称': 'Friendly label shown on cards and filters',
    '主力工作账号': 'Work Account',
    '目录共享策略': 'Symlink Isolation Policy',
    '控制新实例与宿主机配置目录的隔离程度': 'Controls which home directories are symlinked into the sandbox',
    '精简隔离（推荐）': 'Minimal Isolation (Recommended)',
    '完整继承': 'Full Symlinks',
    '空白沙盒': 'Empty Sandbox',
    '初始配置来源': 'Initial Config Source',
    '脱敏继承可保留编辑器习惯并清除原账号登录态': 'Copies editor preferences while stripping credentials',
    '宿主机脱敏继承': 'Sanitized Host Inheritance',
    '空白初始化': 'Clean Defaults',
    '同步扩展服务（MCP）': 'Sync MCP Servers',
    '自动复制工具服务列表并剥离硬编码敏感凭据': 'Copy MCP server list with secret env/headers redacted',
    '创建完成后立即启动': 'Launch Immediately',
    '自动拉起新客户端窗口以便登录新账号': 'Automatically open the new sandbox window for sign-in',
    '请确认操作': 'Please Confirm',
    '详情信息': 'Details',
    '说明': 'Info',

    // Empty states & Details
    '暂未发现客户端账号': 'No Antigravity Accounts Detected',
    '请先启动 Antigravity 客户端，或点击下方按钮创建多开隔离实例': 'Start Antigravity, or click below to create an isolated multi-account profile',
    '当前没有活跃会话': 'No Active Sessions',
    '当客户端正在执行对话任务或等待人工确认时，会在此处实时显示进度': 'Live progress appears here when Antigravity runs tasks or waits for approval',
    '暂无活跃上下文窗口': 'No Active Context Windows',
    '客户端启动并加载会话后，将自动展示实时上下文水位与构成占比': 'Real-time context window usage and breakdown appear once a conversation loads',
    '暂无历史会话记录': 'No Conversations Found',
    '本地会话数据库产生记录后可在此全文检索': 'Full-text search becomes available once local conversation databases exist',
    '未找到匹配的会话记录': 'No Matching Conversations',
    '请尝试更换搜索关键词，或将右侧筛选切换为「全部账号」': 'Try another keyword or switch the account filter to All Accounts',
    '暂无调用明细': 'No Call Records',
    '暂无模型统计': 'No Model Usage',
    '暂无账号统计': 'No Account Usage',
    '暂无趋势数据': 'No Trend Data',
    '暂无体检项目': 'No Health Checks Yet',
    '暂无本地缓存条目': 'No Cached Sessions',
    '暂无多轮记录': 'No Turn History',
    '暂无运行中的 CDP 注入目标窗口（Antigravity 2.33+ 已关闭默认 CDP 端口，可直接使用插件 MCP App 或 Web 控制台）': 'No active CDP targets (Antigravity 2.33+ disables default CDP; use the native MCP App or Web Console)',
    '当前无活跃虚拟机': 'No active cloud VMs',
    '暂未绑定云算力账号': 'No Colab accounts linked yet',
    '绑定 Google 账号后可实时巡检算力点余额、燃烧速率与残留孤儿虚拟机，防止忘记关机白扣配额': 'Link a Google account to monitor CCU balance, burn rate, and orphan VMs',
    '上下文 /context 与遥测详情': 'Context & Telemetry Inspector',
    '历史会话详情': 'Session Details',
    '模型调用详情': 'Model Call Details',
    '窗口注入诊断详情': 'CDP Target Diagnostics',
    '会话标题': 'Session Title',
    '会话标识': 'Conversation ID',
    '所属账号': 'Account',
    '所属实例': 'Instance',
    '工作区目录': 'Workspace',
    '工作区路径': 'Workspace Path',
    '当前模型': 'Model',
    '调用模型': 'Model',
    '容量水位': 'Context Usage',
    '对话步数': 'Steps',
    '最后活跃': 'Last Active',
    '生成速度 · TTFT': 'Decode Speed · TTFT',
    '输入量': 'Input Tokens',
    '输出量': 'Output Tokens',
    '远程端口': 'CDP Port',
    '页面地址': 'Page URL',
    '系统设置已保存生效': 'Settings saved',
    '已重新读取当前配置': 'Configuration reloaded',
    '测试通知已发送，请检查接收会话': 'Test notification sent',
    'Webhook 测试推送已发送': 'Webhook test sent',
    '云算力状态已更新': 'Colab status refreshed',
    '缓存目录扫描完成': 'Cache scan complete',
    '环境检查无异常，无需变更': 'Environment is healthy; no fixes needed',
    '已在浏览器打开 Google 授权页，授权完成后将自动同步': 'Opened Google OAuth in your browser; syncing automatically upon completion',
    '无法连接本地后台服务，请确认 AgyMate 服务正在运行': 'Cannot connect to local AgyMate Hub; check `agymate service status`',

    // Context breakdown categories
    '系统与工具': 'System & Tools',
    '规则/技能/MCP': 'Rules / Skills / MCP',
    '对话消息': 'Conversation Messages',
    '对话消息历史': 'Conversation History',
    '历史上下文': 'History Context',
    '工作区上下文': 'Workspace Context',
    '工作区环境信息': 'Workspace Info',
    '工具定义': 'Tool Definitions',
    '内置工具定义': 'Built-in Tools',
    '工具输出': 'Tool Output',
    '工具返回结果': 'Tool Results',
    '模型与思考': 'Model & Thinking',
    '模型回复': 'Model Response',
    '模型生成': 'Model Generation',
    '用户与对话': 'User & Dialog',
    '用户输入': 'User Input',
    '系统指令': 'System Instructions',
    '系统提示词': 'System Prompt',
    '扩展工具 (MCP)': 'MCP Tools',
    '扩展工具服务': 'MCP Servers',
    '技能指令': 'Skills',
    '自定义规则': 'Custom Rules',
    '用户全局规则': 'Global Rules',
    '长期记忆': 'Memory',
    '代码上下文': 'Code Context',
    '文件上下文': 'File Context',
    '引用文件': 'Referenced Files',
    '未分类项': 'Other',
    '其他内容': 'Other',
    '其他开销': 'Overhead',
    '协议开销': 'Protocol Overhead',
    '未命名会话': 'Untitled Session',
    '未知模型': 'Unknown Model',
    '无工作区路径': 'No workspace',
    '未绑定工作区': 'No workspace',
  };

  // Dynamic regex patterns (zh-CN -> en)
  const PATTERNS = [
    [/^展开其余 (\d+) 条$/, 'Show $1 more'],
    [/^基准 (.+)$/, 'Sub $1'],
    [/^省 (.+)$/, 'Saved $1'],
    [/^(\d+) 待确认$/, '$1 waiting'],
    [/^在线 (\d+\/\d+)(.*)$/, 'Online $1$2'],
    [/^(\d+) 个实例 · (\d+) 在线$/, '$1 profiles · $2 online'],
    [/^(\d+) 个实例 \((\d+) 在线\)$/, '$1 profiles ($2 online)'],
    [/^ · (\d+) 个实例 \((\d+) 在线\)$/, ' · $1 profiles ($2 online)'],
    [/^运行 (\d+)$/, 'Running $1'],
    [/^([\d,]+) 次调用$/, '$1 calls'],
    [/^入 (.+) · 出 (.+)$/, 'In $1 · Out $2'],
    [/^实时数据流已连接 · 在线 (.+)$/, 'Live stream connected · Online $1'],
    [/^(\d+) 个账号 · 在线 (\d+\/\d+)$/, '$1 accounts · Online $2'],
    [/^今日共 (\d+) 次模型调用$/, '$1 model calls today'],
    [/^共 (\d+) 次调用 · (\d+) 次生图$/, '$1 calls · $2 images'],
    [/^共 (\d+) 次调用$/, '$1 calls'],
    [/^命中率 ([\d.]+%) · 已省 (.+)$/, 'Hit $1 · Saved $2'],
    [/^缓存已节省 (.+)$/, 'Cache saved $1'],
    [/^输入 (.+) · 输出 (.+)$/, 'In $1 · Out $2'],
    [/^吞吐 (.+) · 缓存命中 (.+)$/, 'Tokens $1 · Cache $2'],
    [/^订阅基准 (.+) · (\d+) 个账号$/, 'Baseline $1 · $2 accounts'],
    [/^最近 (\d+) 条会话$/, 'Latest $1 sessions'],
    [/^端口 (\d+)$/, 'Port $1'],
    [/^实时消耗 ([\d.]+) 点\/小时$/, 'Burning $1 CCU/h'],
    [/^([\d.]+)\/小时$/, '$1/h'],
    [/^预计 ([\d.]+) 小时耗尽$/, 'Est. empty in $1h'],
    [/^约 ([\d.]+) 小时耗尽$/, 'Empty in ~$1h'],
    [/^(\d+)天(\d+)小时$/, '$1d $2h'],
    [/^(\d+)小时(\d+)分$/, '$1h $2m'],
    [/^(\d+)分钟$/, '$1m'],
    [/^(\d+)秒$/, '$1s'],
    [/^约 (\d+) 分钟$/, '~$1m'],
    [/^约 ([\d.]+) 小时$/, '~$1h'],
    [/^约 (\d+)天(\d+)小时$/, '~$1d $2h'],
    [/^按当前消耗（([\d.]+)%\/小时）预计 (.+)耗尽$/, 'At $1%/h, est. empty in $2'],
    [/^当前窗口额度已耗尽，(.+)后自动恢复$/, 'Quota exhausted; resets in $1'],
    [/^当前窗口额度已耗尽，等待重置$/, 'Quota exhausted; awaiting reset'],
    [/^受上级窗口限制，暂不适用$/, 'Limited by parent quota window'],
    [/^上下文 (.+) \/ (.+)$/, 'Context $1 / $2'],
    [/^扫描耗时 ([\d.]+ms)$/, 'Scanned in $1'],
    [/^客户端版本 (.+)$/, 'Client $1'],
    [/^巡检于 (.+) · 运行中 (\d+) 台$/, 'Checked $1 · $2 active'],
    [/^窗口浮窗已同步（共 (\d+) 个窗口）$/, 'HUD synced ($1 window(s))'],
    [/^窗口浮窗重注完成（共 (\d+) 个窗口）$/, 'HUD re-injected ($1 window(s))'],
    [/^已将窗口置于前台：(.+)$/, 'Focused window: $1'],
    [/^正在启动实例：(.+)$/, 'Launching profile: $1'],
    [/^已停止实例 (.+)$/, 'Stopped profile $1'],
    [/^已删除隔离实例 (.+)$/, 'Deleted profile $1'],
    [/^实例 (.+) 自愈修复完成$/, 'Healed profile $1'],
    [/^账单已导出至 (.+)$/, 'CSV saved to $1'],
    [/^目标账号：(.+)$/, 'Target account: $1'],
    [/^绑定 (.+)$/, 'Link $1'],
    [/^保存设置失败：(.+)$/, 'Failed to save settings: $1'],
    [/^读取配置失败：(.+)$/, 'Failed to load config: $1'],
    [/^刷新数据失败：(.+)$/, 'Refresh failed: $1'],
    [/^同步浮窗失败：(.+)$/, 'HUD sync failed: $1'],
    [/^发送测试通知失败：(.+)$/, 'Test alert failed: $1'],
    [/^Webhook 测试失败：(.+)$/, 'Webhook test failed: $1'],
    [/^操作失败：(.+)$/, 'Action failed: $1'],
    [/^执行体检失败：(.+)$/, 'Doctor check failed: $1'],
    [/^扫描缓存失败：(.+)$/, 'Cache scan failed: $1'],
    [/^加载账单失败：(.+)$/, 'Failed to load billing: $1'],
    [/^加载会话失败：(.+)$/, 'Failed to load sessions: $1'],
  ];

  // Reverse cache for exact source recovery
  const origTextMap = new WeakMap();
  const origAttrMap = new WeakMap();
  const ATTRS = ['placeholder', 'data-tip', 'aria-label', 'title'];

  let currentPref = 'auto'; // auto | zh | en
  let activeLang = 'zh';    // zh | en
  let observer = null;
  let flushScheduled = false;

  function detectSystemLang() {
    const nav = (navigator.language || navigator.userLanguage || 'en').toLowerCase();
    return nav.startsWith('zh') ? 'zh' : 'en';
  }

  function resolveLang(pref) {
    const p = String(pref || 'auto').toLowerCase().trim();
    if (p === 'zh' || p.startsWith('zh-')) return 'zh';
    if (p === 'en' || p.startsWith('en-')) return 'en';
    return detectSystemLang();
  }

  function translateText(raw) {
    if (activeLang !== 'en' || !raw || typeof raw !== 'string') return raw;
    if (!/[\u4e00-\u9fff]/.test(raw)) return raw;
    const trimmed = raw.trim();
    if (!trimmed) return raw;

    let translated = EXACT[trimmed];
    if (!translated) {
      for (let i = 0; i < PATTERNS.length; i++) {
        const [re, rep] = PATTERNS[i];
        if (re.test(trimmed)) {
          translated = trimmed.replace(re, rep);
          break;
        }
      }
    }
    if (!translated) {
      // Partial token replacement for compound status chips/badges
      translated = trimmed
        .replace(/5小时/g, '5h')
        .replace(/周配额/g, 'Weekly')
        .replace(/日配额/g, 'Daily')
        .replace(/月配额/g, 'Monthly')
        .replace(/运行中/g, 'Running')
        .replace(/待确认/g, 'Waiting')
        .replace(/在线空闲/g, 'Idle')
        .replace(/空闲/g, 'Idle')
        .replace(/离线/g, 'Offline')
        .replace(/小时/g, 'h')
        .replace(/分钟/g, 'm')
        .replace(/天/g, 'd')
        .replace(/台实例/g, ' VMs')
        .replace(/台/g, ' VMs')
        .replace(/个账号/g, ' accounts')
        .replace(/个窗口/g, ' windows')
        .replace(/次调用/g, ' calls');
      if (translated === trimmed) return raw;
    }

    const lead = raw.match(/^\s*/)[0];
    const trail = raw.match(/\s*$/)[0];
    return lead + translated + trail;
  }

  function syncTextNode(node) {
    const parent = node.parentElement;
    if (!parent) return;
    const tag = parent.tagName;
    if (tag === 'SCRIPT' || tag === 'STYLE' || tag === 'CODE' || tag === 'PRE') return;

    const cur = node.nodeValue || '';
    let orig = origTextMap.get(node);
    if (orig === undefined) {
      orig = cur;
      origTextMap.set(node, orig);
    } else if (activeLang === 'en') {
      // Detect when app.js wrote fresh Chinese source text into an existing node
      if (/[\u4e00-\u9fff]/.test(cur) && cur !== translateText(orig)) {
        orig = cur;
        origTextMap.set(node, orig);
      }
    } else {
      // In zh mode, keep tracking fresh text updates
      if (cur !== orig) {
        orig = cur;
        origTextMap.set(node, orig);
      }
    }

    const target = activeLang === 'en' ? translateText(orig) : orig;
    if (node.nodeValue !== target) {
      node.nodeValue = target;
    }
  }

  function syncElementAttrs(el) {
    if (!el || el.nodeType !== 1) return;
    let map = origAttrMap.get(el);
    if (!map) {
      map = {};
      origAttrMap.set(el, map);
    }
    for (let i = 0; i < ATTRS.length; i++) {
      const attr = ATTRS[i];
      if (!el.hasAttribute(attr)) continue;
      const cur = el.getAttribute(attr) || '';
      if (map[attr] === undefined) {
        map[attr] = cur;
      } else if (activeLang === 'en') {
        if (/[\u4e00-\u9fff]/.test(cur) && cur !== translateText(map[attr])) {
          map[attr] = cur;
        }
      } else if (cur !== map[attr]) {
        map[attr] = cur;
      }
      const target = activeLang === 'en' ? translateText(map[attr]) : map[attr];
      if (cur !== target) {
        el.setAttribute(attr, target);
      }
    }
  }

  function applyTree(root) {
    if (!root) return;
    if (root.nodeType === 3) {
      syncTextNode(root);
      return;
    }
    if (root.nodeType === 1) {
      syncElementAttrs(root);
    }
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
    let n;
    while ((n = walker.nextNode())) {
      if (n.nodeType === 3) syncTextNode(n);
      else if (n.nodeType === 1) syncElementAttrs(n);
    }
  }

  function updateChrome() {
    document.documentElement.lang = activeLang === 'en' ? 'en' : 'zh-CN';
    document.title = 'AgyMate';
    const btn = document.getElementById('btn-lang');
    if (btn) {
      btn.textContent = activeLang === 'en' ? '中' : 'EN';
    }
  }

  function applyAll() {
    updateChrome();
    if (document.body) {
      applyTree(document.body);
    }
  }

  function scheduleFlush() {
    if (flushScheduled) return;
    flushScheduled = true;
    queueMicrotask(() => {
      flushScheduled = false;
      if (activeLang === 'en' && document.body) {
        applyTree(document.body);
      }
    });
  }

  function ensureObserver() {
    if (observer || !document.body) return;
    observer = new MutationObserver(() => {
      if (activeLang === 'en') scheduleFlush();
    });
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      characterData: true,
      attributes: true,
      attributeFilter: ATTRS,
    });
  }

  function setPreference(pref, opts) {
    currentPref = String(pref || 'auto').toLowerCase().trim();
    if (currentPref !== 'zh' && currentPref !== 'en') currentPref = 'auto';
    activeLang = resolveLang(currentPref);
    if (!opts || opts.persist !== false) {
      try {
        localStorage.setItem(STORAGE_KEY, currentPref);
      } catch (e) {}
    }
    applyAll();
    ensureObserver();
    window.dispatchEvent(new CustomEvent('agymate:langchange', { detail: { pref: currentPref, lang: activeLang } }));
  }

  function init() {
    const qs = new URLSearchParams(location.search);
    const urlLang = qs.get('lang');
    let saved = null;
    try {
      saved = localStorage.getItem(STORAGE_KEY);
    } catch (e) {}
    currentPref = urlLang || saved || 'auto';
    if (currentPref !== 'zh' && currentPref !== 'en') currentPref = 'auto';
    activeLang = resolveLang(currentPref);

    const boot = () => {
      applyAll();
      ensureObserver();
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', boot, { once: true });
    } else {
      boot();
    }
  }

  window.AgyI18n = {
    getPref: () => currentPref,
    getLang: () => activeLang,
    setPreference,
    toggle: () => {
      const next = activeLang === 'en' ? 'zh' : 'en';
      setPreference(next, { persist: true });
      return next;
    },
    t: translateText,
    refresh: applyAll,
  };

  init();
})();
