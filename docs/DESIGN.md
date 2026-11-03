# FastV 设计文档

| 项目 | 内容 |
|---|---|
| 项目名称 | FastV |
| 文档版本 | v0.1 |
| 文档状态 | 设计草案 |
| 日期 | 2026-10-31 |
| 目标平台 | Windows 10/11 x64 优先，后续扩展 macOS/Linux |
| 桌面框架 | Wails v3 |
| 前端 | SolidJS + TypeScript + Vite |
| 后端 | Go |
| 代理内核 | Xray-core 为主，sing-box 为后续扩展 |

---

## 1. 项目概述

FastV 是一个使用 Go + Wails v3 实现的桌面代理客户端，参考 v2rayN 的核心工作流，提供节点管理、分享链接导入、订阅更新、代理启停、系统代理、路由规则、节点测速、日志查看和托盘驻留等能力。

项目不重新实现 VMess、VLESS、Trojan、Shadowsocks、TLS、Reality、QUIC 等协议，而是通过外部代理内核执行真实的网络代理：

- Go 后端：负责配置编排、数据库、订阅、进程管理、系统集成和业务状态。
- Wails v3：负责桌面窗口、原生托盘/菜单、对话框、剪贴板、通知和前后端绑定。
- SolidJS 前端：负责高性能 UI 渲染和用户交互。
- Xray-core / sing-box：作为独立子进程运行，承担实际代理流量。

### 1.1 设计目标

1. 复刻 v2rayN 的核心使用路径，而不是像素级复制 UI。
2. 保持代理数据面与 UI 解耦，前端框架不影响代理吞吐。
3. 支持大规模节点列表、日志高频刷新和实时流量图表。
4. 代理内核崩溃或被强制结束后，不遗留系统代理和孤儿进程。
5. 敏感凭据不以明文落盘，不进入日志，不直接下发到前端。
6. 保持模块边界清晰，后续可增加 sing-box、跨平台 TUN 等扩展能力。

### 1.2 非目标

- 不在本项目内实现代理协议栈。
- 不提供云端账号、远程配置托管或遥测服务。
- 第一版不支持按进程分流、macOS/Linux TUN。
- 第一版不追求与 v2rayN 完全一致的路由语法和导入格式。
- 不复制 v2rayN 的源码、图标、品牌素材。

---

## 2. 术语

| 术语 | 说明 |
|---|---|
| Profile | 一个代理节点配置 |
| Subscription | 订阅，一个包含多个分享链接的远程地址 |
| Core | 代理内核进程，例如 xray.exe |
| Local Inbound | Xray Socks 入站，同时兼容 SOCKS 和 HTTP 的本地代理入口 |
| System Proxy | Windows 全局 HTTP/HTTPS 代理设置 |
| TUN | 虚拟网卡模式，需要管理员权限 |
| Event | Wails 后端主动推送给前端的事件 |
| Binding | Wails 生成的 TypeScript 服务调用封装 |

---

## 3. 需求设计

### 3.1 功能需求

#### P0：MVP 必须实现

- 单实例运行。
- 节点 CRUD、启用/禁用、排序、搜索。
- VMess、VLESS、Trojan、Shadowsocks 解析与配置生成。
- TCP、WebSocket、gRPC 传输。
- TLS、Reality、SNI、ALPN、Fingerprint、Flow 等常用参数。
- 分享链接导入：`vmess://`、`vless://`、`trojan://`、`ss://`。
- 订阅添加、更新、删除、手动/定时更新。
- 节点切换、代理启动/停止。
- 本地 SOCKS/HTTP 入站。
- Windows 系统代理设置与恢复。
- 日志查看、清空、级别过滤。
- 托盘图标、托盘菜单、关闭窗口隐藏到托盘。
- 开机启动。
- 基础路由：直连、代理、阻断。
- 中英文国际化。
- 深色/浅色主题。

#### P1：增强功能

- Hysteria2、TUIC、WireGuard。
- sing-box 适配。
- 延迟测试、下载速度测试、批量测速和排序。
- 完整路由规则编辑器。
- GeoSite/GeoIP 规则集下载与更新。
- 流量统计、上下行速率图表。
- TUN 模式。
- 配置备份/恢复。
- 自动更新检查。
- 协议链接注册与深链接启动。
- 多窗口：日志窗口、规则编辑器窗口。

#### P2：长期功能

- 按进程分流。
- 负载均衡和故障转移。
- 多订阅合并、订阅分组。
- macOS/Linux 适配。
- 插件化代理内核。

### 3.2 非功能需求

| 指标 | 目标 |
|---|---|
| 应用冷启动 | 不启动内核时 < 1.5 秒 |
| 前端可交互 | < 2 秒 |
| 10,000 节点列表滚动 | 平均 55 FPS 以上 |
| 日志吞吐 | 每秒 2,000 行不阻塞 UI |
| 流量图表刷新 | 1 秒粒度，绘制耗时 < 16ms |
| 配置生成 | 单节点 < 50ms |
| 内核启动探测 | 正常情况 < 3 秒 |
| UI 常驻内存 | 目标 < 150MB，不含内核 |
| 崩溃恢复 | 主程序被强杀后不遗留内核进程，下次启动恢复系统代理 |
| 凭据保护 | 数据库和日志中无明文密码/UUID |

---

## 4. 总体架构

```text
┌─────────────────────────────────────────────────────┐
│                    SolidJS 前端                      │
│                                                     │
│  页面：Dashboard / Servers / Subscriptions /        │
│        Routing / SpeedTest / Logs / Settings        │
│                                                     │
│  TanStack Virtual / Kobalte / uPlot / UnoCSS        │
└───────────────────┬─────────────────────────────────┘
                    │ Wails v3 generated bindings
                    │ typed service calls
                    │ batched events
┌───────────────────▼─────────────────────────────────┐
│                    Go 后端                          │
│                                                     │
│  Service 层：ProfileService / SubscriptionService   │
│              ProxyService / RoutingService          │
│              SpeedTestService / SettingsService     │
│                                                     │
│  业务层：ProfileManager / SubscriptionManager       │
│          ProxyManager / ConfigBuilder               │
│                                                     │
│  基础设施：SQLite / SecretStore / Logging           │
│            SystemProxy / Autostart / Tray           │
└───────────────────┬─────────────────────────────────┘
                    │ os/exec + Job Object
┌───────────────────▼─────────────────────────────────┐
│              Xray-core / sing-box                   │
│                                                     │
│  127.0.0.1:10808 Socks/HTTP Inbound                 │
│         ↓ routing                                   │
│  proxy / direct / block outbound                    │
└─────────────────────────────────────────────────────┘
```

### 4.1 架构原则

1. 业务逻辑不依赖 Wails。
2. Wails Service 只做参数校验、DTO 转换和调用业务层。
3. 代理内核与 UI 进程隔离，避免内核崩溃拖垮应用。
4. 所有敏感数据处理集中在 SecretStore。
5. 所有网络代理配置由 Go 生成，前端不生成核心配置。
6. 大数据列表采用后端分页/过滤 + 前端虚拟滚动。
7. 高频数据采用批量事件，禁止一行日志一个事件。

---

## 5. 技术选型

| 领域 | 选型 | 原因 |
|---|---|---|
| 桌面框架 | Wails v3 | Go 原生后端、内置托盘/菜单/窗口、跨平台 |
| 前端框架 | SolidJS | 细粒度响应式、无虚拟 DOM、高频更新开销低 |
| 语言 | TypeScript | 类型安全，配合自动生成 bindings |
| 构建 | Vite | 快速开发、静态构建、生态成熟 |
| 路由 | @solidjs/router | 轻量 SPA 路由 |
| 基础组件 | Kobalte | 无样式、可访问性好、DOM 轻量 |
| 表格/虚拟列表 | TanStack Solid Table + Virtual | 大列表只渲染视口行 |
| 图表 | uPlot 或 Canvas | 高频流量图表开销低 |
| 样式 | UnoCSS + CSS Variables | 按需生成、主题切换方便 |
| 后端语言 | Go | 并发、进程管理、跨平台系统集成 |
| 数据库 | SQLite + modernc.org/sqlite | 单机、事务、无 CGO 依赖 |
| 日志 | log/slog + 滚动文件 | 标准库、结构化日志 |
| 代理内核 | Xray-core | 协议和传输支持完整，贴近 v2rayN |
| 第二内核 | sing-box | 支持 Hysteria2、TUIC、TUN 等扩展能力 |
| 打包 | Wails build + Inno Setup/NSIS | Windows 安装包可控 |

Xray-core 客户端集成细节见 [cores/xray/README.md](cores/xray/README.md)。

### 5.1 为什么不用 SSR

Wails 加载的是本地静态资源，不存在 SEO 和首屏服务端渲染需求。使用 Vite SPA 静态构建即可，避免 Next.js/Nuxt/SvelteKit 的服务端运行和 hydration 复杂度。

### 5.2 为什么选 SolidJS

- 组件只执行一次，状态更新直接操作 DOM；
- 适合高频流量、延迟、日志和大量行更新；
- runtime 小，启动快；
- 配合 TanStack Virtual 可稳定处理万级节点列表；
- 代价是组件生态不如 Vue/React，因此基础组件采用 Kobalte headless 方案。

若团队 Vue 经验明显更强，可在保持 Go 服务接口不变的前提下切换为 Vue 3 + Naive UI；该替换不影响核心架构。

---

## 6. 模块设计

### 6.1 目录结构

项目采用根入口 + 内部特性包 + Wails 服务适配层的模块化单体结构。完整规则见 [ARCHITECTURE.md](ARCHITECTURE.md)。

```text
fastV/
├── main.go
├── internal/
│   ├── bootstrap/          # 唯一组合根
│   ├── buildinfo/          # 产品版本信息
│   ├── model/              # 共享模型和 DTO
│   ├── store/              # 本地持久化
│   ├── subscription/       # 订阅抓取、解析和节点构建
│   ├── core/               # Xray 检测、发布查询和安装
│   ├── proxy/              # Xray 配置生成、进程和本地代理
│   ├── diagnostics/        # 出口 IP、归属地和网络质量检测
│   └── service/            # Wails Service 适配层
│       ├── app/
│       ├── core/
│       ├── dashboard/
│       ├── diagnostics/
│       ├── node/
│       ├── proxy/
│       ├── settings/
│       └── subscription/
├── frontend/
├── test/fixtures/
├── docs/
├── go.mod
└── README.md
```

关键约束：

- 根目录只保留 Wails 入口和项目级文件。
- 业务代码全部进入 `internal/`。
- Service 是传输适配层，不承载解析、下载、ZIP 和存储细节。
- `internal/core` 和 `internal/subscription` 不依赖 Wails。
- `internal/bootstrap` 是唯一依赖组装入口。
- 前端只通过生成的 bindings 调用 Go。
- 构建产物、平台资源和本地工具不入库。
### 6.2 领域模型

#### Profile

```go
type Profile struct {
    ID             string
    SubscriptionID string
    Name           string
    Protocol       ProtocolType
    Address        string
    Port           int
    SortOrder      int
    Enabled        bool

    // 非敏感协议元数据
    Meta json.RawMessage

    // uuid/password/privateKey 等加密数据
    SecretBlob []byte
}
```

`Meta` 包含：

- 传输方式：tcp/ws/grpc/httpupgrade/xhttp
- TLS/Reality 参数
- SNI、ALPN、Fingerprint
- WebSocket path、Host
- gRPC serviceName
- Mux 参数

`SecretBlob` 解密后包含：

- UUID
- Password
- PrivateKey
- 其他认证凭据

#### Subscription

```go
type Subscription struct {
    ID                string
    Name              string
    URL               string
    Enabled           bool
    UpdateIntervalMin int
    ETag              string
    LastModified      string
    LastUpdatedAt     time.Time
    LastError         string
}
```

订阅 URL 可能包含 token，因此数据库中以密文保存，前端只显示脱敏后的域名。

#### RoutingRule

```go
type RoutingRule struct {
    ID          string
    Name        string
    Type        RuleType
    Payload     json.RawMessage
    Action      RuleAction
    OutboundTag string
    Enabled     bool
    SortOrder   int
}
```

### 6.3 数据库设计

使用 SQLite，开启 WAL 模式，启动时执行嵌入式 migration。

```sql
CREATE TABLE profiles (
    id TEXT PRIMARY KEY,
    subscription_id TEXT,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL,
    address TEXT NOT NULL,
    port INTEGER NOT NULL,
    meta_json TEXT NOT NULL,
    secret_blob BLOB,
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_profiles_subscription
    ON profiles(subscription_id, sort_order);

CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url_ciphertext BLOB NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    update_interval_min INTEGER NOT NULL DEFAULT 360,
    etag TEXT,
    last_modified TEXT,
    last_updated_at INTEGER,
    last_error TEXT,
    user_agent TEXT
);

CREATE TABLE routing_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    action TEXT NOT NULL,
    outbound_tag TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
```

数据库位置：

```text
%APPDATA%\FastV\data.db
```

运行时文件：

```text
%APPDATA%\FastV\
├─ data.db
├─ runtime\
│  ├─ xray-config.json
│  └─ core.pid
├─ logs\
│  └─ fastv-2026-10-31.log
├─ cores\
│  └─ xray\
│     ├─ xray.exe
│     └─ version.json
├─ geo\
└─ proxy_backup.json
```

### 6.4 敏感数据存储

- 生成 32 字节随机主密钥。
- Windows 下使用 Credential Manager 或 DPAPI 保存主密钥。
- 敏感字段使用 AES-256-GCM 加密。
- 每条记录使用独立 nonce。
- 日志输出前统一脱敏：password、uuid、privateKey、subscription URL。
- 前端接口只返回 `ProfileView`，其中凭据字段为掩码。
- 只有生成核心配置时才在内存中解密，配置写入临时文件后设置仅当前用户可读。

---

## 7. Wails v3 集成设计

### 7.1 Application 组装

```go
func main() {
    profileSvc := services.NewProfileService()
    proxySvc := services.NewProxyService()
    subscriptionSvc := services.NewSubscriptionService()

    app := application.New(application.Options{
        Name:        "FastV",
        Description: "A modern proxy client",
        Services: []application.Service{
            application.NewService(profileSvc),
            application.NewService(proxySvc),
            application.NewService(subscriptionSvc),
        },
        Assets: application.AssetOptions{
            Handler: application.AssetFileServerFS(assets),
        },
    })

    app.NewWebviewWindowWithOptions(application.WebviewWindowOptions{
        Title:    "FastV",
        Width:    1200,
        Height:   800,
        MinWidth: 960,
        MinHeight: 640,
        URL:      "/",
    })

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

> 实际 API 以锁定的 Wails v3 版本为准。升级 Wails 后必须重新生成 bindings 并执行回归测试。

### 7.2 Service 接口

仅暴露 DTO，不暴露领域实体。

```go
type ProfileService struct {
    manager *appsvc.ProfileManager
}

func (s *ProfileService) List(req dto.ProfileListRequest) (dto.ProfilePage, error)
func (s *ProfileService) Get(id string) (dto.ProfileView, error)
func (s *ProfileService) Save(input dto.ProfileInput) (dto.ProfileView, error)
func (s *ProfileService) Delete(ids []string) error
func (s *ProfileService) ImportText(text string) (dto.ImportResult, error)
```

```go
type ProxyService struct {
    manager *appsvc.ProxyManager
}

func (s *ProxyService) Start() (dto.ProxyState, error)
func (s *ProxyService) Stop() error
func (s *ProxyService) State() dto.ProxyState
func (s *ProxyService) SetSystemProxy(enabled bool) error
func (s *ProxyService) SelectProfile(id string) error
```

```go
type SubscriptionService struct {
    manager *appsvc.SubscriptionManager
}

func (s *SubscriptionService) List() ([]dto.SubscriptionView, error)
func (s *SubscriptionService) Add(input dto.SubscriptionInput) error
func (s *SubscriptionService) Update(id string) (dto.UpdateResult, error)
func (s *SubscriptionService) UpdateAll() (dto.UpdateAllResult, error)
func (s *SubscriptionService) Delete(id string) error
```

### 7.3 事件设计

| 事件名 | 数据 | 频率 |
|---|---|---|
| `proxy:state` | ProxyState | 状态变化时 |
| `proxy:traffic` | TrafficSnapshot | 1 秒 |
| `core:log` | LogBatch | 100～250ms 批量 |
| `core:error` | CoreError | 异常时 |
| `profiles:changed` | ProfileChangeSummary | 数据变化时 |
| `subscriptions:changed` | SubscriptionSummary | 更新完成时 |
| `speedtest:progress` | SpeedTestProgress | 200～500ms |
| `app:notification` | Notification | 按需 |

事件发送示例：

```go
application.Get().Event.Emit("proxy:traffic", snapshot)
```

前端使用：

```ts
import { Events } from "@wailsio/runtime";

Events.On("proxy:traffic", (event) => {
    trafficStore.push(event.data);
});
```

### 7.4 单实例与深链接

- 使用 Wails v3 单实例能力，确保只运行一个 FastV 进程。
- 第二次启动时，将命令行参数转发给主实例。
- 主实例显示窗口并处理 `vmess://`、`vless://`、`trojan://`、`ss://` 链接。
- 注册协议处理器由用户主动开启，写入 HKCU，不要求管理员权限。

---

## 8. 代理内核设计

### 8.1 内核生命周期

```text
Stopped
  → Starting
  → Running
  → Stopping
  → Stopped

Starting → Failed
Running  → Failed
```

规则：

- 同一时间只允许一个内核实例。
- 启动/停止操作使用互斥锁串行化。
- 启动失败时清理临时配置和端口。
- 停止时必须等待子进程退出或超时强杀。
- 应用退出时先停内核，再恢复系统代理。

### 8.2 启动流程

1. 检查当前节点是否存在且启用。
2. 检查核心可执行文件是否存在、哈希是否匹配。
3. 分配本地 Socks 入站端口，默认 `127.0.0.1:10808`。
4. 解密节点凭据。
5. 生成 Xray JSON 配置。
6. 执行 `xray -test -config` 校验配置。
7. 启动子进程，并绑定 Windows Job Object。
8. 轮询本地端口，等待内核就绪。
9. 若开启自动系统代理，则设置 Windows 代理。
10. 推送 `Running` 状态和当前节点。

### 8.3 进程管理

Windows 关键点：

- 使用 `CREATE_NO_WINDOW` 避免控制台窗口。
- 使用 Job Object 并设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。
- 主程序崩溃或退出时，内核进程自动结束。
- 异步读取 stdout/stderr，防止管道阻塞。
- 日志进入环形缓冲区，并以批次推送给前端。
- 记录 PID 和启动时间，用于异常诊断。

### 8.4 配置生成

禁止字符串拼接。使用 Go 结构体或 `map[string]any` 构造配置，再 `json.MarshalIndent` 输出。

配置中的固定 tag：

```text
local-in
proxy
direct
block
dns-out
```

生成的配置至少包含：

- log
- inbounds
- outbounds
- routing
- dns
- policy
- stats（按需）

要求：

- 每次生成后运行 `xray -test`。
- 为每种协议和传输方式准备 golden config。
- 核心版本升级时，先跑完整配置测试集。
- 配置写入使用临时文件 + 原子重命名。

### 8.5 流量统计

P0 可以通过本地代理连接状态和内核日志实现基础状态；P1 建议启用 Xray Stats API：

- 通过 gRPC 查询 inbound/outbound 流量；
- 每秒聚合一次；
- 只向 UI 推送聚合快照；
- 统计失败不影响代理主流程。

---

## 9. 订阅与分享链接

### 9.1 支持格式

- `vmess://`：Base64 JSON
- `vless://`：URI + query 参数
- `trojan://`：URI + query 参数
- `ss://`：SIP002 和 legacy 格式
- 后续：`hysteria2://`、`tuic://`、`socks://`、`http://`

### 9.2 订阅抓取

- 默认直接连接，不经过本地代理，避免订阅依赖代理才能更新。
- 设置连接、TLS、响应超时。
- 支持自定义 User-Agent。
- 支持 ETag 和 Last-Modified。
- 最大响应体限制，例如 5MB。
- 记录 HTTP 状态码和错误摘要。
- 更新失败保留旧节点，不清空订阅。

### 9.3 解析策略

1. 判断输入是单条分享链接还是订阅内容。
2. 尝试 Base64 解码订阅内容。
3. 按换行拆分为多条链接。
4. 每条链接独立解析，单条失败不影响其他节点。
5. 使用 `protocol + address + port + credential hash` 去重。
6. 返回成功数、失败数、重复数和错误行号。

### 9.4 订阅更新策略

每个订阅节点分配稳定的 `source_key`。更新时：

- 匹配到已有节点：更新连接参数；
- 用户修改过名称或排序：保留本地覆盖值；
- 新节点：插入；
- 远端删除的节点：标记失效或删除，默认不立即物理删除；
- 整个更新放在一个事务中；
- 更新完成后推送 `subscriptions:changed`。

---

## 10. 系统代理设计

### 10.1 Windows 系统代理

修改注册表：

```text
HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings
```

涉及值：

- ProxyEnable
- ProxyServer
- ProxyOverride
- AutoConfigURL

设置后调用 WinINet API 通知系统刷新。

### 10.2 备份与恢复

启动前将原始值写入：

```text
%APPDATA%\FastV\proxy_backup.json
```

退出时：

1. 检查当前系统代理是否仍为 FastV 设置的值；
2. 是则恢复备份；
3. 否则提示用户当前代理已被外部修改，不盲目覆盖。

异常退出恢复：

- 应用启动时读取 backup 文件；
- 如果 backup 处于 active 状态，先执行恢复；
- 恢复后删除 backup 或标记 inactive。

### 10.3 本地入站

默认：

```text
Local Socks/HTTP Inbound: 127.0.0.1:10808
```

系统代理地址：

```text
http://127.0.0.1:10808
```

端口冲突处理：

- 优先使用配置端口；
- 被占用则尝试随机高位端口；
- 实际端口通过状态推送前端；
- 系统代理始终指向实际端口。

### 10.4 TUN

P1 引入，使用 sing-box TUN inbound。要求：

- 显示明确的管理员权限提示；
- 仅在启用 TUN 时提权；
- 普通系统代理模式不提权；
- 退出时删除路由和虚拟网卡；
- 异常退出后下次启动清理残留。

---

## 11. 前端设计

### 11.1 页面结构

```text
┌─────────────────────────────────────────────┐
│ 标题栏 / 当前节点 / 代理状态                 │
├──────────────┬──────────────────────────────┤
│ 左侧导航      │ 主内容区                      │
│              │                              │
│ 首页          │  服务器列表 / 订阅 / 路由 ...  │
│ 服务器        │                              │
│ 订阅          │                              │
│ 路由          │                              │
│ 测速          │                              │
│ 日志          │                              │
│ 设置          │                              │
├──────────────┴──────────────────────────────┤
│ 状态栏：内核版本 / 代理模式 / 上行 / 下行     │
└─────────────────────────────────────────────┘
```

### 11.2 状态管理

使用 SolidJS 原生 signals/store，不引入重型全局状态库。

建议 store：

- `appStore`：代理状态、主题、语言、当前节点。
- `profileStore`：分页结果、筛选条件、选中项。
- `subscriptionStore`：订阅列表和更新状态。
- `logStore`：环形日志缓冲区。
- `trafficStore`：最近 5～10 分钟流量数据。
- `settingsStore`：用户设置。

更新原则：

- 单节点延迟更新只修改对应节点；
- 不整体复制大数组；
- 日志批量 append；
- 事件批处理后再提交 UI 状态。

### 11.3 大列表

服务器列表要求：

- 虚拟滚动，只渲染视口内的行；
- 固定行高；
- 后端分页/过滤；
- 延迟、启用状态等列独立更新；
- 支持键盘上下选择和快捷键操作。

### 11.4 日志窗口

- 后端环形缓冲区最多 5000 行；
- 前端只渲染视口内日志；
- 自动滚动通过节流实现；
- 搜索在 Go 后端完成；
- 日志默认按级别颜色标记；
- 提供暂停滚动和复制选中行。

---

### 11.5 流量图表

- 使用 uPlot 或 Canvas；
- 每秒一个数据点；
- 最多保留 10 分钟；
- 上下行双曲线；
- 不在图表上使用大面积动画或阴影。

### 11.6 UI 性能约束

- 不使用 SSR。
- 不引入全量重型组件库。
- 页面按路由懒加载。
- 避免大面积 `backdrop-filter` 和持续动画。
- 图标使用 SVG symbol 或按需引入。
- 表格行避免嵌套大量响应式组件。
- 高频数据更新与 Solid 组件树解耦，使用局部 signal。

---

## 12. 可靠性设计

### 12.1 异常场景

| 场景 | 处理 |
|---|---|
| 内核启动失败 | 返回明确错误，保持 Stopped，不设置系统代理 |
| 内核中途崩溃 | 推送 Failed，清理系统代理，记录日志 |
| 应用被强杀 | Job Object 杀死内核，下次启动恢复代理 |
| 端口被占用 | 自动换端口并重试 |
| 订阅更新失败 | 保留旧节点，记录 last_error |
| 配置校验失败 | 阻止启动，展示 xray 错误摘要 |
| 数据库损坏 | 启动时检测并提示恢复备份 |
| 系统代理被用户修改 | 不盲目覆盖，提示用户 |

### 12.2 日志

- 使用 `log/slog` 输出结构化日志。
- 文件按大小/日期滚动。
- UI 日志只保留最近一段时间。
- 核心 stdout/stderr 单独采集并脱敏。
- 敏感字段统一替换为 `***`。
- 崩溃时记录 panic 堆栈和应用版本。

### 12.3 错误模型

Service 返回可识别的错误码：

```text
CORE_NOT_FOUND
CORE_TEST_FAILED
CORE_START_TIMEOUT
PROFILE_INVALID
PORT_CONFLICT
SUBSCRIPTION_FETCH_FAILED
SUBSCRIPTION_PARSE_FAILED
SYSTEM_PROXY_FAILED
SECRET_DECRYPT_FAILED
```

前端根据错误码显示本地化提示，不直接展示底层堆栈。

---

## 13. 安全设计

1. 本地代理仅监听 `127.0.0.1`，默认不监听 `0.0.0.0`。
2. 敏感数据 AES-GCM 加密，主密钥不落盘明文。
3. 日志、崩溃报告、错误消息全部脱敏。
4. 订阅响应限制大小和超时，防止恶意超长内容。
5. 分享链接解析器做长度、字段和协议白名单校验。
6. 只允许用户主动安装核心更新，校验 SHA-256。
7. 生产构建禁用不必要的 DevTools 和远程调试。
8. 前端设置严格 CSP，不加载远程脚本。
9. 协议处理器注册需用户确认，提供注销入口。
10. 不采集遥测，不上传节点和订阅数据。

---

## 14. 测试设计

### 14.1 后端

- 分享链接解析器表驱动测试。
- Base64、URL 编码、异常格式的 fuzz test。
- Xray 配置生成 golden test。
- 使用真实 xray 执行 `-test`。
- CoreManager 使用假内核测试启动、超时、崩溃、强杀。
- SQLite repository 集成测试。
- 系统代理注册表操作使用可注入接口测试。
- 加密/解密和日志脱敏测试。

### 14.2 前端

- 组件测试：服务器表格、订阅表单、日志窗口。
- Store 测试：批量事件、环形缓冲、分页加载。
- 性能测试：10,000 节点虚拟列表滚动。
- 事件压力测试：每秒 2,000 条日志不阻塞渲染。

### 14.3 端到端

- 启动应用 → 导入链接 → 启动代理 → 浏览器访问 → 停止 → 系统代理恢复。
- 订阅更新前后节点数量与去重。
- 强制结束应用后重启，检查内核无残留、系统代理恢复。
- 切换节点后配置正确重新生成。
- 关闭窗口隐藏到托盘，托盘菜单可恢复。

---

## 15. 构建与发布

### 15.1 开发命令

```bash
wails3 dev
```

### 15.2 构建

```bash
wails3 build
```

构建流水线：

1. 生成 Wails bindings。
2. 构建前端静态资源。
3. 编译 Go 应用。
4. 复制固定版本 xray-core。
5. 复制 Geo 数据、LICENSE、第三方声明。
6. 使用 Inno Setup 或 NSIS 生成安装包。
7. 对 exe 和安装包进行代码签名。
8. 生成 SHA-256 校验值。

### 15.3 CI 检查

- `go test ./...`
- `go vet ./...`
- `go test -race ./...`
- `npm run build`
- bindings 生成后工作区无未提交变更
- `wails3 build`
- Xray 配置校验测试
- Windows 冒烟测试

---

## 16. 许可证与合规

- v2rayN 为 GPL-3.0，参考其代码或资源时需遵守 GPL。
- Xray-core 为 MPL-2.0。
- sing-box 为 GPL-3.0。
- Wails 为 MIT。
- 建议 FastV 采用 GPL-3.0 发布，并附带第三方许可证清单。
- 不使用 v2rayN 的名称、图标和品牌素材。
- Geo 数据、规则集和内核发行文件需要分别核对再分发许可。

---

## 17. 迭代计划

### M0：工程骨架，1 周

- Wails v3 初始化。
- SolidJS 页面框架和主题。
- Service 注册与 bindings 生成。
- SQLite migration。
- 单实例、托盘、窗口隐藏。
- 日志和事件总线。

### M1：代理闭环，2 周

- Profile CRUD。
- 分享链接解析。
- Xray 配置生成与校验。
- CoreManager 启动/停止。
- 系统代理设置与恢复。
- 状态机和错误处理。

### M2：订阅与易用性，1～2 周

- 订阅抓取、解析、更新。
- 去重与稳定 source_key。
- 节点搜索、排序、批量操作。
- 托盘当前节点切换。
- 开机启动、国际化。

### M3：性能与观测，1～2 周

- 虚拟列表。
- 日志环形缓冲和批量推送。
- 流量统计和图表。
- 延迟测试和批量测速。
- 10,000 节点压力测试。

### M4：扩展能力，2～3 周

- sing-box 适配。
- Hysteria2、TUIC、WireGuard。
- 路由规则编辑器和规则集。
- TUN 模式。
- 配置备份/恢复。

---

## 18. 验收标准

1. 应用启动后可在 2 秒内操作 UI。
2. 导入、启动、停止、切换节点全流程可用。
3. `xray -test` 对所有内置配置模板通过。
4. 强制结束 FastV 后无 xray 孤儿进程。
5. 异常退出后重启能够恢复系统代理。
6. 10,000 节点列表滚动保持流畅。
7. 每秒 2,000 行日志不造成页面卡死。
8. 数据库和日志中检索不到明文密码/UUID。
9. 订阅更新失败不会清空旧节点。
10. 安装包包含许可证和第三方声明。

---

## 19. 关键架构决策

| 编号 | 决策 | 原因 |
|---|---|---|
| ADR-001 | 使用 Wails v3 | 内置托盘、菜单、窗口、单实例和 Service 模型 |
| ADR-002 | 使用 SolidJS | 高频 UI 更新成本低，适合大列表和实时数据 |
| ADR-003 | 代理协议使用外部内核 | 避免重复实现高风险协议栈 |
| ADR-004 | Xray-core 优先，sing-box 第二 | 兼顾兼容性和扩展协议 |
| ADR-005 | SQLite + AES-GCM | 单机可靠存储，敏感数据可控 |
| ADR-006 | 前端不做核心配置生成 | 统一由 Go 生成并校验，避免配置漂移 |
| ADR-007 | 高频事件批量推送 | 降低 Wails IPC 和前端渲染压力 |
| ADR-008 | Windows 优先 | 系统代理、TUN、托盘和用户群集中在 Windows |

---

## 20. 风险与待确认事项

1. Wails v3 具体版本的 API 可能调整，需要锁定版本并维护升级清单。
2. SolidJS 组件生态较小，部分复杂组件需要基于 Kobalte 自建。
3. Xray-core 配置格式随版本变化，需要配置生成器和版本绑定。
4. sing-box 的 TUN 和系统路由清理需要额外稳定性测试。
5. 订阅链接格式存在大量非标准变体，需要持续收集样本。
6. Windows 安全软件可能误报外部内核，需要代码签名和稳定发布流程。
7. 全量功能对齐 v2rayN 的周期远大于 MVP，需要严格控制范围。

---

## 21. 结论

FastV 采用以下最终技术路线：

> **Wails v3 + Go Service 后端 + SolidJS 高性能前端 + SQLite + Xray-core 子进程，后续增加 sing-box 和 TUN。**

第一阶段的重点是打通：

```text
导入节点 → 生成配置 → 启动内核 → 设置系统代理 → 查看状态/日志 → 停止并恢复代理
```

在该闭环稳定后，再扩展订阅、测速、路由、TUN 和跨平台能力。


