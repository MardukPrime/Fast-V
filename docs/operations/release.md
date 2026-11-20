# 发布说明

## 正式构建参数（必须遵守）

所有发布产物必须使用 production 构建参数，避免把开发/调试配置带进 Release：

| 平台 | 必需参数 |
|---|---|
| 前端 | `pnpm build`（Vite production 模式） |
| Windows | `go build -tags production -trimpath -buildvcs=false -ldflags "-s -w -H=windowsgui -X .../buildinfo.Version=... -X .../buildinfo.Commit=... -X .../buildinfo.BuildDate=..."` |
| macOS / Linux | 同上，但 tags 为 `production purego`，不使用 `-H=windowsgui` |
| Android | `go build -buildmode=c-shared -overlay ... -tags "production,android"`，版本/commit 通过 ldflags 注入；APK/AAB 需同时包含 arm64-v8a 与 x86_64 |

- 版本号必须来自发布 tag（不带 `v` 前缀）并写入 `buildinfo.Version`；commit 使用完整 `GITHUB_SHA`。
- Windows 必须使用 `-H=windowsgui`（PE subsystem 2），否则会弹出控制台窗口。
- 不要使用临时 ProductID（例如 `com.mardukprime.fastv.xxx`）、debug 构建钩子或 `?android=1` 等开发参数作为发布产物。
- 本地 Windows 构建可直接运行 `scripts/build-windows.ps1`，该脚本已包含上述参数并校验 PE subsystem。

发布 job 会在上传前运行 `scripts/verify-release-artifacts.py`，验证：

1. Windows/macOS/Linux 二进制内嵌 release 版本与 commit；Windows PE 为 GUI 子系统；
2. 二进制中不存在 debug/test ProductID 后缀；
3. Android APK/AAB 同时包含 arm64-v8a 与 x86_64 的 `libwails.so`，且内嵌版本正确；
4. `SHA256SUMS.txt` 覆盖 `dist` 内全部发布资产。

任何一项失败都会阻止 GitHub Release 发布。

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
- `android`：ubuntu-latest 安装 Android SDK/NDK（API 36、NDK 29）+ JDK 21，使用 Wails CLI 生成 overlay，`go build -buildmode=c-shared -tags production,android` 产出 `libwails.so`，再由 `packaging/android` Gradle 工程打包 `FastV-<version>-android-universal.apk` 与 `.aab`。
  - **正式签名**：在仓库 Secrets 配置 `ANDROID_KEYSTORE_BASE64`（keystore 的 base64）、`ANDROID_KEYSTORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD` 后，CI 自动使用正式 keystore；未配置时为 debug 签名（仅适合侧载测试，Play 会拒绝 debug 签名包）。
  - 生成 keystore：`keytool -genkeypair -v -keystore fastv-release.jks -alias fastv -keyalg RSA -keysize 4096 -validity 10000`，然后 `base64 -w0 fastv-release.jks` 填入 Secret。
  - **Play 发布前检查**：AAB 使用正式 keystore 签名；targetSdk 满足当年 Play 要求（当前 36）；隐私政策 URL、Data safety 表单；VPN/代理功能上线时需声明 `VpnService` 用途并提交相应权限说明；首次发布需人工在 Play Console 创建应用并上传 AAB。当前 Android 包为**管理预览版**：仅订阅/节点/规则/备份，VpnService 与代理内核待接入。
- `release`：汇总全部 artifact，生成合并的 `SHA256SUMS.txt` 与 `release-info.txt`，创建 GitHub Release；版本号含 `-pre`/`-beta`/`-rc` 等后缀自动标记为 **pre-release**。
- 应用内「检查更新」按当前平台与架构选择资产（Windows zip / macOS zip / Linux tar.gz）；自动安装目前仅 Windows 可用，macOS/Linux 会提示手动下载。

## 稳定版发布（v1.0.0 及以后）

- **正式版**使用不带预发布后缀的语义化标签（例如 `v1.0.0`），工作流检测到版本不含 `-` 时自动发布为非 pre-release；预发布继续使用 `vX.Y.Z-pre.N`。
- 发布前检查清单：
  1. `main` 上 CI 全绿（含 `go test -count=2`）、发布工作流 smoke 测试通过；
  2. CHANGELOG 完成对应版本小节；README 版本徽章与下载说明更新；
  3. `dist` 预期资产全部存在：Windows zip/setup.exe、macOS arm64+amd64 zip、Linux tar.gz/deb（amd64+arm64）、AppImage（amd64）；
  4. `SHA256SUMS.txt` 覆盖全部二进制/压缩资产且校验一致；
  5. 在真实 Windows 环境安装/卸载一次，Linux 用 `dpkg -i` 与 AppImage 各启动一次；
  6. macOS 产物在真实机器右键打开验证（未签名/未公证）。
- macOS DMG 与签名/公证：需要 Apple Developer ID 证书与 macOS 构建机，当前 CI 使用 Linux 交叉编译并只产出 zip；正式签名流程：
  ```bash
  # 在 macOS 构建机上
  codesign --force --deep --options runtime --sign "Developer ID Application: <team>" FastV.app
  hdiutil create -volname FastV -srcfolder FastV.app -ov -format UDZO FastV-<version>-macos-<arch>.dmg
  xcrun notarytool submit FastV-<version>-macos-<arch>.dmg --keychain-profile <profile> --wait
  xcrun stapler staple FastV-<version>-macos-<arch>.dmg
  ```
  在配置证书与 macOS runner 之前，发布说明需明确标注“未签名、未公证，需右键打开”。

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
