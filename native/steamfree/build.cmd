@echo off
setlocal
rem Builds the Steam-free steam_api64.dll into %1. Static C runtime (/MT), so
rem the server PC needs no Visual C++ redistributable.
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "%~dp0..\hostctl\vcvars.cmd"
if errorlevel 1 exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0generate.ps1" -Out "%OUT%"
if errorlevel 1 exit /b 1
ml64 /nologo /c /Fo"%OUT%\unsupported.obj" "%OUT%\unsupported.asm"
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MT /std:c++17 /LD /I"%OUT%" "%~dp0core.cpp" "%OUT%\unsupported.obj" /Fo"%OUT%\core.obj" /Fe"%OUT%\steam_api64.dll" /link /DEF:"%OUT%\exports.def" /IMPLIB:"%OUT%\steamfree.lib" /INCREMENTAL:NO shell32.lib
exit /b %ERRORLEVEL%
