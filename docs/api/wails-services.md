# Wails Service 契约

本文档描述当前已实现的 Wails v3 Service。方法签名以 Go 源码和 `frontend/bindings` 为最终依据。

## AppService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Info | - | AppInfo | 应用名称、版本、平台和构建信息 |
| IsElevated | - | bool | 当前进程是否以管理员权限运行 |
| RestartAsAdmin | - | - | 以管理员权限重新启动 |
| AutostartEnabled | - | bool | 读取开机自启状态（HKCU Run） |
| SetAutostart | enabled | bool | 设置开机自启并返回新状态 |
| CheckUpdate | - | UpdateInfo | 检查应用更新（GitHub Release） |

## BackupService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Export | - | string | 导出完整配置 JSON（含节点凭据，注意保管） |
| Import | data | BackupResult | 校验并恢复配置，返回恢复数量 |
| WebDAVTest | - | - | 测试已保存的 WebDAV 连接与写入权限 |
| WebDAVUpload | - | string | 上传备份并返回远端 URL |
| WebDAVDownload | - | BackupResult | 下载云端备份并恢复（Restore 校验后落盘） |
| CreateSnapshot | label | SnapshotInfo | 创建本地配置快照（手动；最多保留 20 个） |
| ListSnapshots | - | []SnapshotInfo | 快照列表（最新在前，损坏文件跳过） |
| RestoreSnapshot | id | BackupResult | 回滚到指定快照（回滚前自动创建安全快照） |
| DeleteSnapshot | id | - | 删除快照（id 做白名单校验） |
| ExportSnapshot | id | string | 通过系统保存对话框把快照导出为 JSON 备份文件，返回路径（取消返回空串） |
| ImportSnapshot | label | SnapshotInfo | 通过系统打开对话框选择 JSON 备份并导入为新快照（取消返回错误「已取消导入」） |

## HistoryService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Traffic | days | []DailyTraffic | 最近 N 天每日上下行流量（1-180） |
| Latencies | limit | []LatencySample | 最近 N 条 URL 延迟样本（1-300） |
| Clear | - | - | 清空流量与延迟历史 |

## SelfCheckService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Run | - | []CheckResult | 运行 10 项自检（内核/配置/端口/权限/系统代理/网络/Geo/数据目录/订阅） |

## DashboardService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Summary | - | DashboardSummary | 订阅、节点、内核、当前节点和最近订阅汇总 |

## SubscriptionService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | - | []SubscriptionView | 订阅列表 |
| Add | SubscriptionInput | SubscriptionView | 拉取、解析并保存订阅 |
| Update | id | SubscriptionView | 更新单个订阅 |
| UpdateAll | - | UpdateAllResult | 更新全部启用的订阅 |
| SetOptions | id, autoUpdate, intervalMinutes, userAgent | SubscriptionView | 更新单个订阅的自动更新开关、独立间隔（0=跟随全局）与自定义 User-Agent |

## EnvironmentService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Inspect | - | EnvironmentReport | 检查 fastv:// 协议注册、开机自启、PAC 文件残留与数据目录 |
| Cleanup | - | CleanupResult | 清理上述系统集成残留（不删除用户数据），返回已清理/失败项 |

## NodeGroupService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | - | []NodeGroup | 节点分组列表 |
| Save | NodeGroup | NodeGroup | 新建或更新分组（节点必须存在；urltest 间隔 0/5–1440） |
| Delete | id | - | 删除分组 |
| Apply | groupID, nodeID | ProxyState | 切换到分组内指定节点 |
| TestGroup | groupID | GroupTestResult | 测速分组并切换到延迟最低节点，返回全部结果 |

## RuleSetService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | - | []RuleSet | 规则集列表 |
| Save | RuleSet | RuleSet | 新建或更新规则集定义（不会立即下载） |
| Update | id | RuleSet | 下载并解析规则集，成功后刷新条目数/时间 |
| UpdateAll | - | int | 更新全部启用的规则集，返回成功数量 |
| Delete | id | - | 删除规则集 |
| ExpandedRules | - | []RoutingRule | 内部使用：展开已启用规则集为路由规则 |

## 订阅与规则数据模型

- `Subscription`/`SubscriptionView`：新增 `autoUpdate`、`updateIntervalMinutes`、`userAgent`。
- `NodeGroup`：`{id,name,type:"manual"|"urltest",nodeIds,testUrl,intervalMinutes,updatedAt}`。
- `RuleSet`：`{id,name,url,kind:"domain"|"ip"|"combined",action,enabled,autoUpdate,intervalMinutes,updatedAt,ruleCount,lastError}`。
| UpdateAll | - | UpdateAllResult | 更新全部启用订阅 |
| Delete | id | - | 删除订阅及其节点 |
| Nodes | id | []NodeView | 查看订阅解析出的节点 |
| QRCode | id | string | 订阅链接二维码（PNG data URL，不暴露 URL 文本） |

## NodeService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | NodeFilter | []NodeView | 搜索和筛选节点（后端按 Order 稳定排序） |
| Active | - | NodeView | 获取当前节点 |
| SetActive | id | NodeView | 设置当前节点 |
| Config | id | NodeConfig | 提取可编辑节点配置 |
| Preview | NodeConfig | NodeConfigPreview | 生成分享链接和 Xray outbound 预览 |
| SaveConfig | NodeConfig | NodeView | 保存编辑后的节点配置 |
| SetEnabled | id, enabled | NodeView | 启用或禁用节点 |
| Delete | id | - | 删除节点 |
| TestLatency | ids | []NodeLatency | 直接 TCP 连通性测试 |
| TestURL | ids | []NodeLatency | 真实链路 URL 延迟测速（批量临时内核） |
| ImportLinks | text | NodeImportResult | 从分享链接/剪贴板批量导入手动节点 |
| ExportLinks | ids | []NodeLink | 导出分享链接（为空导出全部） |
| QRCode | id | string | 节点分享链接二维码（PNG data URL） |
| SetFavorite | id, favorite | NodeView | 收藏/取消收藏 |
| Reorder | ids | - | 手动排序 |
| SetEnabledBatch | ids, enabled | BatchResult | 批量启用/禁用 |
| DeleteBatch | ids | BatchResult | 批量删除 |
| SetTag | id, tag | NodeView | 设置节点标签（空标签清除） |
| SetTagBatch | ids, tag | BatchResult | 批量设置标签 |
| Tags | - | []string | 已有标签列表 |

## SettingsService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Get | - | Settings | 读取客户端和内核设置 |
| Save | Settings | Settings | 校验并保存设置 |
| Reset | - | Settings | 恢复默认设置 |
| TunFallback | - | Settings | 关闭 TUN 模式并切换为系统代理（TUN 自检失败时的一键回退） |

## CoreService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Info | - | CoreInfo | 当前内核状态 |
| CheckLatest | - | CoreInfo | 检查 GitHub 最新版本 |
| DownloadLatest | - | CoreInfo | 下载、校验并安装最新版 |
| AutoUpdate | - | CoreUpdateResult | 检查并按需更新 |
| GeoData | - | GeoDataInfo | 获取 geoip.dat / geosite.dat 状态 |
| UpdateGeoData | - | GeoDataInfo | 下载并更新 GeoData 文件 |

## ProxyService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Start | - | ProxyState | 生成配置、校验并启动 Xray |
| Stop | - | ProxyState | 停止 Xray |
| State | - | ProxyState | 获取代理状态 |
| SwitchNode | id | ProxyState | 切换当前节点；代理运行中时自动重启 |
| Test | - | ProxyTestResult | 通过本地 SOCKS/HTTP 入站访问测试地址 |
| Restart | - | ProxyState | 重启正在运行的代理 |
| Logs | - | []LogEntry | 获取捕获的 Xray 运行日志 |
| ClearLogs | - | - | 清空运行日志 |
| Traffic | - | TrafficStats | 实时流量（窗口增量、会话累计、速率） |
| TestURL | ids | []NodeLatency | 真实链路 URL 延迟（复用临时内核测速） |

## RoutingService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | - | []RoutingRule | 按优先级获取路由规则 |
| Save | RoutingRule | RoutingRule | 校验并保存规则 |
| Delete | id | - | 删除规则 |
| Reorder | ids | - | 按给定顺序重排规则 |
| Reset | - | []RoutingRule | 恢复默认规则 |
| Presets | - | []RoutingPreset | 内置规则预设 |
| ApplyPreset | id | []RoutingRule | 应用预设（替换全部规则） |
| Import | rules | []RoutingRule | 原子导入规则（任一非法则不落盘） |

## DiagnosticsService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Check | mode, includeSpeed | NetworkProbe | 检测直连或本地代理出口的 IP、归属地和网络质量 |
| ExportReport | - | string | 在数据目录生成脱敏诊断报告 ZIP，返回文件路径 |
| DNSAudit | - | DNSAuditResult | 对比系统 DNS / Xray DNS 配置并实测多解析器 A 记录、延迟与风险提示 |
| TunAudit | - | TunAuditResult | TUN 依赖自检（平台/开关/管理员/内核/wintun/代理状态）并给出修复建议与回退提示 |

## RescueService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Inspect | - | RescueReport | 检测系统代理残留、地址不一致、WinHTTP 残留、端口占用与核心异常 |
| Repair | - | RescueResult | 一键修复：代理未运行则重置系统代理并刷新 DNS；运行中则刷新 DNS 并重启内核 |

## RuleDebugService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Debug | input, kind | Result | 离线预演域名/IP/进程/端口/协议命中的规则、动作与出站（kind: auto/domain/ip/process/port/network） |

`mode` 取值为 `direct` 或 `proxy`。`proxy` 模式要求本地代理处于运行状态。
## 当前设置字段

| 字段 | 作用 |
|---|---|
| theme | 深色、浅色或跟随系统 |
| language | 界面语言 |
| layoutMode | 导航布局：sidebar 或 dock（默认 dock，存储 v6 起旧 sidebar 配置自动迁移） |
| dockTransparency | 底栏玻璃背景透明度，5-90 |
| autoUpdate | 订阅自动更新开关 |
| updateIntervalMinutes | 订阅自动更新间隔（全局默认值） |
| proxyMode | rule、global 或 direct，写入 Xray 路由 |
| coreAutoUpdate | 内核自动更新 |
| coreType | xray 或 singbox |
| xrayLocalPort | 本地 SOCKS/HTTP 入站端口 |
| xrayLogLevel | Xray `log.loglevel` |
| xrayMuxEnabled | 出站 Mux 开关 |
| xrayMuxConcurrency | 出站 Mux 并发数 |
| xraySniffing | 入站流量嗅探 |
| tunMode | TUN 模式开关；Windows 下使用 Xray TUN inbound + Wintun，需要管理员权限和 wintun.dll |
| testUrl | 代理测试地址 |
| xrayApiEnabled | 启用本地 Xray API 和 StatsService |
| xrayApiPort | Xray API 监听端口，仅绑定 127.0.0.1 |
| xrayDnsServers | Xray DNS 服务器列表，逗号或换行分隔 |
| xrayDnsStrategy | dns.queryStrategy，支持 UseIP、UseIPv4、UseIPv6、AsIs |
| xrayDomainStrategy | routing.domainStrategy，支持 IPIfNonMatch、IPOnDemand、AsIs |
| configOverride | 用户 JSON 对象，深合并进生成的 Xray 配置（对象递归合并、数组/标量替换、null 删除字段；≤128 KB） |
| xrayTunMtu | TUN inbound MTU，范围 576-9000 |
| xraySockoptTfo | 启用 sockopt.tcpFastOpen |
| xraySockoptMptcp | 启用 sockopt.tcpMptcp |
| xraySockoptKeepIdle | sockopt.tcpKeepAliveIdle |
| xraySockoptKeepIntvl | sockopt.tcpKeepAliveInterval |
| xraySockoptUserTimeout | sockopt.tcpUserTimeout |
| xraySockoptCongestion | sockopt.tcpCongestion |
| xraySockoptDomainStrategy | sockopt.domainStrategy |
| xraySockoptJson | 高级 sockopt JSON，覆盖同名字段 |
| xrayFinalMaskJson | outbound.streamSettings.finalmask JSON 对象 |
| systemProxyMode | Windows 系统代理：system（自动设置/恢复）、pac（生成本地 PAC 并设置 AutoConfigURL）或 off |
| statsEnabled | 启用 Xray StatsService 实时流量统计（默认开启） |
| allowLan | 本地入站监听 0.0.0.0（局域网共享） |
| inboundAuthEnabled | SOCKS 入站用户名/密码认证 |
| inboundUsername / inboundPassword | 入站认证凭据（密码 ≥6 位） |
| autoSelectEnabled | 按 URL 延迟自动选择最优节点（TUN 下暂停） |
| autoSelectIntervalMinutes | 自动选路间隔（1-1440 分钟） |
| autoSelectToleranceMs | 切换容差（0-5000 ms，避免抖动） |
| failoverEnabled | 连续健康检查失败自动故障切换 |
| loadBalanceEnabled | 启用 Xray 多节点负载均衡（需 ≥2 个启用节点，TUN 下回退单节点） |
| loadBalanceMode | leastPing / leastLoad / roundRobin |
| closeToTray | 关闭窗口时隐藏到托盘（而非退出） |
| minimizeToTray | 最小化时隐藏到托盘（best effort） |
| autoCheckUpdate | 启动时自动检查应用更新 |
| autoRestartCore | 核心异常退出时自动重启（看门狗） |
| subscriptionUpdateViaProxy | 订阅直连更新失败时通过本地代理重试一次 |
| hotkeysEnabled | 启用全局快捷键 |
| hotkeyToggleProxy / hotkeyCycleMode / hotkeyNextNode | 三个全局快捷键组合（如 Ctrl+Shift+F；空串表示禁用；保存时规范化并校验重复） |
| webdavUrl / webdavUsername / webdavPassword | WebDAV 服务器地址与 Basic Auth 凭据（本地保存） |
| webdavPath | 远端备份路径，默认 /FastV/fastv-backup.json |

## 契约规则

- Service 只返回前端 DTO，不暴露节点 Raw、订阅 URL 或凭证。
- 错误通过 Promise rejection 返回。
- 修改字段或方法后必须重新生成 bindings。
- 前端只通过 `frontend/bindings` 调用 Go。
## ProxyState 运行节点信息

`ProxyState` 在代理启动后包含：

| 字段 | 说明 |
|---|---|
| nodeId | 当前节点 ID |
| nodeName | 节点名称 |
| nodeProtocol | 节点协议 |
| nodeAddress | 节点服务器地址 |
| nodePort | 节点服务器端口 |
| nodeSubscription | 所属订阅 |
| resolvedIp | 启动时解析到的服务器 IP，失败时为空 |
| proxyMode | 当前代理模式 |
| inbound | 本地入站类型，TUN 模式下为 TUN + SOCKS/HTTP |
| localAddr | 本地 SOCKS/HTTP 地址 |
| startedAt | 启动时间 |
| pid | Xray 进程 PID |

负载均衡模式下 `nodeId` 为空、`nodeName` 为「负载均衡（N 个节点）」，其余字段不变。
## NetworkProbe 字段

| 字段 | 说明 |
|---|---|
| mode | direct 或 proxy |
| proxyAddr | 代理模式下的本地入站地址 |
| ip | 公网出口 IP |
| ipVersion | IPv4 或 IPv6 |
| country / countryCode / region / city | 归属地 |
| asn / organization / isp | ASN 和运营商 |
| timezone | 时区 |
| colo / tls / warp | Cloudflare 节点、TLS 和 WARP 状态 |
| latencyMs / jitterMs | 平均延迟和抖动 |
| downloadMbps / downloadBytes | 可选下载测速结果 |
| geoError / downloadError | 非致命的部分失败信息 |
| checkedAt | 检测时间 |
## NodeConfig 字段

| 字段 | 说明 |
|---|---|
| id / name / protocol / address / port | 节点基础信息 |
| uuid / password / method / security / flow | 协议凭证与加密参数 |
| network / tls / serverName / fingerprint / alpn | 传输与安全层 |
| host / path / serviceName | WebSocket、gRPC、HTTPUpgrade、XHTTP 参数 |
| publicKey / shortId / spiderX | REALITY 参数 |
| allowInsecure | 允许不安全 TLS |
| outboundJson | 高级 Xray outbound JSON 覆盖；为空时由表单生成 |

`NodeConfigPreview` 同时返回重新生成的分享链接和 Xray outbound JSON。保存高级 JSON 后，代理配置会优先使用该 outbound。
## RoutingRule 字段

| 字段 | 说明 |
|---|---|
| id / name | 规则标识和名称 |
| type | domain_suffix、domain_keyword、domain_regex、ip、geosite、geoip、port、network |
| value | 规则内容；部分类型支持逗号或换行分隔多个值 |
| action | proxy、direct、block |
| enabled | 是否参与匹配 |

## LogEntry 字段

| 字段 | 说明 |
|---|---|
| id | 日志序号 |
| timestamp | 采集时间 |
| level | debug、info、warning、error |
| source | stdout、stderr、core、tun、system |
| message | 原始日志内容 |
## GeoDataInfo 字段

| 字段 | 说明 |
|---|---|
| directory | GeoData 所在内核目录 |
| geoIp | geoip.dat 文件状态 |
| geoSite | geosite.dat 文件状态 |
| files | 目录内全部 .dat 文件 |
## SubscriptionView 用量字段

| 字段 | 说明 |
|---|---|
| uploadBytes | 订阅端返回的上传流量 |
| downloadBytes | 订阅端返回的下载流量 |
| totalBytes | 套餐总流量 |
| expireAt | 到期时间，Unix 秒 |
| usageUpdatedAt | 用量信息最近更新时间 |
| autoUpdate | 该订阅是否参与自动更新（全局开关仍为总开关） |
| updateIntervalMinutes | 该订阅独立更新间隔；0 表示跟随全局 |
| userAgent | 拉取该订阅时使用的 User-Agent；空表示默认 FastV/<版本> |

数据来自订阅响应头 `Subscription-Userinfo`，格式示例：

```text
upload=1024; download=2048; total=1048576; expire=1893456000
```
## DashboardSummary 用量字段

| 字段 | 说明 |
|---|---|
| uploadBytes | 所有订阅上传流量合计 |
| downloadBytes | 所有订阅下载流量合计 |
| totalBytes | 所有订阅套餐流量合计 |