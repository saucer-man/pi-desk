# Pi Desk

基于 **Node.js + Pi SDK + React** 的本地编码工作台，直接在浏览器中使用。

- 多个独立会话，支持串行 / 并行子 agent。
- 对话、执行轨迹、工具详情与 Token / 上下文统计。
- 模型、prompt、skills、MCP 和插件管理。
- SSH 工作区：本机运行 Pi SDK，远端仅运行 Go helper。
- 会话沿用 `~/.pi` 中的 Pi 原生格式，可与 Pi 客户端共用；同一会话避免同时写入。

## 运行

需要 **Node.js 22.19+** 和 **Git**；使用 SSH 时需要本机 OpenSSH。

从 [GitHub Actions](https://github.com/saucer-man/pi-desk/actions/workflows/verify.yml) 下载构建包，解压后执行：

```sh
npm ci --omit=dev
npm start
```

打开 <http://127.0.0.1:30140/>，添加工作区，在「设置 → 模型管理」配置模型后开始对话。服务只监听本机。

```sh
npm start -- --no-open       # 不自动打开浏览器
npm start -- --port 30141    # 指定端口
```

发行包已包含远端 helper，无需 Go。PTY 原生依赖在部分环境需要 Python 3 和 C++ 编译工具。

## 从源码运行

另需 **Go 1.25+**，用于构建远端 helper。

```sh
git clone https://github.com/saucer-man/pi-desk.git
cd pi-desk
npm ci
npm run build
npm start
```

开发：`npm run dev`；构建与测试：`npm run check`。升级时停止旧服务、替换程序文件并重新安装依赖，保留 `~/.pi` 数据。

## 运行截图

以下为真实运行界面，使用演示会话，不含个人数据。

![对话页面](docs/screenshots/chat.jpg)

![设置页面](docs/screenshots/settings.jpg)

界面复用了 DeepSeek Harness 的部分组件与样式，相关许可见 [DSH LICENSE](frontend/src/web/dsh/LICENSE.txt)。
