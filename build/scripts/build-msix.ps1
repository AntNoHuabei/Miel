param(
    [Parameter(Mandatory = $true)][string]$Executable,
    [Parameter(Mandatory = $true)][string]$RuntimeDirectory,
    [Parameter(Mandatory = $true)][string]$Output,
    [ValidateSet('amd64')][string]$Architecture = 'amd64'
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$windowsRoot = Join-Path $root 'windows'
$stage = Join-Path $windowsRoot 'msix-stage'
$makeAppx = Get-Command 'makeappx.exe' -ErrorAction Stop

function Write-Asset([string]$source, [string]$target, [int]$width, [int]$height) {
    Add-Type -AssemblyName System.Drawing
    $input = [Drawing.Image]::FromFile($source)
    try {
        $bitmap = New-Object Drawing.Bitmap $width, $height
        $graphics = [Drawing.Graphics]::FromImage($bitmap)
        try {
            $graphics.Clear([Drawing.Color]::Transparent)
            $graphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $scale = [Math]::Min($width / $input.Width, $height / $input.Height)
            $drawWidth = [int]($input.Width * $scale)
            $drawHeight = [int]($input.Height * $scale)
            $x = [int](($width - $drawWidth) / 2)
            $y = [int](($height - $drawHeight) / 2)
            $graphics.DrawImage($input, $x, $y, $drawWidth, $drawHeight)
            $bitmap.Save($target, [Drawing.Imaging.ImageFormat]::Png)
        } finally {
            $graphics.Dispose()
            $bitmap.Dispose()
        }
    } finally {
        $input.Dispose()
    }
}

if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
try {
    New-Item -ItemType Directory -Path (Join-Path $stage 'Assets') -Force | Out-Null
    Copy-Item -LiteralPath $Executable -Destination (Join-Path $stage 'miel.exe')
    Copy-Item -LiteralPath $RuntimeDirectory -Destination (Join-Path $stage 'runtime') -Recurse
    $manifest = Get-Content -LiteralPath (Join-Path $windowsRoot 'msix\app_manifest.xml') -Raw
    $manifest = $manifest.Replace('ProcessorArchitecture="x64"', 'ProcessorArchitecture="x64"')
    Set-Content -LiteralPath (Join-Path $stage 'AppxManifest.xml') -Value $manifest -Encoding UTF8

    $icon = Join-Path $root 'appicon.png'
    Write-Asset $icon (Join-Path $stage 'Assets\StoreLogo.png') 50 50
    Write-Asset $icon (Join-Path $stage 'Assets\Square150x150Logo.png') 150 150
    Write-Asset $icon (Join-Path $stage 'Assets\Square44x44Logo.png') 44 44
    Write-Asset $icon (Join-Path $stage 'Assets\Wide310x150Logo.png') 310 150
    Write-Asset $icon (Join-Path $stage 'Assets\SplashScreen.png') 620 300

    New-Item -ItemType Directory -Path (Split-Path $Output -Parent) -Force | Out-Null
    if (Test-Path -LiteralPath $Output) { Remove-Item -LiteralPath $Output -Force }
    & $makeAppx.Source pack /d $stage /p $Output /o
    if ($LASTEXITCODE -ne 0) { throw "makeappx exited with code $LASTEXITCODE" }
} finally {
    if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
}
