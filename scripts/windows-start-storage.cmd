@echo off
setlocal
for /F "tokens=1 delims==" %%V in ('set YOUKAI_ 2^>nul') do set "%%V="
set "YOUKAI_GAME_SOURCE=drive-storage"
set "YOUKAI_STORAGE_MATCH_AB=B"
set "YOUKAI_STORAGE_FAST_BADGES=0"
set "YOUKAI_STORAGE_FAST_BEFORE_CLICK=1"
set "YOUKAI_STORAGE_PACED_FAST_DETAIL=1"
set "YOUKAI_STORAGE_BATCH_EVIDENCE=1"
set "YOUKAI_ACTIVE_CHECKPOINT_SNAPSHOT=1"

set "YOUKAI_DEFER_WRAP_LIVE_READ=1"
set "YOUKAI_STORAGE_REUSE_SCROLL_PAGES=1"
if /I "%~1"=="--self-test" set "YOUKAI_SELFTEST=1"
pushd "%~dp0"
if errorlevel 1 exit /b 1
if defined YOUKAI_SELFTEST (
  "%~dp0Youkai Storage Wrap Scroll QA.exe" 2>&1
) else (
  start "" /wait "%~dp0Youkai Storage Wrap Scroll QA.exe"
)
set "APP_EXIT=%ERRORLEVEL%"
popd
exit /b %APP_EXIT%
