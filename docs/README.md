# FastV 文档索引

本目录是 FastV 的工程文档入口。设计文档、架构决策、接口契约和运维说明都从这里索引。

## 推荐阅读顺序

1. [DESIGN.md](DESIGN.md)：产品目标、需求范围、系统设计和核心流程。
2. [ARCHITECTURE.md](ARCHITECTURE.md)：代码分层、目录结构、依赖规则和包职责。
3. [adr/README.md](adr/README.md)：重要架构决策记录。
4. [api/wails-services.md](api/wails-services.md)：Wails Service 接口契约。
5. [api/events.md](api/events.md)：后端事件名和载荷。
6. [testing/strategy.md](testing/strategy.md)：测试策略和 fixture 规范。
7. [operations/build.md](operations/build.md)：构建、打包和发布。

## 文档目录

```text
docs/
├── README.md
├── DESIGN.md
├── ARCHITECTURE.md
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
    └── strategy.md
```

## 文档维护规则

- 需求变化更新 `DESIGN.md`。
- 分层、目录、依赖变化更新 `ARCHITECTURE.md`。
- 重要技术选型新增 ADR，不直接改写历史 ADR。
- Wails Service 或事件变化必须同步更新 `api/`。
- 构建、签名、发布流程变化更新 `operations/`。
- 新增测试框架或测试数据规范更新 `testing/`。
- 过期文档标记为 Superseded，并链接到替代文档。
