@echo off
setlocal
rem Release build: static C runtime (/MT) so the DLL, loader and harness need no
rem Visual C++ redistributable on the host PC.
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "%~dp0vcvars.cmd"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /LD "%~dp0bridge.cpp" "%~dp0choice_adapter.cpp" "%~dp0saved_choices.cpp" "%~dp0lan_provider.cpp" "%~dp0engine_mailbox.cpp" "%~dp0game_b002.cpp" "%~dp0tick_slot.cpp" /Fo"%OUT%\\" /Fe"%OUT%\hi-hostctl.dll" /link /INCREMENTAL:NO /IMPLIB:"%OUT%\hi-hostctl.lib" ws2_32.lib iphlpapi.lib bcrypt.lib
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 "%~dp0harness.cpp" /Fo"%OUT%\harness.obj" /Fe"%OUT%\hostctl-harness.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 "%~dp0loader.cpp" /Fo"%OUT%\loader.obj" /Fe"%OUT%\hostctl-loader.exe" /link /INCREMENTAL:NO
exit /b %ERRORLEVEL%
