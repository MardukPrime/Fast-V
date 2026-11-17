# 发布说明

## 自动发布（GitHub Actions）

仓库内置 `.github/workflows/release.yml`：推送 `v*` 标签（或在 Actions 页面手动 `workflow_dispatch` 填写 tag）即可自动完成构建与发布。

```powershell
# 1. 确认工作区干净、CI 通过
git status
# 2. 打标签并推送（标签里的版本号会注入 buildinfo.Version）
git tag -a v0.2.0-pre.1 -m "FastV v0.2.0-pre.1"
git push origin main
git push origin v0.2.0-pre.1
```

工作流行为：

- Windows x64 构建（`windows-latest`，CGO 关闭，`-tags production -H windowsgui`）。
- 版本号从 tag 去掉前缀 `v` 得到；含 `-pre`/`-beta`/`-rc` 等后缀自动标记为 **pre-release**。
- 产物：`FastV-<version>-windows-amd64.zip`（含 `Fast-V.exe`、LICENSE、README）、`SHA256SUMS.txt`、`release-info.txt`，同时上传 Actions artifact。
- 发布说明自动生成（`generate_release_notes`），并附带便携版运行要求。
- 应用内「检查更新」按 release 资产名（含 `windows` 且以 `.zip` 结尾）选择更新包，因此资产命名不可随意更改。

## 手动发布步骤（备用）

1. 更新版本号和 CHANGELOG。
2. 运行全部测试和配置校验。
3. 执行 `wails3 build`（或 `go build -tags production -trimpath -ldflags="-w -s -H windowsgui"`）。
4. 下载并校验固定版本 Xray-core。
5. 收集 Geo 数据、LICENSE 和第三方声明。
6. 使用 Inno Setup 或 NSIS 生成安装包。
7. 对 exe 和安装包进行代码签名。
8. 生成 SHA-256 校验文件。
9. 在 Windows 10/11 虚拟机执行冒烟测试。
10. 发布安装包、校验值和更新日志。

## 版本策略

- 应用版本：语义化版本。
- 核心版本：单独记录，不与应用版本混用。
- 数据库 schema 版本：通过 migration 管理。
- 配置文件版本：设置中记录 schemaVersion。

## 回滚

- 保留上一个稳定版本安装包。
- 数据库 migration 必须提供向前兼容策略。
- 核心升级失败时允许回退到内置版本。
