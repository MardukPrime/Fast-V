# FastV 架构与目录设计

| 项目 | 内容 |
|---|---|
| 文档版本 | v0.3 |
| 状态 | 当前实现基线 |
| 日期 | 2026-11-03 |
| 桌面框架 | Wails v3.1 |
| 前端 | SolidJS + TypeScript + Vite |
| 后端 | Go |
| 架构风格 | 模块化单体 + 根入口 + 内部特性包 + Wails 服务适配层 |

---

## 1. 架构目标

FastV 的目录组织遵循以下原则：

- 根目录只保留 Wails 入口和项目级文件。
- 所有业务代码进入 `internal/`，避免被外部模块导入。
- Wails Service 只作为传输适配层，不承载解析、下载、存储等核心逻辑。
- 业务能力按特性分包，不建立无边界的 `utils`、`common`、`services` 大包。
- 只有 `internal/bootstrap` 负责组装具体实现。
- 核心模块不依赖 Wails，便于单元测试和未来迁移。
- 前后端类型契约通过 `internal/model` 和自动生成的 bindings 维护。

---

## 2. 当前目录结构

```text
fastV/
├── main.go
├── internal/
│   ├── bootstrap/
│   │   └── app.go
│   ├── buildinfo/
│   │   └── buildinfo.go
│   ├── model/
│   │   └── models.go
│   ├── store/
│   │   └── store.go
│   ├── subscription/
│   │   ├── fetcher.go
│   │   ├── parser.go
│   │   └── nodes.go
│   ├── diagnostics/
│   │   ├── checker.go
│   │   └── checker_test.go
│   ├── core/
│   │   ├── manager.go
│   │   ├── release.go
│   │   └── installer.go
│   ├── proxy/
│   │   ├── builder.go
│   │   ├── manager.go
│   │   ├── process_windows.go
│   │   ├── tun_windows.go
│   │   ├── tun_other.go
│   │   └── process_other.go
│   └── service/
│       ├── app/
│       │   └── service.go
│       ├── core/
│       │   └── service.go
│       ├── dashboard/
│       │   └── service.go
│       ├── diagnostics/
│       │   └── service.go
│       ├── node/
│       │   └── service.go
│       ├── proxy/
│       │   └── service.go
│       ├── settings/
│       │   └── service.go
│       └── subscription/
│           └── service.go
├── frontend/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── components/
│   │   ├── pages/
│   │   └── styles.css
│   ├── bindings/
│   └── package.json
├── test/
│   └── fixtures/
├── docs/
├── go.mod
├── go.sum
└── README.md
```

以下内容由本地工具生成，不纳入版本控制：

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

## 3. 分层和职责

### 3.1 根入口

`main.go` 只负责：

- `go:embed frontend/dist`
- 调用 `bootstrap.Run(assets)`

不写业务逻辑，不直接创建 Store、数据库或 Service。

### 3.2 组合根

`internal/bootstrap/app.go` 是唯一允许知道所有具体实现的包：

- 创建 Store
- 创建 Core Manager 和 Wails CoreService
- 创建各业务 Service
- 注册 Wails Application、窗口和托盘
- 管理启动和退出生命周期

### 3.3 模型层

`internal/model` 负责：

- 共享领域模型
- 前端 DTO
- View 转换函数
- ID、时间、source key 等稳定的模型辅助函数

模型层不导入 Wails、SQLite、HTTP 或 Windows API。

### 3.4 存储层

`internal/store` 负责：

- 本地 JSON 持久化
- 订阅、节点、设置和当前节点状态
- 原子写入
- 数据目录回退
- 订阅更新时保留节点 ID 和启用状态

当前存储为 JSON，但接口边界保持在 Store 内部，后续可以替换为 SQLite。

### 3.5 订阅模块

`internal/subscription` 负责：

- HTTP 拉取订阅
- Base64 解码
- VMess、VLESS、Trojan、Shadowsocks 分享链接解析
- 节点去重
- 生成稳定 source key
- 构建存储节点

该包不依赖 Wails，也不依赖 store。

### 3.6 内核模块

`internal/core` 负责：

- Xray-core 版本检测
- GitHub Release 查询
- 平台和架构对应资源选择
- 下载
- GitHub SHA-256 digest 校验
- ZIP 解压
- staging 验证
- 安装和版本记录

该包不导入 Wails。内核状态变化通过回调通知服务适配层。

### 3.6.1 代理运行模块

`internal/proxy` 负责：

- 将当前节点和设置转换为 Xray JSON 配置
- 支持 VMess、VLESS、Trojan、Shadowsocks 分享链接
- 根据代理模式生成路由规则
- 将 Xray 参数写入配置
- 校验配置
- 启动和停止 Xray 进程
- Windows Job Object 进程树清理
- 本地 SOCKS/HTTP 入站
- 通过本地代理访问测试地址
- 解析节点域名并返回运行节点信息
- 在 Windows 下使用 Xray TUN inbound、Wintun 和自动路由接管

该包不依赖 Wails。
### 3.6.2 网络诊断模块

`internal/diagnostics` 负责：

- 直连出口 IP 和代理出口 IP 检测
- Cloudflare trace、ipwho.is 和 ipify 兜底
- 归属地、ASN、ISP、Cloudflare 节点、TLS、WARP
- 延迟、抖动和可选下载速度
- 不抓取带 Cloudflare challenge 或验证码的第三方网页

该包不依赖 Wails。
### 3.7 Wails 服务适配层

`internal/service/*` 只负责：

- 暴露前端可调用方法
- 参数校验
- 调用对应内部模块
- 将内部错误映射为前端可处理的错误
- 通过 Wails Event 推送状态

Service 不实现协议解析、下载器、ZIP 解压或数据库细节。

### 3.8 前端

`frontend/src` 只通过生成的 bindings 调用 Go：

- `App.tsx`：窗口壳、导航、主题和当前状态
- `pages/`：Dashboard、Servers、Subscriptions、Settings、Placeholder
- `components/`：共享图标和基础组件
- `styles.css`：双主题和液态玻璃视觉系统

---

## 4. 依赖规则

```text
main.go
  -> internal/bootstrap
  -> internal/service/*
  -> internal/core / internal/subscription / internal/store
  -> internal/model

internal/core          -> internal/model, standard library
internal/proxy         -> internal/model, standard library
internal/subscription  -> internal/model, standard library
internal/diagnostics   -> internal/model, standard library
internal/store         -> internal/model, standard library
internal/service/*     -> internal/core, internal/proxy, internal/subscription, internal/diagnostics, internal/store, internal/model, Wails
internal/bootstrap     -> all concrete implementations
```

禁止：

- `internal/model` 导入 Wails 或 infrastructure。
- `internal/core`、`internal/proxy`、`internal/subscription` 或 `internal/diagnostics` 导入 Wails。
- `internal/store` 直接向 Wails Event 发送消息。
- Service 中直接操作 SQLite、ZIP、注册表或系统 API。
- 在 `main.go` 中写依赖组装。
- 创建无边界 `utils`、`common`、`helpers` 包。

---

## 5. 数据流

### 添加订阅

```text
SubscriptionsPage
  -> SubscriptionService.Add
  -> subscription.Fetch
  -> subscription.ParseContent
  -> subscription.BuildNodes
  -> store.AddSubscription
  -> SubscriptionView
```

### 更新订阅

```text
SubscriptionsPage
  -> SubscriptionService.Update
  -> subscription.Fetch
  -> subscription.ParseContent
  -> subscription.BuildNodes
  -> store.ReplaceSubscriptionNodes
  -> SubscriptionView
```

更新失败时保留旧节点，并记录 `LastError`。

### 节点管理

```text
ServersPage
  -> NodeService.List / SetActive / SetEnabled / Delete
  -> Store
  -> NodeView
```

`NodeService.TestLatency` 执行直接 TCP 连通性检查，不经过代理链路。

### 启动代理

```text
DashboardPage
  -> ProxyService.Start
  -> proxy.Manager
  -> proxy.BuildConfig
  -> xray -test
  -> xray run
  -> 等待 127.0.0.1:<port> 可连接
  -> ProxyState
```

本地入站使用 SOCKS 协议并同时兼容 HTTP 代理请求。代理测试通过该本地入站访问设置中的测试地址。

### 网络诊断

```text
NetworkPage
  -> DiagnosticsService.Check("direct" | "proxy")
  -> diagnostics.Checker
  -> Cloudflare trace
  -> ipwho.is
  -> Cloudflare speed test（可选）
  -> NetworkProbe
```

代理出口检测必须使用当前运行中的本地 SOCKS/HTTP 入站。

### 更新内核



```text
SettingsPage
  -> CoreService.CheckLatest / DownloadLatest / AutoUpdate
  -> core.Manager
  -> GitHub Release API
  -> download + SHA-256 + unzip + staging validation
  -> install
  -> CoreInfo
```

---

## 6. 测试

测试文件与被测包同目录：

```text
internal/subscription/parser_test.go
internal/service/subscription/service_test.go
internal/service/settings/service_test.go
internal/service/node/service_test.go
internal/core/manager_test.go
```

测试不依赖真实网络，使用 `httptest`、临时数据目录和本地 TCP listener。核心下载集成测试只在本地存在 `xray.zip` fixture 时运行，否则跳过。

常用命令：

```bash
go test ./internal/...
go vet ./internal/...
pnpm --dir frontend run typecheck
pnpm --dir frontend run build
```

---

## 7. 演进方向

随着功能增长，保持以下边界：

1. 新业务能力先建 `internal/<feature>`，不要继续往根目录加 Service 文件。
2. Wails 交互统一放 `internal/service/<feature>`。
3. 跨模块共享类型放 `internal/model`，但不要把所有实现细节都放进去。
4. 需要替换持久化时，只改 `internal/store`。
5. 需要替换内核时，扩展 `internal/core` 或新增对应内核包。
6. 保持 `main.go` 和 `bootstrap` 的职责不扩散。
