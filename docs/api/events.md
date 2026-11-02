# 事件契约

后端通过 Wails v3 Event Bus 向前端推送状态。事件名在 `internal/transport/wails/events/names.go` 中集中定义。

| 事件 | 载荷 | 频率 | 说明 |
|---|---|---|---|
| proxy:state | ProxyStateDTO | 状态变化 | 代理状态机变化 |
| proxy:traffic | TrafficSnapshotDTO | 1 秒 | 上下行流量 |
| core:log | LogBatchDTO | 100-250ms | 批量日志 |
| core:error | CoreErrorDTO | 异常时 | 内核错误 |
| profiles:changed | ProfileChangeDTO | 数据变化 | 节点列表变化 |
| subscriptions:changed | SubscriptionSummaryDTO | 更新后 | 订阅变化 |
| speedtest:progress | SpeedTestProgressDTO | 200-500ms | 测速进度 |
| app:notification | NotificationDTO | 按需 | 系统通知 |

## 规则

- 事件载荷必须可 JSON 序列化。
- 高频事件必须批量发送。
- 不发送密码、UUID、私钥或完整订阅 URL。
- 前端事件监听器必须在页面卸载时取消。
- 新增事件时同步更新本文档。
