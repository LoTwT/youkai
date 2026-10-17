# M1 Windows 实机测试报告

更新日期：2026-10-17。本文是项目首份实测报告，记录 M1 的 Windows 实机安装运行验证，对应[实施清单 M1](../specs/scanner-implementation-plan.md#m1正式工程资源与会话底座)第 5 项的 Windows 部分。测试由用户按随制品提供的测试说明在 Windows 实机逐步执行，7 项全部通过；制品来源与测试方式约定见[实施清单的开发与 Windows 测试方式](../specs/scanner-implementation-plan.md#开发与-windows-测试方式)，打包与识别自检说明见[开发指南](../development.md#打包与安装包)。

## 制品与环境

| 项 | 记录 |
| --- | --- |
| 制品 | Package workflow 第 3 次运行（run 38993717827），commit `5d2a2cd`，版本 0.0.1；安装包由其 Windows job 构建（见[打包 workflow](../development.md#打包-workflow)）。 |
| 安装包核对 | 经 Lody 附件传输到测试机；26,166,597 字节；SHA-256 与分发消息及 packaging-manifest（制品清单）完全一致，完整摘录见下文第 1 项。 |
| 随包资源 | 4 个 VC++ x64 DLL 为 14.51.36247.0，放 EXE 同目录（应用本地部署见 [T3](../specs/t3-ocr-runtime-model-samples.md#绑定与运行库的实际交接)）；ORT 运行库与模型的摘要经制品清单核对一致。 |
| 系统 | Windows 11 专业版 23H2（Build 22631.6199，x64）。 |
| 系统 VC++ Redistributable | 14.44.35211（x64/x86），System32 内 4 个 v14 DLL 齐全。 |
| Smart App Control | 关闭。 |
| 显示配置 | 本轮未记录；M1 测试项不含采集与输入，显示配置按 [T2 的候选](../specs/t2-platform-capture-input.md#32-可验收的窄显示配置候选)在后续采集测试时记录。 |
| 输入与预期 | 输入为上述安装包与按测试说明执行的用户操作；单项预期以测试说明为准，7 项实际结果均达到预期。 |

## 测试结果

7 项全部通过，无失败项需要复现；未覆盖与待办见[未覆盖项](#未覆盖项)与[观察项](#观察项安装版没有落盘日志待修复)。

### 1. 下载与核对 — 通过

安装包经 Lody 附件传输，大小与摘要如下，与分发消息及 packaging-manifest 完全一致：

```text
大小：26,166,597 字节
SHA-256：0748c676a06483227278656db2fea4f02a917fb099eebba96e42dcfbabaad6d5
```

### 2. 安装 — 通过

- 无 SmartScreen、Smart App Control 或 UAC 拦截：文件无 MOTW、SAC 关闭；浏览器下载触发 SmartScreen 的路径本轮未覆盖（见未覆盖项）。
- 安装到用户自选路径 `C:\test\Youkai`；默认路径 `%LOCALAPPDATA%\Programs\Youkai` 未覆盖。自定义路径下安装、运行、卸载全程正常，属顺带验证。
- 创建桌面与开始菜单快捷方式（用户级）。
- 安装目录布局完整：`Youkai.exe`、4 个 VC++ DLL（EXE 同目录，14.51.36247.0）、`onnxruntime\`（含 `onnxruntime.dll`、`onnxruntime_providers_shared.dll`、`LICENSE`、`ThirdPartyNotices.txt`）、`models\PP-OCRv6_rec_small.onnx`、`THIRD-PARTY-NOTICES.md`、`Uninstall.exe`。
- 8 个关键文件的 SHA-256 全部与 manifest 一致。
- 注册表卸载项就位：HKCU 下 `Uninstall\com.lotwt.youkai`，DisplayName `Youkai`、版本 `0.0.1`、InstallLocation 正确。

### 3. 识别自检（YOUKAI_SELFTEST）— 通过

退出码 0；stderr 输出见[关键输出](#关键输出)，加载的库与模型均为安装目录内的随包副本，识别文本与分数达到期望。

### 4. 启动与界面（用户操作）— 通过

主窗口正常打开，中文显示正常；“开始扫描”为灰色（说明文案未逐字核对），历史记录为空。无应用图标为预期（UI 尚未设计图标）。关闭窗口后进程干净退出，两次验证均无残留。

### 5. 第二实例 — 通过

运行中再启动：第二进程退出码 1、立即退出，原实例（PID 不变）不受影响；全程仅 1 个进程，无第二个窗口。占用语义见[开发指南的跨进程占用与锁文件](../development.md#跨进程占用与锁文件)。

### 6. 数据目录 — 通过（附两项说明）

- `%LOCALAPPDATA%\Youkai\youkai.lock`（0 字节空锁文件）就位。
- `%APPDATA%\Youkai` 不存在，MyGo 的 `PathUserData` 重定向生效（目录约定见[架构第 5 节](../specs/scanner-architecture.md#本地目录保留与清理)）。
- `%LOCALAPPDATA%\com.lotwt.youkai` 不存在：本轮未开始扫描，扫描锁目录未创建，符合预期。
- 说明一（观察项）：`cache`、`logs` 子目录不存在——目录由首个写入者创建，本轮无写入，属当前实现预期；但意味着安装版正常启动后没有任何落盘日志，记为待修复项，见[观察项](#观察项安装版没有落盘日志待修复)。
- 说明二：带 stderr 重定向启动验证了日志链路完整：数据、缓存、日志、资源目录路径行与 `data-root lock acquired`、`exit wrap-up complete` 均可见，退出码 0（摘录见[关键输出](#关键输出)）。

### 7. 卸载 — 通过

经“设置→应用”卸载，有正常卸载窗口与确认。`C:\test\Youkai` 完全移除；桌面与开始菜单快捷方式均清除；注册表卸载项删除；数据根 `%LOCALAPPDATA%\Youkai`（含 `youkai.lock`）按设计保留（见[架构第 5 节](../specs/scanner-architecture.md#本地目录保留与清理)）；无残留进程。

## 关键输出

识别自检（第 3 项，退出码 0）的 stderr 原文：

```text
youkai: self-test: library="C:\\test\\Youkai\\onnxruntime\\onnxruntime.dll" model="C:\\test\\Youkai\\models\\PP-OCRv6_rec_small.onnx"
youkai: self-test passed: text="等级15/15" score=1.000
```

带 stderr 重定向启动（第 6 项说明二，退出码 0）可见：数据、缓存、日志、资源目录的路径行，`data-root lock acquired` 与 `exit wrap-up complete`。日志链路完整，仅缺落盘（见观察项）。

## 未覆盖项

1. 干净环境（无系统 VC++ redist）下应用本地 VC++ 运行库的不可或缺性：本机 System32 已有 14.44，自检通过不能单独证明；需在干净系统验证实际加载的是随包副本（对应 [T7 尚需完成的工程证据](../specs/t7-public-release-acceptance.md#尚需完成的工程证据)）。
2. 默认安装路径 `%LOCALAPPDATA%\Programs\Youkai`（本轮为自定义路径）。
3. 浏览器下载（带 MOTW）触发 SmartScreen 的拦截与放行路径（本轮文件无 MOTW）。
4. M2 及以后的 Windows 实机项：截图、SendInput、托盘与全局快捷键、安装包外的游戏响应，以及 [T2 两种显示配置候选](../specs/t2-platform-capture-input.md#32-可验收的窄显示配置候选)中选一组实测（清单见[实施清单的开发与 Windows 测试方式](../specs/scanner-implementation-plan.md#开发与-windows-测试方式)）。

## 观察项：安装版没有落盘日志（待修复）

- 现象：安装版正常启动与退出后，`%LOCALAPPDATA%\Youkai\` 下没有 `logs` 与 `cache` 子目录，没有任何落盘日志。
- 原因：当前实现的日志走 stderr，Windows EXE 为 GUI 子系统，无控制台时 stderr 不可见；目录由首个写入者创建，正常启动不写文件，`logs` 目录因此不会创建。带 stderr 重定向启动能看到完整日志行（见关键输出），说明日志内容存在、只是不落盘。
- 影响：[架构第 5 节](../specs/scanner-architecture.md#本地目录保留与清理)约定诊断日志目录为 `%LOCALAPPDATA%\Youkai\logs\`（日志按大小轮转），T7 的[使用与问题定位](../specs/t7-public-release-acceptance.md#2-首版交付门槛)交付项要求问题反馈能携带版本与会话信息；安装版用户无法重定向 stderr，出问题时没有可取回的日志，诊断能力缺失。
- 处置：记为待修复项——安装版（GUI 子系统）应把诊断日志写入架构约定的 `logs` 目录；修复另起 PR，不随本报告变更代码。

## 结论

通过的范围：GitHub Actions Windows runner 构建的安装包（Package workflow 第 3 次运行，run 38993717827，commit `5d2a2cd`，v0.0.1）已在 Windows 11 专业版 23H2（Build 22631.6199，x64）实机完成 M1 的安装运行验证——下载核对、安装（自定义路径）、识别自检、启动与界面、第二实例、数据目录与卸载共 7 项全部通过；随包资源布局与摘要和制品清单一致，ORT 运行库与模型在实机安装布局下加载并完成识别（自检期望文本 `等级15/15`）。覆盖结论限于该制品、该系统配置（已装 VC++ 14.44、Smart App Control 关闭）、自定义安装路径与无 MOTW 的传输路径。

不能推出的结论：

1. 干净环境（无系统 VC++ redist）下的可用性与应用本地 VC++ 的不可或缺性（本机 System32 已有 14.44）。
2. 默认安装路径与浏览器下载（带 MOTW）SmartScreen 路径的表现。
3. 截图、SendInput、托盘与全局快捷键、安装包外的游戏响应等 M2 及以后事项，以及 T2 显示配置候选的实测结论。
4. macOS 安装版的实机结论：对应实机测试尚未进行，目前仅有开发机上的本机验证（见实施清单 M1 第 5 项）。
5. M1 通过条件整体满足或 T7 交付门槛通过：本轮安装版未开始扫描，停止、保存、重开与再次运行未在安装版上验证；本报告只覆盖 M1 第 5 项的 Windows 实机部分。
