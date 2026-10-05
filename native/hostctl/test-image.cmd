@echo off
setlocal
if "%~1"=="" exit /b 2
set "OUT=%~f1"
if not exist "%OUT%" mkdir "%OUT%"
call "C:\Program Files (x86)\Microsoft Visual Studio\2017\BuildTools\VC\Auxiliary\Build\vcvarsall.bat" x64 >nul
if errorlevel 1 exit /b 1
cl /nologo /W4 /WX /EHsc /MD /std:c++17 "%~dp0image_validation_test.cpp" /Fo"%OUT%\image-test.obj" /Fe"%OUT%\image-test.exe" /link /INCREMENTAL:NO
if errorlevel 1 exit /b 1
"%OUT%\image-test.exe"
exit /b %ERRORLEVEL%
