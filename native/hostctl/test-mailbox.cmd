@echo off
setlocal
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "%~dp0vcvars.cmd"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 "%~dp0mailbox_test.cpp" "%~dp0engine_mailbox.cpp" "%~dp0lan_provider.cpp" /Fo"%OUT%\\" /Fe"%OUT%\mailbox-test.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
"%OUT%\mailbox-test.exe"
exit /b %ERRORLEVEL%
