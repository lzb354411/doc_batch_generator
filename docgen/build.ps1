# build.ps1 —— 正式构建脚本：生成资源 + 编译正式版 EXE。
#
# 用法（在 docgen/ 目录下）：
#    powershell -ExecutionPolicy Bypass -File build.ps1
#
# 产物：docgen\资料文档生成.exe（带图标、版本信息 1.0.1）

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Fyne GUI 依赖 CGO；MSYS2 的 mingw64 gcc 需要其 bin 目录在 PATH 中
# 才能找到 cc1/as/ld 等子进程（否则 gcc 静默失败、cgo 报 exit status 2）。
$env:PATH = "C:\msys64\mingw64\bin;C:\Users\14544\go\bin;" + $env:PATH
$env:CGO_ENABLED = "1"

$version = "1.0.1"
$outName = "资料文档生成.exe"

Write-Host "==> 1/3 生成资源文件（图标/版本信息/清单）..."
& go-winres make
if ($LASTEXITCODE -ne 0) { throw "go-winres make 失败" }

Write-Host "==> 2/3 编译 $outName (v$version) ..."
Push-Location $root
# -H windowsgui：PE 子系统设为 GUI（Subsystem=2），运行时不弹出黑色命令行窗口
& "C:\Program Files\Go\bin\go.exe" build -trimpath -ldflags "-s -w -H windowsgui -X docgen/internal/ui.Version=$version" -o $outName .
Pop-Location
if ($LASTEXITCODE -ne 0) { throw "go build 失败" }

Write-Host "==> 3/3 校验产物..."
$exe = Join-Path $root $outName
$vi = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($exe)
Write-Host ("    FileVersion   : " + $vi.FileVersion)
Write-Host ("    ProductName   : " + $vi.ProductName)
Write-Host ("    FileSize      : {0:N2} MB" -f ((Get-Item $exe).Length / 1MB))

Add-Type -AssemblyName System.Drawing
try {
    $icon = [System.Drawing.Icon]::ExtractAssociatedIcon($exe)
    if ($icon -and $icon.Width -gt 0) {
        Write-Host ("    Embedded icon : 是（{0}x{1}）" -f $icon.Width, $icon.Height)
    } else {
        Write-Host "    Embedded icon : 否"
    }
} catch {
    Write-Host "    Embedded icon : 检测失败 - $($_.Exception.Message)"
}

Write-Host "==> 完成：$exe"