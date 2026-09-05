# Pi Desk

Pi Desk 是 Pi coding agent 的 Wails v3 桌面客户端。Pi 继续负责 agent runtime，Pi Desk 提供桌面 UI、workspace/session 管理、定时任务、Repository、Terminal，以及受信任的远程 SSH workspace。

主要能力：

- Repository：模糊文件检索与只读多标签预览。
- Extensions：通过 Pi 官方 CLI 管理全局及 workspace 的扩展包。
- 定时任务：一次/每小时/每天/工作日/每周计划，绑定受信任的本地 workspace。
- 队列与输入：流式提示词队列、文件引用与图片附件。
- 远程 SSH workspace：host key 校验与 lease 隔离的远端 Repository、Terminal 与 Pi task。

## 运行模型

```text
Vue -> Wails service -> Go host -> pi --mode rpc
                         |-> local filesystem/Git/PTY
                         |-> SSH -> remote-helper -> remote root
```
