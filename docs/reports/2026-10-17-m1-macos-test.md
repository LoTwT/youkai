# M1 macOS 实机测试报告

更新日期：2026-10-17。本文是项目第二份实测报告，记录 M1 的 macOS 实机安装运行验证（DMG 安装版），对应[实施清单 M1](../specs/scanner-implementation-plan.md#m1正式工程资源与会话底座)第 5 项的 macOS 部分；Windows 侧同日完成的对应验证见[M1 Windows 实机测试报告](2026-10-17-m1-windows-test.md)。测试由用户按随制品提供的测试说明在 macOS 实机逐步执行，8 项全部通过；制品由 Package workflow 的 macOS job 构建（打包与识别自检说明见[开发指南](../development.md#打包与安装包)）。

## 制品与环境

| 项 | 记录 |
| --- | --- |
| 制品 | Package workflow 第 3 次运行（run 38993717827）的 macOS job，commit `5d2a2cd`，版本 0.0.1；artifact `youkai-darwin-arm64-5d2a2cd12d8ab48708ada9bd8c62ea4a5e3f535d`，内含 `Youkai 0.0.1 arm64.dmg` 与 `packaging-manifest-darwin-arm64.txt`（见[打包 workflow](../development.md#打包-workflow)）。 |
| 获取方式 | 用户先用 Chrome 浏览器下载，被 Safe Browsing 拦截（见[观察项](#观察项)）；实际经 `gh run download` 获取。`gh` 下载的文件不带隔离标记，手动用 `xattr -w com.apple.quarantine` 写入模拟标记后进入安装；该模拟的边界见第 1 项与[未覆盖项](#未覆盖项)。 |
| DMG 核对 | 29,784,421 字节；SHA-256 与 packaging-manifest 完全一致，完整摘录见第 1 项。 |
| 随包资源 | `libonnxruntime.1.29.0.dylib` 43,184,400 字节，SHA-256 `68f6e54e695583adc371aef610ec4abb1ffaa3df656582922de7690f7e2000eb`；`PP-OCRv6_rec_small.onnx` 21,234,383 字节，SHA-256 `6f327246b50388f3c176ae304bd95767ea6dc0c9ae92153ef8cbe210b3c14884`；均与 manifest 一致。 |
| 系统 | macOS 27.0.1（Build 26A434），Apple Silicon（arm64），系统界面语言英文；满足 Info.plist 的 `LSMinimumSystemVersion=14.0`。 |
| 显示配置 | 本轮未记录；M1 测试项不含采集与输入，显示配置按 [T2 的候选](../specs/t2-platform-capture-input.md#32-可验收的窄显示配置候选)在后续采集测试时记录。 |
| 测试前状态 | `/Applications/Youkai.app` 不存在（安装无替换弹窗）；正式版数据根 `~/Library/Application Support/Youkai` 仅有空 `youkai.lock`（另一会话开发测试遗留），无历史记录。 |
| 输入与预期 | 输入为上述 DMG（隔离标记为手动模拟写入）与按测试说明执行的用户操作；单项预期以测试说明为准，8 项实际结果均达到预期。 |

## 测试结果

8 项全部通过，无失败项需要复现；浏览器真实下载路径未走通、隔离标记为手动模拟写入的边界见第 1 项与[未覆盖项](#未覆盖项)，其余观察见[观察项](#观察项)。

### 1. 下载与核对 — 通过（附条件说明）

- 浏览器真实下载路径未走通：Chrome（含增强保护提示条）以 "file is dangerous" 拦截 Actions 制品下载（截图留存，见[观察项](#观察项)）。
- 改用 `gh run download` 获取成功；DMG 大小与 SHA-256 和 packaging-manifest 完全一致：

```text
大小：29,784,421 字节
SHA-256：7ee0d2ddc2a046f55e521a9fc1a5699146c5330c53c051ad5ab3617a561d738c
```

- 隔离标记为 `gh` 下载后手动写入模拟（agent 名 `gh-run-download`）；标记经 DMG 挂载卷、Finder 拷贝向 `/Applications` 内 App 的传播与 Gatekeeper 拦截、放行为实测，“浏览器写入标记”本身未发生（见[未覆盖项](#未覆盖项)）。

### 2. 安装 — 通过

- 双击 DMG 挂载成功（卷 `/Volumes/Youkai` 带 quarantine 标记），拖入“应用程序”，无替换确认（原不存在）。
- `codesign --verify --deep --strict /Applications/Youkai.app` 退出码 0；`codesign -dv`：`Identifier=com.lotwt.youkai`、`Signature=adhoc`、arm64 thin，TeamIdentifier 未设置（无付费证书路线见 [T7](../specs/t7-public-release-acceptance.md#3-不依赖付费证书的打包路线)）；内置 dylib 单独 `codesign --verify --strict` 退出码 0。
- `Contents/Resources/` 布局完整：`onnxruntime/libonnxruntime.1.29.0.dylib`（含 ORT 的 `LICENSE`、`ThirdPartyNotices.txt`）、`models/PP-OCRv6_rec_small.onnx`、`THIRD-PARTY-NOTICES.md`；两个文件的 SHA-256 与 manifest 一致（见上表）。
- Info.plist：`CFBundleShortVersionString=0.0.1`、`LSMinimumSystemVersion=14.0`。

### 3. 首次打开与 Gatekeeper — 通过（截图留存）

系统界面语言为英文，弹窗文字逐字转录：

- Finder 双击打开被 Gatekeeper 拦截，弹窗标题 `"Youkai" Not Opened`；正文 `Apple could not verify "Youkai" is free of malware that may harm your Mac or compromise your privacy.`；按钮 `Move to Trash`（默认）与 `Done`，弹窗内无直接打开入口。
- 放行路径：系统设置 → 隐私与安全性（Privacy & Security）→ Security 区出现 `"Youkai" was blocked to protect your Mac.` 与 `Open Anyway` 按钮 → 点击后经 Touch ID 指纹验证确认 → App 正常打开。
- “右键 → 打开”旁路未测试：本轮走上述系统设置标准路径，新版 macOS 已移除该旁路（见[未覆盖项](#未覆盖项)）。

### 4. 识别自检（YOUKAI_SELFTEST）— 通过

命令与完整输出见[关键输出](#关键输出)：退出码 0，加载的库与模型均为 App 包内的随包副本，识别文本与分数达到期望（期望文本 `等级15/15`）。此时 Gatekeeper 已放行，终端直接运行二进制无拦截。

### 5. 启动与界面（用户操作）— 通过

- 主窗口正常打开，中文显示正常，无乱码；“开始扫描”为灰色并有说明（说明文案未逐字核对，同 Windows 报告口径）；历史记录为空，与测试前数据根状态一致。
- 关闭窗口后进程干净退出：`pgrep -fl Youkai` 无输出（退出码 1），无残留。

### 6. 第二实例 — 通过

- 运行中（原实例 PID 75865）终端再启动二进制：第二进程立即以退出码 1 退出，输出 `lock held by another process`（原文见[关键输出](#关键输出)），原实例 PID 不变、不受影响。
- `open -n` 补充测试：`open` 本身退出码 0，3 秒后仅剩原实例，无第二个窗口与残留进程，用户未观察到闪烁或 Dock 图标跳动。占用语义见[开发指南的跨进程占用与锁文件](../development.md#跨进程占用与锁文件)。

### 7. 数据与日志目录（只读）— 通过（附两项说明）

测试前后对比（基线取于 17:20:54，复核于自检与运行之后）：

| 目标 | 结果 |
| --- | --- |
| `~/Library/Application Support/Youkai/youkai.lock` | 存在，0 字节，mtime（10-17 00:38）前后一致 |
| `~/Library/Caches/Youkai` | 前后均不存在（目录由首个写入者创建，本轮无写入，符合当前实现预期） |
| `~/Library/Logs/Youkai/youkai.log` | 前后零变化：603 字节、mtime 17:08:32、SHA-256 `363c0dc412dc5f984f32c6f07a0dfb9a92721e7c81d8cc3d571ffa07117f634f` |

- 说明一：该 `youkai.log` 是另一会话在本机实测日志落盘修复时、运行修复版构建（日志首行 `commit=93b3cdc507dd-modified`）产生的，非本轮被测 DMG（commit `5d2a2cd`）的产物。本轮以其为基线，确认被测版本运行后文件大小、mtime 与 SHA-256 零变化——被测版本确实只写 stderr、不落盘，与 Windows 报告观察项同源（见[观察项](#观察项)）；该基线文件按要求未删除。
- 说明二：与 Windows 报告口径相同，被测版本安装版正常启动后没有自己的落盘日志，该问题已由 PR #16 修复、待下一轮实机确认（见[观察项](#观察项)）；macOS 目录约定（数据、缓存、日志）见[架构第 5 节](../specs/scanner-architecture.md#本地目录保留与清理)。

### 8. 移除 — 通过

- 经用户确认后执行：`hdiutil detach` 推出 `/Volumes/Youkai`；`/Applications/Youkai.app` 移入 `~/.Trash`（可恢复；DMG 文件保留在 `~/Downloads/youkai-m1-test/build/darwin-arm64/`）。
- 移除后复核：`/Applications/Youkai.app` 不存在；无 Youkai 进程残留；`~/Library` 下 Youkai 相关目录全部原样保留（未做任何删除）：`Application Support/Youkai`（含 `youkai.lock`）、`Application Support/Youkai Dev`、`Application Support/Youkai Dev.bak-pr9b`、`Application Support/com.lotwt.youkai`、`Caches/com.lotwt.youkai.dev`、`Logs/Youkai/youkai.log`（含本机开发测试遗留目录；目录归属见[架构第 5 节](../specs/scanner-architecture.md#本地目录保留与清理)）。
- Gatekeeper 放行条目随 App 移除从系统设置消失；再次安装同 bundle 是否需重新放行未验证（见[结论](#结论)）。

## 关键输出

识别自检（第 4 项，退出码 0）的命令与输出原文：

```text
$ YOUKAI_SELFTEST=1 /Applications/Youkai.app/Contents/MacOS/Youkai; echo "exit=$?"
2026/10/17 18:05:34 youkai: self-test: library="/Applications/Youkai.app/Contents/Resources/onnxruntime/libonnxruntime.1.29.0.dylib" model="/Applications/Youkai.app/Contents/Resources/models/PP-OCRv6_rec_small.onnx"
2026/10/17 18:05:34 youkai: self-test passed: text="等级15/15" score=1.000
exit=0
```

第二实例（第 6 项，退出码 1）的输出原文：

```text
2026/10/17 18:02:06 youkai: app: acquire data-root lock on "/Users/caoyujie/Library/Application Support/Youkai": lock held by another process
exit=1
```

## 未覆盖项

1. 真实浏览器写入的隔离标记：Chrome Safe Browsing 拦截制品下载，浏览器下载路径未走通；本轮为 `gh` 下载后手动写 `com.apple.quarantine` 模拟（agent 名 `gh-run-download`）。隔离标记经 Finder 从 DMG 拷贝向 `/Applications` 内 App 的传播、Gatekeeper 拦截与放行全流程为实测；Safari 下载路径未尝试（可能不被拦截）。
2. “右键 → 打开”旁路：未测试（新版 macOS 已移除该旁路；本轮走系统设置 Open Anyway 标准路径）。
3. Open Anyway 后 Touch ID 确认弹窗的文字未逐字记录（用户口述为指纹验证）。
4. “开始扫描”灰色的说明文案未逐字核对。
5. DMG 窗口布局（背景图、Applications 快捷方式样式）未截图记录。
6. 最低系统版本 14.0 的下限行为未验证（本机 27.0.1）。
7. 录屏、辅助功能权限、游戏扫描等 M2 及以后内容，本轮不涉及。

## 观察项

### Chrome Safe Browsing 拦截 GitHub Actions 制品下载

- 现象：用户以 Chrome 下载 Actions 制品 zip 时被 Safe Browsing 拦截（增强保护提示条与 "Chrome blocked this download because the file is dangerous" 提示，截图留存），浏览器下载路径未走通；本轮改用 `gh run download` 获取。
- 意义：这是真实用户路径的观察——GitHub Actions 的 zip 制品经浏览器下载可能被 Chrome Safe Browsing 拦截。后续交用户测试的制品需考虑 `gh` CLI 等获取方式；正式发布制品随 GitHub Releases 分发，其浏览器下载是否同样被拦截未验证。

### Gatekeeper 首次打开需系统设置放行与身份验证

- 现象：ad-hoc 签名的 DMG 安装版首次打开被 Gatekeeper 拦截，弹窗内无打开入口（实际提示与放行步骤见第 3 项）；放行需经系统设置“隐私与安全性”的 `Open Anyway`（“仍要打开”）并完成 Touch ID 身份验证。
- 处置：与[实施清单已定约定](../specs/scanner-implementation-plan.md#已定的工程约定)的安排一致，macOS 首次打开说明随 M4 操作说明编写；本轮已实测确认提示文案与放行步骤，M4 说明按本报告第 3 项编写，简体中文系统的对应文案未实测。

### 被测版本日志不落盘（已修复，2026-10-18 实机确认）

- 现象：与 [M1 Windows 实机测试报告](2026-10-17-m1-windows-test.md)的观察项同源——被测版本（`5d2a2cd`）安装版正常启动与退出后没有自己的落盘日志；本轮以修复版构建产生的 `~/Library/Logs/Youkai/youkai.log` 为基线，确认被测版本运行后零写入（第 7 项说明一）。
- 状态：修复已合入 main（PR #16，`0450033`）：日志每行先写 `~/Library/Logs/Youkai/youkai.log` 再镜像 stderr（见[开发指南的诊断日志](../development.md#诊断日志)）。修复版构建（在 `93b3cdc` 上加本地修改，日志首行 `commit=93b3cdc507dd-modified`）已在本机实测产生落盘日志，即本轮基线文件；被测 DMG 早于该修复，本轮结果不代表修复后行为。修复已于 2026-10-18 经升级链实测确认：0.0.3→0.0.4 轮的诊断日志摘录即取自落盘 `~/Library/Logs/Youkai/youkai.log`（含 0.0.3 与 0.0.4 两次运行的行），见[M1 自动更新升级实测报告](2026-10-18-m1-auto-update-test.md)。

## 结论

通过的范围：GitHub Actions macOS runner 构建的 DMG（Package workflow 第 3 次运行 run 38993717827 的 macOS job，commit `5d2a2cd`，v0.0.1）已在 macOS 27.0.1（Build 26A434，Apple Silicon，系统界面英文）实机完成 M1 的安装运行验证——下载核对、安装、首次打开与 Gatekeeper 放行、识别自检、启动与界面、第二实例、数据与日志目录（只读）与移除共 8 项全部通过；随包资源布局与摘要和制品清单一致，应用与内置 dylib 的 ad-hoc 签名通过 `codesign --deep --strict` 校验，ORT 运行库与模型在 DMG 安装布局下加载并完成识别（自检期望文本 `等级15/15`）；隔离标记经 DMG 向安装副本的传播与 Gatekeeper 拦截、放行为实测，但标记本身为 `gh` 下载后手动写入（模拟浏览器写入）。覆盖结论限于该制品、该系统配置、英文系统界面与该获取路径。

不能推出的结论：

1. 真实浏览器下载路径的表现：Chrome 拦截了制品下载，浏览器写入的隔离标记与 Safari 路径未验证。
2. 最低系统版本 14.0 的下限行为（本机 27.0.1）。
3. 重装或换版本后的 Gatekeeper 评估与录屏/辅助功能权限体验（[T7 打包路线](../specs/t7-public-release-acceptance.md#3-不依赖付费证书的打包路线)的验收项）；再次安装同 bundle 的放行状态未验证。
4. 修复后版本的落盘日志：被测 `5d2a2cd` 版不落盘；PR #16 修复后的安装版已于 2026-10-18 经升级链实测确认落盘（见[观察项](#观察项)与[M1 自动更新升级实测报告](2026-10-18-m1-auto-update-test.md)）。
5. 录屏、辅助功能权限、游戏扫描等 M2 及以后事项。
6. M1 通过条件整体满足或 T7 交付门槛通过：本轮安装版未开始扫描，停止、保存、重开与再次运行未在安装版上验证；本报告只覆盖 M1 第 5 项的 macOS 实机部分。
