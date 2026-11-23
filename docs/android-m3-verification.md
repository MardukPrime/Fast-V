# Android M3 设备侧验收清单

系统级代理（VpnService + Go 数据面）有三件事只能在设备或 CI 上验证。仓库根目录没有
NDK/JDK 时，`go test` 只能覆盖数据面本身，因此这里把剩下三步写成可复现的检查。

## 0. 前置

- 手机上打开「开发者选项 → USB 调试」，`adb devices` 能看到设备。
- 用 CI 工件或 `./gradlew assembleDebug` 产出的 APK 安装：
  ```bash
  adb install -r packaging/android/app/build/outputs/apk/debug/app-debug.apk
  ```
- 清空旧日志，便于对照：
  ```bash
  adb logcat -c
  ```

## 1. 编译与符号（不需要真机，CI 覆盖）

`verify-android` 作业在每次 push 时执行：

1. 用 NDK 交叉编译 `libwails.so`（arm64-v8a + x86_64）；
2. 用 `nm -D` 断言 `Java_com_wails_app_WailsBridge_nativeVpnStart/nativeVpnStop`
   确实存在于**两个 ABI** 的产物里；
3. `./gradlew assembleDebug` 编译 Java 侧（VpnService、桥接方法、`prepare()` 流程）。

本地能在无 NDK 时补的一层：`go test ./internal/vpn -run TestAndroid` 会比对
`WailsBridge.java` 的 `native` 声明与 `android_vpn_jni.go` 的 `//export` 名称，
并检查清单里的 service、前台类型与 `BIND_VPN_SERVICE` 权限。

## 2. 授权与隧道建立（真机）

1. 在 App 里选一个节点，点开始。首次会弹出系统 VPN 授权对话框。
2. 允许后，通知栏应出现「FastV 代理已开启」，且带「断开」按钮。
3. 日志应包含关键行：
   ```bash
   adb logcat -s FastVVpnService:V Wails:V
   # 期望：VPN tunnel established (socks=<port>, mtu=1500)
   ```
4. 接口存在且地址与清单一致：
   ```bash
   adb shell ip -o addr show tun0
   # 期望：inet 10.111.0.2/30 与 inet6 fd00:1::2/64
   ```
5. 断开后 `ip link show tun0` 应报「does not exist」，通知消失，且不残留进程。

失败排查：若日志出现 `数据面启动失败 (code -1)`，说明 Go 侧 `vpn.Manager.Start`
返回了错误（SOCKS 端口没起来或被占用）；出现 `UnsatisfiedLinkError` 则是第 1 步的
符号问题，属于构建回归而不是设备问题。

## 3. 真实连通性（真机）

1. **出口 IP 变化**：连接前后各在浏览器打开 `https://www.cloudflare.com/cdn-cgi/trace`，
   `ip=` 应变成节点出口。
2. **DNS 走隧道**：`adb shell ping -c1 1.1.1.1` 应有回包；浏览器访问任意新域名能解析
   （说明 UDP:53 → SOCKS UDP ASSOCIATE 这条路径通）。
3. **TCP 大流量**：播放一段 1080p 视频不卡顿；同时看 App 内流量统计在增长。
4. **IPv6**：若运营商下发 IPv6，`https://test-ipv6.com` 应显示节点出口的 IPv6 或明确
   失败，**不应**回落到直连 IPv6 地址（回落到直连即为泄漏）。
5. **自身流量不被抓**：节点连通性正常即证明 `addDisallowedApplication` 生效；否则
   App 自己的上游连接会被自己的 tun 捕获，表现为连上就断、日志里出现自连。

## 4. 已知边界

- 数据面当前只做 TCP 与 UDP（含 DNS）；ICMP（ping 外网）不会被转发，属预期。
- 分应用代理（split tunneling）尚未实现：默认全量走隧道。
- 隧道建立后若切换节点，需要先断开再连接（重启 surface 尚未做热切换）。
