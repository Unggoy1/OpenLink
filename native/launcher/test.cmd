@echo off
setlocal
rem Builds the OpenLink Launcher into <output-directory> and runs its tests there. No game is
rem needed: the rejection test loads the DLL into its own process and expects a refusal.
if "%~1"=="" (
    echo usage: test.cmd ^<output-directory^> 1>&2
    exit /b 2
)
set "OUT=%~f1"
call "%~dp0build.cmd" "%OUT%"
if errorlevel 1 exit /b 1
call "%~dp0..\hostctl\vcvars.cmd"
if errorlevel 1 exit /b 1
set "TESTS=%OUT%\tests"
if not exist "%TESTS%" mkdir "%TESTS%"
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 "%~dp0join_machine_test.cpp" /Fo"%TESTS%\\" /Fe"%TESTS%\join-machine-test.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
"%TESTS%\join-machine-test.exe"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 "%~dp0relay_test.cpp" "%~dp0relay.cpp" "%~dp0netcheck.cpp" "%~dp0hotkey.cpp" "%~dp0update.cpp" /Fo"%TESTS%\\" /Fe"%TESTS%\relay-test.exe" /link /INCREMENTAL:NO ws2_32.lib winhttp.lib iphlpapi.lib
if errorlevel 1 exit /b 1
"%TESTS%\relay-test.exe"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 "%~dp0text_test.cpp" "%~dp0text_hook.cpp" /Fo"%TESTS%\\" /Fe"%TESTS%\text-test.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
"%TESTS%\text-test.exe"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 "%~dp0detour_test.cpp" "%~dp0detour.cpp" "%~dp0cbrowser.cpp" "%~dp0browser.cpp" "%~dp0relay.cpp" "%~dp0netcheck.cpp" "%~dp0hotkey.cpp" "%~dp0text_hook.cpp" "%~dp0vote.cpp" "%~dp0overlay.cpp" /Fo"%TESTS%\\" /Fe"%TESTS%\detour-test.exe" /link /INCREMENTAL:NO ws2_32.lib winhttp.lib iphlpapi.lib user32.lib gdi32.lib ole32.lib shlwapi.lib windowscodecs.lib
if errorlevel 1 exit /b 1
"%TESTS%\detour-test.exe"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 "%~dp0overlay_preview.cpp" "%~dp0overlay.cpp" "%~dp0vote.cpp" "%~dp0relay.cpp" "%~dp0netcheck.cpp" "%~dp0hotkey.cpp" /Fo"%TESTS%\\" /Fe"%TESTS%\overlay-preview.exe" /link /INCREMENTAL:NO ws2_32.lib winhttp.lib iphlpapi.lib user32.lib gdi32.lib ole32.lib shlwapi.lib windowscodecs.lib
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /utf-8 "%~dp0rejection_test.cpp" "%~dp0boundary.cpp" /Fo"%TESTS%\\" /Fe"%TESTS%\rejection-test.exe" /link /INCREMENTAL:NO bcrypt.lib advapi32.lib
if errorlevel 1 exit /b 1
"%TESTS%\rejection-test.exe" "%OUT%\openlink-launcher.dll"
exit /b %ERRORLEVEL%
