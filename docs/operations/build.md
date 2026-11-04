# 构建说明

## 前置要求

- Go 版本符合 Wails v3 要求
- Node.js + pnpm/npm
- Wails v3 CLI
- WebView2 Runtime
- Windows SDK/构建工具，按 Wails 文档要求安装

## 开发

```bash
wails3 dev
```

## 正式构建

推荐使用仓库脚本：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build-windows.ps1 -Version 0.1.0
```

脚本会执行前端构建、Go `production` 构建、版本信息注入，并检查 PE 子系统。

等价 Go 参数：

```bash
go build -tags production -trimpath -ldflags "-s -w -H=windowsgui -X github.com/MardukPrime/Fast-V/internal/buildinfo.Version=0.1.0" -o bin/fastv.exe .
```

必须使用 `-H=windowsgui`，否则生成的是控制台子系统程序，双击后会打开终端窗口。

脚本还会验证 PE Optional Header 的 Subsystem 字段：

```text
2 = WINDOWS_GUI
3 = WINDOWS_CUI
```

只有 `WINDOWS_GUI` 才允许作为正式发布包。

## 资源

- 前端构建产物嵌入 Go 二进制
- Xray-core 以固定版本随安装包分发
- 核心文件需要校验 SHA-256
- 发布包附带 LICENSE 和第三方声明
