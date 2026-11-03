# Wails Service 契约

本文档描述当前已实现的 Wails v3 Service。方法签名以 Go 源码和 `frontend/bindings` 为最终依据。

## AppService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Info | - | AppInfo | 应用名称、版本、平台和构建信息 |

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
| UpdateAll | - | UpdateAllResult | 更新全部启用订阅 |
| Delete | id | - | 删除订阅及其节点 |
| Nodes | id | []NodeView | 查看订阅解析出的节点 |

## NodeService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | NodeFilter | []NodeView | 搜索和筛选节点 |
| Active | - | NodeView | 获取当前节点 |
| SetActive | id | NodeView | 设置当前节点 |
| SetEnabled | id, enabled | NodeView | 启用或禁用节点 |
| Delete | id | - | 删除节点 |
| TestLatency | ids | []NodeLatency | 直接 TCP 连通性测试 |

## SettingsService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Get | - | Settings | 读取客户端和内核设置 |
| Save | Settings | Settings | 校验并保存设置 |
| Reset | - | Settings | 恢复默认设置 |

## CoreService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Info | - | CoreInfo | 当前内核状态 |
| CheckLatest | - | CoreInfo | 检查 GitHub 最新版本 |
| DownloadLatest | - | CoreInfo | 下载、校验并安装最新版 |
| AutoUpdate | - | CoreUpdateResult | 检查并按需更新 |

## ProxyService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Start | - | ProxyState | 生成配置、校验并启动 Xray |
| Stop | - | ProxyState | 停止 Xray |
| State | - | ProxyState | 获取代理状态 |
| Test | - | ProxyTestResult | 通过本地 SOCKS/HTTP 入站访问测试地址 |

## 当前设置字段

| 字段 | 作用 |
|---|---|
| theme | 深色、浅色或跟随系统 |
| language | 界面语言 |
| autoUpdate | 订阅自动更新开关 |
| updateIntervalMinutes | 订阅自动更新间隔 |
| proxyMode | rule、global 或 direct，写入 Xray 路由 |
| coreAutoUpdate | 内核自动更新 |
| coreType | xray 或 singbox |
| xrayLocalPort | 本地 SOCKS/HTTP 入站端口 |
| xrayLogLevel | Xray `log.loglevel` |
| xrayMuxEnabled | 出站 Mux 开关 |
| xrayMuxConcurrency | 出站 Mux 并发数 |
| xraySniffing | 入站流量嗅探 |
| testUrl | 代理测试地址 |

## 契约规则

- Service 只返回前端 DTO，不暴露节点 Raw、订阅 URL 或凭证。
- 错误通过 Promise rejection 返回。
- 修改字段或方法后必须重新生成 bindings。
- 前端只通过 `frontend/bindings` 调用 Go。