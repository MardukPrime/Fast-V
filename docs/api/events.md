# 事件契约

后端通过 Wails v3 Event Bus 向前端推送状态。

## 已实现事件

| 事件 | 载荷 | 频率 | 说明 |
|---|---|---|---|
| core:state | CoreInfo | 状态变化 | 内核检测、检查、下载和安装状态 |
| proxy:state | ProxyState | 状态变化 | 代理启动、运行、停止和异常状态 |
| proxy:log | LogEntry | 日志行 | Xray stdout/stderr 和运行状态日志 |
| subscriptions:changed | 无 | 自动更新后 | 订阅自动更新完成 |

## 计划事件

| 事件 | 载荷 | 说明 |
|---|---|---|
| core:log | LogBatch | 内核日志批量推送 |
| proxy:traffic | TrafficSnapshot | 流量统计 |
| speedtest:progress | SpeedTestProgress | 测速进度 |

## 规则

- 事件载荷必须可 JSON 序列化。
- 不发送密码、UUID、私钥或完整订阅 URL。
- 前端事件监听器必须在页面卸载时取消。
- 新增事件时同步更新本文档和 `CHANGELOG.md`。