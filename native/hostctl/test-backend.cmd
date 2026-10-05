@echo off
setlocal
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "%~dp0vcvars.cmd"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 "%~dp0backend_rejection_test.cpp" "%~dp0game_b002.cpp" "%~dp0engine_mailbox.cpp" "%~dp0lan_provider.cpp" "%~dp0tick_slot.cpp" /Fo"%OUT%\\" /Fe"%OUT%\backend-rejection-test.exe" /link /INCREMENTAL:NO bcrypt.lib
if errorlevel 1 exit /b 1
"%OUT%\backend-rejection-test.exe"
exit /b %ERRORLEVEL%
