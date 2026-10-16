# 通用游戏扫描器分析 Handoff

整理日期：2026-10-15。状态：需求与调研交接，设计尚未批准实施。

来源：[fairy 中的原始讨论](lody://session/f29b5ca0-ed64-4be9-b08b-4829bb1c9393?workspace=jh71jrcd7n3s48y9vbscr9p8w98dgyyz)。本文保存交接时的证据与建议；后续正式采用的设计应由对应规格维护，本文引用后续结论，避免形成两份权威定义。

后续分析：[MyGo + Go 扫描器架构评估](specs/scanner-architecture.md)记录接手后的固定源码核查、推荐边界与验收方案；目前仍是待确认草案。以下保留交接时的证据和未确认状态，不将后续建议追记为用户原有决定。

## 1. 用户目标与当前授权

用户从研究 `ZztIsolation/zzz_calculator` 的驱动盘扫描功能开始，随后澄清：

> 我不是要迁移，我是想做个新的，同时未来也可能支持其他游戏的通用扫描器。

> 我主要是想使用 https://mygo.egoist.dev/，所以想问 go。

本次明确要求：将扫描器相关内容交接到 `youkai`，创建指定 Astra 角色的 Lody 会话继续分析。

- 新建扫描器，以 MyGo 为桌面框架意向，评估 Go 扫描内核及多游戏扩展。
- 绝区零驱动盘是当前讨论和研究的具体对象；首版只支持哪些内容尚未正式定案。
- 工作项目为 `/Users/caoyujie/codes/youkai`，独立于 fairy。
- 当前授权覆盖交接、源码与文档调研、方案分析和相关文档整理。尚未要求初始化应用代码、安装依赖、实现扫描、提交、推送或发布。
- 先读取 `/Users/caoyujie/.agents/AGENTS.md`、本项目 `AGENTS.md` 和 `docs/index.md`。GitHub 读取使用全局规则指定的账号；不要把旧建议当成用户已批准的实现要求。

## 2. 既有扫描器的事实基础

### 仓库关系

- [zzz_calculator 研究快照](https://github.com/ZztIsolation/zzz_calculator/tree/d58e276973c3df944bc36fc81a783f3d17018708)：计算器仓库负责启动/连接扫描器和导入结果，主要扫描实现位于独立仓库。
- [ZZZ-Scanner.Next 研究快照](https://github.com/ZztIsolation/ZZZ-Scanner.Next/tree/ff90891140016d3f1b738d73cb6cd9b291e17cec)：同作者的 C#、.NET 8、WinForms 扫描器，研究版本为 Scanner 1.0.49。
- 这些项目用于理解问题和实现机制；本项目没有迁移其代码的要求。

### 已阅读源码所体现的流水线

```text
窗口定位与前置检查
  → 串行选择物品 / 导航仓库
  → 确认选中、详情变化及画面稳定
  → 截图并提取识别区域
  → 有界队列与 OCR 工作线程
  → 按采集顺序合并结果
  → 游戏词典与数值规则清洗
  → 完整或部分结果导出 / 流式发送
```

重点阅读过 `ScanController`、`CaptureSources`、`NativeEdgeClick`、`PaddleOcrRecognizer`、`FastOcrAssist`、`DriveDiscCleaner`、`WebSocketHost` 和相关测试、架构文档。

- 通过可见画面和鼠标操作扫描；研究范围中没有依赖游戏内存读取或注入的方案。
- 前置检查确认仓库界面；扫描期间仍检查界面上下文，避免界面切换后继续操作。
- 截图使用 DXGI，并有 GDI 回退。截图协调器串行管理采集与相关状态。
- OCR 使用 PP-OCRv5 识别模型的 ONNX 推理；固定区域预处理、张量布局、归一化和 CTC 解码都是实现的一部分。Go 调用同一个模型仍需匹配这些处理。
- 图像队列容量为 48，默认 OCR 工作线程至多 2 个，各自持有推理会话，最后恢复采集顺序。它们是被研究实现的参数，不是 youkai 的推荐默认值。
- 部分有限字段采用模板识别，并以阈值和候选差距决定是否回退 OCR。
- 1.0.49 使用随实际副词条数量变化的识别区域，不能只依赖旧文档中固定区域数量的描述。
- 扫描终止可能生成部分结果；计算器对部分导入关闭“删除本次未出现物品”的处理。

### 值得继承的问题意识

1. 导航正确性与 OCR 正确性是不同问题：必须确认截图属于刚刚选择的目标。
2. 两个物品可以拥有完全相同的属性；只按属性内容去重会丢失真实库存。
3. 数值落在合法范围内，不足以证明识别与自动纠正正确，应保留识别依据。
4. 有界队列、结果排序、取消和部分成功需要共同设计。
5. 新游戏的差异常常发生在仓库交互和详情稳定判定，单独换一份坐标配置通常不足以完成接入。

研究中发现的具体限制：`NativeEdgeClick` 在某些路径中根据稳定的画面变化假定前进一行，并非独立测得滚动距离；成功导出没有保留全部原始 OCR 与字段置信依据；大型 `ScanController` 集中了较多职责。这些是阅读到的设计限制，未复现为现场故障。

原项目测试文档中的实机数量、分辨率和速度是作者报告。当前会话没有在 Windows 上运行该扫描器，不能把这些报告当作本项目的实测结果。

## 3. MyGo 的已核实能力与边界

用户选择 Go 的主要动机是使用 [MyGo](https://mygo.egoist.dev/)。调研时官网显示 v0.5.1，源码主分支快照为 [7e8688284e4b1fc752169ed9db5f51a5f213a2e6](https://github.com/egoist/mygo/tree/7e8688284e4b1fc752169ed9db5f51a5f213a2e6)，该快照 `go.mod` 声明 Go 1.27.1。实施前应重新选择并固定版本。

- **界面选择**：支持系统 WebView 中的 Web 前端，也支持 Go 原生 UI。这项选择用户尚未确认。[入门文档](https://mygo.egoist.dev/docs/getting-started)
- **Web 前端**：可以使用 Vue/TypeScript 等前端技术，构建产物嵌入应用；Windows 使用系统 WebView2。Web 前端是可选方案，不是已批准技术栈。[前端文档](https://mygo.egoist.dev/docs/frontend)
- **调用与反馈**：Go 服务可绑定为自动生成的 TypeScript 接口，支持类型化事件和 Channel，适合开始、取消、进度、日志与结果交互。绑定调用各自运行在 goroutine 中；带入的页面 context 会随页面离开或窗口关闭而取消，扫描会话的生命周期需明确设计。[绑定文档](https://mygo.egoist.dev/docs/bindings)
- **桌面与发布**：提供窗口、托盘、全局快捷键、文件对话框，以及安装包和更新能力。[桌面 API](https://mygo.egoist.dev/docs/native) · [分发文档](https://mygo.egoist.dev/docs/distribution)
- **cgo**：MyGo 自身不要求 cgo；CLI 默认 `CGO_ENABLED=0`，官方明确支持为应用依赖设置 `CGO_ENABLED=1`，届时需要目标平台 C 编译器。跨平台原生依赖和模型可通过资源目录分发。[分发文档](https://mygo.egoist.dev/docs/distribution)
- **线程模型**：应用事件循环与部分回调具有主线程约束，耗时扫描应与界面事件处理分离。[架构文档](https://mygo.egoist.dev/docs/architecture)

MyGo 适合承担桌面应用与交互。游戏窗口截图、模拟输入、OCR 和物品解析仍需在扫描模块中实现或接入。框架不要求 cgo，不能直接推导出最终扫描器无需原生依赖、可单文件分发或能无条件交叉编译。

## 4. Go 能力与可参考实现

- [yalue/onnxruntime_go](https://github.com/yalue/onnxruntime_go) 是已查看的 Windows 可用 ONNX Runtime 绑定候选，使用 cgo，要求匹配的原生运行库。运行库、绑定与模型版本需要共同固定；它尚未被选为本项目依赖。
- ONNX Runtime 官方仓库近期出现 Go 绑定相关工作，可从 [官方 issue #32535](https://github.com/microsoft/onnxruntime/issues/32535) 追踪并核实模块及平台成熟度。不要使用“官方完全没有 Go 绑定”这种未经当前源码验证的前提。
- 窗口和输入可研究 Go 对 Win32 API 的调用；截图候选包括 [kbinani/screenshot](https://github.com/kbinani/screenshot)，高性能采集可进一步评估 [DXGI Desktop Duplication](https://learn.microsoft.com/en-us/windows/desktop/direct3ddxgi/desktop-dup-api)。尚未选定后端或做集成实测。
- [YAS 公共库](https://github.com/wormtql/yas/blob/main/yas/src/lib.rs) 将截图、输入、OCR、坐标等放在共享模块；[原神仓库控制器](https://github.com/wormtql/yas/blob/main/yas-genshin/src/scanner_controller/repository_layout/controller.rs) 和 [识别工作线程](https://github.com/wormtql/yas/blob/main/yas-genshin/src/scanner/artifact_scanner/artifact_scanner_worker.rs) 保留游戏流程和结果语义。实际读过这些源码，可参考模块边界；引用的 main 链接会变化。
- Go 标准库 [`plugin`](https://pkg.go.dev/plugin) 当前不支持 Windows。先将游戏适配器作为普通 Go 包编译进程序是已有建议；第三方动态扩展尚无明确需求。

## 5. 前一轮建议，供 Astra 继续评估

这些是已讨论的推荐方向，不等同于用户批准了所有细节。

| 模块 | 建议职责 |
|---|---|
| MyGo 界面与应用服务 | 参数选择、开始/取消、进度、预览、结果查看与导出；集中处理框架绑定 |
| Go 扫描内核 | 会话、生命周期、任务队列、并发识别、超时、错误、结果顺序 |
| 平台实现 | 游戏窗口发现、截图、输入控制、DPI 与坐标转换 |
| 识别能力 | 裁剪、预处理、OCR 推理与模板匹配执行能力；允许游戏配置模型与参数 |
| 游戏适配器 | 页面识别、物品遍历、翻页策略、详情就绪规则、字段解析、词典与业务校验 |
| 输出与消费适配 | 带版本的导出格式、完整性状态、各游戏的数据结构，以及向消费方输入的转换 |

建议的依赖关系：

```text
MyGo 界面 → 应用服务 → Go 扫描内核
                         ├─ 游戏适配器
                         ├─ 平台实现
                         └─ 识别能力
扫描结果 → 导出/消费适配 → 计算器或其他工具
```

- 扫描内核保持为普通 Go 包，便于脱离 GUI 验证。
- 配置承载布局、词典、模型等资源；游戏交互流程先使用有类型的代码表达，按实际需求提炼复用。
- 初期在同一 youkai 仓库内维护应用、内核和适配器，并共同发布。
- 完整、取消、失败和部分成功应有明确语义；消费方不能把部分结果当成完整库存进行删除同步。
- 保留适当的原始识别文本、截图区域、校验与版本依据，支持错误定位和离线回放；保留策略尚待设计。
- 最小验证闭环曾建议为：MyGo 窗口 → 游戏截图 → 单件物品 OCR → 显示结果 → Windows 打包运行。这是待规划的验证方向，尚未创建或运行原型。

## 6. 与 fairy 的边界

fairy 的 [包规范](../../fairy/docs/specs/packages.md) 明确：`@randomplay/core` 负责计算，`@randomplay/data` 负责来源与标准化资料，私有 shared 维护共同契约，两个公开包同版发布。

建议 youkai 独立维护扫描能力和发布节奏：扫描器维护导出契约与版本，消费方将结果转换为计算输入。若复用 fairy 的名称或规则资料，研究其公开、固定版本的数据制品，不应直接依赖私有 shared 或复制一份人工维护的数据源。具体如何生成和消费识别资源尚未定案。

当前交接没有修改 fairy，也没有修改其数据快照或数据管理契约。不要因为扫描器研究而改写 fairy 的本机管理状态或历史档案。

## 7. 尚未确认的事项

| 事项 | 当前状态 |
|---|---|
| 首发操作系统 | 曾建议 Windows 首发并保留平台边界；用户未回答平台选择问题 |
| MyGo 界面模式 | Web 前端或 Go 原生 UI 未选择 |
| 首版游戏与实体范围 | 以绝区零驱动盘作为研究对象，正式支持范围、语言、分辨率和窗口模式待确定 |
| OCR 技术组合 | 模型、绑定、运行库、CPU/GPU 策略和 cgo 接受程度待验证 |
| 首版交付目标 | 从识别原型到完整仓库扫描之间的范围与验收标准待确定 |
| 数据与消费协议 | fairy 是否直接作为资料来源、导出格式与版本策略待确定 |
| 后续游戏与第三方扩展 | 存在未来支持其他游戏的目标，没有明确第二款游戏或动态插件需求 |

## 8. Astra 接手后要推进的工作

1. 读取项目规则和本文，检查 youkai 当前状态；已有资料足够时复用调研，不要重新把任务解释为 C# 迁移。
2. 围绕用户希望使用 MyGo 的动机继续分析，优先收敛界面模式、Windows 支持及 OCR 构建分发的实际约束。
3. 对上述模块划分提出明确意见：哪些边界首版必须保留，哪些可以等第二款游戏出现后再提炼；说明成本、最脆弱的假设与可能改变建议的证据。
4. 提供一条最小可行路径和有依据的验证标准，包括单件识别、连续相同物品、界面切换、取消、部分结果及安装后原生资源加载。把可做的静态核查与必须 Windows 实机完成的验证区分开。
5. 有影响方案的缺失偏好时，在新会话中简洁询问，同时继续不依赖答复的分析。不能把未答复当作平台、UI 或实施授权。
6. 输出可继续讨论的分析和建议；按本项目规则维护必要文档。用户尚未要求应用实现或 Git 发布操作。

## 9. 验证状态

已完成的是对参考仓库源码、MyGo 官方文档与部分构建源码的阅读，以及对 fairy/youkai 项目边界的检查。

未完成的是 MyGo 与截图、OCR 的联合编译运行，Windows 游戏现场扫描，实际准确率、速度、内存和安装包验收。本文不承诺这些性能或兼容性结果。
