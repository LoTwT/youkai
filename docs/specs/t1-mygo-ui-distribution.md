# T1：MyGo 界面与构建分发

更新日期：2026-10-18。状态：界面、平台、结果处理、关窗、分发和浏览器入口均已由用户确认（见第 4 节）；构建与安装已双平台实机验证（见[实施清单 M1](scanner-implementation-plan.md#m1正式工程资源与会话底座)），最低系统与显示配置尚未实测。

本文维护界面框架选择、构建与分发影响、浏览器唤起依据和界面验收。会话所有权、隐藏恢复与退出顺序由[架构第 2、5 节](scanner-architecture.md#2-界面选择与应用层)维护，校对流程由 [T6](t6-review-export-workflow.md) 维护。

## 1. 已确认选择与界面建议

用户选择 MyGo 是因为轻量，并于 2026-10-15 决定使用 MyGo 原生 UI。这项选择的动机是轻量，不代表偏好全 Go 技术栈。随后用户决定跳过单独的 UI 验证版，界面体验与性能检查并入正式功能验收。

曾比较的 Web 前端路线（TypeScript 绑定、页面重载、WebView2）已被这次选择取代，不属于当前实施或验收要求。取舍要点：原生 UI 省去前端依赖、绑定生成和 WebView2 前提，代价是需要熟悉 MyGo 的布局、控件和状态更新方式，并重点验证中文输入法、键盘校对、长列表、图片缩放和目标平台绘制。

最小界面只有一套：结果列表选择物品，详情区域同时展示证据图片与字段表单，提供“下一个待校对项”。不实现整表逐格编辑、批量修改或两套 UI。截图放大与中文输入是基本验收项。

官方 [Table](https://mygo.egoist.dev/docs/ui/table) 提供可见行渲染、键盘选择、列调整、排序状态和单元格内编辑；[Image](https://mygo.egoist.dev/docs/ui/image) 提供位图、缩放和通过 `Window.Update` 更新图片。这些是框架能力说明，不是 youkai 的验收结果。

**扫描使用方式（已确认）：** 扫描期间游戏保持前台，完成或停止后再校对，使用时须明确告知；扫描器自身可隐藏到托盘或菜单栏。

## 2. 关窗与使用提示

两种关窗模式均提供：停止扫描并保存退出，或隐藏后继续。Windows 托盘与 macOS 菜单栏提供状态、打开主窗口、停止和退出入口。关窗偏好、托盘不可用、恢复时的失焦与退出收尾以[隐藏、恢复与退出契约](scanner-architecture.md#隐藏恢复与退出)为准；“隐藏后继续”不意味着失焦后自动恢复。

MyGo 的 [Menus and the tray](https://mygo.egoist.dev/docs/menus#tray-icons) 提供 `NewTray`；[Windows](https://mygo.egoist.dev/docs/windows) 提供 `OnClose`、`Hide`、`Show`、`ShowInactive`（后者不主动聚焦，但不保证不遮挡游戏）。`OnClose` 与 [App.OnBeforeQuit](https://mygo.egoist.dev/docs/app) 可拦截关闭和退出，回调中只安排异步收尾；`App.Exit` 会跳过退出事件，不用于正常退出。

开始区域持续展示以下文案，正常点击“开始扫描”即可，不加勾选确认：

> 扫描期间会自动操作游戏。请保持游戏窗口在前台，不要切换应用或手动操作游戏。扫描完成或停止后，即可校对结果。

开始区域同时显示实际注册的停止快捷键；采集中的恢复入口写作“停止扫描并打开”。文案不能代替平台与游戏条件检查。

## 3. 构建与分发的具体影响

### 首轮框架与工具链候选

正式功能使用 **MyGo v0.5.5 + Go 1.27.2**。v0.5.5 是当前最新发布版（2026-10-16 核对），固定提交 `4d8673cd2658a2c6e86b26c44751e57bf8174f91`，[go.mod](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/go.mod) 要求 Go 1.27.1。main 分支在此之后的提交未发布，不跟随；升级时重新核对本文和架构中引用的源码行为。

选择依据：[v0.5.5 发布记录](https://github.com/egoist/mygo/releases/tag/v0.5.5)包含分栏布局和列表点击修复，并补齐了 cgo 文件的原生 UI 检查（[vet_ui.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/vet_ui.go)）。2026-10-16 已在 v0.5.5 源码上复核本项目引用的单实例、cgo 默认值、资源合并、NSIS、深链接注册、文件对话框和 `App.Exit` 行为，与早先 v0.5.1 的阅读结论一致。

本机 Go 已于 2026-10-15 经用户授权升级至 1.27.2（macOS arm64，SHA-256 与官网一致，签名与公证验证通过）。MyGo v0.5.5 已于 2026-10-16 固定为项目工具依赖（随工程初始化落地）；OCR 运行库与模型已由 fetchdeps 按固定摘要下载并随包分发（见 [T3](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)与[开发指南](../development.md)）；mingw-w64（`x86_64-w64-mingw32-gcc`）已安装。

[proc.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/proc.go) 在未指定时设置 `CGO_ENABLED=0`。OCR 与 Mac 平台桥接要求显式启用 cgo 并使用目标平台 C 工具链。[Go 1.27](https://go.dev/doc/go1.27#ports) 已将 macOS 下限提高到 13，不能沿用 MyGo 官网的 macOS 12+；系统候选由 [T7](t7-public-release-acceptance.md#最低系统候选与证据)维护，OCR 制品与体积见 [T3](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)。

启动配置和服务注册不能有 OCR、模型或游戏探测的副作用；应用就绪时只安排后台预检，不在 `WhenReady` 主线程回调中加载模型。

### 资源和安装

- `mygo dev` 与正式打包会合并目标平台资源，直接 `go run` / `go test` 使用未合并的目录（[resources.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/resources.go)、[分发文档](https://mygo.egoist.dev/docs/distribution)）。应用组装时确定资源根，向 OCR 传入明确路径；测试显式提供资源根。开发时能加载不代表从开始菜单启动也能加载。
- 模型、字典和原生运行库随版本分发，结果和截图写用户数据目录；安装后的用户不需要 Go、C 编译器或前端运行时。
- NSIS 安装为每用户安装；在 Windows 上构建时可自动下载 NSIS，其他主机缺少 `makensis` 时只警告并跳过安装包，所以编译成功不代表已生成安装包。脚本没有 VC++ Runtime 安装步骤。[nsis.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/nsis.go)
- MyGo 可生成 macOS `.app` 与 DMG，本地签名和 DMG 制作在 macOS 上执行。按用户选择采用 ad-hoc 签名，Developer ID 与公证不作为首版前提；路线见 [T7 第 3 节](t7-public-release-acceptance.md#3-不依赖付费证书的打包路线)。
- 双平台正式制品分别在对应系统构建和验收；日常开发与 Windows 测试安排见[实施清单](scanner-implementation-plan.md#开发与-windows-测试方式)。

### 浏览器唤起的新增需求与构建依据

用户于 2026-10-16 确认“唤起桌面 app”：网页链接唤起已安装的原生应用，拟定链接 `youkai://open`，尚未注册。启动、恢复和扫描中收到链接的行为由[架构第 2 节](scanner-architecture.md#浏览器唤起桌面应用)维护。

用户另问过浏览器内界面的可行性：v0.5.5 有 `web` / `js/wasm` 构建路径，可把原生 UI 编译为 WebAssembly（[web.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/web.go)），但桌面采集、输入和 cgo OCR 不会因此进入浏览器，需另做本机通信、鉴权与断线处理。这只是可行性答复，不在首版范围。

| 依据 | 框架能力与集成要求 |
| --- | --- |
| [深链接文档](https://mygo.egoist.dev/docs/app#deep-links)、[deeplink.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/deeplink.go) | 构建配置支持 `urlSchemes`，应用通过 `OnOpenURL` 接收 URL；监听须在 `Run` 前注册才能收到启动时的链接。Windows 的后续启动经单实例转交现有实例，处理器仍须校验路由。 |
| [nsis.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/nsis.go) | 按 URLSchemes 在 HKCU 写入协议注册，卸载时删除。安装、升级与卸载后的处理器归属须验证。 |
| [bundle.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/bundle.go)、[Darwin 注册](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/internal/darwin/deeplink.go) | 应用包写入 `CFBundleURLTypes`（只填名称与 scheme）；运行时注册不能代替构建声明。需验证 DMG 拖拽安装后的识别、首次唤起和多副本归属。 |

浏览器可能要求用户确认打开外部应用，不承诺静默打开；本项目不替用户修改浏览器策略（如 [Edge AutoLaunchProtocolsFromOrigins](https://learn.microsoft.com/en-us/deployedge/microsoft-edge-policies/AutoLaunchProtocolsFromOrigins)）。新增成本在安装注册、URL 分发与扫描状态衔接及双平台浏览器验收，不需要 Web UI、本地 Web 服务或浏览器扩展。

## 4. 产品选择与正式功能验收

### 已确认的产品选择

| 已确认项 | 用户选择 | 对方案的影响 |
| --- | --- | --- |
| 界面 | MyGo 原生 UI（2026-10-15），跳过独立 UI 验证版。 | 界面检查随正式功能验收。 |
| 首发平台 | Windows 与 macOS 同时首发，开发可优先 Windows。 | 两个平台都要完成真实扫描闭环。 |
| 应用语言 | 首版仅简体中文（2026-10-16）。 | 按钮、设置、错误说明和使用文档只提供简体中文，不做语言切换；游戏识别语言见架构第 6 节。 |
| Windows 架构 | 仅 x64。 | 制品与运行库按 amd64 准备，ARM64 不纳入。 |
| macOS 场景与架构 | App Store《云·绝区零》；仅 Apple Silicon。 | 只准备 arm64；围绕该应用窗口核对采集、输入和布局。 |
| 应用内结果处理 | 查看全部结果，对照截图修改字段。 | 首版包含完整结果浏览、图片证据和字段修订。 |
| 扫描与校对节奏 | 扫描时游戏保持前台，完成或停止后校对，须明确告知。 | 开始前展示前台要求，扫描器可隐藏。 |
| 扫描中关闭主窗口 | 两种模式：停止并保存退出，或隐藏后继续。 | 托盘或菜单栏提供返回入口。 |
| 分发形式 | Windows 安装包，macOS DMG 拖拽安装，不走官方渠道。 | 两种制品分别验收安装、数据路径和首次打开。 |
| 自动更新 | 首版包含（2026-10-16）：MyGo v0.5.5 内置更新，从 GitHub Releases（`LoTwT/youkai`）获取经 MyGo 更新密钥签名的版本。 | 更新公钥自 0.0.1 内置；私钥不进仓库，扫描中不安装更新，测试版与正式版渠道分开；规则见[架构第 6 节](scanner-architecture.md#6-首版功能范围)。 |
| 识别资源 | 完整包随附模型、字典与运行库，首次启动无需下载。 | 安装包更大，但省去首次下载与版本匹配；云游戏仍需联网。 |
| 浏览器入口 | 网页唤起桌面 App（2026-10-16）。 | 按第 3 节实现与验收深链接。 |
| 交付标准 | 正常构建打包，不准备 Apple Developer 会员或 Windows 证书；用户在测试前准备环境。 | 付费签名、公证和上架不作为首版前提，见 [T7](t7-public-release-acceptance.md)。 |

客户端范围与显示配置策略见 [T2](t2-platform-capture-input.md#首版平台与客户端范围已确认)；最低系统按依赖与实测确定，见 [T7](t7-public-release-acceptance.md#4-依赖与最低系统的核对顺序)。

### 仍需技术核对的事项

[《云·绝区零》App Store 页面](https://apps.apple.com/cn/app/%E4%BA%91-%E7%BB%9D%E5%8C%BA%E9%9B%B6/id6503421200?platform=mac)列出 macOS 12.0+、Apple M1 或更新芯片。这是游戏的要求，不能当作 youkai 的最低系统，也不能证明截图和输入可用。

MyGo 按平台和架构构建并合并资源，目标为 `windows/amd64` 与 `darwin/arm64`，不需要 macOS 通用包（[分发文档](https://mygo.egoist.dev/docs/distribution#platforms)）。

正式实现可复用[原生模板与项目 CLI](https://mygo.egoist.dev/docs/cli) 及 [UI 测试](https://mygo.egoist.dev/docs/ui/testing)；`ui.NewTester` 可检查选中项与编辑数据的对应关系，真实输入法、GPU 绘制和安装体验仍需在目标系统检查。最脆弱的假设是现有组件以有限定制即可完成连续校对；若需大量自制控件，先收窄交互，仍不满足时再提出路线调整。

### 正式功能的验收范围

| 检查 | 最小输入与通过条件 | 所需条件 |
| --- | --- | --- |
| 校对体验 | 同一组结果、中文字段和裁剪图，完成筛选、键盘切换、放大、修正、保存及重开，连续操作不错改另一项；用 1,000 与 10,000 项合成列表观察滚动和图片内存。 | 固定样例；合成数据不证明真实库存或 OCR 能力。 |
| 使用提示与焦点 | 开始前可见前台要求与停止方式；隐藏在采集输入前完成，进度不抢焦点；收尾后才开放校对。 | 假服务检查界面状态，实际停止在目标系统验收。 |
| 原生构建与初始化 | 就绪回调不阻塞；真实模型与原生库的加载、缺失或损坏有可核查表现。 | 固定 MyGo 版本的 Go 与编译工具链，T3 的组合。 |
| 生命周期 | 用假扫描服务覆盖双启动、慢订阅、旧消息、取消与再次开始、两种关窗模式、偏好记忆、托盘恢复与创建失败、明确退出和保存失败；退出收尾只执行一次。 | 不能据此宣布平台输入或 OCR 能及时停止。 |
| 资源路径 | 在 `mygo dev`、直接测试和安装后，从不同工作目录、中文与空格路径加载模型和原生库；损坏资源给出可处理错误。 | T3 的真实资源，在对应平台验证。 |
| 双平台安装前提 | 在无开发工具的干净环境安装，检查运行前提、原生库与模型路径；按 T7 检查未签名或 ad-hoc 制品的首次打开与权限体验；离线首装另测。 | 与 T2/T3/T7 配合。 |
| 实际关窗与退出 | 三种客户端真正运行截图和 OCR 时，分别取消、隐藏、恢复、退出、再次运行，检查停止输入、识别返回、证据保存和资源释放。 | T2–T5 的实现与双平台实机。 |

## 5. 原生 UI 能力与性能依据

原生 UI 由 Go 描述、框架自行绘制（Windows Direct3D 11、macOS Metal），不为纯原生窗口启动 WebView；组件行为由框架实现，不等同于系统原生控件。[原生 UI](https://mygo.egoist.dev/docs/ui) · [渲染机制](https://mygo.egoist.dev/docs/ui/rendering)

| 能力 | 官方能力及对界面的意义 |
| --- | --- |
| 布局与外观 | 行列、Grid、SplitView、侧栏、标签页；主题支持深浅色和系统文字缩放。[布局](https://mygo.egoist.dev/docs/ui/layout) · [主题](https://mygo.egoist.dev/docs/ui/styling) |
| 结果表格 | 可见行渲染、键盘导航、列调整、排序状态、单元格编辑；数据排序筛选由应用提供。[Table](https://mygo.egoist.dev/docs/ui/table) |
| 字段校对 | 文本与数值输入、补全、撤销重做、剪贴板和输入法组合状态（组合中 Enter 不提交）；本项目的中文输入法尚未验收。[Text input](https://mygo.egoist.dev/docs/ui/text-input) |
| 图片证据 | 位图与 SVG、按比例适配、像素化放大、更新位图；缩放平移与字段标注需应用组合。[Image](https://mygo.egoist.dev/docs/ui/image) |
| 其他 | 菜单、对话框、进度、拖放、快捷键、屏幕阅读器接口等。[组件目录](https://mygo.egoist.dev/docs/ui) · [辅助功能](https://mygo.egoist.dev/docs/ui/accessibility) |

据此，“结果列表 + 图片证据 + 字段表单”不需要先自建控件库。

**上游性能数据（v0.5.1，本项目未复跑，v0.5.5 未重新测量）：** 官方 2026-10-15 的 Windows amd64 基准（Go 1.27.1，六次中位数）中，`counter-native` 示例体积 9.37 MB、启动 6 秒后空闲私有工作集 12.15 MB；`BenchmarkListScroll`（百万变高行、无窗口 CPU 渲染）0.6873 ms/次。[原始数据](https://raw.githubusercontent.com/egoist/mygo/benchmarks/history/2026-10-14.json) · [基准源码](https://github.com/egoist/mygo/blob/408b4def1f3e6a8d3fac852396b4a46d82ba22a3/ui/list_test.go#L1118)。这些示例没有 OCR、模型、截图或游戏并行负载，只说明原生 UI 有较低占用的潜力，不能当作 youkai 的预算。

渲染只在输入、显式失效或动画时发生，空闲窗口不持续重绘；但持续刷新进度、重复解码图片仍会产生开销，缩略图与证据缓存必须有界。`MYGO_FRAME_STATS` 可分解构建、布局、绘制与呈现耗时，用于后续定位。

需要实测证明的是：中文输入法与键盘校对能连续操作且不写错条目；代表性数据加真实裁剪图反复滚动后内存稳定（记录进程总内存与 GPU，而非只看 Go heap）；与游戏和 OCR 同时运行时界面仍能响应。

## 6. 会改变建议的证据

| 证据 | 影响 |
| --- | --- |
| 原生 UI 通过中文输入、连续校对与大量图片验证，定制量小。 | 维持原生路线。 |
| 原生路线需要大量自制控件才能满足已确认的体验。 | 先收窄交互；仍不满足时向用户提出重新评估 Web，比较交互差距、占用与维护代码。 |

轻量是选择动机，但具体内存、体积和响应时间预算尚未约定，在正式验收中取得代表性数据后再定。
