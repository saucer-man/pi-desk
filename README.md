# Pi Desk

Pi Desk 是基于 Wails v3、Go 和 Vue 3 开发的 Pi coding agent 桌面客户端。桌面端负责窗口、工作区、终端、Git 视图和 Pi 进程管理，并在对话消息底部呈现模型请求失败、自动重试和恢复状态；agent loop、模型调用、工具执行、认证及会话运行时由上游 Pi 提供。

项目后端使用 Go，前端使用 Vue 3、TypeScript、Pinia 和 Vite，并通过 `pi --mode rpc` 连接 Pi CLI。

## 编译要求

- Go `1.25.0`（由 `go.mod` 固定）
- Node.js `22.19.0` 或更高版本
- Wails CLI `v3.0.0-beta.6`
- Git

各平台还需要对应的原生工具链：

- Windows：Windows 10/11 和 WebView2 Runtime
- macOS：macOS 12 或更高版本、Xcode Command Line Tools
- Linux：GTK 4、WebKitGTK 6.0、`pkg-config` 和 C 编译器

## 编译流程

安装固定版本的 Wails CLI：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.6
```

安装前端依赖：

```sh
cd frontend
npm ci
cd ..
```

在项目根目录执行生产构建：

```sh
wails3 build
```

也可以通过项目 Taskfile 构建：

```sh
wails3 task build
```

构建产物输出到 `bin/` 目录。Windows 产物为 `bin/pi-desk.exe`，macOS 和 Linux 产物为 `bin/pi-desk`。

### Linux 编译依赖

Ubuntu 24.04 可通过以下命令安装原生依赖：

```sh
sudo apt-get update
sudo apt-get install --no-install-recommends \
  libgtk-4-dev libwebkitgtk-6.0-dev pkg-config build-essential
```

安装后在项目根目录执行：

```sh
wails3 build
```

### 跨平台编译

从非目标系统编译 macOS 或 Linux 桌面程序时需要 Docker。先构建项目提供的交叉编译镜像，再指定目标平台：

```sh
wails3 task setup:docker
wails3 build GOOS=darwin
wails3 build GOOS=linux
```
