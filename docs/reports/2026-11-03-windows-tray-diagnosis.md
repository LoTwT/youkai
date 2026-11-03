# Windows 托盘图标缺失只读诊断（2026-11-03）

状态：**已执行（2026-11-03）**。执行环境为 Windows 11、3840×2160 / 125%、dev 构建、以 High 完整性由提权 PowerShell 启动。下面的“执行结果”记录本次取得的事实与结论；“只读步骤”保留原样，供后续复现或补测未覆盖项。托盘行为的状态以[架构第 5 节](../specs/scanner-architecture.md#隐藏恢复与退出)为准。

## 执行结果

### run4（构建 `48dec0f`，手动开始）

- 11:12:18.470 日志 `youkai: tray created: icon at 2728,1680 32x48`。
- 第 2 步：youkai 与 `explorer.exe` 的 SessionId 都是 10。
- 第 4 步：对 `MyGoApp`、uID=1 调用 `Shell_NotifyIconGetRect`，得到 hr=0，rect 3410,2100–3450,2160（物理像素）。该矩形正好是日志坐标的 1.25 倍，对应 125% 缩放。
- 第 5 步：`HKCU\Control Panel\NotifyIconSettings` 中存在对应项，`InitialTooltip = Youkai · 就绪`。
- 用户点击“开始扫描”：11:16:56.768 `activate: requesting foreground for target hwnd 329278`；11:16:56.794 `activate: target hwnd 329278 accepted after 26 ms; waiting for readiness`；游戏切到前台。
- 用户按 Ctrl+Alt+Shift+S：11:16:58.853 `stop trigger: kind=user code=userCancelled phase=capturing`，扫描 `ended (cancelled)`，共 0 件，收尾正常。
- 用户点击主窗口关闭：11:17:03.849 `tray at close: icon at 2728,1680 32x48`。窗口隐藏；`MyGoWindow` 仍是同一 HWND `0x707CE`，只是不可见；`MyGoApp` 仍在。PowerShell 的 `MainWindowHandle` 改指向 dev 构建自带的控制台窗口 `ConsoleWindowClass`，这说明 11-02 观察到的“主窗口 HWND 变化”不是窗口被重建。
- 关闭后双击托盘图标无反应。根因是 youkai 没有注册 `Tray.OnClick`，由 #115 修复。
- 用户从托盘右键菜单选择“退出”：11:25:03.729 `youkai: exit wrap-up complete`。

### run5（构建 `da43ae3`）

- 日志有 `tray created`；用户确认关闭窗口后，左键单击、双击托盘图标都能恢复主窗口。

### 结论

- 已解释（11-02 现象）：
  - 托盘“找不到”：图标实际存在且已注册，问题在于左键或双击没有处理路径（#115 修复）。11-02 使用的构建早于 #114 的托盘日志，缺失 `tray` 行不能证明未注册。
  - “主窗口 HWND 变化”：`MyGoWindow` 没有被重建；变的是 PowerShell 的 `MainWindowHandle`，它改指向 dev 构建的控制台窗口。
- 未解释：进程在 20–40 分钟后自行消失。dev 构建关闭控制台窗口会连带结束进程，这是可能原因之一，但没有证据，只能作为推断。
- 未覆盖：正式安装版（GUI 子系统，没有控制台窗口）的托盘行为未测。

## 背景与已知结论

- 现场（2026-11-02，Windows 11，3840×2160 / 125%，dev 构建 `64bef82`，`Youkai Dev.exe` 以 High 完整性由提权 PowerShell 启动）：点击主窗口关闭后进程仍在，主窗口 HWND 从 `0x155060E` 变为 `0x2109E8`，通知区域和溢出区都没有 Youkai 图标；`youkai.log` 有 `close behavior=hide`，没有任何 `tray` 行；约 20–40 分钟后进程自行消失。
- 已确认：托盘只在就绪回调创建（`cmd/youkai/main.go` 的 `WhenReady` 调用 `CreateTray`）；MyGo v0.6.2 的 Windows `NewTray` 只登记选项，把 `Shell_NotifyIcon(NIM_ADD)` 推迟到异步消息，并丢弃返回值、无论成败都把图标标记为已添加（模块内 `internal/windows/tray.go` 的 `NewTray` 与 `add`）。因此“创建成功”不等于图标可见，旧实现也没有成功日志。
- 执行前未确认（本次部分回答）：`Shell_NotifyIcon` 的返回值仍被 MyGo 丢弃，无法直接取得；本次 `Shell_NotifyIconGetRect` 返回 hr=0，说明图标已注册，没有出现需要解释的注册失败。进程为什么在 20–40 分钟后退出仍未查明。以下步骤只收集证据，**不尝试修复**。

## 只读边界

- 不向游戏发送任何输入，不激活、移动或截图游戏窗口。
- 不修改受 Git 管理的文件；命令输出写到仓库之外的目录（例如 `%USERPROFILE%\Desktop\youkai-tray-diag\`）。
- 不写注册表，不添加、删除或修改通知区域图标；`Shell_NotifyIconGetRect` 只读取图标位置。
- 不结束进程。需要对照启动第二个实例时，先由操作者决定如何处理现有实例。
- 输出里不要放令牌、凭据或无关的个人数据。

以下命令在**提权 PowerShell**（与现场一致）中执行，`$pidTarget` 换成当前 Youkai 进程的 PID。同一步骤内的命令放在同一个 PowerShell 会话里依次执行，后文会复用前面定义的类型和变量。先确认构建包含本次日志改动：日志中应出现 `youkai: tray created: ...` 或 `youkai: tray unavailable (...)`；否则先换用包含该改动的构建再收集第 1 步之后的证据。

## 1. 基线与日志

```powershell
$pidTarget = 35132
$dataDir = Join-Path $env:LOCALAPPDATA 'Youkai Dev'   # 正式版为 'Youkai'
$log = Join-Path $dataDir 'logs\youkai.log'

Get-Process -Id $pidTarget | Select-Object Id, SessionId, ProcessName, Path, StartTime
Get-Content $log -Tail 200
Get-Content (Join-Path $dataDir 'logs\youkai.crash.log') -Tail 80 -ErrorAction SilentlyContinue
```

记录启动行（`version=... platform=... data=...`）、`close behavior=...`、`tray created`/`tray unavailable` 行，以及关闭前后的 `tray at close` 与关窗决策行。

## 2. 进程、会话与完整性

```powershell
Get-Process -Id $pidTarget | Select-Object Id, SessionId, Path
Get-Process explorer | Select-Object Id, SessionId, Path
(Get-CimInstance Win32_Process -Filter "ProcessId=$pidTarget") |
    Select-Object ProcessId, ParentProcessId, SessionId, ExecutablePath, CommandLine
```

`SessionId` 必须与 Explorer 相同；不同则说明图标被加到了另一个会话的通知区域。

完整性级别（只读打开进程令牌）：

```powershell
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class ProcIL
{
    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern IntPtr OpenProcess(uint access, bool inherit, uint pid);
    [DllImport("advapi32.dll", SetLastError = true)]
    public static extern bool OpenProcessToken(IntPtr proc, uint access, out IntPtr token);
    [DllImport("advapi32.dll", SetLastError = true)]
    public static extern bool GetTokenInformation(IntPtr token, int infoClass, IntPtr info, int len, out int needed);
    [DllImport("kernel32.dll")]
    public static extern bool CloseHandle(IntPtr h);

    public static string Level(uint pid)
    {
        IntPtr proc = OpenProcess(0x1000, false, pid); // PROCESS_QUERY_LIMITED_INFORMATION
        if (proc == IntPtr.Zero) return "OpenProcess failed: " + Marshal.GetLastWin32Error();
        IntPtr token;
        if (!OpenProcessToken(proc, 0x0008, out token))
        {
            CloseHandle(proc);
            return "OpenProcessToken failed: " + Marshal.GetLastWin32Error();
        }
        int needed;
        GetTokenInformation(token, 25, IntPtr.Zero, 0, out needed); // TokenIntegrityLevel
        IntPtr buf = Marshal.AllocHGlobal(needed);
        string result = "GetTokenInformation failed";
        if (GetTokenInformation(token, 25, buf, needed, out needed))
        {
            IntPtr sid = Marshal.ReadIntPtr(buf, 0); // TOKEN_MANDATORY_LABEL.Label.Sid
            int count = Marshal.ReadByte(sid, 1);
            int rid = Marshal.ReadInt32(sid, 8 + 4 * (count - 1));
            uint value = unchecked((uint)rid);
            if (value >= 0x5000) result = "protected (" + value + ")";
            else if (value >= 0x4000) result = "system (" + value + ")";
            else if (value >= 0x3000) result = "high (" + value + ")";
            else if (value >= 0x2000) result = "medium (" + value + ")";
            else if (value >= 0x1000) result = "low (" + value + ")";
            else result = "untrusted (" + value + ")";
        }
        Marshal.FreeHGlobal(buf);
        CloseHandle(token);
        CloseHandle(proc);
        return result;
    }
}
'@

[ProcIL]::Level($pidTarget)
```

预期现场为 `high`。同时记录退出码无法事后获得，只需记录本次输出。

## 3. 窗口与类名（含隐藏窗口）

关闭前执行一次，点击关闭后立即再执行一次并对比。预期主窗口是顶层 `MyGoWindow`（标题 `Youkai`），关闭后仍然存在但 `Visible=False`；另有隐藏的顶层 `MyGoApp` 消息窗口。**若关闭前后 `MyGoWindow` 的 HWND 改变或消失，说明窗口被销毁或重建，这与现有代码不符**，是独立于托盘的问题。

```powershell
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;

public static class WinTrayDiag
{
    public delegate bool EnumProc(IntPtr hWnd, IntPtr lParam);

    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr lp);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint pid);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetClassName(IntPtr hWnd, StringBuilder s, int n);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder s, int n);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern IntPtr GetWindow(IntPtr hWnd, uint cmd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT r);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string win);

    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }
}
'@

$script:pidTarget = 35132
$script:rows = New-Object System.Collections.Generic.List[object]
[WinTrayDiag]::EnumWindows({
    param([IntPtr]$h, [IntPtr]$l)
    [uint32]$ownerPid = 0
    [void][WinTrayDiag]::GetWindowThreadProcessId($h, [ref]$ownerPid)
    if ($ownerPid -eq $script:pidTarget) {
        $cls = New-Object System.Text.StringBuilder 256
        $title = New-Object System.Text.StringBuilder 256
        [void][WinTrayDiag]::GetClassName($h, $cls, 256)
        [void][WinTrayDiag]::GetWindowText($h, $title, 256)
        $r = [WinTrayDiag+RECT]::new()
        [void][WinTrayDiag]::GetWindowRect($h, [ref]$r)
        $owner = [WinTrayDiag]::GetWindow($h, 4) # GW_OWNER
        $script:rows.Add([pscustomobject]@{
            Hwnd    = ('0x{0:X}' -f $h.ToInt64())
            Class   = $cls.ToString()
            Title   = $title.ToString()
            Visible = [WinTrayDiag]::IsWindowVisible($h)
            Owner   = ('0x{0:X}' -f $owner.ToInt64())
            Rect    = "$($r.Left),$($r.Top),$($r.Right),$($r.Bottom)"
        })
    }
    return $true
}, [IntPtr]::Zero) | Out-Null
$script:rows | Format-Table -AutoSize
```

## 4. 任务栏窗口与图标位置

`Shell_NotifyIconGetRect` 只读取位置，不注册或修改图标。MyGo 的托盘图标挂在隐藏的顶层 `MyGoApp` 窗口上，未设置 `TrayOptions.ID`，因此图标编号为 1；若枚举结果里 `MyGoApp` 不属于 `$pidTarget`，说明命中了另一个 MyGo 应用，结果无效。

```powershell
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class WinTrayRect
{
    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }

    [StructLayout(LayoutKind.Sequential)]
    public struct NOTIFYICONIDENTIFIER
    {
        public uint cbSize;
        public IntPtr hWnd;
        public uint uID;
        public Guid guidItem;
    }

    [DllImport("shell32.dll")]
    public static extern int Shell_NotifyIconGetRect(ref NOTIFYICONIDENTIFIER id, out RECT rect);

    public static string Query(IntPtr hwnd, uint id)
    {
        var nid = new NOTIFYICONIDENTIFIER();
        nid.cbSize = (uint)Marshal.SizeOf(typeof(NOTIFYICONIDENTIFIER));
        nid.hWnd = hwnd;
        nid.uID = id;
        RECT r;
        int hr = Shell_NotifyIconGetRect(ref nid, out r);
        return string.Format("hr=0x{0:X8} rect={1},{2},{3},{4}", hr, r.Left, r.Top, r.Right, r.Bottom);
    }
}
'@

$appHwnd = [WinTrayDiag]::FindWindow('MyGoApp', $null)
"MyGoApp hwnd = 0x{0:X}" -f $appHwnd.ToInt64()
[WinTrayRect]::Query($appHwnd, 1)
```

分别在以下三个时刻记录结果：

1. 图标缺失时（关闭主窗口前后各一次）；
2. 点开任务栏的“显示隐藏的图标”溢出面板后；
3. 若能把 Youkai 图标固定到任务栏角，再记录一次。

判读：`hr=0x00000000` 且矩形非空表示图标已注册且在任务栏角；失败在溢出面板打开后仍失败，倾向“没有注册”；打开溢出面板后成功，说明图标存在但位于溢出区。溢出区里根本没有 Youkai 时，再进入下一步。

同时检查任务栏窗口本身：

```powershell
$tray = [WinTrayDiag]::FindWindow('Shell_TrayWnd', $null)
$overflow = [WinTrayDiag]::FindWindow('NotifyIconOverflowWindow', $null)
"Shell_TrayWnd = 0x{0:X}" -f $tray.ToInt64()
"NotifyIconOverflowWindow = 0x{0:X}" -f $overflow.ToInt64()
```

`Shell_TrayWnd` 为空说明当前窗口站/桌面找不到任务栏，`Shell_NotifyIcon` 会直接失败。

## 5. 通知区域注册表（只读）

Windows 11 的每个通知图标在 `HKCU\Control Panel\NotifyIconSettings` 下有一项；只读打印与本程序相关的项。Windows 10 的 `TrayNotify\IconStreams` 也可只读确认键存在，但不解析二进制内容。

```powershell
$exe = (Get-Process -Id $pidTarget).Path
"exe = $exe"
Get-ChildItem 'HKCU:\Control Panel\NotifyIconSettings' -ErrorAction SilentlyContinue | ForEach-Object {
    $v = Get-ItemProperty $_.PSPath
    if ($v.ExecutablePath -and ($v.ExecutablePath -like '*Youkai*')) {
        [pscustomobject]@{
            Key            = $_.PSChildName
            ExecutablePath = $v.ExecutablePath
            IsPromoted     = $v.IsPromoted
            InitialTooltip = $v.InitialTooltip
            IconSnapshot   = $v.IconSnapshot
        }
    }
} | Format-List

reg query "HKCU\Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\TrayNotify" /v IconStreams
```

判读：存在 `ExecutablePath` 等于当前 exe 的项，说明 Explorer 至少登记过该程序的图标；没有项既可能是从未注册成功，也可能只是 Explorer 尚未写回，不能单独作为结论。

## 6. 提权与非提权对照（操作者执行）

这一步会启动应用，不属于只读收集；先处理现有实例（操作者决定如何退出），再执行，并保持同一构建、同一数据根和工作目录。

1. 从**普通**（非提权）PowerShell 启动同一个 dev 构建，观察日志中的 `tray created`/`tray unavailable` 与图标是否出现；关闭窗口后确认图标是否仍在。
2. 从**提权** PowerShell 再启动一次，重复观察。
3. 两次都记录 `[ProcIL]::Level($pid)`、`Shell_TrayWnd` 和 `Shell_NotifyIconGetRect` 的结果。

判读：只有提权时缺失，指向完整性/会话或 shell 交互差异；两种方式都缺失，指向 MyGo 的注册路径或本机通知区域状态。非提权启动只用于托盘对照，不进行游戏输入。

## 7. 进程在 20–40 分钟后消失

在进程仍运行时记录父进程与启动方式；消失后按时间窗口查事件日志和崩溃产物（均为只读）：

```powershell
$since = (Get-Date).AddHours(-2)
Get-WinEvent -FilterHashtable @{ LogName = 'Application'; StartTime = $since } -ErrorAction SilentlyContinue |
    Where-Object { $_.Message -match 'Youkai' -or $_.ProviderName -in @('Application Error', 'Windows Error Reporting', '.NET Runtime') } |
    Select-Object TimeCreated, ProviderName, Id, LevelDisplayName,
        @{ n = 'Message'; e = { $_.Message.Substring(0, [Math]::Min(400, $_.Message.Length)) } } |
    Format-List

Get-WinEvent -FilterHashtable @{ LogName = 'System'; StartTime = $since; Id = 1074, 6006, 6008, 41, 42, 1 } -ErrorAction SilentlyContinue |
    Select-Object TimeCreated, Id, ProviderName, Message | Format-List

Get-ChildItem 'C:\ProgramData\Microsoft\Windows\WER\ReportArchive' -Directory -ErrorAction SilentlyContinue |
    Where-Object Name -match 'Youkai' | Select-Object -First 10 FullName, LastWriteTime
Get-ChildItem "$env:LOCALAPPDATA\CrashDumps" -Filter '*Youkai*' -ErrorAction SilentlyContinue |
    Select-Object Name, Length, LastWriteTime
```

同时记录进程消失的时间点，以及当时是否有睡眠/唤醒、Explorer 重启、Windows Update 或关闭终端窗口等操作。`youkai.crash.log` 有内容时一并附上。

## 交回的证据

- `youkai.log` 从启动到关闭的片段（含版本行、`close behavior`、全部 `tray` 行与关窗决策行）。
- 第 3 步关闭前后的窗口枚举表（PID、类名、标题、可见性、矩形、所有者）。
- 第 4 步三个时刻的 `Shell_NotifyIconGetRect` 结果，以及 `Shell_TrayWnd`/`NotifyIconOverflowWindow` 句柄。
- 第 5 步的注册表输出。
- 第 6 步提权/非提权两次对照的日志与图标观察。
- 第 7 步的事件日志、WER 与崩溃产物结论，以及进程消失的时间点。

这些证据用于区分“注册失败”“注册成功但图标在溢出/其他会话”和“窗口生命周期另有问题”，不用于直接修改代码；修改前需重新核对根因。
