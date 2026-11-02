# 测试策略

## 测试分层

| 层级 | 位置 | 目标 |
|---|---|---|
| 单元测试 | 与源码同目录 | domain、application、parser、builder |
| 集成测试 | test/integration | SQLite、进程、系统代理、订阅 |
| 前端测试 | frontend/src | store、组件、事件处理 |
| 端到端测试 | test/e2e 或脚本 | 导入、启动、停止、恢复代理 |

## 必测场景

- 分享链接格式正确和异常输入。
- Base64 订阅解析。
- Xray 配置 golden test。
- 内核启动超时和崩溃。
- 系统代理备份与恢复。
- 强制结束后孤儿进程检查。
- 10,000 节点虚拟列表。
- 2,000 行/秒日志压力。
- 敏感数据不进入日志和前端 DTO。

## Fixture 规范

- 分享链接样本放 `test/fixtures/subscriptions/`。
- Xray 配置期望放 `test/fixtures/xray/`。
- 测试样本不包含真实凭据。
- 所有 fixture 使用固定假数据。

## 订阅测试

本地订阅测试步骤、fixture 和代理链路测试说明见 [subscription-test.md](subscription-test.md)。