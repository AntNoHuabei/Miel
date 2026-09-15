param(
    [ValidateSet('Fetch', 'Stage', 'Clean')]
    [string]$Action = 'Fetch',
    [string]$ManifestPath = (Join-Path $PSScriptRoot '..\runtime-manifest.json'),
    [string]$StageDirectory = (Join-Path $PSScriptRoot '..\windows\runtime-stage')
)

$ErrorActionPreference = 'Stop'
$manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
$cacheRoot = $env:MIEL_RUNTIME_CACHE
if ([string]::IsNullOrWhiteSpace($cacheRoot)) {
    $cacheRoot = Join-Path $env:LOCALAPPDATA 'Miel\build-cache\runtimes'
}
$platformCache = Join-Path $cacheRoot $manifest.platform

function Get-ArchivePath($runtime) {
    $extension = [IO.Path]::GetExtension(([Uri]$runtime.url).AbsolutePath)
    if ($extension -eq '.nupkg') { $extension = '.zip' }
    $file = '{0}-{1}-{2}{3}' -f $runtime.name, $runtime.version, $runtime.sha256, $extension
    return Join-Path $platformCache $file
}

function Assert-Archive($runtime, [string]$path) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { return $false }
    $stream = [IO.File]::OpenRead($path)
    try {
        $algorithm = [Security.Cryptography.SHA256]::Create()
        try {
            $actual = ([BitConverter]::ToString($algorithm.ComputeHash($stream))).Replace('-', '').ToLowerInvariant()
        } finally {
            $algorithm.Dispose()
        }
    } finally {
        $stream.Dispose()
    }
    if ($actual -ne $runtime.sha256.ToLowerInvariant()) {
        Remove-Item -LiteralPath $path -Force
        throw "Runtime checksum mismatch for $($runtime.name): $actual"
    }
    return $true
}

function Fetch-Archive($runtime) {
    New-Item -ItemType Directory -Path $platformCache -Force | Out-Null
    $target = Get-ArchivePath $runtime
    if (Assert-Archive $runtime $target) {
        Write-Host "Runtime cache hit: $target"
        return $target
    }
    $partial = "$target.partial"
    Remove-Item -LiteralPath $partial -Force -ErrorAction SilentlyContinue
    try {
        Write-Host "Downloading $($runtime.name) $($runtime.version)..."
        Invoke-WebRequest -Uri $runtime.url -OutFile $partial -UseBasicParsing
        if (-not (Assert-Archive $runtime $partial)) { throw 'Downloaded runtime is missing' }
        Move-Item -LiteralPath $partial -Destination $target -Force
        return $target
    } finally {
        Remove-Item -LiteralPath $partial -Force -ErrorAction SilentlyContinue
    }
}

if ($Action -eq 'Clean') {
    if (Test-Path -LiteralPath $StageDirectory) {
        Remove-Item -LiteralPath $StageDirectory -Recurse -Force
    }
    exit 0
}

$archives = @{}
foreach ($runtime in $manifest.runtimes) {
    $archives[$runtime.name] = Fetch-Archive $runtime
}
if ($Action -eq 'Fetch') { exit 0 }

if (Test-Path -LiteralPath $StageDirectory) {
    Remove-Item -LiteralPath $StageDirectory -Recurse -Force
}
New-Item -ItemType Directory -Path $StageDirectory -Force | Out-Null
foreach ($runtime in $manifest.runtimes) {
    $extract = Join-Path ([IO.Path]::GetTempPath()) ("miel-runtime-" + [Guid]::NewGuid().ToString('N'))
    try {
        Expand-Archive -LiteralPath $archives[$runtime.name] -DestinationPath $extract -Force
        $source = Join-Path $extract $runtime.archiveRoot
        if (-not (Test-Path -LiteralPath $source -PathType Container)) {
            throw "Archive root not found for $($runtime.name): $($runtime.archiveRoot)"
        }
        $destination = Join-Path $StageDirectory $runtime.name
        if ($runtime.archiveRoot -eq '.') {
            New-Item -ItemType Directory -Path $destination -Force | Out-Null
            Get-ChildItem -LiteralPath $source -Force | Copy-Item -Destination $destination -Recurse -Force
        } else {
            Copy-Item -LiteralPath $source -Destination $destination -Recurse
        }
        Set-Content -LiteralPath (Join-Path $destination 'VERSION') -Value $runtime.version -NoNewline
    } finally {
        if (Test-Path -LiteralPath $extract) {
            Remove-Item -LiteralPath $extract -Recurse -Force
        }
    }
}
Write-Host "Runtime staging ready: $StageDirectory"
