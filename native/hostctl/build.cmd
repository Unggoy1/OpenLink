@echo off
setlocal
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "C:\Program Files (x86)\Microsoft Visual Studio\2017\BuildTools\VC\Auxiliary\Build\vcvarsall.bat" x64 >nul
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 /LD "%~dp0bridge.cpp" "%~dp0choice_adapter.cpp" "%~dp0saved_choices.cpp" "%~dp0lan_provider.cpp" "%~dp0engine_mailbox.cpp" "%~dp0game_b002.cpp" "%~dp0tick_slot.cpp" /Fo"%OUT%\\" /Fe"%OUT%\hi-hostctl.dll" /link /INCREMENTAL:NO /IMPLIB:"%OUT%\hi-hostctl.lib" ws2_32.lib iphlpapi.lib bcrypt.lib
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 "%~dp0harness.cpp" /Fo"%OUT%\harness.obj" /Fe"%OUT%\hostctl-harness.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 "%~dp0loader.cpp" /Fo"%OUT%\loader.obj" /Fe"%OUT%\hostctl-loader.exe" /link /INCREMENTAL:NO
exit /b %ERRORLEVEL%
