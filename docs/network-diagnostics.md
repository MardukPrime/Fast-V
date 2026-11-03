# 网络诊断与在线 IP 工具集成评估

## 1. 已集成能力

FastV 的网络诊断页面同时支持：

- 直连出口检测
- 本地代理出口检测
- 公网 IPv4 / IPv6
- 国家、地区、城市
- ASN、运营商、ISP
- Cloudflare 边缘节点
- TLS 版本、WARP 状态
- 平均延迟、抖动
- 可选下载速度测试
- 直连出口与代理出口对比

## 2. 已接入的数据源

| 数据源 | 用途 | 是否需要 Key | 说明 |
|---|---|---|---|
| `https://www.cloudflare.com/cdn-cgi/trace` | 出口 IP、国家代码、Cloudflare 节点、TLS、WARP | 否 | 返回纯文本 key=value，稳定且轻量 |
| `https://ipwho.is/{ip}` | 国家、地区、城市、ASN、组织、ISP、时区 | 否 | HTTPS JSON，按 Cloudflare 返回的出口 IP 查询，避免两次请求落在不同出口 |
| `https://api64.ipify.org?format=json` | IP 兜底检测 | 否 | Cloudflare trace 不可用时只获取 IP |
| `https://speed.cloudflare.com/__down?bytes=5000000` | 下载速度 | 否 | 用户主动开启下载测速后才会请求 |

内置检测不会发送节点凭证、节点 Raw、订阅 URL 或本地配置。

## 3. 未直接接入的在线工具

| 工具 | 未接入原因 | 当前处理 |
|---|---|---|
| IPCheck.ing | 页面返回 Cloudflare 403 / challenge，没有发现稳定公开 API | 打开外部浏览器 |
| ip.skk.moe | Cloudflare challenge，自动化请求不可靠 | 打开外部浏览器 |
| IPPure | 页面和前端 JS 可用，但未发现公开 API，纯净度评分规则和调用限制不明确 | 打开外部浏览器 |
| IP看看 | 页面出现 Aliyun Captcha，无法稳定自动请求 | 打开外部浏览器 |
| Whoer.net | 返回 403，匿名性评分属于站点私有能力，没有公开 API | 打开外部浏览器 |

这些能力可以在后续出现官方 API、API Key 和明确使用条款后再接入。

## 4. 外部工具入口

网络诊断页提供以下外部入口：

- IPCheck.ing：IPv4/IPv6、ASN、WebRTC、DNS 泄漏、网速
- ip.skk.moe：本机 IP、递归 DNS 出口、CDN 命中节点
- IPPure：IP 纯净度、地理位置、运营商、风险
- IP看看：公网 IP、ASN、原生 IP、风险值
- Whoer.net：匿名性、DNS/WebRTC 泄漏、代理状态

外部网站可能收集请求 IP、User-Agent 和浏览器信息。打开前应提示用户。

## 5. 代理出口检测

代理出口检测通过本地 SOCKS/HTTP 入站发起：

```text
DiagnosticsService.Check("proxy")
  -> ProxyService.State()
  -> 本地 127.0.0.1:<port>
  -> Cloudflare trace / ipwho.is
```

如果代理未运行，返回明确错误，不发起请求。

## 6. 暂未实现的检测

以下能力需要浏览器环境或额外基础设施，暂不放入内置检测：

- WebRTC 泄漏
- DNS 泄漏
- IP 纯净度评分
- 浏览器指纹
- 匿名性评分
- 邮件/IP 黑名单查询

## 7. 后续扩展

如果接入带 API Key 的服务，建议新增 provider 接口：

```go
type Provider interface {
    Name() string
    Probe(ctx context.Context, client *http.Client) (ProbeResult, error)
}
```

保持当前 `internal/diagnostics` 的边界，不在前端硬编码第三方 URL 和解析逻辑。