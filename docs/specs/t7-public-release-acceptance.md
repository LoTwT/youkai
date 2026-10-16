# T7：构建打包与交付验收

更新日期：2026-10-16。状态：交付规划，未实施或验收。本文维护首版交付门槛、无付费证书的打包路线和最低系统候选。

## 1. 已确认目标与现有条件

- 首版包含既定的 Windows x64 和 Apple Silicon macOS，两平台同时交付；Windows 完整客户端、Windows 官方云客户端、Mac App Store 云客户端分别通过真实扫描闭环。
- 首版采用 MyGo 原生 UI，网页链接唤起桌面应用；浏览器内界面只答复了可行性，不在首版范围，见 [T1](t1-mygo-ui-distribution.md#浏览器唤起的新增需求与构建依据)。
- 用户明确首版只要求正常构建、打包，不走官方渠道。功能、数据正确性、停止保存和实机测试仍按已有设计验收；商店上架、代码签名证书和 Apple 公证不作为首版前提。
- 发布方式（2026-10-16 确认）：正式制品由 GitHub Actions 构建，随版本发布在 GitHub Releases（`LoTwT/youkai`）上，首个发布版为 `0.0.1`；首版包含 MyGo 内置自动更新，更新档案经 MyGo 更新密钥签名后随同一 Releases 发布，更新私钥由用户保管在密码管理器与 GitHub Actions secrets、不进仓库，范围与规则见[架构第 6 节](scanner-architecture.md#6-首版功能范围)。Windows 上的测试使用 GitHub Actions 产出的安装包，用户不在 Windows 上安装开发工具；需要快速排查非 VC++ 问题时可由 Mac 交叉编译免安装包补充，见[实施清单](scanner-implementation-plan.md#开发与-windows-测试方式)。
- 用户拥有 Windows 与 Mac，会在测试前准备设备和客户端；系统、硬件、显示配置及客户端版本在测试准备时记录到 [T2 第 3.2 节](t2-platform-capture-input.md#32-可验收的窄显示配置候选)。开发在 Mac 上进行，Windows 测试安排见[实施清单](scanner-implementation-plan.md#开发与-windows-测试方式)。
- 2026-10-16 用户确认不指定必须兼容的旧系统版本，最低版本依据框架、OCR 运行库、平台 API 和实测结果确定；具体顺序见第 4 节。应用自身界面与使用说明仅提供简体中文，产品选择由 [T1](t1-mygo-ui-distribution.md#已确认的产品选择)维护。
- 开发过程中可形成供用户测试的构建，正式首版仍需通过对应交付门槛；不另增加独立 UI 验证版。没有付费发布者签名不等于跳过平台必要的本地代码签名，具体路线见第 3 节。

## 2. 首版交付门槛

本表提出可检验的交付条件，具体功能规则引用已有权威文档，避免另建一套判定标准。所有项目目前均未通过工程验收。

实施顺序与内部里程碑见[首版实施清单](scanner-implementation-plan.md)。早期构建已安排资源加载和打包检查，完整安装及三种客户端验收仍按本节判断，不能用阶段产物替代首版交付。

| 交付项 | 必须取得的证据 |
| --- | --- |
| 支持范围 | 实体与简体中文语言按[已确认范围](scanner-architecture.md#6-首版功能范围)验收，显示策略按 [T2](t2-platform-capture-input.md#32-可验收的窄显示配置候选)选定各客户端首组配置。列出精确的系统、架构、客户端及显示参数；最低系统从选定依赖与目标机测试共同确定。Windows 两种尺寸候选不能都默认标成支持，Mac 也不能只凭机型推定尺寸；后续新增内置尺寸逐组补验收。 |
| 安装与运行资源 | Windows 安装包和 Mac DMG 在无开发工具的环境安装运行；模型、字典及原生库完整，首次启动不下载识别资源，中文/空格路径及任意工作目录可用。按[公共安装验收](scanner-architecture.md#两平台游戏与安装验收)区分断网识别与云游戏联网。 |
| 三种客户端实扫 | 分别完成准备、范围检查、采集、OCR、停止/正常结束、校对及导出。按 [T5](t5-zzz-inventory-traversal.md)检查详情归属、相同物品、滚动衔接和末尾，不以一种客户端的成功代替另一种。 |
| 识别质量 | 按 [T3 的独立样本与分组指标](t3-ocr-runtime-model-samples.md#4-三组样本与验收)报告整件字段准确率、自动接受错误、拒识与人工校对量；管理字段遵守已确认范围。有限样本通过不意味着未来自动识别必然全对。 |
| 停止与保存 | 失焦、几何变化、权限撤回、取消和保存失败有明确结果；关闭输入许可、原生任务结束和保存完成分别核对。重开保留有效结果，不自动续扫；规则见[公共会话契约](scanner-architecture.md#5-扫描会话与结果语义)。 |
| 校对与文件 | [T6](t6-review-export-workflow.md)的截图对应、问题盘校对确认及保存失败通过；自动接受记录通过当前校验后可直接导出，人工修改后须确认当前修订。核对 [JSON v1](scan-json-format.md)的单位、未知状态、重复物品、修订、访问账目、纳入/排除数和覆盖结论；导入重校验的冲突和资料缺口不覆盖原始审计，也不能借旧确认绕过。完整档案保留全部已采集观测，真实导出再打开一致。 |
| 存储与清理 | 按[公共目录与清理契约](scanner-architecture.md#本地目录保留与清理)及 [T6 存储管理](t6-review-export-workflow.md#历史记录与存储管理)验收两平台实际路径、开发/正式数据隔离、进程全程的数据根独占、清缓存、历史删除、忙碌保护和失败恢复；两个真实进程验证编辑与删除竞争及崩溃释放。升级或卸载保留历史，外部导出与导入来源不受影响。 |
| 桌面入口 | 安装后网页唤起、第二实例、隐藏恢复、扫描中唤起和退出竞争均按公共契约执行，浏览器确认和未安装情形有清楚说明。 |
| 打包交付 | Windows 安装包和 Mac DMG 完整生成，版本、架构、资源清单及制品摘要可核对；按第 3 节记录未签名或 ad-hoc 制品的首次打开行为。随包依赖、模型及字典的再分发条件和声明逐项核实，安装说明与实际支持范围一致。 |
| 自动更新 | 更新公钥自 0.0.1 随包内置；对升级构建验证从 GitHub Releases（`LoTwT/youkai`）检测、下载并安装经 MyGo 更新密钥签名的更新，升级后历史数据保留；扫描进行中不安装更新；测试版与正式版渠道分开。macOS ad-hoc 签名下更新后录屏/辅助功能权限是否需重新授予须实测并写入说明。规则见[架构第 6 节](scanner-architecture.md#6-首版功能范围)。 |
| 使用与问题定位 | 交付前台扫描说明、准备步骤、权限引导、支持配置、已知限制和问题反馈所需的版本/会话信息；截图及游戏账户信息不默认外传。 |

用户的实机反馈应关联制品版本、系统与客户端版本、显示配置、操作步骤和实际结果；开发侧负责提供可复现步骤与自动检查，并修复失败项。构建成功、安装成功与游戏扫描通过分别记录，覆盖结论限于实际验证过的配置。

## 3. 不依赖付费证书的打包路线

| 平台 | 首版路线 | 需要保留的验收 |
| --- | --- | --- |
| Windows x64 | 生成应用 EXE 与 NSIS 安装包，不配置 youkai 的 Authenticode 发布者证书或签名服务；已有第三方文件签名不主动删除。 | 安装、卸载、资源加载、协议注册和首次启动；记录 SmartScreen、Smart App Control 或设备策略的实际表现。 |
| macOS arm64 | 生成 `.app` 与 DMG，采用无需开发者会员的 ad-hoc 本地签名，不配置 Developer ID 或公证凭据。 | 应用及内置原生代码的签名完整性、资源加载、首次打开、录屏/输入授权，以及重装或换版本后的权限体验。 |

MyGo 当前分发文档说明 macOS 默认使用 ad-hoc 签名，Windows 的证书签名属于可配置步骤；NSIS 安装器与 DMG 制作仍需要对应打包工具。含 OCR 的 cgo 依赖还需要目标平台编译器。缺少打包工具而跳过安装包不能算完成，构建端下载工具与用户首次启动下载识别资源是两回事。依据 v0.5.5 的[分发文档](https://mygo.egoist.dev/docs/distribution)，尚未实际构建。

商店外分发仍受操作系统检查。Mac 的 ad-hoc 签名不提供 Developer ID 身份或 Apple 公证，下载后的首次打开可能被 Gatekeeper 阻止；Apple 提供的单应用确认路径取决于系统和管理策略。Windows 未签名的新制品可能出现 SmartScreen 提示，强制策略或 Smart App Control 也可能直接阻止执行。首版说明需准确记录这些限制，不能承诺无提示启动或所有拦截都有继续按钮，也不把关闭整机保护作为安装步骤。[Apple 打开应用说明](https://support.apple.com/en-us/102445) · [Microsoft SmartScreen 说明](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation) · [Smart App Control FAQ](https://support.microsoft.com/en-us/windows/security/threat-malware-protection/smart-app-control-frequently-asked-questions)

需要改善分发信任体验时再评估 Developer ID、公证和发布者证书。录屏、辅助功能等扫描权限仍按 T2 验证。

## 4. 依赖与最低系统的核对顺序

用户已确认：没有必须兼容的指定旧系统，核对依赖后确定最低版本。无需继续向用户索取版本偏好；具体系统下限仍未验证，不能据此自动宣布仅支持最新版或所有旧版本。后续按以下顺序推进，先做资料和制品核查，再安排获授权的工程验收：

1. 固定 MyGo、Go、OCR 绑定、原生运行库、模型与字典的候选组合，记录版本、架构、摘要、加载路径及再分发声明。现有固定源码和滚动官网分开，官网更新不自动升级选型。
2. 从组合要求和 T2 的采集/输入 API 得出最低系统候选，结合现有设备确定能验证哪些版本、还缺哪些环境。不为没有具体需求的旧版本额外维护兼容路径；候选通过对应系统上的安装、原生库加载和三种客户端各自适用的扫描验收后，才写成支持范围。游戏官方系统下限、开发主机版本和 SDK 版本均不能单独作为产品下限。
3. 按第 3 节固定无需付费发布者证书的构建与打包配置，将安装注册、原生资源、本地签名和权限纳入正式功能验收；设备信息在测试开始前补录。
4. 正式功能实现获授权后，先贯通单件采集、OCR、取消、校对与保存，再完成整仓遍历；Windows 可先开发，两平台三种客户端全部达标才算首版可交付。

### 最低系统候选与证据

已固定 [T1 框架与工具链候选](t1-mygo-ui-distribution.md#首轮框架与工具链候选)和 [T3 OCR 候选](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)。下表是首轮工程目标，不是已通过的支持矩阵。

| 平台 | 已取得的约束证据 | 建议 |
| --- | --- | --- |
| macOS arm64 | Go 1.27 要求 macOS 13+；实际 ORT 1.29.0 dylib 的 `LC_BUILD_VERSION` 为 14.0，固定发布流水线也设置 14.0；T2 选用的 filter 几何属性自 14.0 提供。 | 以 **macOS 14.0** 为最低候选，不为首版额外构建低版本 ORT 或采集兼容路径。必须在拟声明的最低系统上验证加载、权限和 Mac 云客户端扫描；较新系统上的成功不能替代此项。 |
| Windows x64 | Go 与 MyGo 的文档覆盖 Windows 10/11，PMv2 需 Windows 10 1703+；实读 ORT DLL 还依赖 VC++ v14/UCRT。没有证据表明这些依赖强制要求 Windows 11。 | 基于用户没有旧系统兼容目标，建议首轮以 **Windows 11 25H2 x64** 为最低测试候选，26H2 为现行版本回归候选，缩小初始系统矩阵。Windows 10 暂不列入首发承诺，这属于维护范围建议，不是假称技术上无法运行。测试准备时结合实际可用系统确定最终声明。 |

Mac 依据：[Go 1.27](https://go.dev/doc/go1.27#ports)、[ORT 1.29.0 发布流水线](https://github.com/microsoft/onnxruntime/blob/2e2543fbe9fae542f921d47a72d21d5a4ef0b710/tools/ci_build/github/azure-pipelines/templates/mac-cpu-packing-jobs.yml)、[T3 实际制品检查](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)、[T2 API 版本](t2-platform-capture-input.md#23-api-可用版本不等于产品下限)。ORT 的通用构建页面写有 macOS 13.3，但它不能覆盖这个具体发布文件的 14.0 标记；SDK 版本也不等于运行系统下限。

Windows 依据：[Go 最低要求](https://go.dev/wiki/MinimumRequirements)、[MyGo 要求](https://mygo.egoist.dev/docs)、[VC++ 支持范围](https://learn.microsoft.com/en-us/cpp/windows/latest-supported-vc-redist?view=msvc-170)。截至核对日，[Microsoft 发布信息](https://learn.microsoft.com/en-us/windows/release-health/windows11-release-information)列出 25H2 和 26H2 均在维护中；本建议不以发布时间推断哪个版本更稳定。缺少相应目标环境时调整候选或补齐验证，不仅凭构建成功宣布整个 Windows 11 系列均受支持。

最低候选必须贯穿应用元数据、Go/C/Objective-C 的部署目标及全部随包原生文件。Mac 要核对实际 Mach-O 的系统标记、加载路径及签名；Windows 要核对最终 EXE/DLL 的导入、架构和运行库。静态文件检查能排除明显不配套，但不能证明系统 API 行为、游戏输入或云画面兼容。

MyGo v0.5.5 的 [config.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/config.go)仍将 `macos.minimumSystemVersion` 默认为 `12.0`，[bundle.go](https://github.com/egoist/mygo/blob/4d8673cd2658a2c6e86b26c44751e57bf8174f91/cmd/mygo/bundle.go)据此生成 `LSMinimumSystemVersion`。按本轮候选构建时应显式设为 `14.0`，再核对 Go 和原生桥接的实际部署目标；修改应用声明本身不会改变已经编译好的 OCR 库下限。

### 尚需完成的工程证据

资料阶段已复算两个 ORT 压缩包和模型的 SHA-256，并静态核对库依赖与模型元数据。以下随正式功能完成：

1. 两平台构建并打包原生 UI、cgo 和随包资源；显式路径加载，检查中文/空格路径、任意工作目录、缺库、错架构、损坏模型及重复运行。
2. Windows 按 [T3 的应用本地部署](t3-ocr-runtime-model-samples.md#绑定与运行库的实际交接)随包提供 VC++ v14 x64 运行库文件；版本不能仅因名称带“2019”就视为足够，须不早于构建 ORT 所用的工具版本。核对再分发条件，以及在未装 VC++ 的干净系统上实际加载的是随包副本。[版本要求](https://learn.microsoft.com/en-us/cpp/windows/latest-supported-vc-redist?view=msvc-170)
3. 在无开发工具的干净环境断网安装，用固定图片完成识别、校对、保存与导出；这仍不包括离线云游戏。Mac 另测无付费签名路线下首次打开和授权，最终资源与签名后制品摘要另记。
4. 三种真实客户端分别通过整仓及取消闭环，按 T3/T5/T6 统计字段、导航、遗漏、重复物品、校对量和负载。Mac 14 与 Windows 11 25H2 等拟声明下限均需相应证据；无法验证的范围保留未验证标签。
