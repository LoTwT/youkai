$ErrorActionPreference = 'Stop'
try {
    $folder = $env:MYGO_APP_DIR
    if ([string]::IsNullOrWhiteSpace($folder) -or -not [IO.Path]::IsPathRooted($folder)) { throw 'Invalid installation folder' }
    $dir = [IO.Path]::GetFullPath($folder).TrimEnd('\') + '\'
    $all = @(Get-CimInstance Win32_Process -Property ProcessId, ParentProcessId, Name, ExecutablePath -ErrorAction Stop)
    $self = @($all | Where-Object { $_.ProcessId -eq $PID })
    if ($self.Count -ne 1) { throw 'Process query was incomplete' }
    $installer = $self[0].ParentProcessId
    foreach ($process in $all) {
        if ($process.ProcessId -eq $PID -or $process.ProcessId -eq $installer) { continue }
        $path = $process.ExecutablePath
        if ([string]::IsNullOrWhiteSpace($path)) {
            # System processes may have no readable path. An unknown app
            # candidate must not be mistaken for an exited application.
            if ($process.Name -ieq 'Youkai.exe') { throw 'Cannot inspect a Youkai process' }
            continue
        }
        if ($path.StartsWith($dir, [StringComparison]::OrdinalIgnoreCase)) {
            Write-Output 'Youkai is still running. Quit it from its menu, resolve unsaved work or save errors, then retry.'
            exit 2
        }
    }
    exit 0
} catch {
    Write-Output "Cannot check whether Youkai has exited: $_"
    exit 3
}
