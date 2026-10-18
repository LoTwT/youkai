# M2 macOS 实测轮 ① 报告：云客户端采集与输入

更新日期：2026-10-18。本文记录 M2 平台能力在 macOS 云客户端上的实测轮 ①，对应[实施清单 M2](../specs/scanner-implementation-plan.md#m2三种客户端的单件完整流程)前两项的 Mac 侧平台验证与 [T2](../specs/t2-platform-capture-input.md#7-后续验收清单) macOS 实机清单的部分条目。平台底座（`internal/platform/macos`，PR #26）尚未接入扫描内核，本轮由开发侧以 tools/fieldprobe 驱动工具（本 PR 提交）驱动真实原生层完成；游戏内状态变化由用户目击确认，关键最终态另以采集帧核对。输入共两轮：第一轮失败，两个根因均在驱动侧查明并修复（顾问会话 Sunna 交叉复核，实验在 /tmp/sunna/，不进仓库），第一轮热键结果作废，修复后全部重测。采集与输入所需的平台能力全部验证通过；合成滚轮被客户端忽略，列表滚动改走已验证的拖拽路径。未覆盖项与不能推出的结论见文末。

## 制品与环境

| 项 | 记录 |
| --- | --- |
| 客户端 | 《云·绝区零》3.2.0（build 2），bundle `com.miHoYo.cloudgames.Nap`；App Store 的 iOS-on-Mac 应用（arm64 iOS 二进制 + Wrapper 结构），安装于 `/Applications/CloudGame.app`。 |
| 机器与系统 | Apple Silicon MacBook，macOS 26，单屏 1512×982 点。 |
| 平台代码 | `internal/platform/macos`（PR #26）+ 本 PR 的 rect 附件修复（见采集 4）与 clock.go 注释修正（见采集 5）。 |
| 驱动 | tools/fieldprobe（本 PR 提交）：驱动真实原生层——窗口枚举、权限预检、前台/AX 探测、采集会话与 raw 流桥、Quartz 输入投递、全局热键。 |
| 权限 | 辅助功能、事件合成、录屏三项就位（TCC 归属宿主应用）。首次授权弹窗流程本轮未观察到，留安装版验收（T2 原有要求）。 |
| 输入与预期 | 开发侧投递合成输入并采样；预期为游戏内可辨认的状态变化（选中切换、详情切换、列表滚动），由用户目击确认，关键项另以采集帧核对最终态。 |

## 采集

### 1. 窗口身份 — 通过

- 主窗口标题“云·绝区零”，典型 1164×762 点；同进程另有 4 条 33pt 全宽窄窗与 1 个 500×500 无标题覆盖窗，全部 layer 0。
- 窗口位置每次启动不同（级联 + 用户拖动）：实测观察到原点 (120,80)、(348,220)、(507,92) 与帧附件 screenRect 记录的 (317,102)，另有一次枚举得到窗口中心 (938,627)。
- 结论：窗口身份须每次会话重新解析——按 bundle 过滤 + layer==0 + onScreen + 完整落在显示器内，过滤后必须唯一；不跨会话缓存 windowID。

### 2. SCStream 单窗口流 — 通过

- 帧率 ~56fps：run5 为 690 帧 / 12.28s，其余各轮一致（run3 694/12.34s、run7 371/6.69s、run9 706/12.50s、run11 483/8.66s）。
- 全部帧 complete 且带像素：8 轮（run3、5–11）逐帧核对，状态全部为 complete、像素全部有效，无 idle/blank/空像素帧。
- 停止→排空→释放完整：raw 桥从 requestStop 到 released ~7ms（见[关键输出](#关键输出)），会话层完整收尾 11ms；各轮停止后无迟到回调。
- 8 轮采集无崩溃、无异常状态帧；rect 修复后所有轮次附件解析正常。

### 3. 几何附件自洽 — 通过

- 附件：surface 1920×1080、scaleFactor 2、contentScale ≈0.7087（实测 0.7086614）、contentRect (0, 0, 824.88, 540)（点），全部轮次一致。
- 换算：窗口 1164×762 点 × 2 = 2328×1524 backing 像素 × 0.7087 ≈ 1650×1080 px，即 surface 内内容宽 1649.76 px、高满幅 → 内容右侧 ~270 px 黑边；顶部约 40–50 px 为云客户端标题条（窗口内容的一部分，非游戏视频）。
- 帧图像核对一致：右侧黑带 ~270–280 px，顶部浅色标题条含“云·绝区零”字样。视频有效区域（黑边与标题条之外）的精确测绘归 M2 布局条目；本轮证明附件足以推导该区域。

### 4. macOS 26 的 rect 附件形态与修复（本 PR）

- 现象：原实现按 Apple 示例代码暗示的 NSValue(CGRect) 形态处理 contentRect/screenRect 附件并调 `objCType`；macOS 26 实际以 NSDictionary{X,Y,Width,Height}（NSNumber）到达，对字典调 `objCType` 抛 `NSInvalidArgumentException`（`-[__NSDictionaryM objCType]`，run2 崩溃日志），首帧即杀死进程。
- 修复（本 PR）：兼容 NSValue 与 NSDictionary 两种形态；未知形态按“无附件”处理并一次性 stderr 记录；`@try/@catch` 兜底。已在 8 轮实测采集中验证（run3、5–11）。
- Apple 示例的 NSValue 形态未在本机出现，仅作兼容保留；平台以实机形态为准。

### 5. mach 时间基与 displayTime — 通过（含一处分析错误更正）

- 本机 mach timebase 为 125/3（1 tick ≈ 41.667ns）。
- displayTime 健康：run5 的 12.064s 墙钟期间 displayTime 推进 12.069s（289,644,250 ticks × 125/3 ns），~17.5ms/帧，与 ~56fps 相符。
- **分析错误更正**：此前会话曾得出“displayTime 慢 42 倍”的结论，根因是把 tick 误当纳秒（289,644,250 ticks 按 1 tick=1ns 得 ~0.29s，恰为 12.06s 的约 1/42）；平台 MachClock 用 `mach_timebase_info` 构造，转换正确。同源的失实注释“Identity is the timebase of Apple Silicon”（clock.go；本机 Apple Silicon 实为 125/3）已随本 PR 修正为“测试便利假值、不描述任何真机”。

### 6. 窗口状态 API 的实测形态

- `SCWindow.isOnScreen`：观测进程须先初始化 AppKit 才有意义——未初始化时全部 false 是伪象；初始化后主窗口 true、最小化/隐藏窗口 false。
- `SCWindow.isActive`：多应用的窗口可同时为 true，不可作前台证据；平台按“仅观测、不作为前台依据”处理，本轮维持。
- SCStream 创建在未初始化 AppKit 的进程触发 `CGS_REQUIRE_INIT` 断言崩溃（崩溃日志在案）→ 驱动侧先 `NSApplicationLoad`；真实应用是 NSApplication 进程，不受影响。

### 7. 前台证据 — 通过（AX 系统级查询本机不可用，fail-closed 维持）

- AX 系统级焦点窗口查询在本机即使辅助功能已信任仍恒返 `AXError -25205`（多次采样一致，见[关键输出](#关键输出)）。平台把 AX 作为可选强化证据、失败即放弃该信号（fail-closed），不降级为更弱检查；该设计经本轮实测维持。
- `NSWorkspace.frontmostApplication` 可用，是前台证据来源；游戏前台期间采样 PID 稳定。

## 输入

### 8. 第一轮失败与根因（如实记录）

第一轮输入测试失败，两个根因均在驱动侧、均在无游戏参与下复现（Sunna 交叉复核）；第一轮热键结果作废——事件根本无法送达。

1. **CGEventPost 退出竞争**：`CGEventPost` 返回 void，事件投递对调用方异步；投递进程必须在事件送达 WindowServer 前存活，返回后立即 `os.Exit` 会丢失在途事件（同一 NULL-source HID-tap 投递：进程存活则光标移动、立即退出则丢失）。仅在进程已建立 WindowServer 连接（调过 `NSApplicationLoad`）后观察到；实测存活 ~1ms 即够，驱动取 300ms 保守窗口。未找到 Apple 公开文档（`CGEventPost` 无确认语义），按实测结论记录。
   **对真实应用的推论**：常驻运行不受影响，但退出路径有风险——⌘Q/停止流程最后一次释放事件后若立即退出可能丢失（游戏里键/鼠标卡在按下态）。集成要求：最后一次释放后停留 ~50ms 或做一次同步 WindowServer 调用再退出（已写入 T2 第 5 节）。
2. **缺 `runtime.LockOSThread()`**：主 goroutine 被调度器迁移后，AppKit run loop 跑在非主线程 → GCD 主队列永远无人排空（前台查询 dispatch_sync 2s 超时后永久失败）、Carbon 热键事件无法送达。修复：init() 锁主线程 + 主循环换成 NSApp `nextEventMatchingMask:` + `sendEvent` 泵 + activationPolicy Accessory（每次调用不抢焦点）。
   MyGo 应用本身免疫：mygo v0.5.5（mygo.go:75）init 即 `LockOSThread`、主线程事件泵、common modes 持续排空主队列——该停摆机制不会出现在真实应用，仅当主线程被 UI 代码阻塞时才会停摆（T2 §2.2 已禁止这种写法）。

### 9. 修复后验证（游戏前台、驱动盘仓库+详情界面）— 通过

| 操作 | 结果 | 证据 |
| --- | --- | --- |
| 绝对位移 | 通过 | 28–43ms 落位且稳定停留（进程内 10ms 粒度轮询 1s；实测样本 32/33/40/41ms）。游戏隐藏系统指针（用户看不见，属预期；事件带坐标、游戏按坐标处理，不影响功能）。 |
| 点击 | 通过 | 零间隔（按下→抬起紧连）有效：用户目击选中切换。80ms 间隔有效：用户目击 + 采集帧最终态核对——选中格 = 点击目标“拂晓生花[1]”、详情面板切换。 |
| 拖拽 | 通过 | 按下→插值移动→抬起（单进程 400ms）产生列表滚动：滚动条顶→底、“拂晓生花”第 3 行→第 1 行。 |
| 全局热键 ⌃⌥⌘S | 通过 | 游戏前台状态下触发 1 次、注销干净；注册/注销全程不抢焦点（前台 PID 不变）；桌面闭环同样通过。语义按已定方案：仅扫描期间注册、可自定义。 |
| 合成滚轮 | 不可用 | 被客户端忽略：帧差异 2.69% vs 噪声基线 2.79%（持平）；用户真实滚轮有效。**遍历列表滚动必须走拖拽**（已验证，写入 T2）。 |

扫描器输入指引（依据本轮）：点击默认用 ≥50–80ms 的按下→抬起间隔——零间隔本机可用，但对逐帧轮询输入的游戏不保险；`kCGMouseEventClickState` 未设置（Sunna 提示，集成时考虑）。

### 10. 运维发现

- 云客户端长时间不操作会自动退出（用户确认）。扫描期间持续操作不触发；空闲等待必须有界。
- 窗口位置会漂移（级联+用户拖动）→ 每次会话重新锁定（与采集 1 一致）。
- 游戏隐藏系统指针：指针可见性不能作为输入是否生效的观察手段（采集与输入本身不受影响）。

### 11. 驱动工具的教训（tools/fieldprobe）

- 退出竞争存活窗口：投递后停留 300ms 再退出（见输入 8）。
- `runtime.LockOSThread()` + NSApp 事件泵 + Accessory 策略（见输入 8）。
- 采集与输入驱动必须并行启动：顺序启动时工具调用间隙 ~10s 会吃掉整个采集窗口，实测翻过车。
- 截图估点误差 ~±30px（槽位 ~82 点），足够格内命中。

### 12. Sunna 的平台建议（记录，本 PR 不实现）

- 主队列 dispatch 任务可加“在途”标记，防止主线程停摆期间任务堆积。
- 停止热键依赖主线程健康；主线程停摆时的安全兜底是“拒绝输入”，需在文档写明。

## 关键输出

raw 流 run5（12.28s、690 帧全 complete；停止→排空→释放 ~7ms）：

```text
{"at":"2026-10-18T19:21:23.017117+08:00","phase":"requestStop"}
{"at":"2026-10-18T19:21:23.023892+08:00","got":true,"phase":"stopped","requestErr":""}
{"at":"2026-10-18T19:21:23.023989+08:00","got":true,"phase":"drained","requestErr":""}
{"at":"2026-10-18T19:21:23.024084+08:00","phase":"released"}
summary: byStatus {"0":690}, duration 12.281s, frames 690   （行文缩写）
```

displayTime 健康（run5 首末帧）：`displayTimeMach` 9004496424866 → 9004784067116，差 289,644,250 ticks × 125/3 ns ≈ 12.069s；同期墙钟 12.064s；690 帧 → ~17.5ms/帧。

AX 系统级焦点查询（多次采样恒同错；frontmost 正常）：

```text
{"axFocused":{"err":"macos: ax focused window query failed (AXError -25205)", ...},"frontmost":{"err":"","pid":96011}}
```

全局热键 ⌃⌥⌘S（游戏前台，修复后重测；修复前第一轮作废结果为 `triggers: null`——事件无法送达）：

```text
{"code":1,"mods":6400,"phase":"hotkeyResult","triggers":["2026-10-18T22:00:57.699236+08:00"],"unregisterErr":""}
```

## 结论

| 能力 | 结果 |
| --- | --- |
| 窗口身份解析（每会话） | 通过 |
| 单窗口采集：帧率、帧状态、停止/排空/释放 | 通过 |
| 几何附件与单位建模（含 rect 形态修复） | 通过 |
| mach 时钟与 displayTime | 通过 |
| 前台证据（frontmost PID；AX 可选、fail-closed） | 通过（AX 系统级查询本机不可用，维持 fail-closed） |
| 绝对位移 / 点击 / 拖拽 / 全局热键 | 通过 |
| 合成滚轮 | 不可用——客户端忽略；遍历列表滚动走拖拽 |

通过的范围：macOS 平台底座（PR #26 + 本 PR 修复）在真实《云·绝区零》云客户端（3.2.0 build 2，macOS 26，单屏 1512×982 点，三项权限就位）上的采集与输入能力验证通过——窗口身份、~56fps 单窗口流、几何自洽、displayTime 时钟、绝对位移、点击（含间隔指引）、拖拽滚动与全局热键；第一轮输入失败的两个驱动侧根因（CGEventPost 退出竞争、缺 LockOSThread）已查明并修复。以上限于该客户端版本、该系统与显示配置、单屏与已授权状态。

不能推出的结论：

1. 权限首次授予/拒绝/撤回与重启后的行为（本轮三项权限已就位，弹窗流程未观察到）——留安装版验收。
2. 异常形态下的拒绝/停止行为：窗口拖动中、遮挡、同应用弹窗、最小化、Space/Stage Manager、锁屏/休眠、显示缩放变体。
3. 云端时序与断流：低帧率、排队/重连、点击晚生效、画质/视频区域变化。
4. youkai 应用内集成：隐藏恢复、退出路径 dwell、菜单/托盘、完整扫描闭环——M2 后续工作。

遗留项（下一轮）：

1. 自动化采样 20–30 件与 9 个中文名复核。
2. Windows 实测轮 ②（平台底座 PR #25 的实机验证）。
3. 退出路径最后一次释放后 dwell ~50ms 的集成实现（T2 第 5 节集成要求）。
