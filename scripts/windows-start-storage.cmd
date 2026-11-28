@echo off
setlocal
for /F "tokens=1 delims==" %%V in ('set YOUKAI_ 2^>nul') do set "%%V="

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
