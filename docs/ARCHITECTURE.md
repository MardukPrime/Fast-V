# FastV 架构与目录设计

| 项目 | 内容 |
|---|---|
| 文档版本 | v0.1 |
| 状态 | 架构基线 |
| 日期 | 2026-10-31 |
| 适用项目 | FastV |
| 架构风格 | 模块化单体 + 六边形架构 + 按业务能力分包 |
| 技术栈 | Wails v3 + Go + SolidJS + SQLite + Xray-core |

---

## 1. 设计目标

本文件定义 FastV 的代码组织方式、依赖方向、包职责、接口边界、测试布局和文档目录。目标不是追求形式上的复杂分层，而是保证项目在功能持续增加后仍然具备以下特性：

1. 可读：打开任意目录可以快速判断代码职责。
2. 可测：核心业务不依赖 Wails、SQLite、Windows API 或真实代理内核。
3. 可替换：Xray-core、sing-box、SQLite、系统代理实现可以独立替换。
4. 可演进：新增协议、订阅格式、路由规则时不需要修改大量旧代码。
5. 可定位：错误、日志、事件、DTO 都有统一归属。
6. 不过度设计：不引入微服务、事件溯源、复杂 DDD 聚合或全局依赖注入框架。

### 1.1 架构关键词

- 模块化单体
- 清晰依赖方向
- 业务能力分包
- 接口由消费者定义
- 组合根唯一
- Wails 仅作为传输层
- 基础设施可替换
- 单元测试与集成测试分离

---

## 2. 架构分层

```text
┌──────────────────────────────────────────────┐
│ main.go                                      │
│ 仅嵌入前端资源并调用 bootstrap.Run           │
└───────────────────┬──────────────────────────┘
                    │
┌───────────────────▼──────────────────────────┐
│ internal/bootstrap                           │
│ 组合根：创建依赖、注册 Service、管理生命周期  │
└───────────┬──────────────────────┬───────────┘
            │                      │
┌───────────▼──────────┐  ┌────────▼───────────┐
│ internal/transport   │  │ internal/           │
│ Wails Services       │  │ infrastructure      │
│ DTO / Events         │  │ SQLite / Core / HTTP │
└───────────┬──────────┘  └────────┬───────────┘
            │                      │
            └──────────┬───────────┘
                       │
┌──────────────────────▼───────────────────────┐
│ internal/application                         │
│ 用例编排、事务边界、业务接口定义             │
└──────────────────────┬───────────────────────┘
                       │
┌──────────────────────▼───────────────────────┐
│ internal/domain                              │
│ 实体、值对象、领域错误、领域规则             │
└──────────────────────────────────────────────┘
```

### 2.1 依赖规则

必须遵守以下方向：

```text
main.go    -> bootstrap
bootstrap  -> transport, application, infrastructure, platform
transport  -> application, domain
application-> domain
infrastructure -> application, domain
platform   -> standard library, 必要的系统包
domain     -> standard library only
```

禁止：

- domain 导入 Wails、SQLite、Windows API 或 infrastructure。
- application 直接导入 Wails、gorm、注册表操作或 xray 包。
- infrastructure 反向调用 transport。
- Wails Service 中写业务规则。
- 任意包通过全局变量获取数据库、日志器或配置。

### 2.2 为什么是模块化单体

FastV 是单机桌面应用，不需要微服务。所有模块在同一进程内运行，通过 Go 接口解耦即可。模块化单体可以在保持部署简单的同时获得清晰的边界。

---

## 3. 最终目录结构

```text
fastV/
├── main.go                            # 仅嵌入前端资源并调用 bootstrap.Run
│
├── internal/
│   ├── bootstrap/
│   │   ├── app.go                     # Wails Application、窗口、托盘
│   │   ├── deps.go                    # 组装具体实现
│   │   └── lifecycle.go               # 启动/退出顺序
│   │
│   ├── domain/
│   │   ├── profile/
│   │   │   ├── profile.go             # Profile 实体
│   │   │   ├── protocol.go            # 协议、传输、TLS 枚举
│   │   │   └── validate.go
│   │   ├── subscription/
│   │   │   └── subscription.go
│   │   ├── routing/
│   │   │   └── rule.go
│   │   ├── proxy/
│   │   │   └── state.go               # 代理运行状态机
│   │   └── setting/
│   │       └── settings.go
│   │
│   ├── application/
│   │   ├── profile/
│   │   │   ├── service.go             # 节点用例
│   │   │   ├── ports.go               # Repository 等接口
│   │   │   └── import.go              # 导入编排
│   │   ├── subscription/
│   │   │   ├── service.go
│   │   │   ├── ports.go
│   │   │   └── update.go
│   │   ├── proxy/
│   │   │   ├── service.go             # 启动、停止、切换节点
│   │   │   ├── ports.go               # CoreRunner/SystemProxy 等
│   │   │   └── state.go
│   │   ├── routing/
│   │   │   ├── service.go
│   │   │   └── ports.go
│   │   ├── speedtest/
│   │   │   ├── service.go
│   │   │   └── ports.go
│   │   └── setting/
│   │       ├── service.go
│   │       └── ports.go
│   │
│   ├── infrastructure/
│   │   ├── persistence/
│   │   │   └── sqlite/
│   │   │       ├── db.go
│   │   │       ├── migrations/
│   │   │       │   ├── 0001_init.sql
│   │   │       │   └── embed.go
│   │   │       ├── profile_repo.go
│   │   │       ├── subscription_repo.go
│   │   │       ├── routing_repo.go
│   │   │       └── setting_repo.go
│   │   ├── core/
│   │   │   ├── manager.go             # 内核生命周期统一管理
│   │   │   ├── process.go             # os/exec 封装
│   │   │   ├── port.go                # 端口分配
│   │   │   ├── xray/
│   │   │   │   ├── builder.go
│   │   │   │   ├── builder_test.go
│   │   │   │   └── stats.go
│   │   │   └── singbox/
│   │   │       └── builder.go
│   │   ├── subscription/
│   │   │   ├── fetcher.go
│   │   │   ├── parser.go
│   │   │   ├── parser_vmess.go
│   │   │   ├── parser_uri.go
│   │   │   └── dedupe.go
│   │   ├── secret/
│   │   │   ├── aesgcm.go
│   │   │   └── wincred.go
│   │   └── logging/
│   │       ├── logger.go
│   │       ├── rotating_writer.go
│   │       └── redact.go
│   │
│   ├── platform/
│   │   ├── systemproxy/
│   │   │   ├── systemproxy.go         # 平台无关接口或类型
│   │   │   └── windows.go
│   │   ├── autostart/
│   │   │   ├── autostart.go
│   │   │   └── windows.go
│   │   ├── jobobject/
│   │   │   └── windows.go
│   │   └── elevation/
│   │       └── windows.go
│   │
│   ├── transport/
│   │   └── wails/
│   │       ├── services/
│   │       │   ├── profile_service.go
│   │       │   ├── subscription_service.go
│   │       │   ├── proxy_service.go
│   │       │   ├── routing_service.go
│   │       │   ├── speedtest_service.go
│   │       │   └── setting_service.go
│   │       ├── dto/
│   │       │   ├── profile.go
│   │       │   ├── subscription.go
│   │       │   ├── proxy.go
│   │       │   └── error.go
│   │       └── events/
│   │           ├── emitter.go
│   │           └── names.go
│   │
│   ├── apperr/
│   │   ├── error.go                   # 错误码和包装
│   │   └── codes.go
│   │
│   └── testutil/
│       ├── fake_core.go
│       ├── fake_repo.go
│       └── fixture.go
│
├── api/
│   └── proto/                         # 可选：Xray Stats gRPC
│
├── configs/
│   ├── xray/
│   │   └── base.json.tmpl
│   └── defaults/
│       └── settings.json
│
├── frontend/
│   ├── src/
│   ├── bindings/                      # Wails v3 自动生成
│   └── package.json
│
├── build/
├── scripts/
├── test/
│   ├── integration/
│   └── fixtures/
│
├── docs/
│   ├── README.md
│   ├── DESIGN.md
│   ├── ARCHITECTURE.md
│   ├── cores/
│   ├── adr/
│   ├── api/
│   ├── operations/
│   └── testing/
│
├── Taskfile.yml
├── go.mod
└── README.md
```

---

## 4. 包职责说明

| 路径 | 职责 | 允许依赖 |
|---|---|---|
| `main.go` | Wails 入口，嵌入 frontend/dist 并调用 bootstrap | `bootstrap` |
| `internal/bootstrap` | 组合根、依赖创建、Wails Application 组装、生命周期 | 所有内部包 |
| `internal/domain` | 实体、值对象、枚举、领域校验、纯业务规则 | 标准库 |
| `internal/application` | 用例编排、事务边界、消费者接口定义 | `domain`、`apperr` |
| `internal/infrastructure` | SQLite、Xray、HTTP、加密、日志等具体实现 | `application`、`domain` |
| `internal/platform` | Windows 系统代理、开机启动、Job Object、提权 | 标准库、系统包 |
| `internal/transport/wails` | Wails v3 Service、DTO、事件映射 | `application`、`domain`、`apperr` |
| `internal/apperr` | 统一错误码和应用错误类型 | 标准库 |
| `internal/testutil` | 测试替身、fixture、假内核 | 测试相关包 |
| `api/proto` | 可选的外部 gRPC/Proto 契约 | 不参与业务层 |
| `configs` | 内置模板和默认配置 | 被 embed |
| `frontend` | SolidJS 前端和生成 bindings | 只能通过 Wails Service 访问后端 |
| `test/integration` | 跨包集成测试 | 可依赖内部包 |
| `docs` | 设计、架构、ADR、API 和运维文档 | 无代码依赖 |

### 4.1 domain 包

domain 只放纯业务概念：

- Profile、Endpoint、Protocol、Transport
- Subscription
- RoutingRule
- ProxyState 状态机
- Settings
- 领域错误和校验规则

禁止在 domain 中出现：

- `database/sql`
- `os/exec`
- `net/http`
- Wails 类型
- Windows 注册表
- JSON 持久化细节
- 日志实现

### 4.2 application 包

application 负责一个完整用例，例如：

- 创建节点
- 导入分享链接
- 更新订阅
- 启动代理
- 停止代理
- 执行测速
- 保存设置

application 定义自己需要的接口。例如：

```go
package proxy

type ProfileReader interface {
    Get(ctx context.Context, id string) (profile.Profile, error)
}

type CoreRunner interface {
    Start(ctx context.Context, spec CoreSpec) error
    Stop(ctx context.Context) error
    State() State
}

type SystemProxy interface {
    Enable(ctx context.Context, address string) error
    Disable(ctx context.Context) error
}
```

接口放在消费者包中，而不是统一放到一个巨大的 interfaces 包里。

### 4.3 infrastructure 包

infrastructure 实现 application 定义的接口：

- SQLite repository 实现 `ProfileRepository`
- Xray adapter 实现 `CoreRunner`
- Windows system proxy 实现 `SystemProxy`
- HTTP fetcher 实现 `SubscriptionFetcher`
- AES-GCM 实现 `SecretStore`
- slog 实现 `Logger`

infrastructure 不持有业务状态，不直接向前端发事件。

### 4.4 transport/wails 包

Wails Service 是传输适配器：

```text
前端 DTO -> Wails Service -> application Service -> domain
```

职责：

- 参数校验和格式转换
- 调用 application
- 将 application 错误映射为 DTO 错误
- 发送 Wails 事件
- 不实现业务重试、事务或配置生成

Wails Service 方法应保持简短，例如：

```go
func (s *ProxyService) Start() (dto.ProxyState, error) {
    state, err := s.proxy.Start(s.ctx)
    if err != nil {
        return dto.ProxyState{}, dto.FromError(err)
    }
    return dto.FromProxyState(state), nil
}
```

### 4.5 bootstrap 包

bootstrap 是唯一允许知道所有具体实现的包：

```go
func Run() error {
    db, err := sqlite.Open(...)
    if err != nil {
        return err
    }

    secretStore := secret.NewAESGCM(...)
    coreRunner := core.NewManager(...)
    systemProxy := systemproxy.NewWindows()

    proxySvc := proxyapp.NewService(coreRunner, systemProxy, ...)
    profileSvc := profileapp.NewService(sqlite.NewProfileRepo(db), secretStore, ...)

    wailsApp := application.New(application.Options{
        Services: []application.Service{
            application.NewService(wailssvc.NewProfileService(profileSvc)),
            application.NewService(wailssvc.NewProxyService(proxySvc)),
        },
    })

    return wailsApp.Run()
}
```

不使用 Wire、Fx 等 DI 框架。当前项目规模使用显式构造更清晰，也方便测试。

---

## 5. 依赖注入与生命周期

### 5.1 构造顺序

```text
读取启动参数
  -> 初始化数据目录
  -> 打开 SQLite
  -> 执行 migration
  -> 创建日志器
  -> 创建 SecretStore
  -> 创建 Repository
  -> 创建 CoreManager
  -> 创建 application Services
  -> 创建 Wails Services
  -> 创建 Wails Application
  -> 创建窗口和托盘
  -> 进入事件循环
```

### 5.2 退出顺序

```text
停止接收新请求
  -> 取消所有 context
  -> 停止测速任务
  -> 停止代理内核
  -> 恢复系统代理
  -> 刷新日志
  -> 关闭 SQLite
  -> 退出 Wails
```

退出顺序集中在 `internal/bootstrap/lifecycle.go`，不允许各模块自行注册无序的退出回调。

### 5.3 Context 规则

- 所有可能阻塞的 application 方法第一个参数是 `context.Context`。
- 不使用 `context.TODO()` 作为长期方案。
- Wails Service 如果无法直接提供 ctx，则由服务持有应用生命周期 context，并为单次请求创建带超时的子 context。
- 内核启动、订阅抓取、测速都必须可取消。
- context 取消不能被当作未知错误吞掉。

---

## 6. 数据流设计

### 6.1 启动代理

```text
SolidJS 页面
  -> ProxyService.Start()
  -> dto 校验
  -> proxy application Service
  -> 读取当前 Profile
  -> 解密凭据
  -> xray ConfigBuilder 生成配置
  -> CoreRunner 校验并启动
  -> SystemProxy 设置系统代理
  -> 返回 ProxyState
  -> transport 推送 proxy:state 事件
```

### 6.2 订阅更新

```text
SubscriptionService.Update(id)
  -> application.SubscriptionService.Update
  -> SubscriptionFetcher 拉取
  -> Parser 解析
  -> 去重和合并
  -> Repository 事务更新
  -> 返回统计
  -> transport 推送 subscriptions:changed
```

### 6.3 实时日志

```text
CoreRunner stdout/stderr
  -> LogRingBuffer
  -> 批量聚合
  -> EventPublisher
  -> Wails Event
  -> SolidJS logStore
  -> 虚拟列表渲染
```

application 不直接调用 Wails API；通过消费者定义的 `EventPublisher` 接口解耦。

---

## 7. 错误、日志与事件规范

### 7.1 错误

统一使用 `internal/apperr`：

```go
type Error struct {
    Code    Code
    Message string
    Cause   error
}

func (e *Error) Error() string
func (e *Error) Unwrap() error
```

规则：

- 底层错误使用 `%w` 包装。
- 业务错误必须带稳定错误码。
- Wails Service 映射为可序列化的 DTO。
- 不把堆栈、文件路径和敏感参数返回前端。
- 日志记录完整错误，前端只显示用户可理解的信息。

### 7.2 日志

- 使用 `log/slog`。
- Logger 通过构造函数注入，不使用全局 logger。
- 结构化字段包括：module、profile_id、subscription_id、state、duration_ms。
- 敏感字段在 Handler 层统一脱敏。
- 核心 stdout/stderr 进入独立 ring buffer。
- 文件日志和 UI 日志分离。

### 7.3 事件

事件名统一放在 `internal/transport/wails/events/names.go`：

```go
const (
    EventProxyState      = "proxy:state"
    EventProxyTraffic    = "proxy:traffic"
    EventCoreLog         = "core:log"
    EventSubscription    = "subscriptions:changed"
)
```

事件载荷只能使用 DTO，不允许直接发送 domain 实体。

---

## 8. 测试目录与策略

### 8.1 单元测试

单元测试与源码同目录：

```text
internal/domain/profile/validate_test.go
internal/application/proxy/service_test.go
internal/infrastructure/core/xray/builder_test.go
internal/infrastructure/subscription/parser_test.go
```

测试目标：

- domain 规则
- application 编排
- 配置生成
- 分享链接解析
- 错误映射
- 日志脱敏

### 8.2 集成测试

跨包、跨进程或依赖真实文件的测试放入：

```text
test/integration/
├── core_start_stop_test.go
├── sqlite_migration_test.go
├── system_proxy_test.go
└── subscription_update_test.go
```

### 8.3 测试替身

统一放入 `internal/testutil`：

- `FakeCoreRunner`
- `FakeProfileRepository`
- `FakeSystemProxy`
- `FakeEventPublisher`
- 固定分享链接 fixture

`testutil` 不能包含生产逻辑。

### 8.4 测试命名

```go
func TestProxyService_Start_WhenCoreFails_ReturnsCoreStartError(t *testing.T)
func TestParser_VMess_WithBase64JSON(t *testing.T)
```

测试名称应表达：对象、场景、期望结果。

---

## 9. 文档目录设计

文档目录作为项目的单一信息源，结构如下：

```text
docs/
├── README.md                  # 文档索引和阅读顺序
├── DESIGN.md                  # 产品与系统设计
├── ARCHITECTURE.md            # 架构、目录、依赖规则
├── adr/
│   ├── README.md              # ADR 索引
│   ├── TEMPLATE.md            # ADR 模板
│   ├── 0001-use-wails-v3.md
│   ├── 0002-use-solidjs.md
│   └── 0003-external-proxy-core.md
├── api/
│   ├── wails-services.md      # 前端可调用服务契约
│   └── events.md              # 事件名和载荷
├── operations/
│   ├── build.md               # 构建步骤
│   ├── release.md             # 发布和签名
│   └── troubleshooting.md     # 常见故障排查
└── testing/
    └── strategy.md            # 测试策略和测试数据说明
```

### 9.1 文档职责

| 文档 | 负责内容 | 不负责内容 |
|---|---|---|
| `DESIGN.md` | 需求、总体方案、核心流程 | 文件级目录规范 |
| `ARCHITECTURE.md` | 分层、目录、依赖、包职责 | 产品需求 |
| `adr/` | 重要架构决策及其原因 | 操作教程 |
| `api/` | Service 方法和事件契约 | 内部实现 |
| `operations/` | 构建、发布、排障 | 架构讨论 |
| `testing/` | 测试策略、fixture 规范 | 具体业务设计 |

### 9.2 ADR 规则

每个重要决策一个 ADR，格式固定：

```text
标题
状态：Proposed / Accepted / Superseded
背景
决策
后果
备选方案
```

ADR 一经接受不直接修改结论，需要变更时新建 ADR 并标记 Superseded。

---

## 10. 命名与文件规范

### 10.1 包命名

- 使用小写单词，不加下划线。
- 包名表达职责，例如 `subscription`、`systemproxy`。
- 不使用 `util`、`common`、`helpers`、`base` 这类无边界名称。
- 不在包名中重复目录层级，例如 `profile/profile_service.go` 中的包仍然是 `profile`。

### 10.2 文件命名

- 一个文件聚焦一个职责。
- `ports.go` 放该业务包消费的接口。
- `service.go` 放用例入口。
- `_test.go` 与源码同包或使用 `_test` 外部测试包。
- 平台实现使用 `_windows.go`、`_linux.go` 后缀，或放在明确平台目录下。

### 10.3 接口命名

- 接口名体现行为，例如 `ProfileReader`、`CoreRunner`、`SystemProxy`。
- 接口尽量小，一个接口只包含调用方真正需要的方法。
- 不提前为所有实现创建接口。
- 接口定义在消费者侧。

### 10.4 DTO 命名

- Wails 输入：`ProfileInput`
- Wails 输出：`ProfileView`
- 分页请求：`ProfileListRequest`
- 分页结果：`ProfilePage`
- 错误：`ErrorDTO`

DTO 与 domain 实体之间使用显式转换函数，禁止直接序列化 domain 对象。

---

## 11. 反模式清单

以下做法禁止进入主干：

1. `utils`、`common`、`helpers` 大杂烩包。
2. 全局数据库连接、全局 logger、全局配置。
3. Wails Service 直接操作 SQLite 或 `os/exec`。
4. domain 导入 Wails 或 infrastructure。
5. 前端直接生成 Xray 配置。
6. 在 handler 中写业务重试和事务。
7. 用 `panic` 处理可恢复业务错误。
8. 每个小功能都创建一个接口。
9. 为了未来可能复用而提前创建 `pkg/`。
10. 在 Go 后端日志中打印完整节点配置或订阅 URL。
11. 一个事件发送一整张表。
12. 把生成的 bindings 当作手写业务代码修改。

---

## 12. 架构评审检查表

新增模块或 PR 时检查：

- [ ] 新代码是否放在正确的业务包？
- [ ] 是否违反依赖方向？
- [ ] 是否在 application 中定义了消费者接口？
- [ ] Wails Service 是否只做 DTO 转换和编排调用？
- [ ] 是否避免全局状态？
- [ ] 是否所有阻塞操作接受 context？
- [ ] 是否对敏感数据做了脱敏？
- [ ] 是否增加了对应单元测试？
- [ ] 是否需要新增或更新 ADR？
- [ ] 前端是否只通过生成的 bindings 调用后端？
- [ ] 是否避免了无边界 `utils` 包？
- [ ] 是否保持 main.go 只做启动？

---

## 13. 结论

FastV 的架构基线是：

> 以根目录 `main.go` 为 Wails 入口，以 `internal/bootstrap` 为唯一组合根，以 `domain` 表达业务规则，以 `application` 编排用例，以 `infrastructure` 实现外部能力，以 `transport/wails` 暴露桌面 API，以 `platform` 隔离操作系统差异。

目录设计遵循 Go 社区常用实践，但不照搬所有模板。对于 FastV 这种单机桌面应用，关键是保持：

- main 轻薄
- domain 纯净
- 接口由消费者定义
- 依赖显式注入
- Wails 仅作为传输层
- 测试不依赖真实内核
- 文档与代码目录一一对应
