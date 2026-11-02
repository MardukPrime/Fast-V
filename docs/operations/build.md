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

## 构建

```bash
wails3 build
```

## 资源

- 前端构建产物嵌入 Go 二进制
- Xray-core 以固定版本随安装包分发
- 核心文件需要校验 SHA-256
- 发布包附带 LICENSE 和第三方声明
