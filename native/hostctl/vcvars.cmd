@echo off
rem Sets up the MSVC x64 build environment for the calling script.
rem Order: an already active developer prompt, %VCVARSALL%, the newest Visual
rem Studio with C++ tools found by vswhere (VS2017 or later, also on GitHub's
rem windows runners), then the VS2017 Build Tools default location.
where cl >nul 2>nul && exit /b 0
if defined VCVARSALL goto run
set "VSWHERE=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer\vswhere.exe"
if exist "%VSWHERE%" for /f "usebackq delims=" %%i in (`"%VSWHERE%" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set "VCVARSALL=%%i\VC\Auxiliary\Build\vcvarsall.bat"
if not defined VCVARSALL set "VCVARSALL=C:\Program Files (x86)\Microsoft Visual Studio\2017\BuildTools\VC\Auxiliary\Build\vcvarsall.bat"
:run
if not exist "%VCVARSALL%" (
    echo vcvars: no Visual Studio C++ x64 tools found; set VCVARSALL to vcvarsall.bat 1>&2
    exit /b 1
)
call "%VCVARSALL%" x64 >nul
