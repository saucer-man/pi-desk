# Pi Desk

Pi Desk 是 Pi coding agent 的 Wails v3 桌面客户端。Pi 继续负责 agent runtime，Pi Desk 提供桌面 UI、workspace/session 管理、Repository、Terminal，以及受信任的远程 SSH workspace。

Repository 支持模糊文件检索和只读多标签预览（文本、Markdown、常见图片/音频与 PDF）；Extensions 可通过 Pi 官方 CLI 管理全局及受信任本地 workspace 的 package，包括安装、更新、移除和资源启停。

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
