# 开发指南

更新日期：2026-10-26。本文维护环境要求与本仓库当前实际可用的开发命令；范围与里程碑安排见[实施清单](specs/scanner-implementation-plan.md)，构建与分发决策见 [T7](specs/t7-public-release-acceptance.md)。

## 环境要求

| 工具 | 要求与用途 |
| --- | --- |
| Go | 1.27.3（`go.mod` 固定 `go 1.27.3`；本机工具链较低时由 `GOTOOLCHAIN` 自动获取；MyGo v0.6.2 要求 1.27.1+）。 |
| MyGo | v0.6.2，作为 Go tool 依赖固定在 `go.mod`（`tool github.com/egoist/mygo/cmd/mygo`），经 `go tool mygo` 使用，不依赖全局安装的 `mygo` 命令。 |
| Xcode 命令行工具 | macOS 打包所需（`codesign`、`hdiutil`、SDK）；开发机 `xcode-select` 指向 `/Applications/Xcode.app`。 |
| mingw-w64 | 本机已安装（`/opt/homebrew/bin/x86_64-w64-mingw32-gcc`，Homebrew mingw-w64 14.0.0_3）。`internal/recognition` 的 OCR 绑定（onnxruntime_go）需要 cgo，Mac 上交叉编译 Windows 目标时用作 `CC`。 |
| makensis | 本机已安装（v3.13）。macOS 上交叉构建 Windows 平台时也会产出 NSIS 安装包；未安装时 MyGo 仅警告并跳过安装包。 |

不使用 Web 前端：`mygo.json` 为纯原生 UI 配置，无需 Bun、Node.js 或 WebView2。

cgo 说明：`internal/recognition` 依赖 cgo 编译 onnxruntime_go 的 C 桥接。直接调用的 `go build`/`go vet`/`go test` 与 `go tool mygo vet .` 在原生构建下默认 `CGO_ENABLED=1`，无需处理。`go tool mygo dev` 与 `go tool mygo build` 则遵循 MyGo v0.6.2 的行为：环境未显式设置 `CGO_ENABLED` 时，MyGo 给子进程设置 `CGO_ENABLED=0`（[T1](specs/t1-mygo-ui-distribution.md) 已记录，显式设置时尊重用户值）。`cmd/youkai` 经应用服务引用识别包，因此必须以 `CGO_ENABLED=1 go tool mygo dev`、`CGO_ENABLED=1 go tool mygo build` 的方式调用，否则 cgo 包编译失败。识别器由应用服务在第一次接受的开始请求时才创建（`internal/app` 的懒加载），包初始化与就绪回调不加载任何原生库与模型（见[架构第 4 节](specs/scanner-architecture.md#4-ocr原生资源与构建)）。

## 用开发数据源跑一次完整扫描

M1 没有真实游戏控制器，扫描由开发数据源驱动：从 `internal/recognition/testdata`（`lines.tsv` 与 6 件盘共 42 张文字行裁剪）按固定顺序逐件接纳观测。它只能通过环境变量显式启用，正式构建默认不启用，也不是“任意图片导入识别”的用户功能；游戏适配器（M2）就位后由此替换。

在本机（macOS）跑一次含真实识别的完整流程：

```sh
go run ./tools/fetchdeps -platform current
CGO_ENABLED=1 YOUKAI_DEV_SOURCE=$PWD/internal/recognition/testdata \
  YOUKAI_DEV_AUTOSTART=1 go tool mygo dev
```

- `YOUKAI_DEV_SOURCE` 指定观测目录（含 `lines.tsv`），启用开发数据源；目录缺失或清单不符会在启动时失败。
- `YOUKAI_DEV_AUTOSTART` 置任意非空值时，应用就绪后自动开始一次扫描（同样仅开发开关生效；就绪回调本身不做耗时工作，开始请求在独立 goroutine 中发出）。
- `YOUKAI_DEV_PACE` 可选，把开发数据源的接纳节奏改为给定时长（如 `3s`）；默认 250ms。需要观察进度或练习中途停止时用它把扫描拉长：主界面的“停止”按钮可随时中途停止。当前实现中关闭主窗口走明确退出收尾；M3 将按[架构第 5 节](specs/scanner-architecture.md#隐藏恢复与退出)改为默认隐藏到托盘或菜单栏，明确退出才收尾。

## 仅观测演练（游戏窗口，PR 4a）

M2 平台会话集成后可用另一组开关接入真实游戏窗口做仅观测演练（不发送任何输入）：

- `YOUKAI_GAME_SOURCE=observe` 启用游戏窗口仅观测演练：会话经统一平台端口（`internal/platform`）打开真实窗口、解析目标、锁定几何基线并持续观测，全程不派发输入、不接纳观测，用于联调平台集成与停止链路；主界面标签显示“游戏窗口（仅观测演练，不发送输入）”。与 `YOUKAI_DEV_SOURCE` 同时设置会拒绝启动。
- Windows 目标身份待实测轮 ②，须显式提供：`YOUKAI_GAME_EXE=<游戏 exe 完整路径>` 与 `YOUKAI_GAME_CLIENT=local|cloud`（分别对应完整客户端与云客户端记录标识）；缺失时拒绝接线并说明原因——猜一个身份可能作用于错误窗口。macOS 目标固定为实测轮 ① 记录的云客户端（bundle `com.miHoYo.cloudgames.Nap`）。
- 停止热键：macOS 默认 ⌃⌥⌘S（实测验证）；Windows 默认 Ctrl+Alt+Shift+S 加 NoRepeat（未验证，避开 Win 键组合）。热键在准备阶段注册、持续到会话释放，全局可用；自定义留待 M3 设置。
- 真实遍历（4b/4c）就位后，`observe` 演练模式仍可用于无输入联调；两者都不设置时保持生产默认（无扫描源，开始被拒）。

## 真实遍历（macOS 云客户端，4b-2）

- `YOUKAI_GAME_SOURCE=traverse` 在 **darwin** 上接入已标定的驱动盘仓库遍历：预检只要求仓库页、列表形状与唯一转盘位；点击开始后先解析并锁定游戏窗口，若游戏未运行或没有合格窗口则立即停止并报告 `targetNotFound`，已打开时自动把游戏应用切到前台一次（扫描期间失焦仍停止）。随后按转盘位置 1→6 自动切换位置，并按一次面板访问准备筛选：打开→重置→勾选 S→关闭→要求漏斗 Active（S 未生效即停止，A/B 不进入列表；重置是否生效并入同一次校验，旧筛选残留是已知残余风险）；再回顶并确认，逐件点击/初始读取、等待详情归属、截帧接纳。S 选中态尚无像素探针；用户 2026-10-19/25 决定重置与 S 只共用一次面板访问和一次 Active 校验。回顶确认遇到位移、或 Lost 但未取得“最终比较 Undecided + 滑块稳定在顶部”的例外时继续向上，未取得稳定非移动的顶部确认不开始遍历（详见 [T5 §5](specs/t5-zzz-inventory-traversal.md#5-滚动衔接重复物品与末尾) 的 4b-2 实现状态）。每位置自动准备失败即停止并保留已接纳结果，不做补偿动作。滚动位移须等相对 candidate 的 Stationary 确认后才提交；到 `ScrollBudget` 仍不停稳按 Lost 安全停止。当前入口仍要求用户手动把游戏打开到驱动盘仓库页；从待机/主菜单自动导航到仓库页尚未实现。
- 该模式只在 darwin 可用：Windows（及其他平台）没有已标定布局，入口直接拒绝并说明原因，不会猜测锚点或窗口身份。Windows 的 `observe` 演练仍按上一节要求提供目标身份。
- 初始观测范围写入会话记录的 `observedScope`（`kind/page/layout/geometry/slots/startPosition/filterAtStart/sort`），不声称顶部或数量；`filterAtStart` 是准备前的漏斗状态，不代表本轮的 S 筛选声明。每件接纳的 `capture` 带 `position`（1–6）、逐位置重新计数的 `viewportIndex`、`rarityFilter:"S"` 与可选的一次性 `scrollEvidence`。详情行裁剪送识别，品质徽章与选中格边框作为上下文证据落盘、不送识别。
- 与 `YOUKAI_DEV_SOURCE` 同时设置仍会拒绝启动；未设置任何来源时保持生产默认（无扫描源）。这是开发/联调开关，不是面向用户的扫描模式选择。

日志确认要点：`dev scan source wired`、`scan <id> accepted`、每件盘一行 `item N recognized: name=… level=…`、每位置准备成功一行 `youkai: collect: position N prepared: S filter active, top confirmed`、每次已提交滚动一行 `youkai: collect: position N scroll <before> -> <after>: R rows, M correspondences`、`checkpoint generation N saved (… items)` 与结束行的检查点路径（开发应用数据根 `~/Library/Application Support/Youkai Dev/sessions/<id>/checkpoints/<gen>.json`）、退出时 `exit wrap-up complete`。当前实现的退出事件（⌘Q/关窗）异步触发明确退出收尾：停止活动会话 → 等待收尾与最终保存 → 关闭识别器 → 销毁 ORT 环境 → 释放占用；M3 改为关闭默认隐藏、仅明确退出走该收尾。

持久化从扫描过程中开始：每次接纳观测（`internal/scan` 的接纳钩子）都会先写该件的证据图、再以 queued 状态把条目加入检查点文档，终结时只更新状态与载荷；崩溃后重开由 `internal/record` 把排队/运行条目标为中断未处理，已接纳的物品不会消失。

应用服务层的行为（开始请求幂等与忙碌、保存失败可见可重试、重开读取中断记录等）由 `go test ./internal/app` 的服务层测试用假识别器覆盖，不依赖原生库；`internal/app` 的集成测试在 OCR 资源缺失时自动 `t.Skip`。

主界面（`internal/ui`）由 `cmd/youkai` 组装：准备区持续显示前台扫描提示与停止方式，进度区显示阶段、可证实的五类计数与终止原因，结果区按采集序号列出全部条目（失败与未处理不消失）并展示证据图片、识别文本与模型分数，历史记录可重开已保存记录。界面只经订阅泵渲染服务快照（按会话与修订号丢弃旧更新），状态与视图行为由 `go test ./internal/ui` 用 `ui.NewTester` 覆盖。

## 常用命令

在仓库根目录执行。

| 命令 | 用途 |
| --- | --- |
| `gofmt -l .` | 格式检查，无输出即通过。 |
| `go vet ./...` | Go 静态检查。 |
| `go build ./...` | 编译全部包。 |
| `go test ./...` | 运行测试；`internal/recognition` 与 `internal/app` 的集成测试在 OCR 资源缺失时自动跳过，本地需先运行 fetchdeps。 |
| `go test -race ./internal/recognition` | 识别包的竞态检查（含取消与生命周期并发路径）。 |
| `YOUKAI_M2_SAMPLES=/tmp/m2-samples go test ./internal/game/zzz/collect` | 像素匹配器与单页遍历在完整未掩码源帧上的回放；样本目录不入库，未设置时自动跳过。 |
| `go tool mygo vet .` | 先运行 `go vet ./...`，再检查原生 UI 生命周期问题；参数是应用目录（项目根），不能写成 `./...`。 |
| `go tool mygo dev` | 开发运行，编辑 Go 代码、配置或资源后自动重建重启；开发应用名为 `Youkai Dev`（标识 `com.lotwt.youkai.dev`，位于 `.mygo/dev`），与正式安装的数据和权限隔离。退出应用或 Ctrl+C 结束。 |
| `go tool mygo build` | 打包本机平台制品到 `build/<os>-<arch>/`：macOS 生成 ad-hoc 签名的 `Youkai.app` 与 DMG。须显式 `CGO_ENABLED=1`，macOS 另设部署目标，完整命令与说明见[打包与安装包](#打包与安装包)。 |
| `CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 go build ./...` | Windows 交叉编译检查（OCR 绑定需要 cgo 与 mingw-w64）；只证明能编译，不代表能在 Windows 上运行。需要交叉编译识别测试二进制时把 `build` 换成 `test -c -o <path> ./internal/recognition`。 |

`internal/game/zzz/collect/testdata/maccloud` 的掩码夹具由 `go run ./tools/zzzframemask -src /tmp/m2-samples` 重新生成；来源 SHA、掩码与量化口径见该目录 README。

`go tool mygo build` 亦接受 `-platform` 指定目标，如 `-platform windows/amd64`（在 macOS 上产出 EXE，装有 makensis 时同时产出 NSIS 安装包）。按已确认的测试方式，交给用户测试的 Windows 安装包仍以 GitHub Actions 产物为准，见下节。

## 跨进程占用与锁文件

两个占用域（语义见[架构第 5 节](specs/scanner-architecture.md#跨进程占用与观测接纳)）都用 OS 排他原语实现（macOS 用 `flock`，Windows 用 `LockFileEx`），由 `internal/app` 提供并已接入入口；持有者崩溃后由 OS 释放。

| 占用域 | 锁文件位置 | 说明 |
| --- | --- | --- |
| 数据根占用 | 数据根内的 `youkai.lock`（正式版 `%LOCALAPPDATA%\Youkai\`、开发版对应 `Youkai Dev` 根；macOS 为 `~/Library/Application Support/<应用名>/`） | 入口在 `mygo.App.Run` 之前取得、进程退出释放；同一实际目录的路径别名（符号链接、`..`、大小写变体）天然落入同一占用域。第二实例取得失败即记录日志退出。 |
| 扫描占用 | `~/Library/Application Support/com.lotwt.youkai/scan.lock`（macOS）；`%LOCALAPPDATA%\com.lotwt.youkai\scan.lock`（Windows） | 按用户固定，目录名用基础应用标识 `com.lotwt.youkai`（不带开发版后缀、不是数据根的显示名），因此开发版与正式版在同一 OS 用户下共享同一把锁；由数据根占用持有者按需取得（先数据根后扫描）。应用服务在接纳开始请求时取得，会话结束（保存收尾完成）后释放；显式退出收尾兜底释放。 |

开发者需知：锁文件为空文件，内容无意义，互斥完全来自 OS 原语；**不要删除运行中实例的锁文件**（删除后新进程会锁到新文件，占用域被拆散）。`internal/app` 的跨进程测试把扫描锁基目录注入为临时目录（Windows 的 Known Folder 无法按进程重定向，因此经测试注入而非环境变量），避免触碰真实用户目录或与正在运行的应用互相干扰。

## 诊断日志

标准库 `log` 的输出在取得数据根占用后写入 `internal/app` 的日志 sink：每行**先落日志文件、再尽力镜像到 stderr**（镜像结果丢弃）。顺序是修复在 Windows 安装版上生效的关键——GUI 子系统无控制台，stderr 句柄无效、每次写入都失败，若像 `io.MultiWriter` 那样把 stderr 放在前面，它的失败会让文件一行都收不到；文件在前则安装版获得可取回的落盘日志，开发与重定向排查继续看 stderr（M1 报告观察项“安装版没有落盘日志”的修复，行为由 `internal/app` 的 `OpenDiagnosticLog` 提供）。

| 项 | 值 |
| --- | --- |
| 正式版日志目录 | Windows `%LOCALAPPDATA%\Youkai\logs\`；macOS `~/Library/Logs/Youkai/`（`Paths.Logs`，见[架构第 5 节](specs/scanner-architecture.md#本地目录保留与清理)） |
| 开发版日志目录 | 同规则按应用名单独命名（macOS 为 `~/Library/Logs/Youkai Dev/`），与正式版互不混写 |
| 文件 | `youkai.log` 为当前文件，跨运行追加、不截断（重启不抹掉上一次运行的证据）；`youkai.log.1`…`youkai.log.3` 为轮转备份，`.1` 最新；`youkai.crash.log` 为 Go 运行时 panic/fatal 输出的追加文件（`runtime/debug.SetCrashOutput`），不参与轮转，外部终止或 SIGKILL 不会被它捕获 |
| 轮转与保留 | 单文件 1 MiB 上限、保留 3 份备份（`internal/app` 的常量，按实机证据调整），轮转文件上限 4 个；`youkai.crash.log` 另计且不轮转。日志只按大小轮转，属诊断数据，不按时间清理，与历史记录的删除分开（架构第 5 节） |

- 打开时机在数据根占用之后：第二实例占用失败即退出，不会打开日志文件，因此两个进程不会交错或截断同一文件；代价是路径解析失败、占用失败等早期错误只在 stderr（开发期可见；安装版无从看到，但这类错误本身也意味着应用未能启动）。
- 每次运行的第一行日志写明版本、构建提交（取 `debug.ReadBuildInfo` 的 VCS 信息，工作区有本地修改时加 `-modified` 后缀；构建未打标时为 `unknown`）、平台与数据根，用户反馈附带日志即可定位构建。发布与打包 workflow 保证这个提交号干净：对 `mygo.json` 的改写（版本/渠道前缀/移除 updates）经 `git update-index --skip-worktree` 对 Git 隐藏；VCS 戳记按 `git status` 判定，未跟踪文件同样算作修改，因此构建前的其余过程文件只进被 `.gitignore` 忽略的目录（fetchdeps 产物、VC++ DLL）或 runner 的临时目录（`RUNNER_TEMP`，VC++ 步骤的说明文件）——test-v0.0.1 至 test-v0.0.4 的 Windows 构建曾因该说明文件写在仓库根而携带 `-modified`，已修复。构建产物因此携带标签或提交的干净提交号，不带 `-modified`。
- 日志打不开（如目录不可写）不阻止启动：stderr 照常输出并提示一次原因；运行中文件写入失败同样只退回 stderr 镜像（降级逻辑在 `internal/app`，Windows 上备份文件被杀毒或备份软件占用时轮转重试而不丢行，每轮失败只提示一次）。
- `YOUKAI_SELFTEST` 自检在路径解析之前运行，仍只写 stderr（安装布局验证按[打包与安装包](#打包与安装包)的重定向方式读取）。

### 扫描耗时诊断

日志时间戳使用微秒级格式（`log.LstdFlags|log.Lmicroseconds`），便于与行内毫秒耗时对照。遍历与保存新增的计时字段都只用于观测，不改变导航、阈值或等待时长：

- `youkai: collect: item ...`：每件被接纳或跳过的物品一行。`visit_index` 为访问序号，`seq` 为内核接纳序号（跳过件为 0，可与 `item N recognized` 日志对照）；`acquire_ms` 为等待处理槽（`Acquire`，反映 OCR 背压）；`ready_ms` 为点击前的平台就绪复查（`Observe`，含窗口枚举）；`pre_click_ms` 为点击前稳定等待，其中 `select_wait_ms` 是选中框恢复等待（包含在 `pre_click_ms` 内）；`click_ms` 为点击派发（含平台派发前复查与按住时长）；`click_detail_ms` 为点击到详情稳定（`awaitDetail`）；`reference_ms` 为参照往返（不含接纳）；`retries` 为无效点击重试次数；`project_ms` 为接纳前的投影（裁剪识别行）；`admit_ms` 为接纳交付（`slot.Admit`，含证据与检查点持久化）；`total_ms` 为本件总耗时。
- `youkai: collect: scroll ...`：每次滚动的 `drag_ms`（拖动派发）与 `settle_ms`（等待停稳）。
- `youkai: collect: position=N summary ...`：每个位置结束时一行；`establish_ms` 建列表、`wheel_ms` 切转盘、`filter_ms` 筛选准备、`top_ms` 回顶（甩动+确认）、`visit_ms` 逐件合计、`scroll_ms` 滚动合计、`bottom_ms` 底部确认拖动（以短步结束时为 0）、`collect_ms` 采集总耗时、`total_ms` 位置总耗时；`visits` 为已完成的访问次数（含跳过的非 S 件），`scrolls` 为已验证滚动次数。
- 检查点保存行在原有条目数后追加 `bytes`（生成文件字节数）与 `ms`（本次保存耗时）。
- `youkai: scan <id> stop trigger: ...`：会话结束时记录第一个停止触发的 `kind`、`code`、`phase` 与原始 `message`，之后的附加故障各记一行 `later fault`；检查点的 `termination` 只保存种类、代码和阶段。
- 滚动或底部确认失败（Lost、无对应、被拒或未确认）时，匹配器证据追加各整行候选的误差（`row candidates +1=… +3=…`），并把拖动前后两帧的列表与滚动条区域裁剪为 PNG 写入日志目录下的 `diagnostics/`（如 `~/Library/Logs/Youkai Dev/diagnostics/`），日志行 `collect: diagnostics:` 给出路径。每次遍历最多 6 组，目录只保留最新 48 个文件。

## GitHub Actions 检查

`.github/workflows/ci.yml` 在每个 pull request 与推送到 `main` 时，于 `macos-latest`（Apple Silicon）与 `windows-latest` 两个 runner 上运行轻量检查：`gofmt -l .`（要求无输出）、`go vet ./...`、`go test ./...`，macOS 上额外运行 `go tool mygo vet .`。检查不打安装包、不跑长时间测试、不下载 OCR 资源；供 Windows 测试的安装包由手动触发的 Package workflow 按需构建（见[打包 workflow](#打包-workflow)）。仓库根 `.gitattributes` 将文本文件统一为 LF 换行，Windows 检出不会因此产生 `gofmt` 误报。

OCR cgo 绑定接入后，两个 runner 都要编译 `internal/recognition` 的 C 桥接：macOS runner 自带 Xcode 的 `clang`，`windows-latest` 镜像自带 MinGW gcc（写入镜像工具清单，Windows Server 2025 为 gcc 15.2.0），且原生构建 `CGO_ENABLED` 默认为 1，CI 无需额外装工具链；资源缺失时识别集成测试用 `t.Skip` 跳过，因此 PR 检查仍不下载 OCR 资源。以上 runner 镜像结论来自 runner-images 的镜像清单，首次在本仓库 PR 上实际运行 CI 时确认；若镜像变动导致 gcc 缺失，在 CI 中补装 MinGW。CI 编译通过不代表 cgo 绑定能在 Windows 实机加载运行库，该验证归 Windows 测试（见[实施清单的开发与 Windows 测试方式](specs/scanner-implementation-plan.md#开发与-windows-测试方式)）。

## 第三方资源下载

OCR 运行库与模型不进 Git，由 `tools/fetchdeps` 按固定 URL 与 SHA-256 下载（固定组合与摘要在 [T3](specs/t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)，VC++ 运行库不在此工具范围，见[实施清单的工程约定](specs/scanner-implementation-plan.md#已定的工程约定)）。在仓库根目录运行，不需要 API key：

| 命令 | 用途 |
| --- | --- |
| `go run ./tools/fetchdeps` | 下载全部平台的资源（双平台打包所需）。 |
| `go run ./tools/fetchdeps -platform current` | 只下载当前平台所需的资源（公共模型仍会下载）。 |
| `go run ./tools/fetchdeps -platform windows/amd64` | 只下载指定平台的资源，交叉打包前使用。 |

产物与缓存位置（均被 `.gitignore` 忽略，不要手工编辑）：

| 路径 | 内容 |
| --- | --- |
| `resources/windows-amd64/onnxruntime/` | Windows 的 `onnxruntime.dll`、`onnxruntime_providers_shared.dll` 与 ORT 的 `LICENSE`、`ThirdPartyNotices.txt`。 |
| `resources/darwin-arm64/onnxruntime/` | macOS 的 `libonnxruntime.1.29.0.dylib` 与 ORT 的 `LICENSE`、`ThirdPartyNotices.txt`。 |
| `resources/models/` | 两平台共用的识别模型 `PP-OCRv6_rec_small.onnx`。 |
| `.fetchdeps-cache/` | 下载缓存（保存压缩包原文件；摘要一致时直接复用，不访问网络）。 |

行为约定：已存在且摘要一致的文件跳过；压缩包与每个解出文件都按固定 SHA-256 校验，不符立即失败；所有文件先写临时文件、校验通过后改名，失败不会留下半成品。目录名对应 MyGo 的 `resources/` 约定：平台子目录的内容只随对应平台的应用发布，其余随所有平台发布。

## 打包与安装包

两个目标平台的本地打包命令（先按上节下载对应平台资源；版本号取 `mygo.json`，当前 `0.0.1`）：

| 平台 | 命令 |
| --- | --- |
| macOS | `go run ./tools/fetchdeps -platform darwin/arm64 && MACOSX_DEPLOYMENT_TARGET=14.0 CGO_ENABLED=1 go tool mygo build -platform darwin/arm64` |
| Windows 交叉 | `go run ./tools/fetchdeps -platform windows/amd64 && CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go tool mygo build -platform windows/amd64` |

- 产物：macOS 为 ad-hoc 签名的 `Youkai.app` 与 `Youkai 0.0.1 arm64.dmg`（`build/darwin-arm64/`）；Windows 为 `Youkai.exe`、随包资源与 NSIS 每用户安装包 `Youkai Setup 0.0.1 amd64.exe`（`build/windows-amd64/`）。本机装有 makensis 时才产出安装包，缺失只警告并跳过，须自行确认安装包确实生成（workflow 会强制校验）。
- 自动更新与本地构建：`mygo.json` 带 `updates`（公钥与 GitHub 源）时，`mygo build` 会把更新源嵌入产物——本地不带私钥的构建只警告并跳过更新档案（`youkai-*-darwin-arm64.tar.gz`、`update-darwin-arm64.json`），安装该产物后仍会检查正式渠道的更新；带 `MYGO_UPDATER_PRIVATE_KEY` 时产出签名更新档案（本地演练见[下文](#自动更新的本地演练)）。需要完全关闭更新的构建（如交给测试的免更新制品）先从 `mygo.json` 移除 `updates` 再构建。`go run`/`go test` 与 `mygo dev` 的产物不嵌入更新源，应用内更新区显示“当前构建未启用自动更新”。
- `CGO_ENABLED=1` 必须显式设置（MyGo 对未设置的环境默认 `CGO_ENABLED=0`）；`MACOSX_DEPLOYMENT_TARGET=14.0` 与 `mygo.json` 的 `macos.minimumSystemVersion` 一致（T7 最低系统候选），影响 C 代码编译与链接的最低系统标记，MyGo 不代设。
- 资源布局：MyGo 把 `resources/` 与 `resources/<平台>/` 合并为应用资源根——macOS 是 `Youkai.app/Contents/Resources`，Windows 是 EXE 所在目录；识别器经 `internal/app` 的 `OCRResources` 从资源根的 `onnxruntime/`、`models/` 以绝对路径加载，不依赖工作目录。VC++ 运行库（4 个 x64 DLL）只由打包 workflow 从 Windows runner 放入 `resources/windows-amd64/` 顶层（即 EXE 同目录，见 `.gitignore`；本地交叉打包产物不含它们，不影响打包本身）。
- 随包声明：`resources/THIRD-PARTY-NOTICES.md` 随所有平台分发；ORT 的 `LICENSE` 与 `ThirdPartyNotices.txt` 由 fetchdeps 放在 `onnxruntime/` 内。

**识别自检（`YOUKAI_SELFTEST`）**：任何构建（含安装后的应用）在该环境变量非空时启动，会在触碰用户数据与占用锁之前加载随包 ORT 运行库与模型、识别一张内置固定样本（期望文本 `等级15/15`，取自 `internal/recognition` 的测试裁剪）并退出：成功退出码 0，失败非 0 并在 stderr 记录原因。用于验证安装布局（DMG 拷贝出的应用、Windows 安装目录）识别可用，是打包与开发开关，不是用户可见的图片导入功能。macOS 直接运行 `Youkai.app/Contents/MacOS/Youkai`；Windows EXE 为 GUI 子系统无控制台，用 PowerShell 重定向：

```powershell
$env:YOUKAI_SELFTEST="1"
Start-Process "$env:LOCALAPPDATA\Programs\Youkai\Youkai.exe" -NoNewWindow -Wait -PassThru `
  -RedirectStandardError err.txt -RedirectStandardOutput out.txt | Out-Null
Get-Content err.txt
```

### 打包 workflow

`.github/workflows/package.yml`（Package）只用 `workflow_dispatch` 手动触发（合并到 main 后在 Actions 页运行；PR 不打安装包，轻量检查见[上节](#github-actions-检查)），在 `macos-latest` 与 `windows-latest` 各一个 job：

- 各自运行 fetchdeps 并以显式 `CGO_ENABLED=1` 打包（macOS 另设 `MACOSX_DEPLOYMENT_TARGET=14.0`）。
- **关闭自动更新**：两个 job 构建前用 `jq` 移除检出里 `mygo.json` 的 `updates` 段（不提交），产物因此不嵌入更新配置、不产出更新档案；识别自检日志首行的 `updates=disabled` 被强制断言。带更新签名的发布构建由标签触发的 Release workflow 承担，见[发布与自动更新](release.md)。
- Windows job 用 `vswhere` 定位 runner 上 Visual Studio 的 `VC\Redist\MSVC` 下名称形如版本号的最新目录（`v145` 之类非版本目录不计），在其 `x64` 下取编号最高的 `Microsoft.VC14*.CRT`，把 `MSVCP140.dll`、`MSVCP140_1.dll`、`VCRUNTIME140.dll`、`VCRUNTIME140_1.dll` 复制到 `resources/windows-amd64/` 顶层再打包，实际使用的版本目录与 CRT 目录名写入制品清单；不从 vc_redist 安装器解包、不进 Git。
- 两个 job 都强制校验产物生成（缺 DMG、缺安装包——包括缺 NSIS 被跳过——或随包文件缺位都会失败），并在打包布局上运行识别自检（Windows job 因此能在 runner 上实际验证 ORT DLL、VC++ DLL 与模型的加载）。
- 上传安装包、DMG 与制品清单（文件名、大小、SHA-256、原生库与模型摘要、VC++ 运行库版本、提交 SHA）为 workflow artifact；测试构建以提交 SHA 标识，不发布 Releases（正式发布与自动更新见[发布与自动更新](release.md)）。

### 自动更新的本地演练

2026-10-17 在本机用一次性测试密钥对（`go tool mygo keygen` 生成，用后即弃，与正式密钥无关）演练了 MyGo 的签名与校验链路，演练材料全部放在 `/tmp`（不进仓库）：

1. **构建侧签名**：`mygo.json` 临时换上测试公钥后 `MYGO_UPDATER_PRIVATE_KEY=<测试私钥> go tool mygo build -platform darwin/arm64`，产出签名归档 `youkai-0.0.1-darwin-arm64.tar.gz` 与清单 `update-darwin-arm64.json`；清单的 `notes` 取自 `CHANGELOG.md` 的 `## 0.0.1` 小节，首版无已发布版本时差量更新记 "no version is published yet" 跳过。打包产物自检首行报告 `updates=enabled`。
2. **应用侧校验与替换**：清单 URL 改指 `http://127.0.0.1:8765`（MyGo 的更新客户端允许回环 HTTP，`mygo.json` 的 `url` 源则强制 HTTPS，故经 ldflags 注入 feed 的等价方式演练）；用一个经 `-ldflags -X` 注入同一 feed 与公钥、版本 `0.0.0` 的一次性驱动程序（包在伪 `.app` 中，`MYGO_ENV=production`）执行 `Enabled → Check → Install`：检测到 `0.0.1` 与说明、带进度下载 36,009,549 字节、验签通过并把伪 bundle 整体替换为归档里的 `Youkai.app`（含 `_CodeSignature`）。
3. **拒绝路径**：同尺寸翻 1 字节的损坏归档、用另一把一次性密钥重签的清单，都在安装时报 `mygo: installing the update: the update is not signed with the app's key` 且不替换；签名字段缺失的清单在检查时报 `incomplete manifest`。
4. **无私钥行为**：带 `updates` 但不设 `MYGO_UPDATER_PRIVATE_KEY` 的本地构建只警告 "Not signing an update" 并跳过更新档案，产物仍嵌入更新源（自检 `updates=enabled`，安装后可收到正式渠道更新）；按 Package workflow 的方式移除 `updates` 再构建，产物自检为 `updates=disabled`。

未覆盖（只能在真实发布后验证）：真实 GitHub Releases 渠道隔离（`test-v` / latest）、差量更新、Windows 侧替换与重装后的行为，见[发布与自动更新](release.md)的升级实测清单。

### 完整应用演练（安装 → 自动重启）

2026-10-17 修复“装后不自动重启”后补充的整链路演练：不再用驱动程序，而是跑**完整应用**，覆盖检查、下载、安装替换、明确退出收尾与自动重启的真实接线（含关窗拦截与数据根占用锁）。材料仍在 `/tmp`、密钥对一次性（`go tool mygo keygen` 生成，用后即弃）：

1. **构建两个版本**：`mygo.json` 的 `updates.url` 强制 HTTPS，本地回环源经 ldflags 注入等价变量（`mygo build` 不支持自定义 ldflags，故用 `go build` 手工构建并手工组装 `.app`）：

   ```sh
   CGO_ENABLED=1 go build -ldflags "-X github.com/egoist/mygo.packageUpdateFeed=http://127.0.0.1:8765/update-darwin-arm64.json -X github.com/egoist/mygo.packageUpdateKey=<测试公钥> -X 'github.com/egoist/mygo.packageName=Youkai Upd Test' -X github.com/egoist/mygo.packageVersion=<版本> -X github.com/egoist/mygo.packageIdentifier=com.lotwt.youkai.updtest -X github.com/egoist/mygo.production=1" -o <bundle>/Contents/MacOS/YoukaiUpdTest ./cmd/youkai
   ```

   演练专用名称与标识（`Youkai Upd Test` / `com.lotwt.youkai.updtest`）使数据根、日志与单实例锁都和正式版隔离。`Info.plist` 必须带 `CFBundleShortVersionString`：**macOS 上 `mygo.App.Version()` 读 bundle 的 Info.plist**，缺了它版本号为空、检查比较失真。`MYGO_ENV` 无需设置（`production=1` 已按 `mygo build` 的方式链接）。
2. **本地更新源**：`python3 -m http.server 8765 --bind 127.0.0.1` 服务新版归档与清单；归档用 `COPYFILE_DISABLE=1 tar -czf … -C <staging> YoukaiUpdTest.app` 制作——macOS 的 bsdtar 默认把扩展属性写成 `._*` AppleDouble 条目，多出的顶层条目会让 MyGo 拒绝归档（`the update holds no app bundle`）。清单（`update-darwin-arm64.json`）的 `signature` 为 Ed25519 对归档 SHA-256 摘要的签名（base64），一次性小程序用测试私钥生成即可。
3. **演练运行**：`YOUKAI_DEV_AUTOUPDATE=1 <bundle>/Contents/MacOS/YoukaiUpdTest`——该开发开关让检查发现的新版本自动安装（仍遵守“扫描中禁止安装”），无需点击界面。诊断日志（`~/Library/Logs/Youkai Upd Test/youkai.log`）应依次出现：`version=0.0.1` → `update check: found 0.0.2 (running 0.0.1)` → `downloading and verifying` → `downloaded N bytes` → `0.0.2 installed` → `update installed, restarting` → `exit wrap-up complete` → `exit settled, relaunching into the installed update` → **`version=0.0.2`**（自动重启的新进程）→ `no version newer than 0.0.2`。2026-10-17 本机按此演练通过（下载 10,001,699 字节，1 秒内完成替换、收尾与重启）。

未覆盖（只能在真实发布后验证，与上节合并看待）：真实渠道、差量更新，以及 **Windows 实机**的安装替换、自动重启与 HKCU `DisplayVersion` 刷新（本机只能交叉编译验证）。

## 与 Windows 测试方式的关系

用户不在 Windows 上安装开发工具，Windows 上的功能与安装测试使用 GitHub Actions 构建的安装包：开发期测试从 Package workflow 的产物获取（见[上节](#打包-workflow)），正式发布制品随版本发布在 GitHub Releases（约定见[实施清单](specs/scanner-implementation-plan.md#开发与-windows-测试方式)）。

Mac 上的 Windows 交叉编译检查只用于尽早发现编译与平台隔离问题：交叉编译通过不代表能在 Windows 上运行，VC++ 运行库（DLL 来源与实际加载）也不在本地交叉编译的覆盖范围内，需要快速排查非 VC++ 问题时可另行交叉编译免安装包供测试。只能在 Windows 实机验证的事项见实施清单第 1 节。
