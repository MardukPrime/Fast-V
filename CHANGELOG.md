# Changelog

All notable changes to FastV are documented here.

## [Unreleased]

### Added

- 分享链接导入 / 导出、剪贴板批量导入、手动节点、收藏、排序与批量启用/禁用/删除。
- 节点与订阅二维码（Go 端生成 PNG data URL）。
- 订阅格式兼容：分享链接、Base64 与 Clash / Meta YAML（vmess / vless / trojan / ss），不支持类型逐条跳过。
- 节点标签分组、按标签筛选与批量设置标签。
- 真实链路 URL 延迟批量测速（临时内核 + SOCKS5 探测），延迟历史记录。
- 自动选择最优节点（容差防抖）与连续失败自动故障切换；TUN 下暂停自动选路。
- Xray 负载均衡（leastPing / leastLoad / roundRobin + observatory），流量统计汇总多出站。
- 入站增强：局域网共享（0.0.0.0）与 SOCKS 入站认证。
- 路由增强：Windows 进程分流（Xray process 字段）、内置规则预设、规则 JSON 导入导出。
- 实时流量统计（Xray StatsService）与每日流量历史。
- 10 项一键自检（内核、配置、端口、权限、系统代理、测试地址、GeoData、数据目录、订阅状态）。
- Windows 系统代理自动设置与恢复。
- 系统托盘、关闭 / 最小化到托盘、开机自启、应用更新检查与下载安装（退出后脚本替换并重启）。
- 完整配置备份 / 恢复。
- 日志导出与复制全部。
- 默认浅色主题；面板内边距、字体栈与排版统一优化。

### Changed

- 设置页拆分为客户端 / 内核 / 智能选路 / 流量统计 / 系统集成 / 备份恢复区块。
- 代理启动前使用 `xray run -test` 校验配置。
- 强调色主题（5 套：天空蓝 / 紫罗兰 / 翡翠绿 / 琥珀橙 / 玫瑰红）与全局快捷键（Ctrl+Shift+F 启停代理、Ctrl+Shift+M 切换代理模式、Ctrl+Shift+N 下一个节点）；托盘菜单支持代理模式切换与重启代理。设置存储升级到 v5。
- 默认导航布局改为底栏模式（存储 v6；旧版本仍为 sidebar 的配置自动迁移到 dock，之后可手动切回侧边模式）。
- 强调色统一覆盖：把剩余硬编码天空蓝组件（服务器页 tint、路由/日志/网络页强调色、弹窗与导航高亮）全部改为随强调色变化；浅色主题中性深色与玻璃高光保持不变。设置存储升级到 v7。
- 全局快捷键支持自定义录制（Ctrl/Alt/Shift/Win + A-Z/0-9/F1-F24，保存时后端校验并规范化，重复组合拒绝），修改后通过 settings:changed 动态重注册、无需重启。
- 新增 WebDAV 云备份：测试连接、上传备份、从云端恢复（Basic Auth、自动创建目录、路径穿越与大小限制防护）。
- 多语言界面（简体中文 / English）：全部页面与弹窗文案 i18n 化，运行时切换即时生效；语言设置随配置持久化。
- UI 审查修复：Xray 未安装文案不再泄漏后端中文、网络诊断标签截断、路由表英文列宽；新增 [UI 覆盖与文字布局审查](docs/ui/UI_AUDIT.md)。
- UI refresh（参考 Clash Verge Rev / v2rayN / FlClash / Hiddify）：顶栏快捷代理模式切换；概览页压缩为连接中枢并补充套餐到期提示；服务器页单行工具栏、按需批量条与紧凑操作列；订阅页卡片化（用量进度、剩余天数、异常态）；新增共享设计令牌与键盘焦点可见性；亮色主题次要文字对比度提升至 WCAG AA。

## [0.1.0] - 2026-10-31

### Added

- Initial Wails v3 + Go project scaffold.
- SolidJS frontend with generated Wails bindings.
- Application metadata service.
- Source-only repository policy; build assets and platform packaging are local-only.
- Architecture, design, ADR, API, operations and testing documentation.
- Go and frontend verification commands documented in the README.
- Xray-core client integration documentation, including validated example configs.
- UI design rules for layout, components, pages, routing editor and accessibility.
- Dashboard, subscription management and basic settings are implemented and running.
- Liquid-glass UI refresh with restrained translucency, tighter typography, navigation icons, dashboard metrics and theme previews.
- Xray-core version detection, GitHub release check, SHA-256 verified download, local install and optional automatic updates.
- Frameless window mode with a custom title bar and window controls.
- Liquid-glass surface refinement with first-class light and dark themes and improved dark-mode text contrast.
- Node management with search, filters, active node selection, enable/disable, deletion and direct TCP latency testing.
- Subscription updates preserve node identity and enabled state through stable source keys.
- Settings split into client settings and core settings, with Xray-specific parameters shown conditionally.
- Backend refactored into a root entrypoint, composition root, feature packages and Wails service adapters.
- Added Xray config generation for VMess, VLESS, Trojan and Shadowsocks nodes.
- Added Xray start/stop, local SOCKS/HTTP inbound, Windows Job Object cleanup and proxy testing through the local inbound.
- Wired proxy mode, local port, log level, Mux and sniffing settings into the generated Xray config.
- Added subscription auto-update scheduler driven by settings.
- Added running-node details to the overview, including resolved IP, protocol, subscription, inbound and PID.
- Added a TUN mode switch under client network takeover, using the Xray TUN inbound with Wintun, administrator preflight and automatic Windows route setup/cleanup.
- Corrected the previous Xray TUN limitation and added Xray TUN configuration validation.
- Theme selection now previews immediately and follows system theme changes when set to system mode.
- Settings major sections are now collapsible and remember their open/closed state.
- Added node config extraction/editing, Xray outbound JSON preview, advanced JSON override, quick node switching and save-and-apply.
- Added ordered routing rule editing mapped to Xray routing rules, with automatic proxy restart.
- Added Xray runtime log capture, live streaming, filtering, pause, auto-scroll, copy and clear.
- Hardened IP detection with ipwho.is self fallback, alternate IP endpoints, request retries and a clearer unreachable-interface error.
- Rewrote README as a product-focused overview of features, usage and roadmap.
- Added GeoData status/update management and Xray API/StatsService configuration in settings.
- Added automatic UAC elevation for TUN mode and a liquid-glass bottom quick dock.
- Added switchable sidebar and bottom-dock navigation layouts.
- Added a sliding liquid-glass selection indicator for bottom navigation and transparent-glass selected states for option cards.
- Unified the liquid-glass surface system across panels, sidebar, title bar, settings and quick actions with pointer-responsive highlights.
- Fixed dock-mode settings alignment and added Xray DNS, routing-domain and TUN MTU settings.
- Added adjustable bottom-dock glass transparency with live preview.
- Unified global content width, topbar, page, settings and dock alignment across both navigation modes.
- Added Subscription-Userinfo parsing with package quota, upload/download usage and expiry display.
- Added FinalMask JSON and structured Sockopt settings mapped into generated Xray outbound streamSettings.
- Added a production Windows build script that enforces `-H=windowsgui` and verifies the PE GUI subsystem.
- Redesigned the dashboard around connection status, current node, package traffic and mainstream proxy quick actions.
- Upgraded the Go module/toolchain to Go 1.27.3.
- Added direct and proxied network diagnostics with exit IP, geolocation, ASN, Cloudflare edge, latency, jitter and optional download speed.
- Removed external detection-site links; only directly integrated network diagnostics remain.
- Enabled WebView2 GPU acceleration by default and raised small typography/contrast for clearer rendering.
- Reduced glass blur and edge blending to improve panel and text edge crispness.