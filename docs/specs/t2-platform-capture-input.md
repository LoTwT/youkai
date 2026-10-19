# T2：平台采集与输入评估

更新日期：2026-10-20。状态：首版两平台、三种客户端范围已由用户确认，Windows 首版显示配置已定（1920×1080，见第 3.2 节）；两平台底座已实现（见 2.1、2.2 实现状态），macOS 云客户端已完成实测轮 ①——采集与输入验证通过（见[M2 macOS 实测轮 ① 报告](../reports/2026-10-18-m2-macos-field-round1.md)）。Windows 实测轮 ② 推迟；完成前不宣称 Windows M3 通过。本文只维护平台专项，公共模块与会话契约以[架构](scanner-architecture.md)为准。

## 1. 已有约束与当前问题

沿用 [T1 已确认的产品选择](t1-mygo-ui-distribution.md#已确认的产品选择)：MyGo 原生 UI；Windows x64 与 Apple Silicon macOS 同时首发；macOS 目标是 App Store《云·绝区零》。扫描时游戏保持前台，扫描器可隐藏到托盘或菜单栏，但不能因此放宽游戏前台、未最小化与未遮挡的要求。

T2 负责目标窗口身份、采集、坐标、短输入、平台故障和原生资源释放。T5 负责视频内的页面/布局、动作效果、导航及库存结束证据；T3 负责识别；架构（T4）负责会话、记账、结果与导出。T2 不决定实体、稀有度或字段范围。

### 首版平台与客户端范围已确认

用户于 2026-10-15 确认以下范围；具体窗口身份和显示配置仍需核对。

| 平台 | 首版目标客户端 |
| --- | --- |
| Windows x64 | 《绝区零》完整客户端，以及安装在本机的官方《云·绝区零》Windows 客户端。 |
| Apple Silicon macOS | 从 App Store 下载并直接打开的《云·绝区零》客户端。 |

以下统一用“完整客户端”和“云客户端”区分两种运行方式；云客户端同样安装在本机。两平台共三种场景均需完成真实扫描闭环验收。

官方事实补充：[《云·绝区零》官网](https://zzz.mihoyo.com/cloud-feat/)的页面脚本提供 Windows 桌面端下载与安装包入口文案；[2025-06-06 上线公告](https://www.taptap.cn/moment/679782441631616889)说明 Windows 与 macOS PC 端已上线。两种 Windows 场景都有本机客户端窗口，差别在于完整游戏由本机运行渲染，还是由云端运行、客户端接收视频并传递操作。youkai 均采集和操作本机窗口；云视频的延迟、画质与断流约束仍需验证。

| 入口 | 可以共用的部分 | 增加或保留的成本 |
| --- | --- | --- |
| Windows《绝区零》完整客户端 | Windows 窗口核验、GDI、SendInput、停止与分发。 | 需验证本地渲染画面可采集、游戏权限与输入可用；与 Mac 云端分别验证布局和输入效果。没有云视频传输这一层，仍不能把截图完成当作页面就绪。 |
| Windows《云·绝区零》客户端 | 同一 Windows 平台能力，不因此新增截图后端。 | 需记录官方客户端版本、实际进程与游戏窗口；核对客户端外壳、视频黑边、画质变化、输入往返延迟、断流/重连。Mac 云端经验可能复用，布局和输入行为不能直接假定一致。 |
| 双入口合计（已确认范围） | 共用 Windows 平台实现与统一的证据/停止语义。 | 增加一套入口身份、页面配置、样本及完整扫描回归；属于双入口支持成本，不是加一个选择按钮。是否能共用 T5 布局须用样本证明。两种入口均须完成真实扫描闭环。 |

## 2. 双平台最小方案与依据

### 2.1 Windows：GDI 客户区裁剪与 SendInput

**建议**先用一套前台桌面采集路径：核定目标窗口及其客户区在桌面的物理像素矩形，从桌面 DC 用 GDI 复制该区域。这里的“窗口采集”是桌面区域裁剪，不能承诺绕过遮挡或最小化。首版不并列维护 GDI、WGC、DXGI。

| 能力 | 官方事实与源码依据 | 最小方案建议及限制 |
| --- | --- | --- |
| 窗口身份 | `EnumWindows` 枚举顶层窗口，`GetWindowThreadProcessId` 给出所属进程。[枚举](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumwindows) · [进程](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getwindowthreadprocessid) | 会话锁定 HWND、进程实例及可执行文件身份，窗口类/标题只作筛选线索；确认是游戏画面而非启动器、登录窗或同名窗口。进程重启或窗口替换需重新预检，不沿用旧 HWND；实际名称与类名留待实机记录。 |
| 前台与可见性 | `SetForegroundWindow` 受系统限制，即使满足文档条件仍可能被拒绝；`IsWindowVisible` 为真也可能完全被遮挡。[前台限制](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setforegroundwindow) · [可见性](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-iswindowvisible) | 在输入前检查实际前台窗口、最小化状态和几何；焦点移交失败保持准备/失败状态，不靠一次激活请求宣告就绪。已知遮挡、模态窗口或不确定的目标点命中均拒绝输入。 |
| 客户区与 DPI | `GetClientRect` 不含窗口边框；`ClientToScreen` 转为设备坐标。PMv2 下应用看到显示器的原始像素。[客户区](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclientrect) · [转换](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-clienttoscreen) · [DPI](https://learn.microsoft.com/en-us/windows/win32/hidpi/high-dpi-desktop-application-development-on-windows) | 与 MyGo 的进程/线程 DPI 配置统一，优先核对 PMv2；客户区两个角转换后形成物理像素矩形。不得在窗口创建后由截图依赖随意修改进程 DPI 模式，或把 UI 逻辑尺寸直接当屏幕像素。 |
| 截图 | Microsoft 示例说明 DC、位图与 `BitBlt` 路径；既有参考的固定 Go 源码从桌面 DC 采集并将 BGRA 转为 RGBA。[GDI 示例](https://learn.microsoft.com/en-us/windows/win32/gdi/capturing-an-image) · [固定源码](https://github.com/kbinani/screenshot/blob/089614a940181bba6e846e21ff436ea652a5b374/windows.go) | 平台输出带尺寸、步长、颜色约定的自有图像与几何版本；正确释放 DC/位图。不照抄示例的缩放显示步骤，不把 GDI 成功、非黑图或函数返回时间当作游戏新帧的保证。 |
| 短输入 | `SendInput` 返回插入系统输入流的事件数量；受 UIPI 限制，错误信息不一定指出 UIPI。它也不会重置已按下的键。[SendInput](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput) | 一个串行执行者派发有界的单次点击、短按或小步滚动；核对完整/部分投递，记录自身按下的键鼠并清理。权限不匹配时停止并说明，不默认提权、不反复盲发；返回成功仍需 T5 验证动作效果。 |

额外两个坐标陷阱必须保留：

- `GetWindowRect` 会受 DPI 虚拟化影响，且可能包含不可见调整边框；它不是游戏内容矩形。`GetDpiForWindow` 的值又取决于目标窗口自身的 DPI awareness，DPI-unaware 窗口会返回 96，不能拿这个数统一乘整张图。[窗口边界](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getwindowrect) · [窗口 DPI](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getdpiforwindow)
- `MOUSEINPUT` 绝对坐标为 0–65535，默认映射主屏，带 `MOUSEEVENTF_VIRTUALDESK` 才映射整个虚拟桌面。平台负责从屏幕物理像素转换、舍入与边界检查，不能直接传图像像素或丢掉负屏幕坐标。相对移动受鼠标速度等设置影响，不作为定位基准。[MOUSEINPUT](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-mouseinput)

**Go 路径建议：**用 `golang.org/x/sys/windows` 的系统 DLL 调用能力接入少量 user32/gdi32 API，固定 x64 结构布局和句柄生命周期；截图参考源码只证明路径可表达，不视为已选依赖或游戏兼容证明。[Go Windows 包](https://pkg.go.dev/golang.org/x/sys/windows#NewLazySystemDLL)

若采用 `SetWinEventHook` 辅助观察前台、移动与销毁，使用进程外通知，回调只更新观测和请求停止，保留所在线程的消息循环；输入前仍重新核验。前台通知不能过滤掉扫描器自身激活，否则可能漏掉用户打开主窗口造成的失焦。窗口通知是异步的，不能消除“核验后、投递前”焦点改变的竞争。[SetWinEventHook](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwineventhook)

**后端重评门槛：**在已确定入口及窄显示配置下，正确的客户区裁剪仍持续黑屏、拿到旧渲染内容，或正式扫描所需频率下采集成本不达标，才取得对照证据并比较 WGC/DXGI。普通遮挡、失焦或云视频停滞首先是前提失效/语义问题，换后端不能证明输入安全或云端已响应。目前没有这类证据，不新增备用后端。

**实现状态（2026-10-18）：** Windows 平台底座已按本节最小方案实现为普通包 `internal/platform/windows`：不依赖 UI、内核、适配器或识别包，也不定义跨平台接口；Win32 调用全部经 `golang.org/x/sys/windows` 系统调用与少量 user32/gdi32/shcore 过程声明，无 cgo，SendInput 结构布局固定 amd64。已实现并有任意平台可运行的纯逻辑测试（API 替身）：窗口枚举与进程身份核验（窗口替换、PID 复用与进程重启均拒绝沿用旧 HWND）、客户区几何与 DPI 核对（进程 PMv2 核验、目标窗口与显示器 DPI 不一致即拒绝，不用任何 DPI 数乘整张图，几何按版本化并拒绝旧帧坐标）、GDI 客户区裁剪（BGRA→RGBA 自有图像类型、DC/位图逆序释放并逐项记账）、SendInput 串行短输入（0–65535 绝对坐标域转换含虚拟桌面映射与舍入、投递计数、自身按键记账与停止时释放、UIPI 形态的零投递后停发不盲发）、输入前重查实际前台/最小化/几何版本/落点遮挡、`RegisterHotKey` 停止热键（专用线程消息循环、注册冲突开始前报告、干净注销）。PR 4a（2026-10-19）又为端口词汇扩展了点击按住时长（两批 SendInput：[move, down]、停顿、[up]）、拖拽（按下→逐步绝对移动→抬起，步间闸门在许可关闭时立即补抬起并标 Aborted）与最后投递时间记账，并由 `internal/platform` 的 Windows 适配器接入统一端口（观测重测几何、故障分类、停止序列，见 2.1 与第 7 节的 fake 覆盖）。未采用可选的 `SetWinEventHook` 通知路径：输入前仍直接重查实际前台，通知的异步性并不能消除“核验后、投递前”的竞争。实机行为——DPI 虚拟化实际表现、前台限制、遮挡、热键真实触发、句柄生命周期与 SendInput 实际计数——按第 7 节留实测轮 ② 验证。

### 2.2 macOS：ScreenCaptureKit 窗口流与 Quartz 输入

**建议**使用 `SCShareableContent` 找到目标 `SCWindow`，以 `SCContentFilter(desktopIndependentWindow:)` 创建单窗口 `SCStream`；只取图像，不采音频或麦克风。输入使用 Quartz 的鼠标、键盘、滚轮事件创建函数及 `CGEventPost`。不通过 AppleScript/System Events，也不依赖游戏暴露完整的辅助功能控件树。

| 能力 | 已核实事实 | 对方案的约束 |
| --- | --- | --- |
| 窗口身份 | `SCWindow` 提供 windowID、frame、owningApplication、isOnScreen；所属应用提供 bundleIdentifier 与 processID。[SCWindow](https://developer.apple.com/documentation/screencapturekit/scwindow) | 锁定 bundle 身份、进程实例与 windowID；名称只作显示。App Store 包装、真正渲染进程、登录窗与游戏窗的关系须实机记录，不预填猜测的 bundle ID。 |
| 前台 | `NSWorkspace.frontmostApplication` 表示接收键事件的应用；SDK 特别说明 Stage Manager 下 `SCWindow.isActive` 可在窗口不在屏幕上时仍为真。[前台应用](https://developer.apple.com/documentation/appkit/nsworkspace/frontmostapplication) · [isActive](https://developer.apple.com/documentation/screencapturekit/scwindow/isactive) | 前台 PID 相符不等于目标游戏窗就绪。检查屏幕内可见性与窗口歧义；必要时读取 AXFocusedWindow 的角色、位置、尺寸，不能仅用应用名称或 isActive 放行。AX 查询失败/超时或无法唯一对应目标时拒绝输入，不用私有窗口 ID API 补洞。 |
| 采集 | 单窗口过滤、流回调与帧附件均有官方示例；帧可携带状态、显示时间、contentRect、contentScale、scaleFactor。[示例](https://developer.apple.com/documentation/screencapturekit/capturing-screen-content-in-macos) · [帧信息](https://developer.apple.com/documentation/screencapturekit/scstreamframeinfo) | 配置明确的小型原生帧队列和有界交接；只把有效状态及图像交给内核。窗口流能取得图像不证明窗口可点击，更不表示云视频内容在前进。 |
| 输入 | `CGEventCreateMouseEvent` 接受全局坐标；键盘和滚轮有独立创建函数，`CGEventPost` 投递到事件流且无游戏确认返回值。SDK 提供 `CGPreflightPostEventAccess` 查询事件合成权限。[Quartz 投递](https://developer.apple.com/documentation/coregraphics/cgevent/post(tap:)) · [预检](https://developer.apple.com/documentation/coregraphics/cgpreflightposteventaccess()) | 按目标就绪与输入许可串行投递短操作。macOS 云客户端已实测（实测轮 ①）：绝对位移、点击与拖拽均有效；**合成滚轮事件被客户端忽略**，遍历列表滚动必须走拖拽（按下→插值移动→抬起，已实测产生列表滚动）。Windows 的落点、按键映射与滚轮行为仍待实测；不把 Windows 的一个滚轮刻度直接当作同样的列表位移。 |
| 录屏授权 | 官方示例要求 Screen Recording 权限；示例说明首次授权后需重启应用才能采集。[示例](https://developer.apple.com/documentation/screencapturekit/capturing-screen-content-in-macos) | 预检录屏权限，并以枚举/启动流和首个有效画面的实际结果判断可用。授权、重启和不同系统版本的提示行为均纳入安装后验收；没有固定“点了允许立即成功”的承诺。 |
| 辅助功能与输入授权 | Apple 工程师说明事件投递需要用户明确授予辅助功能权限；`AXIsProcessTrustedWithOptions` 查询信任，异步提示不改变当前返回值。[Apple 工程师说明](https://developer.apple.com/forums/thread/103992) · [AX 查询](https://developer.apple.com/documentation/applicationservices/1459186-axisprocesstrustedwithoptions) · [用户授权说明](https://support.apple.com/guide/mac-help/mh43185/mac) | 分别检查 AX 信任与事件合成能力；录屏已获准不能代替输入授权。请求必须由明确的用户操作发起，之后重新查询。监听输入与合成输入是不同能力，不先要求 Input Monitoring。 |
| 应用沙箱 | Apple 当前文档将辅助应用使用 Accessibility API 列为 App Sandbox 限制。[App Sandbox 限制](https://developer.apple.com/documentation/security/protecting-user-data-with-app-sandbox) | 当前 AX 路径建议按非 App Sandbox 的普通 DMG 应用核对，签名/公证另交 T1/T7；DMG 格式本身不证明未启用沙箱。游戏来自 App Store 不代表扫描器也必须进入 App Sandbox；签名不代替 TCC 授权。 |

辅助功能查询用于核对窗口，不要求用 AX 控件执行游戏操作。若目标不暴露足够窗口属性，需要在“唯一目标窗口 + 前台进程 + 屏幕窗口列表/几何”的候选判定上补实机证据；无法排除登录弹窗、同进程其他窗口或遮挡时，停止或拒绝开始，不声明所有 App Store 云应用都兼容。

**Go 路径建议：**用薄层 Objective-C/C ABI 桥接 ScreenCaptureKit、CoreGraphics/ApplicationServices；Go 持有会话控制和自有图像，原生层持有 SCStream、delegate、dispatch queue 与 Core Foundation 对象。优先显式复制所需图像数据后交给 Go，回调不等待 OCR、保存或 UI；不为减少一次复制先引入跨语言零拷贝生命周期。若需保留原生 buffer，必须有明确 retain/release 和上限。C 不长期保存未固定的 Go 指针，使用句柄关联回调；Go context 不会自动停止原生流。[cgo 规则](https://pkg.go.dev/cmd/cgo)

AppKit 相关操作交由应用已有的主线程调度，帧回调用单独串行队列；停止流使用异步完成回调，不能在主线程等扫描 goroutine、也不能持服务锁同步等待主线程。原生停止完成且在途回调处理妥当后才释放资源；旧流/旧会话回调必须能被拒绝。不增加独立 Swift 辅助应用或另一套 UI。

**macOS 实现状态（2026-10-18）：**本节方案的最小实现已落地在 `internal/platform/macos`（M2 平台底座 PR，未接入扫描内核/适配器/UI）：SCShareableContent 枚举与 bundle+PID+windowID 身份锁定（同名/替换窗口判歧义）、desktopIndependentWindow 单窗口流（仅图像、BGRA、帧回调独立串行队列并在回调内显式复制像素为 Go 自有图像、有界最新帧缓存、异步停止并区分“已请求/原生已停/回调已排空/已释放”、迟到回调计数拒绝）、SCFrameStatus 与 displayTime 的 mach→Go 单调时间锚定转换、AX 信任（不弹授权）与事件合成预检及基于实际枚举/启动流结果的录屏可用性判定、Quartz 短输入串行派发（未派发/部分/已投递三态如实表达 CGEventPost 无确认返回值、自身按下的键鼠单独记账并在停止时尝试释放且失败如实记录）、前台 PID + 屏幕内可见性 + AXFocusedWindow（有限超时、失败即拒绝）的就绪核验、点/像素与 contentRect/contentScale/scaleFactor 的单位建模及几何版本化（旧版本坐标拒绝输入）、Carbon RegisterEventHotKey 全局停止热键（在主线程注册、经宿主应用主 run loop 派发、无 run loop 时限期失败而非挂起、注册失败上报）。这些主队列 dispatch 共享在途标记：上一次入队任务未确认执行前新调用立即失败（不排队、不等待超时），迟到任务最终执行后标记经恢复探针自动清除，主线程停摆期间不再逐次堆积主队列任务（实测轮 ① 报告 §12 的建议已实现）。几何/缓存/竞争/权限映射/热键生命周期的纯逻辑有单元测试并随双平台 CI 运行；需要真实原生环境的路径（实际 SCStream 出帧与附件、CGEvent 送达游戏、AX 实窗查询、热键触发且不抢焦点、权限授予/撤回行为）不写假装通过的测试，留实测轮 ① 验证。该包自包含、不定义跨平台共享接口；Windows 侧由并行 PR 实现，统一集成接口由后续 PR 裁定。

**macOS 端口接入（PR 4a，2026-10-19）：**本节方案已由 `internal/platform` 的 macOS 适配器接入统一端口：每会话按 bundle、layer 0、onScreen、最小候选尺寸 320×240 点、完全落在某显示器内过滤候选并要求恰好一个（为零报 targetNotFound、多于一个报 targetAmbiguous 并记录候选清单；后续观测只有通过过滤的同进程窗口才算歧义来源，目标自身不参与过滤）。最小尺寸用于排除云客户端在游戏前台时出现的瞬时状态角标（实测轮 ② 前预检遇到 66×20 的同进程小窗，旧谓词把它当成第二候选而报 targetAmbiguous）；320×240 远低于任何可用游戏窗口、远高于此类角标。用户点击开始后，控制器调用平台的可选 `Activator` 将已锁定的目标应用切到前台一次；该能力只服务用户发起的准备，扫描期间失焦仍是停止条件，且每次输入仍逐次复核前台。AX 恒不查询（本机系统级焦点查询不可用，前台证据用 `NSWorkspace.frontmostApplication`，完全可见分数设为 1）；录屏按枚举（其他进程窗口）+流启动+首个完整帧的实际证据判定，枚举无其他进程窗口即快速失败；帧只返回几何有效者（BGRA→RGBA 新分配转换、Content=有效区、After 支持新帧与 idle 复用 Reused）；输入派发带按住时长与拖拽（实测滚轮被忽略，遍历滚动走拖拽），派发前在平台串行段内重查闸门、几何、身份、前台、在屏内、落点在内容区与事件合成权限；观测线程每 250ms 重查观测与两项输入权限，每个故障码只上报一次；Close 按停观测→关派发（释放自身按键）→停流排空→注销热键→dwell（约 50ms，见第 5 节）执行。两个适配器与 fake（`internal/platform/platformfake`）都在两平台 CI 可运行，fake 覆盖项见第 7 节。

**macOS 实测轮 ① 结论（2026-10-18）：**平台底座已在真实云客户端（macOS 26）上验证，采集与输入所需的平台能力全部通过，完整证据见[实测轮 ① 报告](../reports/2026-10-18-m2-macos-field-round1.md)。对本文有约束力的实测事实：窗口位置每次启动漂移（级联+用户拖动），窗口身份须每次会话按 bundle + layer 0 + onScreen + 完整在屏内重新解析，过滤后必须唯一；rect 附件（contentRect/screenRect）在 macOS 26 以 NSDictionary{X,Y,Width,Height}（NSNumber）到达而非 Apple 示例暗示的 NSValue(CGRect)，平台已兼容两种形态、未知形态按无附件处理并记录；`SCWindow.isOnScreen` 须观测进程初始化 AppKit 后才有意义（未初始化时全 false 为伪象），`isActive` 多应用可同时为真、仅作观测不作前台依据；AX 系统级焦点窗口查询在本机恒返 AXError -25205（辅助功能已信任仍如此），平台的可选输入、失败即拒绝（fail-closed）设计维持，前台证据用 `NSWorkspace.frontmostApplication`；SCStream 在未初始化 AppKit 的进程创建会触发 CGS_REQUIRE_INIT 断言（真实 NSApplication 应用不受影响，开发工具须先 NSApplicationLoad）；合成滚轮被云客户端忽略，遍历列表滚动必须走拖拽（点击默认 ≥50–80ms 按下→抬起间隔，kCGMouseEventClickState 未设置、集成时考虑）；云客户端长时间空闲自动退出，空闲等待必须有界；退出路径的 dwell 要求见第 5 节。全局停止热键维持已定方案：仅扫描期间注册、可自定义，实测注册/注销全程不抢焦点。

### 2.3 API 可用版本不等于产品下限

下表来自 Apple DocC 可用性与已安装 SDK 的声明；只限定使用相应 API 的条件。

| 能力 | macOS 起始版本 | 对候选的影响 |
| --- | --- | --- |
| SCStream、SCWindow、单窗口 filter、基本帧附件 | 12.3 | 基本流方案的框架边界，不是 youkai 已支持 12.3。 |
| SCStreamFrameInfo.screenRect、SCWindow.isActive | 13.1 | 选择较低部署目标时不能无条件调用；isActive 仍非前台证明。 |
| SCContentFilter.contentRect / pointPixelScale、SCScreenshotManager | 14.0 | 14+ 可简化初始点/像素核对；首版仍只建议一套 SCStream，不额外维护单帧后端。 |
| 当前 Apple 截图样例 | macOS 15、Xcode 16 | 是样例要求，不能当作框架或产品最低版本。 |

出处：[SCStream](https://developer.apple.com/documentation/screencapturekit/scstream)、[screenRect](https://developer.apple.com/documentation/screencapturekit/scstreamframeinfo/screenrect)、[pointPixelScale](https://developer.apple.com/documentation/screencapturekit/sccontentfilter/pointpixelscale)、[SCScreenshotManager](https://developer.apple.com/documentation/screencapturekit/scscreenshotmanager)。事件权限预检自 macOS 10.15 可用，基础事件投递更早；它们不能据此证明当前游戏兼容。

技术简化候选是 **macOS 14+ 使用一套流路径**。Go 1.27 要求 macOS 13+，选定 ORT 1.29.0 arm64 制品的最低系统标记为 14.0，覆盖 12.3–13.x 会改变工具链或运行库候选；系统候选由 [T7](t7-public-release-acceptance.md#最低系统候选与证据)统一维护。游戏的 macOS 12+/M1+ 条件仍只引用 [T1 的 App Store 核查](t1-mygo-ui-distribution.md#仍需技术核对的事项)。Windows 的 PMv2 自 Windows 10 1703 可用，同样不能推出产品最低 Windows 版本。

## 3. 图像、窗口与输入坐标

### 3.1 每层坐标必须带单位与来源

| 层 | Windows | macOS |
| --- | --- | --- |
| 采集图像 | GDI 输出位图的物理图像像素、宽高、行步长和颜色通道；不随校对 UI 的缩放改变。 | CVPixelBuffer 的输出像素尺寸与行步长；可能被 SCStream 配置缩放或留边，不保证等于窗口 backing 像素。 |
| 窗口/显示几何 | DPI awareness 已明确的客户区与屏幕物理像素；保留显示器、虚拟桌面原点及 DPI 观测。 | 窗口/AX 几何以屏幕点表达；SCStream 附件提供图像与内容之间的缩放信息，不能使用扫描器自身窗口的 Retina 倍率代替目标。 |
| 游戏视频区域 | 完整客户端也需确认游戏渲染区域是否等于客户区；云入口另有客户端外壳和视频区域。 | App Store 云客户端窗口、输出图像、窗口内容区及视频有效区域是不同矩形。标题栏、黑边、工具栏和视频内黑边须分别确认。 |
| 最终输入 | 屏幕物理像素转换到 SendInput 的绝对坐标域。 | 转到 Quartz 全局显示坐标；与左上原点的屏幕点几何对齐，避免混入 AppKit 的其他坐标原点。Retina 图像像素不得直接作为点击坐标。 |

已读 SDK 对 `SCStreamFrameInfoContentRect` 的注释是“surface 内以点表示的内容位置/尺寸”，`scaleFactor` 是像素/点比例，`contentScale` 是原始内容到 surface 的缩放；它们不是三个可随意互换的比例。配置的 `sourceRect` 是逻辑点，`destinationRect` 是输出像素。应按所用 API 的单位建立变换，再以标记点与图像验证，不能仅看到 `CGRect` 就当像素。[Apple 帧几何讲解](https://developer.apple.com/videos/play/wwdc2022/10155/)

建议把“图像有效矩形 ↔ 对应屏幕矩形”的转换与采集几何绑定：Windows 在无额外缩放时为物理像素平移；macOS 根据窗口位置、帧内容范围和缩放转换到屏幕点。只有已经证明两矩形表示同一内容时，才可用对应边界构造比例映射。裁剪/放大后的 OCR 图需要先逆变换回源帧像素，不能直接投递其坐标。转换结果还须落在已验证的视频/操作区域内部。

平台负责窗口与图像几何，T5 以画面锚点确认视频区域及游戏布局，并把拟操作目标绑定到源帧及几何版本。窗口移动/缩放、DPI、显示器、SCStream 输出配置或视频区域改变后，旧坐标失效。首版建议结束本次采集并重新预检，不在仍派发输入时悄悄改比例。

**云视频额外未知量：**编码分辨率可能小于窗口输出图像，客户端可能把它放大并添加黑边；即使窗口尺寸不变，画质/比例或播放器覆盖层也可能变化。ScreenCaptureKit 的 contentRect 描述系统采集内容，不能直接当作内部云视频矩形。T5 需验证布局锚点、视频区域和文字有效像素；固定坐标比例或强行缩成 1920×1080 不能替代该证据。无法从公开 UI 得到编码分辨率时记录“未知”，以可观测区域与样本为依据。

### 3.2 可验收的窄显示配置候选

以下是正式实现后用来建立首批支持记录的候选，当前没有任何一行通过验收。

**已知信息：** 用户已定案（2026-10-18）：Windows 首版认证配置为 1920×1080（窗口/无边框模式），2560×1440 留后续版本。Mac 首组验证设备为 14 英寸 MacBook Pro（M1 Pro），尺寸尚不能确认。机型不能推出截图尺寸、窗口点尺寸、视频分辨率或缩放倍率。单屏、SDR、100% Windows 缩放与无边框形态仍是实测核验项，不假定成立。用户拥有 Windows 与 Mac 设备并参与测试，会在测试前准备设备和客户端；具体参数在实测准备时补录到本节，不作为当前待答项。

**显示配置策略（Windows 定案 2026-10-18；其余沿用 2026-10-16 确认）：** 首版 Windows 认证 1920×1080（窗口/无边框模式），不是与 2560×1440 两档并列达标；2560×1440 与游戏内置尺寸留后续版本逐组扩展，不承诺任意尺寸自动适配。Mac 首版合理支持一个常用尺寸。两种 Windows 客户端分别验收。第 3.2 节原有的实测核验要求保留：游戏设置项、系统缩放 100% 与客户区像素实测记录都须逐项测定填入验收记录，不以候选数值假定三者一致。

Mac 先在现有设备上检查客户端提供的显示或窗口选项，选取可复现、能容纳完整仓库与详情、文字可辨且便于其他用户设置的一组；没有内置选项时固定可复现的窗口几何。现有材料没有尺寸使用率数据，不宣称某个数值最常用。确定后记录设置步骤、窗口点尺寸、输出像素和视频区域；新增尺寸时逐组补齐验收，不按比例换算宣布兼容。

| 场景 | 第一组候选 | 必须填入验收记录的参数 |
| --- | --- | --- |
| Windows 完整客户端 | 1920×1080（首版认证配置，窗口/无边框模式）；单主屏、SDR、100% 缩放待实测核验，排除独占全屏与跨屏。 | 分别记录桌面尺寸、游戏显示设置和实测客户区，不假定三者相同。另记 Windows/游戏版本、进程/窗口身份、DPI awareness、捕获尺寸/颜色、内容区域及点击落点。游戏无法配置成候选形态即修订，不宣称适配。 |
| Windows 云客户端 | 在同一 1920×1080 配置下验证一种可复现形态；单屏、SDR、100% 缩放及窗口模式待实测。使用官方客户端，记录实际窗口尺寸。 | 另记录客户端外壳、视频矩形、黑边、可见画质设置；桌面、窗口和云视频尺寸分别测定，编码尺寸不知道就记未知，不能由候选数值推断。 |
| Apple Silicon macOS 云客户端 | 首版只选一组合理且具有代表性的尺寸，具体数值待测。优先用现有 Mac 的内建屏、默认缩放和普通窗口；首组排除全屏 Space、Stage Manager、跨屏和扫描中改尺寸。 | macOS/应用版本、bundle/PID/windowID、屏幕点尺寸、目标窗口点矩形、输出像素尺寸、逐帧 scaleFactor/contentScale/contentRect、有效视频矩形及安全点击落点，并记录复现该尺寸的设置步骤。不预设 2 倍或 1080p。 |

每个候选均按已确认的简体中文游戏界面验收，并记录界面缩放及云画质选项（若存在），交 T5/T3 核对其支持边界。窄范围不等于隐藏异常：留待后续版本的 2560×1440、125%/150% DPI、另一个 macOS 显示缩放、负坐标副屏、拖动/跨屏、全屏切换等即使暂不支持，也要验证能拒绝开始或停止，不能错点。后续逐步覆盖游戏内置尺寸是用户认可的方向；具体新增支持记录仍须取得对应客户端的证据。

## 4. 帧时序、短输入与交给内核的故障

### 4.1 区分采集新鲜度与游戏就绪

- **采集调用/到达时间：**GDI 可记录调用开始/完成，SCStream 可记录回调到达；这只能说明本机何时取得数据。GDI 没有在这条路径上提供目标游戏的呈现序号，不伪造“已产生新画面”标记。
- **系统帧时间/状态：**ScreenCaptureKit 的 displayTime 是 WindowServer 显示该帧的 mach 时间；应注明时钟域并转换到可比较的单调时间，不能直接与 Go 墙钟比较。complete 表示系统生成新帧；idle 表示显示内容未改变，blank/suspended/stopped 各有不同含义。[帧状态](https://developer.apple.com/documentation/screencapturekit/scframestatus)
- **动作效果：**系统新到达帧、新时间戳甚至图像变化，都不能证明云端已处理点击。客户端光标、状态提示或动画可能继续变化而视频尚未更新。页面、选中标记、详情对应关系及动作后就绪由 T5 证明，不能用固定睡眠代替。

图像重复可由静态页面、相同内容、采集队列或云视频冻结造成；idle、相同哈希或一段时间未变化，均不能直接宣布断流或库存末页。反过来，回调持续到达也不证明云连接正常。平台可报告“没有新的可用采集证据/超过采集等待期限”，T5 可报告“动作效果未证实/连接页面”，原因要按实际证据保留。

一次动作返回“已投递但效果未知”后，超时不授权重复点击或滚动；迟到的首次动作可能在重试后才生效。重试及结束策略交 T5/内核处理，平台不自行重放。

### 4.2 交给内核的数据语义

以下是平台专项提出的数据语义与后端限制，不是 Go 接口、全局状态机或最终导出契约。共享部分统一见[平台观测与内核的交接](scanner-architecture.md#平台观测与内核的交接)；本表用于保留平台细节及依据。

| 交接对象 | 需要表达的输入/输出语义 |
| --- | --- |
| 目标观测 | 目标进程实例/窗口标识、观测时间、前台/可见性/最小化状态、已知遮挡或歧义、权限检查结果、屏幕与窗口几何及版本。标题不作为身份保证。 |
| 图像观测 | 图像像素尺寸、步长/格式/颜色约定、有效图像区域、目标和几何版本、流实例、采集请求/到达时间，以及后端实际提供的状态与帧时间。没有源视频时间就明确未知。 |
| 短输入请求 | 预期会话/目标/几何版本、对应源帧、有限的一次操作与落点；平台在真正派发前重查许可及窗口条件。平台不解释“下一页”或字段。 |
| 输入结果 | 区分未派发、全部/部分事件进入系统、投递结果未知和必要释放的结果；不输出“物品选择成功”。CGEventPost 无确认返回值的限制应原样表达。 |
| 故障或不可用 | 原因类别、原始平台错误（如有）、发生阶段、观测时间和最后有效几何/帧标识；通知内核关闭输入许可、停止采集并统一收尾。平台不另开恢复会话。 |
| 资源收尾 | 请求停止与原生流已停止/回调排空分别报告；图像、句柄、事件对象按拥有者释放，旧回调不再写入新会话。慢 UI、OCR 或保存不得阻塞故障通知。 |

采集后端缓存与 OCR 任务队列须区分：连续流可有有界的“最新可用帧”缓存，但内核已接纳为证据的采集任务必须保留其序号、图像和终结记账，不能被后来的相似帧覆盖。帧等待需要后端状态、截止时间和取消入口；静态 idle 不是即时故障，超时也只报告证据不足，不臆测网络原因。

### 4.3 平台条件失效后的处理

| 观测或事件 | 平台/适配器上报 | 给内核的处理建议 |
| --- | --- | --- |
| 进程退出、窗口销毁/替换、身份不一致 | 目标失效及最后一次目标观测。 | 关闭新输入许可，结束本次采集并收尾；不能按同名新窗口继续。 |
| 失焦、最小化、移出支持显示范围、已知遮挡 | 前提不成立；焦点返回只是一条新观测。 | 一旦触发停止，不因随后隐藏 UI、游戏重获焦点或遮挡消失自动重开许可。 |
| 窗口移动/尺寸/DPI/显示器/图像变换变化 | 几何版本失效，拒绝旧帧坐标输入。视频内部布局变化由 T5 同样上报。 | 停止本次采集，重新开始前重新测量与预检，不复用旧比例。 |
| macOS 授权撤回，或窗口/输入 API 明确失败 | 分别报告录屏、AX、事件合成的预检与实际失败；Windows 保留投递计数及权限诊断。 | 关闭许可并收尾；重新授权后的新预检不恢复当前库存遍历。无错误码不能反推“权限正常”。 |
| 空 buffer、尺寸/附件无效、SC blank/suspended/stopped 或流错误 | 后端不可用或无有效图像，保留具体状态；全黑像素本身只是一条质量线索。 | 不把无效帧交作新证据；明确停止/撤权立即走停止入口，暂时无帧在有界等待后报告采集失败。 |
| 长时间无有效采集证据，或读取缓存旧帧 | 超过采集等待期限/帧来源不满足当前请求，不宣布云断流。 | 停止派发依赖该帧的新输入；期限用实机数据定，不由 T2 虚设硬时限。 |
| 云排队、掉线/重连界面，或动作效果到期仍不确定 | T5 的页面/连接证据及动作未证实；OS 流可能仍正常。 | 停止本次采集并按架构语义保存部分结果；重连成功不意味着原位置与库存状态可续扫。 |

`SCStreamDelegate` 的停止错误回调可用于明确流故障，包括用户主动停止共享的情形；回调仍不能描述云游戏内部网络状态。[停止回调](https://developer.apple.com/documentation/screencapturekit/scstreamdelegate/stream(_:didstopwitherror:))

**停止能力的实际边界：**许可关闭要与短操作派发入口同步，防止已排队的下一次操作绕过取消；不能把一次批量调用塞成整段库存遍历。已进入系统的事件无法撤回，核验到投递之间的焦点竞争无法由普通用户态 API 原子消除。只尝试释放扫描器自身记录的按键/鼠标，不批量松开用户的键，也不为清理而强抢游戏焦点；撤权时释放也可能失败，必须如实记录。Go context 不能中断任意 GDI、AX 或其他原生调用；AX 查询应使用有限消息超时。T2 不另给“零误投”“200 ms 硬停止”或“5 s 必定退出”的保证，时限按测量统一。

## 5. 隐藏、恢复与退出契约的平台约束

服务拥有会话、单会话占用、快照与修订通知、有界队列和异步收尾均由[架构生命周期](scanner-architecture.md#生命周期)维护。以下只核对这些契约与平台行为能否一致，不另定义状态名称。

| 契约行为 | 必须满足的平台条件与反例 |
| --- | --- |
| 开始并隐藏，在派发输入前完成 | 权限与使用提示、托盘创建、隐藏及焦点移交先完成；实际游戏前台、目标与几何就绪才允许进入正式采集。Windows 激活请求可能被系统拒绝，macOS 前台应用相符也可能仍在登录弹窗。可在准备阶段取预检帧，不代表已开始库存采集。 |
| 隐藏本身不中断，进度不激活窗口 | Hide/托盘更新不能触发主窗激活、自动弹进度窗或关闭必要消息循环。窗口流能继续出图不允许忽略游戏失焦/遮挡；Windows 的前台和可见标记同样不证明没有置顶覆盖层。 |
| 用户恢复可交互主窗，先关许可再显示，停止本次采集并收尾 | 主窗显示/激活必须排在关闭许可之后。此时仍可异步显示收尾状态；不能把“已请求停止”显示成全部资源已释放。没有自动续扫。 |
| 用户先点托盘/关闭按钮已经导致失焦 | OS 可能在菜单回调或 OnClose 执行前就改变焦点；此时按已有失焦走停止。再次 Hide 不能把正在收尾的会话救回，关窗偏好不覆盖已经发生的停止原因。 |
| 明确退出/⌘Q 优先于关窗偏好 | 先关闭许可，再统一异步停止/保存；回调快速返回，不阻塞 UI 主线程等采集/OCR。正常退出可等待清理，进程崩溃、强制退出与系统关机不是同等级可保证的路径。**退出路径集成要求（实测轮 ①）**：最后一次释放事件后停留 ~50ms 或做一次同步 WindowServer 调用再退出——`CGEventPost` 无确认语义，进程立即退出可能丢失在途释放事件（键/鼠标卡在按下态；驱动侧复现，见报告）。**dwell 已实现（PR 4a，2026-10-19）**：macOS 适配器的 Session.Close 在注销热键前按 `max(0, 50ms − 距最后事件投递的时间)` 停留，整个会话未投递过事件则不停留；内核的控制器释放（scan.Releaser）保证 Close 在会话封存与进程退出路径（Quit 等 wrapDone）之前完成，因此进程不会抢在 dwell 前退出。Windows 无此需求（SendInput 为同步插入），不停留。 |

“前台”还不等于“无遮挡”：Windows 的 `WindowFromPoint` 可辅助检查落点所属窗口，但它忽略隐藏/禁用窗口，并非全图无遮挡证明；macOS 的独立窗口流可能绕过其他窗口的遮挡。建议结合窗口叠放/相交检查与 T5 的当前页面证据，发现未知遮挡时拒绝输入，并实测置顶悬浮窗/系统提示等反例。[点命中限制](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-windowfrompoint)

停止入口必须在隐藏后可用。Windows 可用 `RegisterHotKey`，注册失败/冲突要在开始前处理，所在线程持续处理 WM_HOTKEY；macOS 要验证所选原生快捷键确实为后台全局注册，不能把 MyGo 窗口内按键或菜单快捷键当全局停止。若方案使用事件监听，另核对是否增加 Input Monitoring 权限。快捷键、菜单“停止”和退出均交同一停止入口，不为显示停止界面先抢焦点。[Windows 全局快捷键](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerhotkey)

## 6. 主要成本与可推翻假设的证据

| 假设/成本项 | 当前建议 | 可推翻或迫使调整的证据 |
| --- | --- | --- |
| Windows GDI 在窄配置可得到正确画面 | 一套 GDI 路径，成本主要是身份、DPI、失焦、遮挡和句柄清理。 | 同一目标可见画面在正确裁剪下仍黑屏、持续旧帧，或采集开销不满足扫描节奏；再对照 WGC/DXGI，不先维护全部后端。 |
| macOS 云应用能采集且接受短输入 | 一套 SCStream + Quartz；原生桥接、授权、几何、回调与安装身份是独立成本。 | 已获所需授权、窗口确认正确却采集缺失/受保护，或人工鼠标有效而事件无效。不能靠换 OCR 解决；须报告该首发场景受阻并重新评估。 |
| Mac 点/像素映射和视频区域稳定 | 先接受一组有记录的配置，不照搬 Windows。 | 窗口固定时视频范围/缩放仍动态变化，或窗口有效范围与输入对应关系无法可靠确定；需 T5 校准/拒绝机制或调整支持配置。 |
| 云帧可证实动作效果 | 平台提供时序与状态，T5 验证语义。 | 新到达帧持续显示动作前状态、画面自变但选择不变、重连回到不同页面；按未证实动作/部分结果处理，重估云适配与回归。 |
| 单进程可完成正常停止和资源释放 | 短调用、异步停止、有限查询等待、有界队列。 | 原生调用可复现长时间阻塞、崩溃或泄漏；先定位，再判断是否需要隔离，不能预先承诺硬退出时限。 |
| Windows 云端可直接复用 Mac 适配 | 只预期共用页面语义与故障类别；不预期共用所有坐标。 | 客户端比例、输入映射、画质或仓库布局不一致；保留独立配置和实机回归。 |
| 最低系统候选覆盖所需能力 | macOS 14+ 是简化候选；最终与 MyGo/OCR/分发要求取交集。 | 必需依赖要求更高系统、当前系统版本行为不同，或目标用户必须覆盖更低版本；由 T1/T2/T3/T7 共同核对。 |

双平台首发成本不能只在 Windows 估算上加“Mac 开窗”：macOS 原生采集/输入、授权与发行身份、点/像素映射、云视频语义、实际扫描闭环和干净机器回归都是独立工作。没有双平台测试条件时，只能报告资料评估完成，不能报告首发支持完成。

## 7. 后续验收清单

以下随正式功能完成。macOS 侧部分条目已由实测轮 ①（2026-10-18，见[M2 macOS 实测轮 ① 报告](../reports/2026-10-18-m2-macos-field-round1.md)）覆盖并逐项标注；PR 4a（2026-10-19）为两平台适配器补齐了任意平台可运行的 fake 驱动测试（macOS：候选解析与回滚、Preflight、帧转换与 Reuse、派发拒绝路径与按住/拖拽/中止、实测几何换算、观测线程各码一次、Close 顺序/幂等/dwell/超时；Windows：解析与 DPI、几何变化拒绝、派发拒绝路径、两批与拖拽分批、UIPI 粘滞、热键触发与 Close；collect 与 app 另有 `platformfake` 端到端）。fake 覆盖纯逻辑与接线，不能证明原生行为；其余尚未运行。

### 不运行游戏的检查

| 检查 | 可验收的证据 |
| --- | --- |
| 身份/几何纯逻辑 | 构造窗口替换、PID/句柄复用、同名多窗、负坐标、客户区边框、DPI 变化、点/像素非 1:1、capture 留边、视频黑边与裁剪逆变换；无有效几何时不产生输入请求，旧版本拒绝。往返坐标误差和边界规则明确记录。 |
| 输入许可与异步竞争 | 取消与下一次派发竞争、已排队动作、输入部分投递、回调晚到、权限变为未知、恢复主窗先于原生停止完成等序列；许可关闭后没有新普通操作被接受，释放操作单独记账。 |
| 帧与云时序回放 | 合成/授权录制资料覆盖 idle、相同图、空白、乱序/迟到帧、客户端动画而视频冻结、点击后旧详情、掉线/重连与相邻同属性物品；不从哈希重复推断断流或末页。页面期望由 T5 提供。 |
| 队列与资源 | 原生帧缓存、已接纳证据任务与慢 UI/OCR 的界限；满队列、停止及晚到数据都有归属，不泄漏或覆盖已接受证据。原生资源生命周期须在对应平台验证，纯逻辑检查不能代替。 |
| 正式构建静态检查 | Windows 的 DPI 声明、amd64 API 布局和消息循环；macOS 的 deployment target、API availability、框架链接及 App Sandbox 配置。读取源码或构建成功不能证明游戏兼容。 |
| 应用集成 | 开始并隐藏、隐藏不激活、恢复先关许可、失焦后再 Hide 不续扫、明确退出优先、保存失败与重复停止；验证同一个服务会话和停止入口，不另维护 UI 状态真相。 |

### Windows x64 实机

- 对完整客户端与云客户端分别记录第 3 节参数，锁定实际进程/窗口；启动器、同名窗口、游戏重启、登录弹窗不能误选。
- 人工选择安全仓库目标，取得正确帧；核对客户区角点、内容区与输入落点，单击/短按/滚轮产生对应可辨认变化。GDI 成功或 SendInput 计数正确不单独算通过。
- 普通/较高权限游戏、输入部分失败、置顶遮挡、窗口移动、失焦/快速切回、最小化、DPI 改变、副屏负坐标、锁屏/休眠后恢复：分别验证拒绝或停止及原因，不自动恢复遍历。
- 实测开始并隐藏、进度、托盘菜单、打开主窗、两种关窗与退出；记录故障观测、许可关闭、最后一次派发及原生结束的时间。验证隐藏后全局停止键与消息循环仍有效。
- Windows 云客户端必须加做下方云端时序/断流检查；完整客户端与云客户端分别完成完整扫描及安装后的回归，不能仅跑其中一种。

### Apple Silicon macOS 实机

- 使用用户确认的 App Store 云绝区零应用，记录应用/系统版本和真实窗口身份；从目标窗口得到可核验图像，实测点、输出像素、有效视频区域和一次安全操作的对应关系。（实测轮 ① 已覆盖前半：应用/系统版本、窗口身份、~56fps 可核验图像、点/像素/输出几何（surface/scaleFactor/contentScale/contentRect 自洽，右侧黑边与顶部标题条已量出）与安全点击/拖拽的对应已记录；视频有效区域与布局的精确测绘归实施清单 M2 第 1 项。）
- 分别验证录屏/辅助功能拒绝、授予、撤回、应用重启后的行为；检查事件合成预检与真实投递效果。以最终 app bundle 及安装方式验收，不能把终端/开发启动进程获得的权限当发行版已获准。（未覆盖：本轮三项权限已就位（TCC 归属宿主应用），首次授权弹窗流程未观察到；事件合成预检与真实投递效果已实测通过。权限授予/拒绝/撤回与重启行为留安装版验收。）
- 覆盖普通窗口、选定显示缩放及其不支持变体、窗口拖动、Space/Stage Manager 切换、遮挡、同应用弹窗、最小化、锁屏/休眠；继续出图也不能掩盖输入前提失效。（未覆盖：本轮仅普通窗口形态；窗口位置漂移已记录并要求每会话重新解析。其余变体留后续实测。）
- 核对原生全局停止入口、菜单栏菜单、主窗恢复、关窗和 ⌘Q；停止期间继续显示收尾状态，采集流回调结束后释放资源，连续启停无累积原生资源增长。（部分覆盖：全局热键 ⌃⌥⌘S 实测触发、注销干净、不抢焦点；停止→排空→释放完整，8 轮采集（跨进程）均如此。菜单栏/主窗恢复/关窗/⌘Q、收尾状态显示与同进程连续启停的资源核查属应用集成（M2/M3），退出路径 dwell 要求见第 5 节。）
- **云端必验：**静态页面、低帧率、视频延迟、客户端仍动画但游戏图像不变、断流/排队/重连、点击晚生效、视频区域或画质变化。已投递动作未证实就不继续导航，重连不自动续扫。（未覆盖：本轮仅正常网络与正常帧率；另有运维事实——云客户端长时间空闲自动退出，空闲等待必须有界。）
- 与 Windows 一样，用人工核对的固定库存完成真实扫描、证据保存、校对及架构规定的结果交付，核对逐项出现次数与导航证据；只有 UI 可打开、能截一帧或能点一次均不算闭环完成。实体与字段采用架构第 6 节的范围。（部分覆盖：2026-10-19 样本采集轮已完成自动化采样 131 件不重复（120 视口 + 10 名称 + 1 补采）、9 个中文名复核与五类管理状态覆盖，精选 27 件入库识别测试数据（见[M2 样本采集轮报告](../reports/2026-10-19-m2-macos-sample-round.md)）；扫描闭环属 M2 后续。）

双平台验收记录至少包含支持配置、应用与依赖版本、截图与坐标证据、输入与停止日志、失败原因、原生资源与进程总内存观测。稳定性与耗时预算由架构统一。

## 8. 待实测事项

实际目标进程与窗口标识、Windows 云客户端版本与窗口形态、两平台画面可捕获性、目标是否接受事件、权限撤回时效、坐标与视频区域、原生桥接构建、全局停止键、云延迟与断流，均待正式功能实测。Apple DocC 页面以官方 DocC JSON 读取，部分声明另由本机 `MacOSX26.5.sdk` 头文件核对；SDK 版本不是产品最低系统。
