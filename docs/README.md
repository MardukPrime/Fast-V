# FastV 文档索引

本目录是 FastV 的工程文档入口。设计文档、架构决策、接口契约、内核集成和运维说明都从这里索引。

## 推荐阅读顺序

1. [DESIGN.md](DESIGN.md)：产品目标、需求范围、系统设计和核心流程。
2. [ARCHITECTURE.md](ARCHITECTURE.md)：代码分层、目录结构、依赖规则和包职责。
3. [ui/DESIGN_RULES.md](ui/DESIGN_RULES.md)：UI 布局、组件、交互和页面设计规则。
4. [network-diagnostics.md](network-diagnostics.md)：网络诊断、数据源和在线 IP 工具集成评估。
5. [cores/README.md](cores/README.md)：代理内核文档入口。
6. [cores/xray/README.md](cores/xray/README.md)：Xray-core 客户端集成文档。
7. [adr/README.md](adr/README.md)：重要架构决策记录。
8. [api/wails-services.md](api/wails-services.md)：Wails Service 接口契约。
9. [api/events.md](api/events.md)：后端事件名和载荷。
10. [testing/strategy.md](testing/strategy.md)：测试策略和 fixture 规范。
11. [operations/build.md](operations/build.md)：构建、打包和发布。

## 文档目录

```text
docs/
├── README.md
├── DESIGN.md
├── ARCHITECTURE.md
├── network-diagnostics.md
├── ui/
│   └── DESIGN_RULES.md
├── cores/
│   ├── README.md
│   └── xray/
│       ├── README.md
│       ├── integration.md
│       ├── tun.md
│       ├── node-editor.md
│       ├── profile-mapping.md
│       ├── transports.md
│       ├── routing-dns.md
│       ├── operations.md
│       ├── examples.md
│       └── upstream.md
├── adr/
│   ├── README.md
│   ├── TEMPLATE.md
│   ├── 0001-use-wails-v3.md
│   ├── 0002-use-solidjs.md
│   └── 0003-external-proxy-core.md
├── api/
│   ├── wails-services.md
│   └── events.md
├── operations/
│   ├── build.md
│   ├── release.md
│   └── troubleshooting.md
└── testing/
    ├── strategy.md
    └── subscription-test.md
```

## 内核文档规则

- `cores/` 下的文档只描述 FastV 如何集成对应内核。
- 协议和配置语义以官方文档为准，FastV 文档负责客户端映射、进程管理和 UI 行为。
- 每个内核必须记录测试版本和官方文档快照日期。
- 重要版本或兼容性变化需要同步更新 ADR 或 `CHANGELOG.md`。

## 文档维护规则

- 需求变化更新 `DESIGN.md`。
- 分层、目录、依赖变化更新 `ARCHITECTURE.md`。
- UI 布局、组件或交互规则变化更新 `ui/`。
- 内核版本、配置映射或进程管理变化更新 `cores/`。
- 重要技术选型新增 ADR，不直接改写历史 ADR。
- Wails Service 或事件变化必须同步更新 `api/`。
- 构建、签名、发布流程变化更新 `operations/`。
- 新增测试框架或测试数据规范更新 `testing/`。
- 过期文档标记为 Superseded，并链接到替代文档。
