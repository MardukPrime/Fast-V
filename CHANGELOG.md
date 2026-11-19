# Changelog

All notable changes to FastV are documented here.

## [Unreleased]

（下一版本开发中，暂无条目）

## [1.0.1] - 2026-11-19

### Fixed

- 首页布局空白：把「最近 7 天流量」图表移入当前节点所在左列，左列与右侧快捷操作/系统状态同高填满，节点卡片下方不再出现大块空白；最近订阅面板取消固定最小高度，页面底部不再留白。

### Added

- 首页连接卡片新增实时流量波形：代理运行后按真实上下行速率绘制滚动曲线（40 个采样窗口，约 1 分钟趋势），双色下行/上行、面积渐变；配合状态点脉冲、连接光晕漂移与波形呼吸动画，停止代理后自动归零为静息状态。遵循 `prefers-reduced-motion`。

## [1.0.0] - 2026-11-19

首个正式稳定版。汇总自 0.2.x 以来全部功能与产品化收尾。

### Added

- 多平台构建与发布：GitHub Actions 矩阵构建 Windows x64、macOS arm64/amd64（`FastV.app`）与 Linux amd64/arm64（tar.gz），统一生成 SHA-256 校验与平台说明；CI 增加跨平台编译校验（Wails purego 后端）。
- 应用内更新检查按当前平台/架构选择 release 资产；自动安装仍仅限 Windows。
- 配置覆写：设置页可编辑 JSON 对象，深合并进生成的 Xray 配置（对象递归合并、数组/标量替换、null 删除字段），保存时校验 JSON 与 128 KB 上限；已实测生成配置包含覆写字段。
- 订阅独立自动更新：每个订阅的自动更新开关、独立更新间隔（或跟随全局）与自定义 User-Agent；调度器按各订阅到期时间逐个更新，更新请求携带订阅自己的 UA。
- 节点分组：手动分组与自动测速分组（按间隔测速并自动切换最优节点），支持分组过滤、测速并应用、分组管理界面。
- 规则集订阅：远程域名/IP 列表定时下载（兼容 hosts / Adblock / Clash 前缀格式），解析后展开为路由规则并排在用户规则之后；下载失败保留上次规则。
- 深链接导入：Windows 注册 `fastv://` 协议，支持 `fastv://import?url=`、`fastv://node?link=` 与命令行分享链接导入订阅/节点，前端 Toast 反馈。
- 网络救援：检测系统代理残留、系统代理地址不一致、WinHTTP 代理残留、本地端口占用与核心异常，并提供一键修复（重置系统代理、清理 WinHTTP、刷新 DNS、必要时重启内核）。
- 核心看门狗：核心异常退出且非用户主动停止时，按 1s/2s/4s 退避自动重启，最多连续 3 次，稳定运行 60s 后重置计数；用户停止优先。
- 规则调试器：离线匹配域名/IP/进程/端口/协议规则（含规则集规则），返回命中规则、动作、出站与默认策略，并标注 GeoIP/GeoSite 需运行时判定。
- 诊断报告导出：生成脱敏 ZIP（设置白名单摘要、脱敏日志、自检结果），不包含订阅 URL、密码、节点凭据。
- 订阅更新失败时通过本地代理自动重试一次（新增设置开关）；设置存储升级到 v10。
- DNS 审计：读取系统 DNS 与 Xray DNS 配置，自带 DNS 报文构造/解析向多解析器实测 A 记录与延迟，提示明文 DNS、解析器不一致、系统代理模式下可能绕过代理等风险。
- PAC 系统代理模式：生成 `fastv.pac`（私有/本地地址直连，其余走本地代理），通过 AutoConfigURL 接管系统代理，停止时恢复原注册表状态并清理文件。
- 配置快照与回滚：`snapshots/` 下最多保留 20 个快照（手动/自动），支持创建、列表、回滚、删除；回滚前自动创建「回滚前快照」保证可撤销；快照 ID 白名单防路径穿越、单快照 ≤8MB、损坏文件跳过。
- TUN 自检与回退向导：检查平台支持、TUN 开关、管理员权限、Xray 内核与 wintun.dll、代理状态；失败时展示修复建议，可一键关闭 TUN 并切换到系统代理后重启代理。
- 环境清理：检查/清理 fastv:// 协议注册、开机自启项与 PAC 文件，不删除订阅/节点/设置等用户数据，并显示数据目录。
- 托盘状态图标：运行/启动中/异常/停止使用不同颜色，托盘图标随代理状态自动更新。
- 安装与分发：Windows NSIS 安装包（桌面/开始菜单快捷方式、卸载程序，卸载不删除用户数据）；Linux `.deb` 包（安装到 `/opt/fastv`，含 desktop 入口与 GTK/WebKitGTK 依赖声明）；发布工作流对安装包做静默安装/卸载冒烟测试与 `dpkg-deb` 校验；macOS 暂无 DMG/公证（继续提供 zip 并注明签名限制）。
- Linux AppImage：新增 amd64 AppImage 打包（AppDir + AppRun + desktop/图标，AppImage 运行时依赖系统 GTK3/WebKitGTK），发布资产与校验和包含 AppImage；发布前增加预期资产完整性检查；CI 单元测试改为 `-count=2` 以捕获不稳定用例。
- 配置快照导出/导入：快照可导出为与本地备份兼容的 JSON 文件（使用系统保存对话框），也可从本地 JSON 备份文件导入为新快照（校验备份格式、8 MB 上限、保留策略）。
- 运行日志持久化：`<数据目录>/logs/fastv.log` 记录启动/退出/崩溃（含堆栈）与内核/代理日志，5 MB 轮转、保留 3 个文件，便于问题排查。
- 敏感配置加密：Windows 使用当前用户 DPAPI 加密 WebDAV 密码与入站认证密码后写入 `data.json`；旧版明文配置可平滑读取/迁移，解密失败时清空该字段并继续启动；备份与快照仍为明文（导出文件需妥善保管）。
- 首次运行引导：三步引导介绍代理模式、订阅/节点导入与快照、网络救援、诊断报告、日志位置，关闭后不再显示（localStorage 标记）。
- 文档：新增 [用户手册](docs/USER_GUIDE.md)（安装/网络接管/节点分组/路由规则/订阅/备份/安全/诊断/个性化/更新卸载）与 [常见问题 FAQ](docs/product/FAQ.md)（20 条：现象/原因/解决）。
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