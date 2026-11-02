# Wails Service 契约

本文档定义前端可调用的 Wails v3 Service。方法签名以 Go 源码和生成的 bindings 为最终依据。

## ProfileService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | ProfileListRequest | ProfilePage | 分页查询节点 |
| Get | id | ProfileView | 获取节点详情 |
| Save | ProfileInput | ProfileView | 新建或更新节点 |
| Delete | ids | - | 批量删除节点 |
| ImportText | text | ImportResult | 导入分享链接或订阅文本 |

## SubscriptionService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| List | - | []SubscriptionView | 订阅列表 |
| Add | SubscriptionInput | - | 添加订阅 |
| Update | id | UpdateResult | 更新单个订阅 |
| UpdateAll | - | UpdateAllResult | 更新全部订阅 |
| Delete | id | - | 删除订阅 |

## ProxyService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| Start | - | ProxyState | 启动当前节点 |
| Stop | - | - | 停止代理 |
| State | - | ProxyState | 获取当前状态 |
| SelectProfile | id | - | 切换当前节点 |
| SetSystemProxy | enabled | - | 设置或恢复系统代理 |

## RoutingService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| ListRules | - | []RoutingRuleView | 路由规则列表 |
| SaveRule | RoutingRuleInput | RoutingRuleView | 保存规则 |
| DeleteRules | ids | - | 删除规则 |
| Reorder | ids | - | 调整规则顺序 |

## SpeedTestService

| 方法 | 输入 | 输出 | 说明 |
|---|---|---|---|
| TestLatency | ids | []LatencyResult | 批量延迟测试 |
| TestDownload | id | DownloadResult | 下载速度测试 |
| Cancel | - | - | 取消当前测速 |

## 契约规则

- 所有 Service 只返回 DTO。
- 不返回 database/sql 类型、channel、context 或未导出字段。
- 错误使用统一错误 DTO。
- 敏感字段只返回掩码。
- 方法名使用动词开头。
- 输入输出结构变更需要同步更新本文档和前端类型。
