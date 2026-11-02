# ADR-0001: 使用 Wails v3 作为桌面框架

- 状态：Accepted
- 日期：2026-10-31

## 背景

FastV 需要 Go 后端、Windows 桌面窗口、系统托盘、原生菜单、单实例和前后端类型安全绑定。Wails v2 的托盘和系统集成需要额外 Win32 胶水，而项目希望减少平台适配代码。

## 决策

使用 Wails v3 作为桌面框架：

- Go 侧使用 Application 和 Service 模型。
- 前端使用 Wails v3 生成的 TypeScript bindings。
- 高频状态使用 Event Bus 推送到前端。
- 锁定 Wails v3 的具体版本，升级前重新生成 bindings 并回归测试。

## 后果

### 正面影响

- 内置托盘、菜单、窗口、对话框和单实例能力。
- Go Service 边界清晰，易于测试。
- 前端类型安全。

### 负面影响

- Wails v3 API 仍在演进，需要锁定版本。
- 团队需要熟悉 v3 的 Service 生命周期和 bindings 生成流程。

## 备选方案

| 方案 | 优点 | 缺点 | 未采用原因 |
|---|---|---|---|
| Wails v2 | 资料多、稳定 | 托盘和原生集成胶水多 | v3 更适合本项目 |
| Fyne | 纯 Go | 复杂表格和现代 UI 效率较低 | UI 目标不适合 |
| Tauri | 生态大 | 后端不是 Go，和项目目标不符 | 技术栈不匹配 |
