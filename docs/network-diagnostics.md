# 网络诊断

FastV 只集成能够直接请求、稳定解析且无需浏览器验证的检测能力。网络诊断页不提供外部网站入口。

## 已集成能力

- 直连出口检测
- 本地代理出口检测
- 公网 IPv4 / IPv6
- 国家、地区、城市、时区
- ASN、运营商、ISP
- Cloudflare 边缘节点
- TLS 版本、WARP 状态
- 平均延迟、抖动
- 可选 5 MB 下载速度测试
- 直连出口与代理出口对比

## 已接入数据源

| 数据源 | 用途 | 是否需要 Key |
|---|---|---|
| `https://www.cloudflare.com/cdn-cgi/trace` | 出口 IP、国家代码、Cloudflare 节点、TLS、WARP | 否 |
| `https://ipwho.is/{ip}` | 国家、地区、城市、ASN、组织、ISP、时区 | 否 |
| `https://api64.ipify.org?format=json` | Cloudflare trace 不可用时的 IP 兜底 | 否 |
| `https://speed.cloudflare.com/__down?bytes=5000000` | 用户主动开启下载测速时使用 | 否 |

内置检测不会发送节点 Raw、节点凭证、订阅 URL 或本地配置。

## 代理出口检测

代理出口检测通过当前本地 SOCKS/HTTP 入站发起：

```text
DiagnosticsService.Check("proxy")
  -> ProxyService.State()
  -> 127.0.0.1:<port>
  -> Cloudflare trace / ipwho.is
```

如果代理未运行，会返回明确错误，不发起请求。

## 未集成能力

以下能力需要浏览器环境、专用服务接口或额外基础设施，当前不集成：

- WebRTC 泄漏
- DNS 泄漏
- IP 纯净度评分
- IP 风险评分
- 浏览器指纹
- 匿名性评分
- 邮件或 IP 黑名单查询

项目不会通过网页抓取、验证码绕过或外部网站跳转来实现这些能力。

## 后续扩展

如果未来出现提供官方 API、API Key 和明确使用条款的服务，可以新增 provider 接口：

```go
type Provider interface {
    Name() string
    Probe(ctx context.Context, client *http.Client) (ProbeResult, error)
}
```

Provider 必须位于 `internal/diagnostics`，由后端解析和校验，不在前端硬编码第三方 URL。