# ADR-0004: Android 使用内嵌 Xray-core，桌面继续使用外部内核

- 状态：Accepted
- 日期：2026-11-19
- 关联：[ADR-0003](0003-external-proxy-core.md)、[Android Phase 2](../product/ANDROID_PHASE2.md)

## 背景

ADR-0003 决定桌面版通过 `os/exec` 启动外部 Xray-core 二进制。Android 上这一模型不成立：

- 应用数据目录挂载为 `noexec`（targetSdk ≥ 29 后无法执行下载到 data 目录的二进制）；
- 通过 `jniLibs` 携带可执行 `.so` 依赖打包/解包细节，且 Google Play 对可执行载荷有额外审查；
- 后续 M3 的 `VpnService` 需要把 tun fd 交给内核，进程内运行更自然。

同时希望**不改变桌面构建的行为与体积**：桌面继续使用外部内核（可独立更新、进程隔离）。

## 决策

- 新增 `internal/coremode` 与 `internal/proxy/embedded`（构建标签 `embeddedcore || android`）：
  - Android 构建默认通过 `github.com/xtls/xray-core` 的 `core.StartInstance` 在应用进程内运行内核；
  - 桌面正式构建不带标签，继续走外部二进制 + Job Object（ADR-0003 不变）；
  - 桌面 `-tags embeddedcore` 仅用于开发和验证内嵌链路。
- proxy manager 增加内嵌分支：复用同一份配置生成器，启动/停止/状态/日志与外部内核一致。
- core 服务在内嵌模式直接报告「已就绪」，不再检测/下载外部内核。
- 缺少 GeoIP/GeoSite 数据时，自动移除相关路由条件并记录警告；内嵌模式暂不采集实时流量统计（后续用进程内 API 补齐）。

## 后果

### 正面影响

- Android 不需要可执行文件，也能运行真实 Xray-core，本地 SOCKS 可用。
- 桌面行为、安装包大小与升级路径不变。
- 内嵌链路可在桌面用 `embeddedcore` 标签完整验证，降低 Android 调试成本。

### 负面影响

- Android APK 体积增加（内嵌内核使 `libwails.so` 明显变大）。
- 内嵌内核与应用同生命周期：内核崩溃会影响 App 进程，看门狗策略需要重新设计。
- Xray-core 升级会要求重新构建 APK，不能像桌面那样替换外部二进制。
- 缺少 Geo 数据时路由语义暂时降级。

## 备选方案

| 方案 | 优点 | 缺点 | 未采用原因 |
|---|---|---|---|
| 下载/启动外部二进制 | 复用桌面实现 | Android noexec 限制、可执行载荷审查 | 不可行 |
| 把内核编译成可执行 `lib*.so` 从 APK 启动 | 改动小 | 依赖解包行为、Play 审查风险 | 规避风险 |
| 仅做管理客户端 | 简单 | 无法代理流量 | 产品不完整 |

## 验证

- `go test -tags embeddedcore -run TestEmbeddedCoreServesSocks -v ./internal/proxy/embedded/`：内嵌内核启动 SOCKS，本地 HTTP 经 SOCKS 取回内容。
- 桌面 `-tags "production,embeddedcore"` 构建实测：启动代理 → 内嵌内核 → `curl --socks5` 返回 200；停止代理正常。
- Android 交叉编译（`GOOS=android`）与 CI APK 构建通过；真机验证待补充。
