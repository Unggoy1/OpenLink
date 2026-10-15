@echo off
setlocal
rem Builds the OpenLink Launcher into <output-directory>: openlink-launcher.exe and
rem openlink-launcher.dll (x64, static C runtime /MT, so players need no Visual C++
rem redistributable). The version comes from git, like the Go programs (build.ps1).
if "%~1"=="" (
    echo usage: build.cmd ^<output-directory^> 1>&2
    exit /b 2
)
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "%~dp0..\hostctl\vcvars.cmd"
if errorlevel 1 exit /b 1
set "VERSION=dev"
for /f "usebackq delims=" %%v in (`git -C "%~dp0." describe --tags --always --dirty 2^>nul`) do set "VERSION=%%v"
rem The release workflow passes the tag (a shallow checkout cannot describe it).
if defined OPENLINK_RELEASE_VERSION set "VERSION=%OPENLINK_RELEASE_VERSION%"
rem The in-game browser script is embedded from lua\browser.lua.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0embed-lua.ps1" -Source "%~dp0lua\browser.lua" -Out "%OUT%"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 /DOPENLINK_VERSION=\"%VERSION%\" /I"%OUT%" /LD "%~dp0bridge.cpp" "%~dp0backend.cpp" "%~dp0boundary.cpp" "%~dp0relay.cpp" "%~dp0netcheck.cpp" "%~dp0hotkey.cpp" "%~dp0text_hook.cpp" "%~dp0browser.cpp" "%~dp0cbrowser.cpp" "%~dp0detour.cpp" "%~dp0vote.cpp" "%~dp0overlay.cpp" "%~dp0mapheader.cpp" "%~dp0..\hostctl\tick_slot.cpp" /Fo"%OUT%\\" /Fe"%OUT%\openlink-launcher.dll" /link /INCREMENTAL:NO /IMPLIB:"%OUT%\openlink-launcher.lib" ws2_32.lib iphlpapi.lib bcrypt.lib advapi32.lib winhttp.lib user32.lib gdi32.lib ole32.lib shlwapi.lib windowscodecs.lib
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 /DOPENLINK_VERSION=\"%VERSION%\" "%~dp0launcher.cpp" "%~dp0boundary.cpp" "%~dp0netcheck.cpp" "%~dp0hotkey.cpp" "%~dp0update.cpp" /Fo"%OUT%\\" /Fe"%OUT%\openlink-launcher.exe" /link /INCREMENTAL:NO ws2_32.lib winhttp.lib bcrypt.lib advapi32.lib user32.lib ole32.lib shell32.lib uuid.lib
if errorlevel 1 exit /b 1
echo OpenLink Launcher %VERSION% built in %OUT%
exit /b 0
