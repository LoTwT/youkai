# M1 自动更新升级实测报告

更新日期：2026-10-18。本文记录 M1 的自动更新升级链实测，对应[实施清单 M1](../specs/scanner-implementation-plan.md#m1正式工程资源与会话底座)第 6 项（接入 MyGo 自动更新并验证升级）在真实 Release 上的验证，逐项对应[发布与自动更新的升级实测清单](../release.md#发布后验证)。测试渠道 test-v0.0.1 至 test-v0.0.4 的四轮发布与三轮升级由用户在两平台实机完成；Windows 侧 0.0.3→0.0.4 的升级后检查为另一会话的只读核对。本报告只记录实际发生过的事实，未覆盖项与不能推出的结论见文末。

## 制品与环境

| 项 | 记录 |
| --- | --- |
| 升级链 | test-v0.0.1（`46461b7`，PR #18）→ test-v0.0.2（`0087849`，PR #19）→ test-v0.0.3（`cef7f25`，PR #20）→ test-v0.0.4（`676a4ff`）；均为测试渠道普通 Release、不设 latest。0.0.2 起 Release workflow 全自动发布，且发布含差量包。 |
| 0.0.1 的发布 | test-v0.0.1 的发布步骤因资产名校验失败由主会话手动完成；该校验缺陷由 PR #19 修正，自 0.0.2 起全自动发布不再需要补救（见[发布与自动更新的失败处理](../release.md#发布步骤)）。 |
| 安装来源 | 0.0.1 安装包由用户用浏览器从 Release 页面下载并安装，Windows 与 macOS 的下载与安装均未被拦截。 |
| Windows | Windows 11 专业版 23H2；安装于默认路径 `%LOCALAPPDATA%\Programs\Youkai`。 |
| macOS | macOS 27.0.1（Apple Silicon）；`/Applications/Youkai.app`。 |
| 升级方式 | 应用内检查并安装更新（release 构建无自动安装开关）；逐轮日志见[关键输出](#关键输出)。 |
| 输入与预期 | 输入为上述安装包、各轮 Release 与按应用内界面执行的用户操作；单项预期以[升级实测清单](../release.md#发布后验证)为准。 |

## 测试结果

### 1. 下载与安装 0.0.1（两平台）— 通过

浏览器从 Release 页面下载 0.0.1 安装包并安装，Windows 与 macOS 的下载与安装均未被拦截；Windows 安装于默认路径 `%LOCALAPPDATA%\Programs\Youkai`。

### 2. 0.0.1 → 0.0.2（两平台）— 通过，附三点说明

- 检测到新版本、有下载进度、无报错，升级生效。
- 说明一：未自动重启，且界面无版本号。根因见 PR #20：youkai 的关窗拦截取消了 MyGo 的 Relaunch，退出收尾完成后走普通退出；0.0.1 旧版本不含该修复，属当时的实现行为。
- 说明二：Windows 注册表卸载项 `DisplayVersion` 仍为 0.0.1。
- 说明三：日志落盘修复（PR #16）在 Windows 实机确认生效。

### 3. 0.0.2 → 0.0.3（两平台）— 通过

- 按预期不自动重启（自动重启由执行升级的旧版本发起，0.0.2 尚不含 PR #20 的修复）；手动打开后为 0.0.3。
- Windows 日志：`09:36:04 uninstall entry version refreshed to 0.0.3`。

### 4. 0.0.3 → 0.0.4（两平台）— 通过（用户确认自动重启）

- macOS：日志逐行见[关键输出](#关键输出)；升级后 `/Applications/Youkai.app` 为 0.0.4，用户确认无 Gatekeeper 再次拦截。
- Windows（另一会话只读检查）：同一秒 09:48:53 内依次——`0.0.4 installed` → `restarting` → `exit wrap-up complete` → `exit settled, relaunching` → `version=0.0.4 commit=676a4ff2291a-modified` → `uninstall entry version refreshed to 0.0.4`；下载 13260 字节（差量，完成行出现两次，见[观察项二](#观察项二下载完成日志行重复已随本报告所在-pr-修复)）；注册表 `DisplayVersion` = 0.0.4；`Youkai.exe` 文件版本 0.0.4.0，原地覆写、无残留暂存文件；当前运行进程即 09:48:53 自动重启的实例；事件查看器无 Youkai 错误。
- 用户确认两平台升级后历史数据保留、再次检查显示已是最新。

## 关键输出

macOS 0.0.3 → 0.0.4（诊断日志摘录，`downloaded 12301 bytes` 行实际出现两次，见[观察项二](#观察项二下载完成日志行重复已随本报告所在-pr-修复)）：

```text
09:48:41 update check: found 0.0.4 (running 0.0.3)
09:48:43 update install: downloading and verifying 0.0.4
09:48:43 update install: downloaded 12301 bytes   （出现两次）
09:48:43 update install: 0.0.4 installed
09:48:43 update installed, restarting the application
09:48:43 exit wrap-up complete
09:48:43 exit settled, relaunching into the installed update
09:48:44 version=0.0.4 commit=676a4ff2291a
09:48:46 update check: no version newer than 0.0.4
```

Windows 0.0.2 → 0.0.3（诊断日志摘录）：

```text
09:36:04 uninstall entry version refreshed to 0.0.3
```

Windows 0.0.3 → 0.0.4（只读检查汇总，同一秒 09:48:53 内依次发生，行文缩写）：

```text
09:48:53 0.0.4 installed
09:48:53 restarting
09:48:53 exit wrap-up complete
09:48:53 exit settled, relaunching
09:48:53 version=0.0.4 commit=676a4ff2291a-modified
09:48:53 uninstall entry version refreshed to 0.0.4
```

## 未覆盖项

1. 签名错误包被拒：仅本机演练覆盖（一次性测试密钥对，见[开发指南的自动更新本地演练](../development.md#自动更新的本地演练)），安装版实机未测。
2. 扫描进行中禁止安装：仅单元测试覆盖；M1 安装版无扫描源，实机无法触发。
3. 正式渠道（`v*`）：尚无正式版发布，正式渠道构建的检查、安装与渠道隔离的另一侧未验证。
4. 干净环境（无系统 VC++）的 Windows：本轮实机已装有 VC++，应用本地运行库的不可或缺性未验证。
5. 24 小时定时检查的实际触发：各轮升级在启动检查与手动操作时段完成，定时触发未单独验证。
6. macOS 升级后录屏与辅助功能权限：M2 才申请这些权限，本轮无权限可核。

## 观察项一：Windows 构建的提交号带 -modified（已随本报告所在 PR 修复）

- 现象：Windows 构建的诊断日志首行为 `commit=<sha>-modified`（test-v0.0.3 为 `cef7f2509193-modified`、test-v0.0.4 为 `676a4ff2291a-modified`），macOS 同提交为干净的 `commit=`。
- 根因：Release 与 Package workflow 的 Windows job 在 `go tool mygo build` 之前执行 VC++ 步骤，把未跟踪文件 `vc-redist-info.txt` 写进仓库根；Go 的 VCS 戳记按 `git status` 判定，未跟踪文件同样会使 `vcs.modified=true`。`mygo.json` 的版本改写已被 `skip-worktree` 隐藏，fetchdeps 产物与 VC++ DLL 都在被忽略目录，该文件是构建前唯一落进检出的未跟踪文件（该写法自 test-v0.0.1 的 workflow 即存在）。macOS job 没有 VC++ 步骤，构建前只写被忽略路径，因此干净。
- 修复：两个 workflow 的 VC++ 步骤改为把说明文件写入 `$env:RUNNER_TEMP`，Package 的制品清单步骤从那里读取；构建前的检出目录与标签提交保持一致（机制说明见[开发指南的诊断日志](../development.md#诊断日志)）。
- 本地演练：在干净检出的 `676a4ff` 上按 workflow 复现构建前的全部写入（`skip-worktree` 后的 `mygo.json` 改写、被忽略的 fetchdeps 产物与 VC++ DLL，说明文件不落检出），`git status --porcelain` 为空，`go build` 的 VCS 戳记 `vcs.modified=false`；把 `vc-redist-info.txt` 放回仓库根后重建，戳记变为 `vcs.modified=true`，与实机现象一致。
- 影响与边界：仅诊断日志的提交号显示，不影响更新流程本身；修复后的下一次 Windows 构建应显示干净 `commit=`，以届时构建日志为准。

## 观察项二：下载完成日志行重复（已随本报告所在 PR 修复）

- 现象：两平台 0.0.3→0.0.4 的诊断日志中，`update install: downloaded N bytes` 连续出现两行相同（macOS 12301 字节、Windows 13260 字节）。
- 根因：MyGo 的进度回调对一次下载的完成报两次——最后一块数据到达（已读字节等于总大小）与读到 EOF 时各触发一次相同的完成进度；`cmd/youkai/updates.go` 原实现在每次见到 `downloaded >= total` 时都记一行。
- 修复：改为每次下载只记一次完成行；差量失败回退整包时进度从零重新开始，两次下载各记一行、字节数不同即可区分。已加单元测试（完成只记一次、差量回退记两行、无总大小不记）。
- 影响与边界：仅日志可读性，不影响下载、验签与安装。

## 结论

通过的范围：test-v0.0.1 至 test-v0.0.4 的测试渠道升级链已在两平台实机完成。0.0.1 经浏览器从 Release 页面下载安装，两平台均未被拦截；三轮升级（0.0.1→0.0.2、0.0.2→0.0.3、0.0.3→0.0.4）均检测到新版本、有下载进度、无报错、升级生效；0.0.3→0.0.4 用户确认两平台安装完成后自动重启进入新版本——macOS 升级后无 Gatekeeper 再次拦截，Windows 为差量下载（13260 字节）原地覆写、无残留暂存文件与事件查看器错误，重启后版本一致（`/Applications/Youkai.app`、`Youkai.exe` 文件版本与注册表 `DisplayVersion` 均为 0.0.4）；两平台升级后历史数据保留，再次检查显示已是最新；日志落盘修复（PR #16）在 Windows 实机确认生效。覆盖结论限于上述制品、系统配置与测试渠道。

不能推出的结论：

1. 签名错误被拒的实机表现（仅本机演练）与扫描进行中禁止安装的实机表现（仅单元测试，M1 安装版无扫描源）。
2. 正式渠道（`v*`）的发布、检查与安装，以及渠道隔离的另一侧（尚无正式版发布，“只收到测试渠道更新”无法排除性地验证）。
3. 干净环境（无系统 VC++）的 Windows 升级链，以及 24 小时定时检查的实际触发。
4. macOS 升级后录屏与辅助功能权限的变化（M2 申请权限后才能实测）。
5. M1 第 6 项整体完成或 M1 通过条件满足：剩余验证见[未覆盖项](#未覆盖项)，M1 其余事项另行判定（[实施清单](../specs/scanner-implementation-plan.md#m1正式工程资源与会话底座)）。
