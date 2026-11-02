import { createSignal, onMount } from "solid-js";

import { AppService } from "../bindings/github.com/MardukPrime/Fast-V/internal/transport/wails/services";
import type { AppInfo } from "../bindings/github.com/MardukPrime/Fast-V/internal/transport/wails/dto";

const navigation = ["概览", "服务器", "订阅", "路由", "测速", "日志", "设置"];

export default function App() {
  const [info, setInfo] = createSignal<AppInfo | null>(null);
  const [active, setActive] = createSignal("概览");

  onMount(async () => {
    try {
      setInfo(await AppService.Info());
    } catch (error) {
      console.error("Failed to load application info", error);
    }
  });

  return (
    <div class="app-shell">
      <aside class="sidebar">
        <div class="brand">
          <div class="brand-mark">F</div>
          <div>
            <div class="brand-name">FastV</div>
            <div class="brand-caption">Proxy Client</div>
          </div>
        </div>

        <nav class="nav">
          {navigation.map((item) => (
            <button
              classList={{ "nav-item": true, active: active() === item }}
              onClick={() => setActive(item)}
            >
              {item}
            </button>
          ))}
        </nav>

        <div class="sidebar-footer">
          <span class="status-dot" />
          <span>内核未启动</span>
        </div>
      </aside>

      <main class="content">
        <header class="topbar">
          <div>
            <h1>{active()}</h1>
            <p>FastV 项目基础框架</p>
          </div>
          <div class="topbar-actions">
            <span class="pill">Wails v3</span>
            <span class="pill">SolidJS</span>
          </div>
        </header>

        <section class="hero">
          <div>
            <span class="eyebrow">FASTV / FOUNDATION</span>
            <h2>项目已初始化</h2>
            <p>
              Go 后端、Wails v3 桌面壳和 SolidJS 前端已经连接。下一步是节点管理、
              订阅解析和 Xray-core 进程控制。
            </p>
          </div>
          <div class="hero-badge">v{info()?.version ?? "0.1.0"}</div>
        </section>

        <section class="cards">
          <article class="card">
            <span class="card-label">应用名称</span>
            <strong>{info()?.name ?? "FastV"}</strong>
            <small>桌面代理客户端</small>
          </article>
          <article class="card">
            <span class="card-label">运行平台</span>
            <strong>{info() ? `${info()!.platform} / ${info()!.arch}` : "..."}</strong>
            <small>Windows 优先</small>
          </article>
          <article class="card">
            <span class="card-label">Wails</span>
            <strong>{info()?.wailsVersion ?? "3.1.0"}</strong>
            <small>Application + Service</small>
          </article>
          <article class="card">
            <span class="card-label">构建版本</span>
            <strong>{info()?.commit ?? "dev"}</strong>
            <small>{info()?.buildDate ?? "unknown"}</small>
          </article>
        </section>

        <section class="panel">
          <div class="panel-heading">
            <h3>下一步</h3>
            <span>Milestone M0</span>
          </div>
          <ul class="roadmap">
            <li>建立 Profile、Subscription 和 Settings 领域模型</li>
            <li>接入 SQLite 与 migration</li>
            <li>实现分享链接解析与订阅更新</li>
            <li>接入 Xray-core 配置生成与进程管理</li>
            <li>实现系统代理设置与崩溃恢复</li>
          </ul>
        </section>
      </main>
    </div>
  );
}