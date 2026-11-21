# T7：构建打包与交付验收

更新日期：2026-11-21。本文维护首版交付门槛、打包路线和最低系统候选。Release 当前只构建 Windows。Package 保留两平台构建。首版已取得的实扫、安装和签名测试渠道升级证据见 [M4](scanner-implementation-plan.md#m4双平台交付验收)。

## 1. 已确认目标与现有条件

- 首版只支持 Windows 本地客户端，范围见[架构第 6 节](scanner-architecture.md#6-首版功能范围)（2026-11-03 用户决定）。
- 首版采用 MyGo 原生 UI，网页链接唤起桌面应用；浏览器内界面只答复了可行性，不在首版范围，见 [T1](t1-mygo-ui-distribution.md#浏览器唤起的新增需求与构建依据)。
- 用户明确首版只要求正常构建、打包，不走官方渠道。功能、数据正确性、停止保存和实机测试仍按已有设计验收；商店上架、代码签名证书和 Apple 公证不作为首版前提。
- 正式制品由 GitHub Actions 构建，安装包与签名更新档案通过 GitHub Releases 分发。渠道、密钥及发布后验证见[发布指南](../release.md)，本机候选与交付制品的验证分工见[实施清单](scanner-implementation-plan.md#开发与-windows-测试方式)。
- 用户拥有 Windows 与 Mac，会在测试前准备设备和客户端；系统、硬件、显示配置及客户端版本在测试准备时记录到 [T2 第 3.2 节](t2-platform-capture-input.md#32-可验收的窄显示配置候选)。开发在 Mac 上进行，Windows 测试安排见[实施清单](scanner-implementation-plan.md#开发与-windows-测试方式)。
- 2026-10-16 用户确认不指定必须兼容的旧系统版本，最低版本依据框架、OCR 运行库、平台 API 和实测结果确定；具体顺序见第 4 节。应用自身界面与使用说明仅提供简体中文，产品选择由 [T1](t1-mygo-ui-distribution.md#已确认的产品选择)维护。
- 开发过程中可形成供用户测试的构建，正式首版仍需通过对应交付门槛；不另增加独立 UI 验证版。没有付费发布者签名不等于跳过平台必要的本地代码签名，具体路线见第 3 节。

## 2. 首版交付门槛

下表维护交付条件。首版实测、免验决定及最终制品待验项集中记录在 [M4](scanner-implementation-plan.md#m4双平台交付验收)。早期打包和升级证据见 [M1](scanner-implementation-plan.md#m1正式工程资源与会话底座)。

| 交付项 | 必须取得的证据 |
| --- | --- |
| 支持范围 | 实体与简体中文语言按[已确认范围](scanner-architecture.md#6-首版功能范围)验收，首版显示策略按 [T2](t2-platform-capture-input.md#32-可验收的窄显示配置候选)选定 Windows 本地客户端首组配置。列出精确的系统、架构、客户端及显示参数；最低系统从选定依赖与目标机测试共同确定。首版开放的 1920×1080 实际客户区见架构，后续尺寸逐组验收。macOS 与云客户端的支持范围随后续版本验收。 |
| 安装与运行资源 | 首版在无开发工具的环境安装运行 Windows 安装包；模型、字典及原生库完整，首次启动不下载识别资源，中文/空格路径及任意工作目录可用。首版安装版断网 OCR 按 [M4 决定](scanner-implementation-plan.md#windows-安装验收记录)免验。Mac DMG 的安装与运行资源验收随后续版本执行。 |
| Windows 本地客户端实扫（首版） | 完成准备、范围检查、采集、OCR、停止/正常结束、校对及导出。按 [T5](t5-zzz-inventory-traversal.md)检查详情归属、相同物品、滚动衔接和末尾。macOS 云与 Windows 云客户端随后续版本分别实扫，任一种客户端的成功不能代替其他客户端。 |
| 识别质量 | 首版独立 OCR 样本及已扫描记录的其余人工字段复核按 [M4 决定](scanner-implementation-plan.md#首版字段验收决定)免验放行。样本评估方法见 [T3](t3-ocr-runtime-model-samples.md#4-三组样本与验收)，管理字段遵守已确认范围。 |
| 停止与保存 | 失焦、几何变化、权限撤回、取消和保存失败有明确结果；关闭输入许可、原生任务结束和保存完成分别核对。重开保留有效结果，不自动续扫；规则见[公共会话契约](scanner-architecture.md#5-扫描会话与结果语义)。 |
| 校对与文件 | [T6](t6-review-export-workflow.md)的截图对应、问题盘校对确认及保存失败通过；自动接受记录通过当前校验后可直接导出，人工修改后须确认当前修订。核对 [JSON/CSV v2](scan-json-format.md)的单位、`managementState`、`equipped`、重复物品、修订、纳入/排除数和覆盖结论；排序标签未验证时不得显示完整覆盖。JSON 与 CSV 来自同一修订和同一批记录，均不含截图；不提供 ZIP、archive 视图或档案导入导出。 |
| 存储与清理 | 按[公共目录与清理契约](scanner-architecture.md#本地目录保留与清理)及 [T6 存储管理](t6-review-export-workflow.md#历史记录与存储管理)验收首版 Windows 实际路径（macOS 路径随后续版本）、开发/正式数据隔离、进程全程的数据根独占、清缓存、删除截图、删除记录、忙碌保护和失败恢复；两个真实进程验证编辑与删除竞争及崩溃释放。升级或卸载保留历史，外部 JSON/CSV 不受清理影响。 |
| 桌面入口 | 安装后网页唤起、第二实例、隐藏恢复、扫描中唤起和退出竞争均按公共契约执行，浏览器确认和未安装情形有清楚说明。 |
| 打包交付 | 首版 Windows 安装包完整生成，版本、架构、资源清单及制品摘要可核对；按第 3 节记录未签名制品的首次打开行为。Mac DMG 随后续版本生成与验收。随包依赖、模型及字典的再分发条件和声明逐项核实，安装说明与实际支持范围一致。 |
| 自动更新 | 更新公钥自 0.0.1 随包内置；对升级构建验证从 GitHub Releases（`LoTwT/youkai`）检测、下载并安装经 MyGo 更新密钥签名的更新，升级后历史数据保留；扫描进行中不安装更新；测试版与正式版渠道分开（`test-v` / `v` 标签，见[发布与自动更新](../release.md)的升级实测清单）。macOS ad-hoc 签名下更新后录屏/辅助功能权限是否需重新授予，随后续版本交付时实测并写入说明。规则见[架构第 6 节](scanner-architecture.md#6-首版功能范围)。 |
| 使用与问题定位 | 交付前台扫描说明、准备步骤、权限引导、支持配置、已知限制和问题反馈所需的版本/会话信息；截图及游戏账户信息不默认外传。 |

用户的实机反馈应关联制品版本、系统与客户端版本、显示配置、操作步骤和实际结果；开发侧负责提供可复现步骤与自动检查，并修复失败项。构建成功、安装成功与游戏扫描通过分别记录，覆盖结论限于实际验证过的配置。

## 3. 不依赖付费证书的打包路线

| 平台 | 首版路线 | 需要保留的验收 |
| --- | --- | --- |
| Windows x64 | 生成应用 EXE 与 NSIS 安装包，不配置 youkai 的 Authenticode 发布者证书或签名服务；已有第三方文件签名不主动删除。 | 安装、卸载、资源加载、协议注册和首次启动；记录 SmartScreen、Smart App Control 或设备策略的实际表现。 |
| macOS arm64（后续版本） | 生成 `.app` 与 DMG，采用无需开发者会员的 ad-hoc 本地签名，不配置 Developer ID 或公证凭据。 | 应用及内置原生代码的签名完整性、资源加载、首次打开、录屏/输入授权，以及重装或换版本后的权限体验。 |

MyGo 当前分发文档说明 macOS 默认使用 ad-hoc 签名，Windows 的证书签名属于可配置步骤；NSIS 安装器与 DMG 制作仍需要对应打包工具。含 OCR 的 cgo 依赖还需要目标平台编译器。缺少打包工具而跳过安装包不能算完成，构建端下载工具与用户首次启动下载识别资源是两回事。依据 v0.10.0 的[固定分发文档](https://github.com/egoist/mygo/blob/ebde9399baf2734b3361856a53f4036859973d1d/docs/distribution.md)。历史双平台构建及实机安装仅覆盖当时的 M1 组合（mac 部分保留为后续版本基础；Release workflow 自 [#118](https://github.com/LoTwT/youkai/pull/118) 起只构建 Windows，见[实施清单 M1](scanner-implementation-plan.md#m1正式工程资源与会话底座)）。原版 MyGo v0.9.0 不满足 [T1 安装契约](t1-mygo-ui-distribution.md#资源和安装)；v0.10.0 的完整模板与 v0.9.0 相同，项目适配现绑定 v0.10.0，但 2026-11-14 的核心 TEMP 回归 27/27 仍属于 v0.9.0，本轮未在 Windows 重跑。该适配仍需独立复审；首轮 Windows headless OCR 自检只支持依赖加载，不验证真实安装器或 GUI。

商店外分发仍受操作系统检查。Mac 的 ad-hoc 签名不提供 Developer ID 身份或 Apple 公证，下载后的首次打开可能被 Gatekeeper 阻止（该平台随后续版本交付）；Apple 提供的单应用确认路径取决于系统和管理策略。Windows 未签名的新制品可能出现 SmartScreen 提示，强制策略或 Smart App Control 也可能直接阻止执行。首版说明需准确记录这些限制，不能承诺无提示启动或所有拦截都有继续按钮，也不把关闭整机保护作为安装步骤。[Apple 打开应用说明](https://support.apple.com/en-us/102445) · [Microsoft SmartScreen 说明](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation) · [Smart App Control FAQ](https://support.microsoft.com/en-us/windows/security/threat-malware-protection/smart-app-control-frequently-asked-questions)

需要改善分发信任体验时再评估 Developer ID、公证和发布者证书。录屏、辅助功能等扫描权限仍按 T2 验证。

## 4. 依赖与最低系统的核对顺序

用户已确认：没有必须兼容的指定旧系统，核对依赖后确定最低版本。无需继续向用户索取版本偏好；具体系统下限仍未验证，不能据此自动宣布仅支持最新版或所有旧版本。后续按以下顺序推进，先做资料和制品核查，再安排获授权的工程验收：

1. 固定 MyGo、Go、OCR 绑定、原生运行库、模型与字典的候选组合，记录版本、架构、摘要、加载路径及再分发声明。现有固定源码和滚动官网分开，官网更新不自动升级选型。
2. 从组合要求和 T2 的采集/输入 API 得出最低系统候选，结合现有设备确定能验证哪些版本、还缺哪些环境。不为没有具体需求的旧版本额外维护兼容路径；首版候选通过 Windows 上的安装、原生库加载和 Windows 本地客户端扫描验收后，才写成支持范围；其余客户端随后续版本按同一标准补充。游戏官方系统下限、开发主机版本和 SDK 版本均不能单独作为产品下限。
3. 按第 3 节固定无需付费发布者证书的构建与打包配置，将安装注册、原生资源、本地签名和权限纳入正式功能验收；设备信息在测试开始前补录。
4. 在目标系统核对最终制品和对应游戏客户端。首版及后续平台的证据要求见第 2 节。

依赖升级的构建和模板回归分别见 [v0.9.0 记录](../reports/2026-11-14-dependency-upgrade.md#r3-核心范围收敛与最小验证)与 [v0.10.0 记录](../reports/2026-11-19-mygo-v0.10.0-upgrade.md)。后续实际安装与签名升级以 [M4](scanner-implementation-plan.md#m4双平台交付验收)为准。

### 最低系统候选与证据

已固定 [T1 框架与工具链候选](t1-mygo-ui-distribution.md#首轮框架与工具链候选)和 [T3 OCR 候选](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)。下表是首轮工程目标，不是已通过的支持矩阵。

| 平台 | 已取得的约束证据 | 建议 |
| --- | --- | --- |
| macOS arm64（后续版本） | Go 1.27 要求 macOS 13+；实际 ORT 1.32.0 dylib 的 `LC_BUILD_VERSION` 为 14.0，固定发布流水线也设置 14.0；T2 选用的 filter 几何属性自 14.0 提供。 | 以 **macOS 14.0** 为最低候选，不为 macOS 交付额外构建低版本 ORT 或采集兼容路径。必须在拟声明的最低系统上验证加载、权限和 Mac 云客户端扫描；较新系统上的成功不能替代此项。 |
| Windows x64 | Go 与 MyGo 的文档覆盖 Windows 10/11，PMv2 需 Windows 10 1703+。实际 ORT DLL 依赖 VC++ v14/UCRT。 | 首轮最低测试候选为 Windows 11 25H2 x64。当前 LO-WINDOWS 实测为 Windows 11 Pro x64 23H2（22631.6199），带开发工具。能声明的最低版本和支持配置需由目标环境与最终制品的证据确定。 |

Mac 依据：[Go 1.27](https://go.dev/doc/go1.27#ports)、[ORT 1.32.0 发布流水线](https://github.com/microsoft/onnxruntime/blob/fab30a46491a40f5e4c968b80e8a59acbbb91b53/tools/ci_build/github/azure-pipelines/templates/mac-cpu-packing-jobs.yml)、[T3 实际制品检查](t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)、[T2 API 版本](t2-platform-capture-input.md#23-api-可用版本不等于产品下限)。ORT 的通用构建页面写有 macOS 13.3，但它不能覆盖这个具体发布文件的 14.0 标记；SDK 版本也不等于运行系统下限。

Windows 依据：[Go 最低要求](https://go.dev/wiki/MinimumRequirements)、[MyGo 要求](https://mygo.egoist.dev/docs)、[VC++ 支持范围](https://learn.microsoft.com/en-us/cpp/windows/latest-supported-vc-redist?view=msvc-170)与 [Microsoft Windows 发行信息](https://learn.microsoft.com/en-us/windows/release-health/windows11-release-information)。

最低候选必须贯穿应用元数据、Go/C/Objective-C 的部署目标及全部随包原生文件。Windows 要核对最终 EXE/DLL 的导入、架构和运行库；Mac 的系统标记、加载路径及签名随后续版本核对。静态文件检查能排除明显不配套，但不能证明系统 API 行为、游戏输入或云画面兼容。

MyGo v0.10.0 的 [config.go](https://github.com/egoist/mygo/blob/ebde9399baf2734b3361856a53f4036859973d1d/cmd/mygo/config.go)仍将 `macos.minimumSystemVersion` 默认为 `12.0`，[bundle.go](https://github.com/egoist/mygo/blob/ebde9399baf2734b3361856a53f4036859973d1d/cmd/mygo/bundle.go)据此生成 `LSMinimumSystemVersion`。本轮候选构建已显式设为 `14.0`（`mygo.json` 与本地打包命令、Package workflow 一致；Release workflow 自 [#118](https://github.com/LoTwT/youkai/pull/118) 起不构建 macOS）；该 macOS 构建保留供后续版本使用。Go 和原生桥接的实际部署目标随最低系统实机验收核对，修改应用声明本身不会改变已经编译好的 OCR 库下限。

### 尚需完成的工程证据

安装与识别依赖的核对方式见 [T3](t3-ocr-runtime-model-samples.md#绑定与运行库的实际交接)。最终制品、目标环境与支持范围按第 2 节及 [M4](scanner-implementation-plan.md#m4双平台交付验收)核对。

随包 VC++ 使用 [T3 的应用本地部署](t3-ocr-runtime-model-samples.md#绑定与运行库的实际交接)。随包布局与摘要已随 M1 核对。未装系统 VC++ 的专门实机加载检查已按 2026-10-18 用户决定免验。再分发条件随正式制品核对。
