# 发布说明

## 自动发布（GitHub Actions）

仓库内置 `.github/workflows/release.yml`：推送 `v*` 标签（或在 Actions 页面手动 `workflow_dispatch` 填写 tag）即可自动完成构建与发布。

```bash
# 1. 确认工作区干净、CI 通过
git status
# 2. 打标签并推送（标签里的版本号会注入 buildinfo.Version）
git tag -a v0.3.0-pre.1 -m "FastV v0.3.0-pre.1"
git push origin main
git push origin v0.3.0-pre.1
```

工作流行为（多平台矩阵）：

- `frontend`：ubuntu-latest 上安装依赖并构建前端，产物作为 artifact 供所有平台复用。
- `windows-x64`：windows-latest，CGO 关闭，`-tags production -H windowsgui`，产物 `FastV-<version>-windows-amd64.zip`；随后用 NSIS 构建 `FastV-<version>-windows-amd64-setup.exe`（脚本 `packaging/windows/fastv.nsi`），并在 runner 上执行静默安装/卸载冒烟测试。
- `macos`：ubuntu-latest 上分别交叉编译 arm64 与 amd64（`-tags "production purego"`、CGO 关闭），生成带 Info.plist 与图标的 `FastV.app`，打包为 `FastV-<version>-macos-arm64.zip` / `-macos-amd64.zip`（ad-hoc 签名，未做开发者签名/公证；DMG 需 macOS runner，暂未提供）。
- `linux`：ubuntu-latest 上交叉编译 amd64 与 arm64，打包为 `FastV-<version>-linux-<arch>.tar.gz` 与 `FastV-<version>-linux-<arch>.deb`（`packaging/linux/build-deb.sh`，安装到 `/opt/fastv`，依赖 libgtk-3-0 + libwebkit2gtk-4.1-0），并用 `dpkg-deb -I/-c` 校验包元数据。
- `release`：汇总全部 artifact，生成合并的 `SHA256SUMS.txt` 与 `release-info.txt`，创建 GitHub Release；版本号含 `-pre`/`-beta`/`-rc` 等后缀自动标记为 **pre-release**。
- 应用内「检查更新」按当前平台与架构选择资产（Windows zip / macOS zip / Linux tar.gz）；自动安装目前仅 Windows 可用，macOS/Linux 会提示手动下载。

> macOS/Linux 使用 Wails v3 的 CGO-free（purego）后端；需要 WebKitGTK/GTK 运行时库。Windows 专属能力（TUN、系统代理、自启、托盘菜单、应用内更新）在其他平台不可用。

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
