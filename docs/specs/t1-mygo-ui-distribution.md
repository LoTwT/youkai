# T1：MyGo 界面与构建分发

更新日期：2026-10-16。状态：界面、平台与架构、校对、关窗、分发及前台扫描使用方式已由用户确认，决定见第 4 节；浏览器入口已明确为网页唤起桌面应用，评估见第 3 节。交付要求进一步澄清为正常构建打包，不走官方分发渠道、无需准备付费签名资格，路线见 [T7](t7-public-release-acceptance.md)；最低系统、显示配置、构建与安装实测仍未完成。本文补充界面体验、应用集成与分发的取舍；公共模块边界、状态和结果语义由主控维护的[架构评估](scanner-architecture.md)收敛。初始用户目标见 [Handoff](../scanner-handoff.md)，原 T1 后续澄清记录在第 1、4 节。

## 1. 已确认选择与界面建议

用户已澄清：选择 MyGo 是因为它足够轻量，并要求进一步核对 MyGo 自身 UI 的能力与性能。2026-10-15，用户明确决定“那就使用 MyGo 原生 UI”。这项选择的主要动机是轻量，不能改写成全 Go 语言偏好。

随后用户决定跳过单独的 UI 验证版，直接确认待定内容。界面体验与性能检查并入正式功能验收，不再把独立样例窗口设为推进产品方案的前置步骤。

**当前选择为 MyGo 原生 UI。** 第 6 节的组件与公开性能证据支持进入实际验证，同时省去 WebView 和前端构建。此前暂推荐 Web 的结论已被用户选择取代。后续确认的平台、结果处理与分发选择见第 4 节；具体显示配置和应用实施尚未确定，也不把官方示例的占用当作 youkai 的预算。

本文保留的 Web 绑定、页面重载和 WebView2 内容用于路线对照，不属于当前纯原生 UI 的实施或验收要求。应用服务与扫描内核的公共契约仍由主控收敛。

最小界面建议只有一套：结果列表选择物品，详情区域同时展示证据图片与字段表单，提供“下一个待校对项”。先不实现整张表逐单元格编辑、批量修改或两套 UI。截图放大与中文输入是基本验收项，不能只用显示几个数字的示例判断可用性。后续操作流程、逐件确认及导出体验统一在 [T6](t6-review-export-workflow.md)维护，本节保留框架选择与组件依据。

| 比较项 | Web 前端 | Go 原生 UI |
| --- | --- | --- |
| 校对交互 | 可以用 HTML/CSS 组织列表、图片和表单，前端框架另行选择。 | 官方已有 Table、Image、文本和数值输入，能够作为同一校对界面的基础。 |
| 数据与更新 | 需要生成 TypeScript 绑定，处理页面重建、事件订阅和图片 URL。 | UI 直接调用 Go 服务，后台结果通过 UI 的更新机制呈现。 |
| 维护面 | 增加前端依赖、构建和 WebView2 兼容性验证。 | 增加对 MyGo 布局、控件行为和状态更新方式的熟悉成本。 |
| 主要待测项 | 长列表、图片加载、页面重载、开发与安装后不同地址下的行为。 | 中文输入法、键盘校对、长列表、图片缩放和目标平台上的绘制。 |

官方 [Table 文档](https://mygo.egoist.dev/docs/ui/table)描述了仅构建可见行、键盘选择、列调整、排序状态和单元格内可编辑文本；[Image 文档](https://mygo.egoist.dev/docs/ui/image)描述了位图、缩放和通过 `Window.Update` 更新图片。它们是滚动官网的能力说明，不是 youkai 已通过的验收结果。不能因原生路线需要验证，就假定它缺少校对组件。

**已确认的扫描使用方式：**用户接受扫描期间让游戏保持前台，扫描完成或停止后再校对，并要求使用时明确告知。扫描器自身可隐藏到托盘或菜单栏。建议在开始前展示使用说明，采集期间保留进度和停止方式，待收尾完成后开放校对；具体焦点、输入和停止保证仍由 T2/T4/T5 与主控收敛，接受该使用方式不代表这些能力已实测通过。

## 2. 应用层交互与公共契约引用

T1 提出的交互建议已由主控汇入[架构第 2 节](scanner-architecture.md#2-界面选择与应用层)和[第 5 节](scanner-architecture.md#5-扫描会话与结果语义)。它们集中维护会话所有权、快照/通知、开始与停止、修订保存，以及隐藏、恢复和退出顺序；这里不再维护第二份状态表。两种关窗模式及前台扫描是已确认要求，首次关窗选择并记忆、开始并隐藏、恢复时停止、明确退出和保存失败处理仍是主控设计草案。

### 两种关窗模式的体验建议

Windows 托盘与 macOS 菜单栏提供状态、打开主窗口、停止和退出入口；界面名称按平台呈现。关窗偏好、托盘不可用、恢复时的失焦与退出收尾以[主控隐藏、恢复与退出契约](scanner-architecture.md#隐藏恢复与退出)为准。尤其不能把“隐藏后继续”解释为失焦后自动恢复；正在采集时恢复可交互主窗口，按主控草案先停止输入再显示收尾。

官方 [Menus and the tray](https://mygo.egoist.dev/docs/menus#tray-icons)提供跨平台 `NewTray`；[Windows 文档](https://mygo.egoist.dev/docs/windows)提供 `OnClose`、`Hide`、`Show` 和 `ShowInactive`，后者不主动聚焦，但不保证显示后不会遮挡游戏。`OnClose` 与 [App.OnBeforeQuit](https://mygo.egoist.dev/docs/app)可拦截关闭和退出；回调中只安排异步收尾，不能等待采集 goroutine，也不以跳过退出事件的 `App.Exit` 代替正常退出。框架能力已有文档依据，项目生命周期和焦点顺序尚未实测。

### 使用提示与界面行为建议

“使用时告知前台扫描限制”是已确认要求。建议在开始区域持续展示以下短文案，正常点击“开始扫描”即可，不额外增加勾选确认步骤：

> 扫描期间会自动操作游戏。请保持游戏窗口在前台，不要切换应用或手动操作游戏。扫描完成或停止后，即可校对结果。

开始区域同时显示实际已注册的停止快捷键；采集时恢复入口可写成“停止扫描并打开”，说明操作后果。进度、完成提示、校对开放时机和保存状态以主控契约为准；文案不能代替平台与游戏条件检查。

Web 绑定 context、页面重载和 WebView 内部资源协议仅属于历史路线对照，相关依据可查[绑定](https://mygo.egoist.dev/docs/bindings)与[前端](https://mygo.egoist.dev/docs/frontend)文档，不进入当前原生 UI 的行为合同。浏览器通过外部 URL scheme 唤起桌面应用是另一项能力，见本轮新增需求。

## 3. 构建与分发的具体影响

固定版本、cgo、资源布局和候选 OCR 组合的基础证据沿用[架构第 1、4 节](scanner-architecture.md)。初轮读取的 MyGo v0.5.1 源码和性能证据保留为原版本记录，本轮新增候选如下，不将旧测量自动改标为新版本。

本机工具链状态（2026-10-15）：用户单独授权下载并安装最新稳定版 Go，现已从 1.25.6 升级至 1.27.2 / macOS arm64，安装目录为 `/usr/local/go`。官方分发地址连接失败后，从阿里云镜像下载同一 `.pkg`，其 SHA-256 与 [Go 官网](https://go.dev/dl/)公布的值一致，Google 安装包签名和 Apple 公证验证通过。安装器报告升级成功，`go version`、`GOTOOLCHAIN=local go version` 和编译器版本均确认 1.27.2。本次仅更新 Go，尚未安装 MyGo/OCR 依赖或运行项目原型。

### 首轮框架与工具链候选

2026-10-16 的后续评估建议以 **MyGo v0.5.5 + Go 1.27.2** 作为正式功能的首轮验证组合；这是主控技术建议，尚未创建模块或安装框架。MyGo 固定提交为 `4d8673cd2658a2c6e86b26c44751e57bf8174f91`，其 [go.mod](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/go.mod)要求 Go 1.27.1，本机现有 1.27.2 满足版本条件，Windows 工具链尚未准备。

选择该版本的具体依据是[发布记录](https://github.com/egoist/mygo/releases/tag/v0.5.5)包含分栏布局和列表点击修复，以及 cgo 文件的原生 UI 检查补齐；后者已在 [vet_ui.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/vet_ui.go)核对，适合将接入 cgo 的正式功能作为验收对象。只复核了本轮相关模块，尚未证明所有 v0.5.1 行为在此版本通过回归；第 6 节性能数据仍属于 v0.5.1。

新版本 [proc.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/proc.go)仍在未指定时设置 `CGO_ENABLED=0`。OCR 与 Mac 平台桥接要求显式启用 cgo，并分别使用目标平台 C 工具链；框架自身不需要 cgo，不能推出完整扫描器也不需要。资源合并与签名规则仍须在最终安装制品验证，不默认 Go OCR 包带有 MyGo 原生资源插件清单。

[Go 1.27 发布说明](https://go.dev/doc/go1.27#ports)已将 macOS 下限提高到 13，不能直接沿用 MyGo 官网的 macOS 12+。结合 OCR 制品与平台 API 后的候选由 [T7](t7-public-release-acceptance.md#最低系统候选与证据)统一维护；OCR 文件、摘要、内嵌字符表和实际磁盘成本见 [T3 静态核查](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)。界面轻量不等于完整安装包只有演示应用的大小。

### Web 与原生 UI 的构建差别

Web 的绑定生成需要先编译并执行主机应用，随后才构建前端及目标程序。v0.5.1 的 `writeClient` 用主机环境构建，`generateBindings` 执行该二进制；未配置绑定输出的原生项目跳过这个步骤。由此，纯原生 UI 可以减少一项主机生成流程，但接入 cgo OCR 后仍然需要相应 C 工具链。[v0.5.1 proc.go](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/cmd/mygo/proc.go)

建议保持启动配置和服务注册没有 OCR、模型或游戏探测副作用。生成模式在 `App.Run` 返回后，`main` 后面的代码仍会继续，所以这些副作用也不能简单搬到 `App.Run` 后。应用就绪时只安排后台预检，避免在 `WhenReady` 的主线程回调中加载模型。[v0.5.1 app.go](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/app.go)

**仅用于 Web 路线对照：**若 OCR 绑定通过链接依赖导致动态库在进程启动时即被加载，仅推迟显式初始化未必足以支持无运行库的绑定生成。当前已选原生 UI，无需这项生成验收；T3 仍需核对真实加载方式、安装依赖及缺库时的失败表现，公共要求见[架构第 4 节](scanner-architecture.md#4-ocr原生资源与构建)。

在 macOS 生成绑定、面向 Windows 编译时，主机编译器和目标编译器是不同用途；不能将 Windows `CC` 全局套到每一个步骤。已确认双平台同时首发，建议 Windows 与 macOS 正式制品分别在对应系统构建和验收。Go 工具链、MyGo CLI/模块、前端依赖及 OCR 组合需要固定；本机 Go 的更新不代表已选定项目依赖组合，MyGo、前端及 OCR 依赖仍未安装。

### 资源和安装

- `mygo dev` 与正式打包都会合并目标平台资源；直接 `go run` / `go test` 使用未合并的项目资源目录。建议应用组装时确定资源根，向 OCR 传入明确路径；直接测试显式提供测试资源根。不能把“开发时可加载”推导成“从开始菜单启动也可加载”。[分发文档](https://mygo.egoist.dev/docs/distribution) · [v0.5.1 resources.go](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/cmd/mygo/resources.go)
- 模型、字典和原生运行库随版本分发，结果及截图写用户数据位置。安装目录和当前工作目录都不承担可写数据存储；安装后的用户不需要 Go、C 编译器或前端开发运行时。OCR 的 DLL 及其运行前提仍须按 T3 选定组合验证。
- v0.5.1 的 NSIS 实现是每用户安装。在 Windows 构建时可下载 NSIS；其他构建主机缺少 `makensis` 时，会提示并跳过安装包，因此编译成功本身不证明生成了安装包。所读脚本没有 WebView2 或 VC++ Runtime 安装步骤。[v0.5.1 nsis.go](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/cmd/mygo/nsis.go)
- MyGo 官方分发流程可生成 macOS `.app` 与 DMG；本地代码签名与 DMG 制作在 macOS 执行。当前按用户选择采用 ad-hoc 路线，Developer ID 和公证不作为首版前提；内置 OCR 原生库、其他机器首次打开和权限仍需验收。macOS 已确认仅支持 Apple Silicon，最低系统版本仍待确定；具体依据与限制统一见 [T7 第 3 节](t7-public-release-acceptance.md#3-不依赖付费证书的打包路线)。
- 若采用 Windows Web，建议检测 WebView2，并接入缺失时的安装流程。Microsoft 分别提供在线 Bootstrapper 与离线 Standalone Installer；“安装完成后断网可用”和“全新机器离线首装”需要分别验收。具体安装集成方案待目标平台及离线首装需求确定，不假定 MyGo 已有覆盖全部前提的配置开关。[Microsoft 分发文档](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution)

### 浏览器唤起的新增需求与构建依据

2026-10-16，用户提出“还有一项是支持浏览器打开 app”，随后明确回复“唤起桌面 app 吧”。采用网页链接唤起已安装的 MyGo 原生应用；该含义不再待答。拟定链接如 `youkai://open`，尚未实际注册；启动、恢复和扫描中收到链接的行为统一由[主控第 2 节](scanner-architecture.md#浏览器唤起桌面应用)维护。

用户同时询问浏览器内界面的可行性。固定 MyGo v0.5.5 已提供 `web` / `js/wasm` 构建路径，可将原生 UI 编译为 WebAssembly 并生成浏览器加载页，因此存在复用原生界面代码的路径。[固定构建实现](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/web.go) 这不使桌面采集、输入和 cgo OCR 自动具备浏览器能力；若以后采用此路线，需隔离平台依赖，由本机 Go 程序继续持有扫描、保存和会话，另做通信适配、连接鉴权、断线恢复及浏览器验收。也可以另写 HTML 界面，具体复用程度须实测；首版仍仅提供网页唤起桌面应用，不新增 Web 工程。

[MyGo 前端文档](https://mygo.egoist.dev/docs/frontend#the-mygo-runtime-package)明确区分框架窗口与普通浏览器：浏览器标签页没有注入的运行时，`isMyGo()` 为 false，相关调用会失败。这描述的是 HTML 前端的桌面绑定，不能据此否定上述原生 UI 的 WebAssembly 路径，也不能认为浏览器可直接调用桌面 Go 服务。无论采用哪种界面，页面关闭不取消服务拥有的扫描与保存；浏览器交互使游戏失焦时仍按公共契约停止。下面保留原来核对过的 v0.5.1 深链接依据，浏览器 UI 和应用协议唤起分别验收。

| 已阅读依据 | 确认的框架能力与集成要求 |
| --- | --- |
| [MyGo 深链接文档](https://mygo.egoist.dev/docs/app#deep-links)、[v0.5.1 接收实现](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/deeplink.go) | 构建配置支持 `urlSchemes`，应用通过 `OnOpenURL` 接收 URL；监听在 `Run` 前注册，才能接收启动参数。Windows 的后续启动结合单实例能力转交给现有实例，处理器仍须校验路由。 |
| [v0.5.1 Windows 安装脚本](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/cmd/mygo/nsis.go) | 按 URLSchemes 生成当前用户的协议注册、程序路径和卸载删除项。安装、升级与卸载的处理器归属须验证，不能仅在开发进程调用注册接口就算安装包完成。 |
| [v0.5.1 Mac 构建](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/cmd/mygo/bundle.go)、[Darwin 注册实现](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/internal/darwin/deeplink.go) | 应用包写入 `CFBundleURLTypes`/`CFBundleURLSchemes`；运行时注册检查包中声明，不能替代构建声明。需验证 DMG 拖拽安装后的系统识别、首次唤起及多个副本的归属。 |

Apple 的[属性列表参考](https://developer.apple.com/library/archive/documentation/General/Reference/InfoPlistKeyReference/Articles/CoreFoundationKeys.html)列出 URL 类型及角色键，正式构建时应核对完整声明；当前读取的 MyGo 生成片段只显式填写名称与 scheme，不据此推导实际注册一定成功或一定失败。浏览器可能要求用户确认打开外部应用，不能把静默打开作为跨浏览器承诺；例如 [Edge 官方策略](https://learn.microsoft.com/en-us/deployedge/microsoft-edge-policies/AutoLaunchProtocolsFromOrigins)区分普通提示与允许跳过提示的策略配置，本项目不替用户修改策略。

此项的新增成本主要是安装注册、URL 分发与既有窗口/扫描状态衔接，以及双平台浏览器验收；无需为唤起入口新增 Web UI、本地 Web 服务或浏览器扩展。当前仅核对文档和固定源码，未注册系统协议、修改浏览器设置、生成安装包或实际唤起应用。验收矩阵统一见[主控第 7 节](scanner-architecture.md#两平台游戏与安装验收)。

## 4. 产品选择与正式功能验收

本节记录已确认的产品行为与平台范围，以及仍需技术核对的事项。以下界面检查在获相应实现授权后随正式功能完成，当前均未运行；跳过独立 UI 验证版不构成性能或兼容性已经达标的结论。

### 已确认的产品选择

2026-10-15，用户在多轮讨论中确认如下选择；这些是产品范围决定，不代表相应能力已经实现或实测通过。

| 已确认项 | 用户选择 | 对方案的影响 |
| --- | --- | --- |
| 首发运行平台 | Windows 与 macOS 同时首发，开发可优先 Windows。 | 两个平台均需完成真实游戏扫描闭环，只有界面可运行不算达标；具体显示配置、截图和输入权限及适配边界交主控与 T2/T5/T7 收敛。 |
| 应用语言 | 2026-10-16 确认首版仅简体中文。 | youkai 自有按钮、设置、错误说明及使用文档提供简体中文，首版不增加英文、繁体中文或语言切换。游戏识别语言由主控范围单独定义；系统对话框与权限提示按实际平台行为验收。 |
| Windows 架构 | 首版仅支持 x64。 | Windows 制品与识别运行库按 amd64 准备，ARM64 不纳入首版支持；最低 Windows 版本另行核对。 |
| macOS 运行场景与架构 | 使用从 App Store 下载并直接打开的《云·绝区零》应用；仅支持 Apple Silicon，Intel Mac 不纳入支持范围。 | macOS 只需准备 arm64 应用及对应识别运行库；T2/T5 围绕该应用窗口核对采集、输入和实际仓库布局，不再把网页端作为待定入口。 |
| 应用内结果处理 | 查看全部结果，对照截图并修改字段。 | 首版包含完整结果浏览、图片证据和字段修订；后续确认的 S 级范围及字段提案统一见[主控第 6 节](scanner-architecture.md#6-首版功能范围建议)。 |
| 扫描与校对的使用节奏 | 接受扫描时游戏保持前台，完成或停止后再校对；使用时须明确告知用户。 | 开始前展示前台要求，扫描器自身可隐藏；文案、停止入口和隐藏时机见第 2 节建议，平台输入与公共停止契约仍由主控收敛。 |
| 扫描中关闭主窗口 | 两种模式均提供：停止扫描并保存退出，或隐藏后继续运行。 | Windows 通过托盘、macOS 通过菜单栏提供返回入口；首次选择、记忆方式及明确退出行为见第 2 节建议，具体取消与保存契约由主控收敛。 |
| 分发形式 | Windows 安装包，macOS DMG 拖拽安装。 | 不走官方分发渠道；两种制品分别完成安装、用户数据路径及首次打开行为验收，签名路线见 T7。 |
| 识别资源准备 | 完整包随附模型、字典与识别运行库，首次启动无需另行下载识别资源。 | 安装包更大，但避免首次资源下载、失败重试和版本匹配流程；其他系统运行前提仍须核对。云游戏本身仍依赖网络，不能承诺离线游戏扫描。 |
| 浏览器入口 | 2026-10-16 确认网页唤起桌面 App。 | 按第 3 节实现和验收深链接；浏览器内界面只作为可行性咨询。 |
| 首版交付标准 | 2026-10-16 先提出公开发布标准，后澄清为正常构建打包，不准备 Apple Developer 会员或 Windows 证书；用户测试前确保环境。 | 付费签名、公证和商店上架移出首版前置条件，功能与数据验收保留；路线汇入 [T7](t7-public-release-acceptance.md)。 |

后续客户端范围已在 [T2](t2-platform-capture-input.md#首版平台与客户端范围已确认)确认，首发覆盖 Windows 完整客户端、官方 Windows 云客户端及 Mac App Store 云客户端三种场景，分别验收扫描闭环。用户已确认首版有限显示配置、后续逐步扩展游戏内置尺寸；Windows 尺寸候选和 Mac 首组选择条件见 [T2 显示配置记录](t2-platform-capture-input.md#32-可验收的窄显示配置候选)，不在本节复制或推定具体尺寸与缩放。

### 仍需技术核对与主控收敛的事项

本轮 T1 产品选择已收敛。后续围绕已确认范围核对工具链、平台能力和正式验收，不重新要求用户选择 UI 或独立验证版，也不把产品决定视为应用实施授权。

官方事实核查（2026-10-15）：[《云·绝区零》App Store 页面](https://apps.apple.com/cn/app/%E4%BA%91-%E7%BB%9D%E5%8C%BA%E9%9B%B6/id6503421200?platform=mac)列出的 Mac 条件为 macOS 12.0 或更高版本、Apple M1 或更新芯片。这与用户提供的应用入口和排除 Intel Mac 的范围一致；该条件属于游戏应用，不能直接当作 youkai 的最低系统支持承诺，也不能证明截图和自动输入已可用。

MyGo 提供按平台和架构构建及合并资源的流程，当前目标为 `windows/amd64` 与 `darwin/arm64`，macOS 无需通用包。接入 cgo OCR 后仍需对应编译工具和原生库，完整扫描器的支持能力须在选定平台验收。[分发文档](https://mygo.egoist.dev/docs/distribution#platforms)

用户已确认不指定必须兼容的旧系统版本，最低系统由主控按依赖要求和实测确定，决定与核对顺序见 [T7](t7-public-release-acceptance.md#4-依赖与最低系统的核对顺序)。T1/T2/T3/T7 联合核对框架、采集/输入 API 和识别依赖，候选依据见 [T2](t2-platform-capture-input.md#23-api-可用版本不等于产品下限)；当前无需付费证书的打包路线按 T7 验收。这项决定没有将任何候选版本变为已验证的支持下限。

首版实体与游戏识别语言已由用户在[主控第 6 节](scanner-architecture.md#6-首版功能范围建议)确认；具体显示参数、OCR 绑定与实测仍由主控和对应任务收敛，T1 的界面选择不替代工程验证。

正式实现可复用[原生模板与项目 CLI](https://mygo.egoist.dev/docs/cli)及 [UI 测试](https://mygo.egoist.dev/docs/ui/testing)。`ui.NewTester` 可检查选中项与编辑数据的对应关系等易出错行为；真实输入法、GPU 绘制和安装体验仍需在目标系统检查。最脆弱的假设是现有组件能以有限定制完成连续校对；若必须维护大量自制控件，先收窄交互，仍不能满足时再提出路线调整。

### 正式功能的验收范围

| 检查 | 最小输入与通过条件 | 所需条件 |
| --- | --- | --- |
| 校对体验 | 同一组结果、中文字段和裁剪图，完成筛选、键盘切换、放大、修正、保存及重新打开；连续操作不能错改另一项。建议用 1,000 项与 10,000 项合成列表观察滚动及图片内存，数量仅为压力输入。 | 选定 UI、固定样例；合成数据不用于证明真实库存或 OCR 能力。 |
| 使用提示与焦点 | 开始前能看到前台扫描要求与实际停止方式；隐藏动作在采集输入前完成，进度更新不抢焦点；完成或取消收尾后才开放校对。对失焦、恢复界面和窗口变化的处理按主控契约检查。 | T1/T2/T4/T6 配合；假服务可检查界面状态，实际输入停止须在目标系统验收。 |
| 原生构建与初始化 | 当前工程无需 Web 绑定生成；应用就绪回调不阻塞，真实模型与原生库的加载、缺失或损坏有可核查表现。启动时即需的原生库由打包与安装保证，不假定都能延迟至 UI 内处理。 | 满足固定 MyGo 版本的 Go 及对应编译工具链；T3 的两平台候选组合。 |
| 生命周期 | 用可控假扫描服务覆盖双启动、慢订阅、旧消息、取消与再次开始；两种关窗模式、偏好记忆、托盘恢复、托盘创建失败、明确退出及保存失败。退出收尾只执行一次，隐藏与恢复不重复启动；页面重载或故障仅为 Web 路线比较项。 | 随正式功能进行的不运行游戏验证；不能据此宣布平台输入或 OCR 已可及时停止。 |
| 资源路径 | 分别在 `mygo dev`、直接测试和安装后，从不同工作目录、含中文与空格的路径加载真实模型及平台原生运行库；证据图可读取，损坏资源给出可处理错误。 | T3 的真实资源；正式声明的平台需在该平台验证。 |
| 双平台安装前提 | Windows 与 macOS 均在无开发工具的干净环境安装，检查运行前提、原生库与模型路径；按 T7 检查 Windows 未签名、Mac ad-hoc 制品的首次打开及权限体验。若要求离线首装，再单独断网安装，使用固定图片检查识别、校对和导出。 | 与 T2/T3/T7 配合；不把云游戏的联网要求算作扫描器资源下载依赖。 |
| 实际关窗与退出 | 按 T2 的三种客户端场景，真正运行截图及 OCR 时分别取消、隐藏、恢复、退出、再次运行，检查停止输入、识别返回、证据保存和资源释放各自的结果；隐藏扫描器时仍按游戏采集条件判断能否继续。 | T2/T3/T4/T5 提供实现，双平台实机可用；采用主控收敛的验收标准。 |

## 5. 维护成本与会改变建议的证据

共同成本是 Go/MyGo 升级、应用服务和保存行为、原生资源组合及目标平台回归。换成原生 UI 只能减少前端和 WebView 的维护面，不能消除 OCR 的 cgo 与分发问题。采用 Web 则要维护前端依赖和绑定生成，检查 MyGo 升级后的调用、事件及协议行为。原生路线要持续检查输入法、焦点、控件状态与图片呈现，避免为了惯用交互维护大量自制控件。

| 证据 | 对建议的影响 |
| --- | --- |
| 用户已明确重视轻量，并选择 MyGo 原生 UI；组件和公开基准能支撑继续验证。 | 按已确认的原生路线推进，验收依据仍是本项目的实际体验与占用。 |
| 原生 UI 通过中文输入、连续校对与大量图片验证，定制量小。 | 支持采用原生路线。 |
| 原生路线需要大量自制控件才能满足确认的体验，Web 同等验证通过。 | 重新考虑 Web，比较具体交互差距、运行占用与额外维护代码。 |
| Windows WebView2 的安装要求与目标用户环境冲突。 | 优先评估原生 UI；OCR 的运行前提仍单独核对。 |
| Web 的图片访问或页面恢复在安装版中表现不可靠。 | 先定位官方集成方式；若必须大量绕过框架，重新比较原生路线。 |

首发平台与架构已确认，识别资源随包提供；系统运行前提与完整离线首装能力仍需技术核对。轻量的动机已经明确，但具体内存、安装体积和响应时间预算尚未约定；在正式界面验收中取得代表性数据，不从官方演示推定本项目已达标。

## 6. 原生 UI 能力与性能核查

本节新增核查以 MyGo v0.5.1（`408b4def1f3e6a8d3fac852396b4a46d82ba22a3`）为源码基准，结合 2026-10-15 读取的官方文档与公开基准结果。所有数字均为上游测量，本项目没有复跑。

### 界面能力

MyGo 的原生 UI 由 Go 描述、框架自行绘制，Windows 使用 Direct3D 11，macOS 使用 Metal，Linux 使用 OpenGL；它不为纯原生窗口启动 WebView。其组件行为由框架实现，不能等同于直接使用操作系统自带表格和输入控件。[原生 UI](https://mygo.egoist.dev/docs/ui) · [渲染机制](https://mygo.egoist.dev/docs/ui/rendering)

| 能力 | 已有官方能力及对候选界面的意义 |
| --- | --- |
| 布局与外观 | 行列、Grid、SplitView、侧栏和标签页，可组织结果与详情区域；主题可调整间距、颜色、字号，跟随深浅色及系统文字缩放。[布局](https://mygo.egoist.dev/docs/ui/layout) · [主题](https://mygo.egoist.dev/docs/ui/styling) |
| 结果表格 | 可见行渲染、选择和键盘导航、列宽与顺序调整、多列排序状态、表头菜单、单元格内可编辑文本。实际数据排序和筛选仍由应用提供。[Table](https://mygo.egoist.dev/docs/ui/table) |
| 字段校对 | 文本与数值输入、搜索、选择和补全、撤销重做、剪贴板及输入法组合状态；文档说明组合输入期间 Enter 不触发表单提交。这是输入法设计能力，尚非本项目 Windows 中文输入法验收。[Text input](https://mygo.egoist.dev/docs/ui/text-input) |
| 图片证据 | 位图和 SVG、按比例适配、像素化放大、更新已有位图；可在 Go 中直接传入图像。完整的缩放平移与字段标注交互仍要由应用组合，不能仅凭 Image 组件宣称已有图片校对器。[Image](https://mygo.egoist.dev/docs/ui/image) |
| 其他交互 | 菜单、对话框、进度、拖放、快捷键、屏幕阅读器接口；另有 Markdown、富文本、代码编辑器、Diff 和图表组件，范围已超出基础按钮和标签。[组件目录](https://mygo.egoist.dev/docs/ui) · [辅助功能](https://mygo.egoist.dev/docs/ui/accessibility) |

据此判断，“结果列表＋图片证据＋字段表单”不需要先自建一套控件库。界面是否顺手仍取决于应用组合及目标平台表现。框架已有[元素生命周期与焦点 API 迁移说明](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/docs/ui/migration.md)，升级成本不能只按组件数量判断；建议固定版本后再验证。

### 体积、内存与开窗

从[官方原始数据](https://raw.githubusercontent.com/egoist/mygo/benchmarks/history/2026-10-14.json)中按完整提交 SHA 提取 v0.5.1 记录。该日文件按 UTC 命名，对应记录日期为 `2026-10-15T06:57:45+08:00`。Windows 运行环境为 amd64、Intel Xeon 6973P-C、Go 1.27.1，六次测量取中位数。以下 MB 均按 1,000,000 字节换算。

| Windows 指标 | 原生 UI | Web UI |
| --- | ---: | ---: |
| 官方小示例程序体积 | `counter-native`：9.37 MB | `hello`：9.66 MB |
| 启动六秒后的空闲私有工作集，含相关子进程 | `counter-native`：12.15 MB | `hello`：64.47 MB |
| 开窗基准的一次循环 | `ContentWindowOpen`：111.6 ms | `WindowOpen`：172.9 ms |

解读时需保留这些限制：

- 程序体积来自去除调试信息的示例构建，没有 OCR、模型和本应用资源，也没有把系统 WebView2 的安装体积加到 Web 示例中。两者都是小示例，但内容不同，不能据此保证相同产品界面能节省固定比例。[构建与统计脚本](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/scripts/bench.ts)
- Windows 内存口径是私有工作集，不能当作全部提交内存或显存。Web 的数字已计入其子进程；只看 `hello/app` 的 4.43 MB 会遗漏主要开销。[内存测量程序](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/internal/idlemem/main.go)
- 开窗基准在已经运行的进程中重复创建窗口并销毁。Web 等待 DOM ready，原生等待 view 构建，且计时循环包含 `Destroy`；它们既非完整应用冷启动，也非一致的屏幕实际呈现时刻，不能写成“启动只需 111.6 ms”。[基准实现](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/internal/e2e/bench_test.go)
- 官网首页约 44 MB 原生、62 MB Web 的概括数字来自 macOS 小应用，不能直接转用到 Windows。平台内存口径也不同：macOS 为 physical footprint，Linux 为 PSS，Windows 为私有工作集。[架构与测量说明](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/docs/architecture.md#benchmarks)

这组证据更支持“原生 UI 有较低运行占用的潜力”，而不是“去掉 Web 后 exe 会缩小很多”。

### 列表、绘制与空闲开销

同一 v0.5.1 Windows 记录的 `BenchmarkListScroll` 为 **0.6873 ms/次**。源码测的是 980×720 的无窗口 CPU 渲染，列表设为百万个变高行，每行两个文本，滚动一步产生一帧；没有加载百万件真实物品或截图，也没有游戏和 OCR 并行负载。[基准源码](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/ui/list_test.go#L1118) · [原始数据](https://raw.githubusercontent.com/egoist/mygo/benchmarks/history/2026-10-14.json)

官方[性能说明](https://mygo.egoist.dev/docs/ui/performance)另列 Apple M5 上 1,000 个静态标签 397 µs/帧、百万行列表的可见行 14.5 µs/帧。它们是预热后的 `ui.Tester` CPU 稳态测量，条件为 Go 1.27.1、`CGO_ENABLED=0 GOMAXPROCS=1`；同页的 2.50 MB 是特定视图保留的 Go heap，不含完整原生窗口进程及 GPU 资源。不能据此宣布实际表格的帧率或进程只占 2.50 MB。

渲染设计本身有利于轻量工具：输入、显式失效或动画变化时才绘制，空闲窗口不持续重绘；字体图集只上传变化，位图可复用 GPU 纹理。与此同时，应用如果持续刷新进度、重解码图片或执行动画，就会继续产生工作。缩略图与证据缓存仍须有界，图片应按需加载；列表虚拟化不自动限制图像资源。[渲染说明](https://mygo.egoist.dev/docs/ui/rendering) · [Image](https://mygo.egoist.dev/docs/ui/image)

### 对 youkai 的结论与待测项

原生 UI 具备候选界面所需的组件，公开基准显示低开销设计确有测量依据，足以成为当前首选验证路线。尚需证明的集中在三件事：

1. 中文输入法、键盘选择和校对保存能连续操作，切换列表项不会把修改写到另一项；不同系统缩放下图片和字段仍清楚。
2. 代表性物品数据加真实裁剪图，反复滚动和切换后内存稳定；分别记录空闲与活动时的进程内存、GPU 占用和帧耗时，避免只看 Go heap。
3. 与游戏及 OCR 同时运行时，进度更新和图片浏览仍能响应；扫描器总体大小与占用重新计入模型、运行库及证据缓存，不能沿用小示例数字。

MyGo 已提供 `MYGO_FRAME_STATS` 分解构建、布局、绘制与呈现耗时，可以用于后续定位。[渲染说明](https://mygo.egoist.dev/docs/ui/rendering) 当前未实现或执行这些验证。

本轮完成官方文档、固定源码和公开基准数据核查，更新本文及索引；保留已有未提交工作。没有创建应用、安装或升级依赖、运行 MyGo/OCR 原型、执行 Windows 扫描、提交、推送或发布。上游测量与本项目待测结论分别记录。
