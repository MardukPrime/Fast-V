# FastV 架构与目录设计

| 项目 | 内容 |
|---|---|
| 文档版本 | v0.2 |
| 状态 | 当前实现基线 |
| 日期 | 2026-11-02 |
| 桌面框架 | Wails v3.1 |
| 前端 | SolidJS + TypeScript + Vite |
| 后端 | Go |
| 当前能力 | Dashboard、订阅、基础设置 |

---

## 1. 架构原则

FastV 当前采用 Wails v3 官方推荐的服务式项目布局：

- 根目录 `main.go` 作为应用入口。
- 根目录 Service 文件作为 Go 与前端的边界。
- `frontend/` 存放 Vite 前端。
- `frontend/bindings/` 由 Wails 自动生成。
- `frontend/dist/` 由 Vite 生成，并嵌入 Go 二进制。
- `build/`、`Taskfile.yml`、`bin/` 等构建配置和产物不入库。

当前阶段不引入微服务、复杂 DDD 分层或额外依赖注入框架。业务规模增长后，再把根目录 Service 拆到独立包中。

---

## 2. 当前目录结构

```text
fastV/
├── main.go                     # Wails 入口，注册 Service、窗口和资源
├── appservice.go               # 应用元数据
├── dashboardservice.go         # 仪表盘聚合数据
├── subscriptionservice.go      # 订阅拉取、更新、删除和节点查询
├── settingsservice.go          # 基础设置
├── models.go                   # 前端 DTO 和内部数据模型
├── store.go                    # 本地 JSON 持久化
├── parser.go                   # 订阅格式和分享链接解析
├── parser_test.go
├── subscription_service_test.go
├── settings_service_test.go
├── frontend/
│   ├── index.html
│   ├── package.json
│   ├── pnpm-lock.yaml
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── bindings/               # Wails 自动生成
│   └── src/
│       ├── App.tsx
│       ├── index.tsx
│       ├── styles.css
│       └── pages/
│           ├── DashboardPage.tsx
│           ├── SubscriptionsPage.tsx
│           ├── SettingsPage.tsx
│           └── PlaceholderPage.tsx
├── docs/
│   ├── README.md
│   ├── DESIGN.md
│   ├── ARCHITECTURE.md
│   ├── ui/
│   ├── cores/
│   ├── adr/
│   ├── api/
│   ├── operations/
│   └── testing/
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

以下文件由本地工具生成或维护，不纳入版本控制：

```text
build/
Taskfile.yml
.github/workflows/
bin/
frontend/dist/
frontend/node_modules/
.fastv-data/
.tools/
*.exe
*.syso
```

---

## 3. Wails v3 Service 边界

```text
SolidJS 页面
  -> frontend/bindings/*.ts
  -> Wails Service 方法
  -> Store / Parser / HTTP Client
```

### AppService

- 返回应用名称、版本、平台、架构和 Wails 版本。

### DashboardService

- 汇总订阅数量、节点数量、启用订阅数量。
- 返回最近更新的订阅。
- 返回当前基础设置和应用信息。

### SubscriptionService

- 添加订阅。
- 拉取 Base64 订阅或分享链接内容。
- 解析 VMess、VLESS、Trojan、Shadowsocks 节点。
- 更新单个订阅或全部订阅。
- 删除订阅及其节点。
- 返回前端安全的 `NodeView`，不暴露原始凭据。

### SettingsService

- 读取、保存和重置基础设置。
- 校验主题、语言、代理模式和订阅更新间隔。

---

## 4. 数据流

### 添加订阅

```text
SubscriptionsPage
  -> SubscriptionService.Add
  -> fetchSubscription
  -> parseSubscriptionContent
  -> Store.AddSubscription
  -> SubscriptionView
```

### 更新订阅

```text
SubscriptionsPage
  -> SubscriptionService.Update
  -> HTTP GET
  -> parse
  -> Store.ReplaceSubscriptionNodes
  -> SubscriptionView
```

更新失败时保留旧节点，并把错误写入订阅的 `LastError`。

### 保存设置

```text
SettingsPage
  -> SettingsService.Save
  -> validate
  -> Store.SaveSettings
  -> Settings
```

---

## 5. 本地持久化

当前使用本地 JSON：

```text
%APPDATA%\FastV\data.json
```

如果系统配置目录不可写，会依次回退到：

```text
%LOCALAPPDATA%\FastV\data.json
<当前工作目录>\.fastv-data\data.json
```

也可以通过环境变量覆盖：

```text
FASTV_DATA_DIR=<目录>
```

持久化数据包括：

- 基础设置
- 订阅 URL 和元数据
- 解析后的节点元数据和原始分享链接

原始分享链接包含凭据，只用于后续内核配置生成，不返回给前端。后续迁移 SQLite 时保持 `Store` 接口不变。

---

## 6. 前端结构

- `App.tsx`：应用壳、导航和主题应用。
- `DashboardPage.tsx`：统计卡片、快速操作和最近订阅。
- `SubscriptionsPage.tsx`：添加、更新、删除订阅，查看节点。
- `SettingsPage.tsx`：主题、语言、代理模式和更新间隔。
- `PlaceholderPage.tsx`：尚未实现的服务器、路由、测速和日志页面。
- `styles.css`：深色/浅色主题、布局和组件样式。

前端只通过 `frontend/bindings` 调用 Go，不直接访问文件或网络。

---

## 7. 测试

```bash
go test .
pnpm --dir frontend run typecheck
pnpm --dir frontend run build
```

当前测试覆盖：

- Base64 订阅解析。
- VMess、VLESS、Trojan、Shadowsocks 分享链接解析。
- 订阅添加、更新、删除和节点查询。
- 设置保存、重置和非法值校验。

---

## 8. 后续演进

当业务复杂度增加时按以下顺序演进：

1. 把根目录 Service 拆分到 `internal/service` 或按业务能力分包。
2. 把 `Store` 从 JSON 迁移到 SQLite。
3. 增加 Xray-core 进程管理和配置生成。
4. 增加代理状态机和系统代理。
5. 增加路由规则编辑器和测速页面。

演进时必须保持 Wails Service 方法签名和前端 DTO 稳定，避免 UI 大改。