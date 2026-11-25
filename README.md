# Pi Desk

Pi Desk 是基于 **Node.js + Pi SDK + React** 的开源编码工作台，在浏览器中运行。

支持独立会话、子 agent、执行轨迹、Token / 上下文统计，以及模型、prompt、skills、MCP 和插件管理。SSH 工作区由本机 Pi SDK 调用远端 Go helper；会话保存在 `~/.pi`，与 Pi 原生客户端共用，同一会话避免同时写入。

## 开发者预览

项目仍在开发中，功能与配置可能调整。Agent 可读写工作区文件并执行命令。

## 运行

### 直接运行

需要 **Node.js 22.19+** 和 **Git**，直接运行 GitHub Releases 中的预构建包：

```sh
npx https://github.com/saucer-man/pi-desk/releases/download/v2.0.0-dev.11/pi-desk.tgz web
```

默认监听 `127.0.0.1:30140`。启动时生成随机访问口令，输出并打开 `http://127.0.0.1:30140/?secret=…`。浏览器验证口令后会移除地址栏里的 `secret`，后续请求通过 Cookie 验证。服务重启后，请使用新链接。在「设置 → 模型管理」配置模型后开始使用。

传入 `--no-open` 可关闭自动打开，`--port 30141` 可指定端口。

无需 npm 账号，也无需手动下载或本机构建。SSH 需要本机 OpenSSH，远端无需 Node.js 或 Go。部分环境安装 PTY 依赖时需要 Python 3 和 C++ 编译工具。

### 从源码运行

另需 **Go 1.25+**，用于构建远端 helper。

```sh
git clone https://github.com/saucer-man/pi-desk.git
cd pi-desk
npm ci
npm run build
npm start
```

开发：`npm run dev`；构建与测试：`npm run check`。程序升级保留 `~/.pi` 中的数据。

### 公网访问

使用独立域名和 HTTPS 反向代理。将请求转发到 `http://127.0.0.1:30140`，保留原始 `Host`，并允许 WebSocket 升级和 SSE 流式传输。请关闭代理缓存，避免在访问日志中记录查询参数。

```sh
npm start -- --public-url https://desk.example.com --no-open
```

使用启动输出的 `Pi Desk (public)` 链接访问。持有完整链接的人可以操作 Pi Desk，请将链接当作密码保管。

## 运行截图

真实运行界面，使用演示会话。

![对话页面](docs/screenshots/chat.jpg)

![设置页面](docs/screenshots/settings.jpg)

## 反馈与许可

问题反馈：[GitHub Issues](https://github.com/saucer-man/pi-desk/issues)。

[Apache-2.0](LICENSE)。部分界面组件与样式来自 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)，相关许可见 [DSH LICENSE](frontend/src/web/dsh/LICENSE.txt)。
