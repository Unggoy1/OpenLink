# Builds the three programs into .\bin (Windows amd64). Requires Go 1.27+.
# The version comes from git (e.g. v0.1.0, or v0.1.0-3-gabc1234-dirty between tags).
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$version = (git describe --tags --always --dirty 2>$null)
if (-not $version) { $version = 'dev' }
$ldflags = "-s -w -X main.version=$version"
go vet ./...
go test ./...
New-Item -ItemType Directory -Force bin | Out-Null
foreach ($c in 'hi-directory', 'hi-hostagent', 'hi-connector') {
    go build -trimpath -ldflags $ldflags -o "bin\$c.exe" "./cmd/$c"
}
# Linux builds: the connector for players on Proton, the directory for a Linux host.
$env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
try {
    foreach ($c in 'hi-connector', 'hi-directory') {
        go build -trimpath -ldflags $ldflags -o "bin\linux-amd64\$c" "./cmd/$c"
    }
} finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
}
"version $version"
Get-ChildItem bin\*.exe, bin\linux-amd64\* | Select-Object FullName, Length
