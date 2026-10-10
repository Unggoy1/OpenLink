@echo off
setlocal
rem Builds the Steam-free DLL and its harness into %1, then runs the owned test
rem suite (tests\run.ps1). Never launches Halo.
if "%~1"=="" exit /b 2
set "OUT=%~f1"
call "%~dp0build.cmd" "%OUT%"
if errorlevel 1 exit /b 1
call "%~dp0..\hostctl\vcvars.cmd"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 "%~dp0tests\harness.cpp" /Fo"%OUT%\harness.obj" /Fe"%OUT%\harness.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0tests\run.ps1" -Out "%OUT%"
exit /b %ERRORLEVEL%
