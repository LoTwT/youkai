Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
Name "Youkai"
OutFile "/fixture/stage/youkai-setup-0.0.1-amd64.exe"
InstallDir "$LOCALAPPDATA\Programs\Youkai"
; An installed app is updated in its folder, the one the user chose or the
; one of a version with another windows.installDirName, as install.ps1 and
; the app's own updates do.
InstallDirRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "InstallLocation"
RequestExecutionLevel user
BrandingText " "
!define MUI_FINISHPAGE_RUN "$INSTDIR\Youkai.exe"
!define MUI_FINISHPAGE_SHOWREADME
!define MUI_FINISHPAGE_SHOWREADME_TEXT "$(MYGO_DESKTOP_SHORTCUT)"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION CreateDesktopShortcut
!include "MUI2.nsh"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
LangString MYGO_DESKTOP_SHORTCUT ${LANG_ENGLISH} "Create a desktop shortcut"
!include "/fixture/packaging/require-safe-installer.nsh"
!ifmacrodef customInit
Function .onInit
  !insertmacro customInit
FunctionEnd
!endif

; Quits the app running from the folder on the stack.
Function CloseApp
  Exch $0
  Push $1
  Push $2
  ; The PowerShell of the system's own architecture, not the 32-bit one
  ; that a 32-bit installer would otherwise start.
  StrCpy $1 "$WINDIR\Sysnative\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $1 +2
  StrCpy $1 "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $1 0 done
  InitPluginsDir
  FileOpen $2 "$PLUGINSDIR\close-app.ps1" w
  FileWrite $2 "$$ErrorActionPreference = 'SilentlyContinue'$\r$\n"
  FileWrite $2 "$$dir = $$env:MYGO_APP_DIR.TrimEnd('\') + '\'$\r$\n"
  FileWrite $2 "# Not the installer, which starts this.$\r$\n"
  FileWrite $2 "$$installer = (Get-CimInstance Win32_Process -Filter $\"ProcessId=$$PID$\").ParentProcessId$\r$\n"
  FileWrite $2 "function Get-AppProcess {$\r$\n"
  FileWrite $2 "$\t@(Get-CimInstance Win32_Process | Where-Object {$\r$\n"
  FileWrite $2 "$\t$\t$$_.ExecutablePath -and $$_.ProcessId -ne $$PID -and $$_.ProcessId -ne $$installer -and $$_.ExecutablePath.StartsWith($$dir, [StringComparison]::OrdinalIgnoreCase)$\r$\n"
  FileWrite $2 "$\t} | ForEach-Object { Get-Process -Id $$_.ProcessId })$\r$\n"
  FileWrite $2 "}$\r$\n"
  FileWrite $2 "$$running = Get-AppProcess$\r$\n"
  FileWrite $2 "if ($$running.Count -eq 0) { exit 0 }$\r$\n"
  FileWrite $2 "if ($$env:MYGO_APP_CHECK -eq '1') { exit 2 }$\r$\n"
  FileWrite $2 "foreach ($$p in $$running) { [void]$$p.CloseMainWindow() }$\r$\n"
  FileWrite $2 "$$deadline = (Get-Date).AddSeconds(10)$\r$\n"
  FileWrite $2 "foreach ($$p in $$running) {$\r$\n"
  FileWrite $2 "$\t$$left = [int]($$deadline - (Get-Date)).TotalMilliseconds$\r$\n"
  FileWrite $2 "$\tif ($$left -lt 0) { $$left = 0 }$\r$\n"
  FileWrite $2 "$\t[void]$$p.WaitForExit($$left)$\r$\n"
  FileWrite $2 "}$\r$\n"
  FileWrite $2 "$$running = @(Get-AppProcess)$\r$\n"
  FileWrite $2 "if ($$running.Count -eq 0) { exit 0 }$\r$\n"
  FileWrite $2 "$$running | Stop-Process -Force$\r$\n"
  FileWrite $2 "$$deadline = (Get-Date).AddSeconds(10)$\r$\n"
  FileWrite $2 "foreach ($$p in $$running) {$\r$\n"
  FileWrite $2 "$\t$$left = [int]($$deadline - (Get-Date)).TotalMilliseconds$\r$\n"
  FileWrite $2 "$\tif ($$left -lt 0) { $$left = 0 }$\r$\n"
  FileWrite $2 "$\t[void]$$p.WaitForExit($$left)$\r$\n"
  FileWrite $2 "}$\r$\n"
  FileWrite $2 "if (@(Get-AppProcess).Count -gt 0) { exit 1 }$\r$\n"
  FileWrite $2 "exit 0$\r$\n"
  FileClose $2
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_SCRIPT", t "$PLUGINSDIR\close-app.ps1")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_DIR", t r0)'
  IfSilent close
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_CHECK", t "1")'
  nsExec::Exec '"$1" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create([IO.File]::ReadAllText($$env:MYGO_APP_SCRIPT)))"'
  Pop $2
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_CHECK", t "")'
  StrCmp $2 "2" 0 done
  MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION "Youkai is running. Close it to continue?" IDOK close
  Abort
  close:
  nsExec::Exec '"$1" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create([IO.File]::ReadAllText($$env:MYGO_APP_SCRIPT)))"'
  Pop $2
  StrCmp $2 "1" 0 done
  MessageBox MB_OK|MB_ICONSTOP "Youkai is still running. Quit it, then try again." /SD IDOK
  SetErrorLevel 2
  Quit
  done:
  Pop $2
  Pop $1
  Pop $0
FunctionEnd

; Quits the app running from the folder on the stack.
Function un.CloseApp
  Exch $0
  Push $1
  Push $2
  ; The PowerShell of the system's own architecture, not the 32-bit one
  ; that a 32-bit installer would otherwise start.
  StrCpy $1 "$WINDIR\Sysnative\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $1 +2
  StrCpy $1 "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $1 0 done
  InitPluginsDir
  FileOpen $2 "$PLUGINSDIR\close-app.ps1" w
  FileWrite $2 "$$ErrorActionPreference = 'SilentlyContinue'$\r$\n"
  FileWrite $2 "$$dir = $$env:MYGO_APP_DIR.TrimEnd('\') + '\'$\r$\n"
  FileWrite $2 "# Not the installer, which starts this.$\r$\n"
  FileWrite $2 "$$installer = (Get-CimInstance Win32_Process -Filter $\"ProcessId=$$PID$\").ParentProcessId$\r$\n"
  FileWrite $2 "function Get-AppProcess {$\r$\n"
  FileWrite $2 "$\t@(Get-CimInstance Win32_Process | Where-Object {$\r$\n"
  FileWrite $2 "$\t$\t$$_.ExecutablePath -and $$_.ProcessId -ne $$PID -and $$_.ProcessId -ne $$installer -and $$_.ExecutablePath.StartsWith($$dir, [StringComparison]::OrdinalIgnoreCase)$\r$\n"
  FileWrite $2 "$\t} | ForEach-Object { Get-Process -Id $$_.ProcessId })$\r$\n"
  FileWrite $2 "}$\r$\n"
  FileWrite $2 "$$running = Get-AppProcess$\r$\n"
  FileWrite $2 "if ($$running.Count -eq 0) { exit 0 }$\r$\n"
  FileWrite $2 "if ($$env:MYGO_APP_CHECK -eq '1') { exit 2 }$\r$\n"
  FileWrite $2 "foreach ($$p in $$running) { [void]$$p.CloseMainWindow() }$\r$\n"
  FileWrite $2 "$$deadline = (Get-Date).AddSeconds(10)$\r$\n"
  FileWrite $2 "foreach ($$p in $$running) {$\r$\n"
  FileWrite $2 "$\t$$left = [int]($$deadline - (Get-Date)).TotalMilliseconds$\r$\n"
  FileWrite $2 "$\tif ($$left -lt 0) { $$left = 0 }$\r$\n"
  FileWrite $2 "$\t[void]$$p.WaitForExit($$left)$\r$\n"
  FileWrite $2 "}$\r$\n"
  FileWrite $2 "$$running = @(Get-AppProcess)$\r$\n"
  FileWrite $2 "if ($$running.Count -eq 0) { exit 0 }$\r$\n"
  FileWrite $2 "$$running | Stop-Process -Force$\r$\n"
  FileWrite $2 "$$deadline = (Get-Date).AddSeconds(10)$\r$\n"
  FileWrite $2 "foreach ($$p in $$running) {$\r$\n"
  FileWrite $2 "$\t$$left = [int]($$deadline - (Get-Date)).TotalMilliseconds$\r$\n"
  FileWrite $2 "$\tif ($$left -lt 0) { $$left = 0 }$\r$\n"
  FileWrite $2 "$\t[void]$$p.WaitForExit($$left)$\r$\n"
  FileWrite $2 "}$\r$\n"
  FileWrite $2 "if (@(Get-AppProcess).Count -gt 0) { exit 1 }$\r$\n"
  FileWrite $2 "exit 0$\r$\n"
  FileClose $2
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_SCRIPT", t "$PLUGINSDIR\close-app.ps1")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_DIR", t r0)'
  IfSilent close
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_CHECK", t "1")'
  nsExec::Exec '"$1" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create([IO.File]::ReadAllText($$env:MYGO_APP_SCRIPT)))"'
  Pop $2
  System::Call 'Kernel32::SetEnvironmentVariable(t "MYGO_APP_CHECK", t "")'
  StrCmp $2 "2" 0 done
  MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION "Youkai is running. Close it to continue?" IDOK close
  Abort
  close:
  nsExec::Exec '"$1" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create([IO.File]::ReadAllText($$env:MYGO_APP_SCRIPT)))"'
  Pop $2
  StrCmp $2 "1" 0 done
  MessageBox MB_OK|MB_ICONSTOP "Youkai is still running. Quit it, then try again." /SD IDOK
  SetErrorLevel 2
  Quit
  done:
  Pop $2
  Pop $1
  Pop $0
FunctionEnd


Section
  Push "$INSTDIR"
  Call CloseApp
  SetOutPath "$INSTDIR"
  File "/fixture/stage/THIRD-PARTY-NOTICES.md"
  File "/fixture/stage/Youkai.exe"
  File /r "/fixture/stage/models"
  File /r "/fixture/stage/onnxruntime"
  File "/fixture/stage/.mygo-files"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortCut "$SMPROGRAMS\Youkai.lnk" "$INSTDIR\Youkai.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "DisplayName" "Youkai"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "DisplayVersion" "0.0.1"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "DisplayIcon" "$INSTDIR\Youkai.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai" "NoRepair" 1
  WriteRegStr HKCU "Software\Classes\youkai" "" "URL:Youkai"
  WriteRegStr HKCU "Software\Classes\youkai" "URL Protocol" ""
  WriteRegStr HKCU "Software\Classes\youkai\shell\open\command" "" '"$INSTDIR\Youkai.exe" "%1"'
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
  !ifmacrodef customInstall
    !insertmacro customInstall
  !endif
SectionEnd

; The finish page offers it, so silent installs (/S) make none.
Function CreateDesktopShortcut
  SetOutPath "$INSTDIR"
  CreateShortCut "$DESKTOP\Youkai.lnk" "$INSTDIR\Youkai.exe"
FunctionEnd

; Deletes the shortcut on the stack, trying again for 5 s while it is in
; use: Explorer opens a new shortcut a few seconds after it appears, not
; sharing it for deletion, and Delete fails meanwhile. It succeeds when
; there is no shortcut.
Function un.DeleteShortcut
  Exch $0
  Push $1
  StrCpy $1 50
  retry:
    ClearErrors
    Delete $0
    IfErrors 0 done
    IntOp $1 $1 - 1
    IntCmp $1 0 done
    Sleep 100
    Goto retry
  done:
  Pop $1
  Pop $0
FunctionEnd

Section "Uninstall"
  Push "$INSTDIR"
  Call un.CloseApp
  Push "$SMPROGRAMS\Youkai.lnk"
  Call un.DeleteShortcut
  Push "$DESKTOP\Youkai.lnk"
  Call un.DeleteShortcut
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.lotwt.youkai"
  DeleteRegKey HKCU "Software\Classes\youkai"
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
  !ifmacrodef customUnInstall
    !insertmacro customUnInstall
  !endif
  ; Last, so that the app's folder is gone once the uninstall is done:
  ; the uninstaller runs from a copy of itself that nothing waits for.
  RMDir /r "$INSTDIR"
SectionEnd
