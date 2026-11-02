# 2026-11-01 Windows 本地客户端截图、布局与输入探针

> 本报告是 2026-11-01 至 11-02 Windows 本地客户端三段探针的证据与结论来源；决策、T2 与架构只引用结论。探针只回答可行性，不是正式扫描验收，开放档位与权限策略以[决策记录的 2026-11-02 Windows 本地客户端决定](../decisions/2026-10-28-scanner-rearchitecture.md#2026-11-02-windows-本地客户端决定)为准。

## 结论

- **第 1 段（截图可行性）：** 平台层有两个真实缺陷——调用了不存在的 `GetCurrentDpiAwarenessContext`，以及把 `GetDC`/`ReleaseDC` 声明在 gdi32。两者已由 [#103](https://github.com/LoTwT/youkai/pull/103) 修复，本轮真机复测通过。
- **第 2 段（分辨率与布局）：** 游戏在窗口模式只提供 6 档 16:9 分辨率；前 5 档完整落在 3840×2100 工作区内，3840×2160 档的客户区向右超出显示器 9 px、向下超出 38 px（相对工作区向下超出 98 px）。各档 GDI 截图尺寸都等于客户区，1280 至 2560 档的单帧中位耗时约 15–44 ms，3840 档约 103 ms。
- **第 3 段（输入可行性）：** 输入路径本身可用。普通权限探针点击管理员权限游戏时 `SendInput` 报告 `inserted=3`、`GetLastError=0`，但游戏无反应，与 UIPI 静默丢弃一致；UAC 提权后的探针可以选中并切换物品。单次点击没有被反作弊拦截，**不能**据此推出连续自动操作同样安全。
- 本轮发现 `WindowFromPoint` 把 `POINT` 按地址而不是按值传递，命中测试总是返回 0；修复已作为 [#105](https://github.com/LoTwT/youkai/pull/105)（`df38601`）合并。探针自身还有若干误判与选择缺陷，输入探针只保留在探针分支，未随本报告修复。

## 环境与构建

| 项目 | 值 |
| --- | --- |
| 系统 | Windows 11 专业版，版本 22631 |
| 硬件 | Intel Core i7-12700K，NVIDIA GeForce RTX 3080 |
| 显示 | 单块 3840×2160 显示器，工作区 3840×2100；系统缩放 125%（DPI 120） |
| 游戏 | 《绝区零》3.3.0 本地客户端，进程 `ZenlessZoneZero.exe`，窗口类 `UnityWndClass`，窗口模式 |
| 窗口形态 | 窗口不能拖动改变大小；窗口模式只提供 1280×720、1600×900、1920×1080、2276×1280、2560×1440、3840×2160 六档 16:9 分辨率 |
| 权限与防护 | 游戏进程完整性为 High（已提权）；HoYoKProtect 内核驱动在运行；ACE 服务存在但已停止 |
| Go | 1.27.3，zip 方式安装在用户目录；探针以普通用户权限运行（提权对照轮除外） |
| 截图探针 | `tools/winprobe`，取自 `probe/windows-local` 的 `3a0e41c`；平台层等价于 `main` 上 #103 的内容 |
| 输入探针 | `tools/wininput`（只保留在 `probe/windows-local`）；输入轮在 #105 的 `WindowFromPoint` 修复后重跑 |

三段都在同一台机器、同一块显示器上完成。第 1、2 段的游戏进程是 pid 9664（窗口 `0x1A06F2`），第 3 段在次日游戏重启后进行，进程是 pid 18848（窗口 `0x1001BC`）。第 1、2 段在 2026-11-01，第 3 段在 2026-11-02。除提权对照外，探针与游戏使用相同或更低的完整性级别；没有为探针开启管理员权限。

## 步骤与人工预期

1. **第 1 段：** 用 `winprobe -list` 核对候选窗口与进程身份，再对 1920×1080 客户区连续截 30 帧；人工预期是唯一锁定游戏窗口、截图尺寸等于客户区且不是黑屏。
2. **第 2 段：** 依次把游戏切到 6 档窗口分辨率，每档用 `winprobe` 截 10 帧（3840 档只保留 1 帧），记录窗口矩形、客户区、显示器与工作区关系、DPI 和单帧耗时；再用 Lanczos 把各档截图重采样到参考画布，按区域比对布局。
3. **第 3 段：** 用 `wininput` 做一次普通权限到游戏的点击（A）、普通权限到记事本的点击（B）、以及提权后与游戏同权限的点击（C）；人工预期分别是“被 UIPI 拦截、生效、生效”。

每轮都保持游戏窗口可见；截图探针不激活窗口，输入探针在倒计时内由操作者手动切到目标。

## 第 1 段：截图可行性

### 平台层缺陷

- 调用了不存在的 `GetCurrentDpiAwarenessContext`。正确的读取方式是 `GetThreadDpiAwarenessContext` + `GetAwarenessFromDpiAwarenessContext`，Per-Monitor V1/V2 还须用 `AreDpiAwarenessContextsEqual` 比较上下文，不能把真实上下文句柄直接与伪句柄数值比较。
- `GetDC` 和 `ReleaseDC` 是 user32 导出，原实现错误地从 gdi32 解析。
- 两项均已在 #103 修复并合并；本轮真机复测通过。

### 实测结果

- 1920×1080 档连续 30 帧：0 黑屏、0 尺寸不符、0 重复帧；截图尺寸与客户区 1920×1080 完全一致。单帧 `captureMs` 中位数 25.4 ms，p90 28.5 ms，最大 37.0 ms；按 100 ms 间隔请求时实际帧率 8.92 Hz。
- 游戏以管理员权限运行、HoYoKProtect 内核驱动加载时，GDI 客户区截图仍正常，没有黑屏。
- 窗口枚举会同时看到同进程的隐藏 `MSCTFIME UI` 与 `Default IME` 窗口；平台选择规则先按可执行文件匹配，再只保留可见且未被 DWM 隐藏的候选，因此最后唯一锁定 `UnityWndClass` 主窗口。该规则已由 #103 修正：选中结果不变，只是歧义错误的候选清单不再包含隐藏或 cloaked 窗口。

## 第 2 段：分辨率与布局

### 六档几何

窗口矩形包含标题栏与不可见边框，客户区由 `GetClientRect` 映射到屏幕坐标。右、下边界为开区间。

| 档位（客户区） | 窗口矩形 | 客户区矩形 | 客户区完整落在工作区 (0,0)–(3840,2100) | GDI 单帧 p50 |
| --- | --- | --- | --- | ---: |
| 1280×720 | (1271,696)–(2569,1463) 1298×767 | (1280,734)–(2560,1454) | 是 | 15.1 ms |
| 1600×900 | (1111,606)–(2729,1553) 1618×947 | (1120,644)–(2720,1544) | 是 | 19.5 ms |
| 1920×1080 | (797,401)–(2735,1528) 1938×1127 | (806,439)–(2726,1519) | 是 | 25.4 ms |
| 2276×1280 | (773,416)–(3067,1743) 2294×1327 | (782,454)–(3058,1734) | 是 | 37.2 ms |
| 2560×1440 | (631,336)–(3209,1823) 2578×1487 | (640,374)–(3200,1814) | 是 | 43.6 ms |
| 3840×2160 | (0,0)–(3858,2207) 3858×2207 | (9,38)–(3849,2198) | 否 | 约 103 ms |

3840 档的客户区右边界 3849 超出显示器右边界 3840 9 px；下边界 2198 超出显示器下边界 2160 38 px，相对工作区下边界 2100 超出 98 px。屏幕外部分的 GDI 截图是纯黑：右侧 9 px 与底部 38 px 条带的像素最大值均为 0。工作区下方被任务栏覆盖，但本轮截图检查只有“非纯黑且尺寸等于客户区”，没有做遮挡采样，因此没有检测到这部分被任务栏遮挡。

> 3840 档在 Mac 副本中只保留了 `window.json` 与 `frame-0000.png`，没有 `frames.json`；约 103 ms 来自 Windows 原件记录，不能由 Mac 副本复核。其余五档的耗时来自 `frames.json` 的 `captureMs.p50Ms`（每档 10 帧；1920 段为 30 帧）。

### 缩放与布局

把各档客户区经 Lanczos 重采样到参考画布后按区域比对：

- 六个档位、所有被测区域的最佳对齐都是**零偏移**，归一化互相关（NCC）为 0.97–0.997。没有观察到按档位变化的整体平移或比例偏差，支持“同一布局按客户区宽度线性缩放”的模型。
- 1600×900 档的一帧停在了别的物品上，内容区 NCC 较低；同一帧的两条按钮栏 NCC 仍为 0.997。这把“内容不同”和“布局不同”分开，不把前者的低分当成缩放失败。
- macOS 云客户端的已验收布局也能用同一模型解释；参考画布、变换与阈值分类见[架构 §6 的参考画布模型](../specs/scanner-architecture.md#参考画布模型w2a-已实现模型未接线未开放任何档位)（#104）。本节不复制该模型的细节。

## 第 3 段：输入可行性

### 平台层缺陷

`WindowFromPoint` 的 Win32 签名按值接收 `POINT`。原实现传入 `POINT` 的地址，x64 调用约定下 Windows 会把地址本身当作坐标，命中测试对所有真实屏幕点都返回 0。该缺陷已由 [#105](https://github.com/LoTwT/youkai/pull/105)（`df38601`）修复并合并。修复后 A/B/C 三轮的 `hitTest` 都命中目标或目标所属窗口。

### 结果

| 轮次 | 探针权限 | 目标 | 位置 | `SendInput` 结果 | 实际效果 |
| --- | --- | --- | --- | --- | --- |
| A | Medium | High 游戏 | 第 1 行第 2 列 | `inserted=3`，`GetLastError=0` | 无反应 |
| B | Medium | Medium 记事本 | 编辑区中心 | `inserted=3`，`GetLastError=0` | 生效：状态栏从“行 1，列 1”变为“行 3，列 6” |
| C | High（UAC 提权） | High 游戏 | 第 2 行第 4 列 | `inserted=3`，`GetLastError=0` | 生效：选中框移到目标格，详情从“识羽之誓[3]”变为“自由蓝调[3]”；游戏无异常 |

- **A（普通权限点管理员游戏）：** `SendInput` 报告完整插入系统输入流，错误码为 0，但游戏没有任何反应。这与 UIPI 的行为一致：低完整性进程的输入被静默丢弃，返回值不反映拦截。Microsoft 的 [SendInput 文档](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput)与 [T2 §2.1](../specs/t2-platform-capture-input.md#21-windowsgdi-客户区裁剪与-sendinput) 都说明 UIPI 拦截不一定体现在错误码上，平台不能把“已投递”当作“已生效”。
- **B（普通权限点记事本）：** 记事本状态栏从“行 1，列 1”变为“行 3，列 6”，说明点击确实生效。探针判为“没生效”是误判：它的记事本生效阈值要求至少 8 个像素变化，实际帧只记录到 5 个通道差 ≥20 的像素（`5 < 8`）。这是探针阈值问题，不是输入失败。
- **C（提权后点游戏）：** 探针与游戏同为 High，点击后选中框与详情都改变，游戏没有崩溃、断连或反作弊提示。说明同权限下输入路径可用，且这一轮单次点击没有被反作弊拦截。

**结论：** 输入路径本身没有问题；不提权时被 UIPI 拦截，提权后可以操作。单次点击通过不等于连续自动操作安全：反作弊是否对连续点击、拖拽或节奏作出反应，仍需单独实测。

### 输入探针自身的局限

以下问题不影响上面的结论，但必须随证据一起读：

- 记事本第一轮没有排除隐藏窗口，隐藏的 `MSCTFIME UI`/`Default IME` 让候选不唯一；改用 `-notepad-title` 后通过。正式目标选择不应重复这个错误。
- 效果阈值会误判。B 轮的 5 px 变化低于 8 px 阈值，但状态栏已经改变；探针报告的是“未达探针阈值”，不是“系统未收到输入”。
- 预期的 UIPI 特征写错了。探针原以为低权限点高权限会得到 0 投递或访问拒绝，实际是 `inserted=3`、`GetLastError=0` 后被静默丢弃；因此 A 轮被记录成“已投递、未生效”，而不是“被拒绝”。

这三处不修，因为输入探针只保留在 `probe/windows-local` 分支上；本报告只记录现象与正确读法。

## 未覆盖项与不能推出的结论

- **连续操作与拖拽未测。** 本轮每次只发一次左键点击；没有连续点击、拖动、滚轮或键盘。C 轮的单次成功不能证明正式遍历的点击/拖拽节奏不会被反作弊拦截。
- **长列表与滑块未测。** 没有在长列表下验证滑块两端、选中框与滑块联动；本轮只验证了单次点击和静态布局。
- **排序标签模板未测。** 没有采集或验证 Windows 的排序标签模板；没有各档 OCR 样本或 OCR 质量结论。
- **Windows 云客户端未测。** 本报告只覆盖本地客户端；云客户端的窗口身份、视频区域、黑边、画质与输入路径都未验证，不能用本地客户端的结果替代。
- **多显示器、非 16:9、无边框全屏、独占全屏都未测。** 本轮只有单显示器、六档 16:9 窗口模式；独占全屏可能让 GDI 截图为黑，未做实测。
- **提权后的应用行为未核对。** 单实例、深链接、自动更新在管理员权限下的行为都还没有实测。
- **HDR 未测。** 本轮没有开启 HDR，也没有验证 HDR 下的截图颜色与亮度。
- **只在一台机器上测过。** 没有第二台 Windows 机器、第二块显示器或不同 DPI 组合的复现；不能把本轮结果当成 Windows 全平台兼容性结论。
- **3840 档的截图可用性只做到“能截到”，没有做布局验收。** 该档客户区超出屏幕，屏幕外部分为纯黑，且 Mac 副本缺少该档的 `frames.json`；不能据此宣布支持 3840。

## 证据

```text
Mac 侧副本:
~/Library/Logs/Youkai Dev/probes/20261101-windows-local-probe/
  SHA256SUMS
  capture-tiers/
    window.json, frames.json, frame-0000.png, frame-0015.png
    1280x720/   { window.json, frames.json, frame-0000.png }
    1600x900/   { window.json, frames.json, frame-0000.png }
    2276x1280/  { window.json, frames.json, frame-0000.png }
    2560x1440/  { window.json, frames.json, frame-0000.png }
    3840x2160/  { window.json, frame-0000.png }   # 无 frames.json
  input-p3/
    stepA/ { A2-medium-run.json, A2-medium-summary.txt,
             A2-medium-before.png, A2-medium-after-019.png,
             A1-dryrun-2-run.json, A1-dryrun-2-summary.txt }
    stepB/ { B2-medium-run.json, B2-medium-summary.txt,
             B2-medium-before.png, B2-medium-after-019.png }
    stepC/ { C2-elevated-run.json, C2-elevated-summary.txt,
             C2-elevated-before.png, C2-elevated-after-019.png }

Windows 侧原件:
%LOCALAPPDATA%\Youkai Dev\probes\20261101-win-local-probe1\
%LOCALAPPDATA%\Youkai Dev\probes\20261101-win-local-probe2\<档位>\
%LOCALAPPDATA%\Youkai Dev\probes\20261102-win-input-p3\
  stepA/, stepB/, stepC/  （与 Mac 副本同名）
```

- A/B/C 三个点击轮在 `input-p3/stepA|B|C/` 下共 12 个文件（每轮 `run.json`、`summary.txt`、点击前后帧）；`stepA/` 另有 A1 dry-run 的 `run.json` 与 `summary.txt`。
- Mac 副本的 `SHA256SUMS` 共 32 条，全部通过；清单不再包含自身条目。
- A/B/C 的 `run.json` 与 `summary.txt` 都记录探针与目标完整性、窗口身份、几何版本、DPI、命中测试、逐批 `SendInput` 计数与 `GetLastError`、前后帧哈希和逐帧判定；本报告的数字与这些字段一致。

## 复现方式

截图（探针随本 PR 进入仓库的 `tools/winprobe`）：

```powershell
go build -o winprobe.exe ./tools/winprobe
.\winprobe.exe -exe "C:\lo\mihoyo\miHoYo Launcher\games\ZenlessZoneZero Game\ZenlessZoneZero.exe" `
  -out "$env:USERPROFILE\Desktop\winprobe-repro" -frames 10 -interval 100
```

输入（探针只保留在 `probe/windows-local`，不随本报告进入 `main`）：

```powershell
go build -o wininput.exe ./tools/wininput
# A：普通权限点游戏；C：在“以管理员身份运行”的 PowerShell 中执行同一命令
.\wininput.exe -target game -exe "C:\lo\mihoyo\miHoYo Launcher\games\ZenlessZoneZero Game\ZenlessZoneZero.exe" `
  -slot "1,2" -out "$env:USERPROFILE\Desktop\wininput-repro"
```

运行前保持游戏窗口可见、未被遮挡；输入探针不会激活窗口，倒计时内由操作者手动切换前台。
