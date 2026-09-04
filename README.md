# Pi Desk

Pi Desk 是 Pi coding agent 的 Wails v3 桌面客户端。Pi 继续负责 agent runtime，Pi Desk 提供桌面 UI、workspace/session 管理、定时任务、Repository、Terminal，以及受信任的远程 SSH workspace。

Repository 支持模糊文件检索和只读多标签预览（文本、Markdown、常见图片/音频与 PDF）；Extensions 可通过 Pi 官方 CLI 管理全局及受信任本地 workspace 的 package，包括安装、更新、移除和资源启停。

定时任务支持一次、每小时、每天、工作日和每周计划。任务会保存明确的 provider/model 和思考强度；新建时优先使用当前 Pi task 的模型与思考强度，无法确定时必须手动选择。任务仅能绑定已信任的本地 workspace；Pi Desk 运行时，到点会创建普通 Pi task，应用并验证保存的执行参数，然后发送提示词；错过多个周期只补跑一次。

输入区的队列支持编辑、删除、立即追加和滚动。正文会按输入框、队列与 Todo 的实际高度预留空间；停留在底部时保持最后一条消息可见，查看历史消息时保留滚动位置。

Windows 支持从资源管理器复制当前已信任本地 workspace 内的文件，再粘贴到输入框或队列编辑框。普通文件插入 `@相对路径` 引用，支持多文件、中文和空格；全部为 PNG/JPEG/GIF/WebP 图片时复用图片附件流程，混合选择按文件引用处理。文件引用不会复制或上传源文件；文件夹、工作区外文件和向 SSH 会话粘贴本机文件会被拒绝。macOS/Linux 保留现有文本与图片粘贴。

## 运行模型

```text
Vue -> Wails service -> Go host -> pi --mode rpc
                         |-> local filesystem/Git/PTY
                         |-> SSH -> remote-helper -> remote root
```

远程 workspace 不把远端目录挂成本地路径。连接成功后，host 校验 host key/config/root identity，签发 generation-bound root capability；Repository、Terminal 和 Pi task 只使用短 read lease/task lease。断连、identity 漂移或 lease 失效会撤销能力并让相关状态 stale。

## 主要目录

- `internal/appservice`：Wails facade、remote lifecycle、Pi task 和 backend coordinator。
- `internal/remotessh`：SSH connection、host identity、helper install、runtime/lease。
- `internal/remotehelper`、`cmd/pi-desk-remote-helper`：远端受限 helper。
- `internal/repository`、`internal/terminal`：本地/远端 Repository 与 Terminal 适配。
- `internal/workspace`、`internal/sessionindex`：workspace catalog 与 Pi session 索引。
- `frontend/src`：Vue UI、services、store 和组件。

## 开发验证

```powershell
cd frontend
npm run check
cd ..
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

涉及 Wails 启动或打包时再运行 `wails3 build`。

真实运行验收使用以下显式任务：

```powershell
# 构建后启动 Windows EXE，捕获实际窗口并拒绝黑屏/空白渲染
wails3 task verify:windows-smoke

# 使用本机已安装的 Pi CLI 验证 RPC、runtime、terminal 和 session
wails3 task verify:pi-live
```