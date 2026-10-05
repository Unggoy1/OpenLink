@echo off
setlocal
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "%~dp0vcvars.cmd"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 "%~dp0slot_test.cpp" "%~dp0tick_slot.cpp" /Fo"%OUT%\\" /Fe"%OUT%\slot-test.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
"%OUT%\slot-test.exe"
exit /b %ERRORLEVEL%
