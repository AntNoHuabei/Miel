param(
    [ValidateSet('Display', 'Numeric')]
    [string]$Format = 'Display'
)

$ErrorActionPreference = 'Stop'

$repository = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$tags = @(& git -C $repository tag --points-at HEAD --list 'release/v*' --sort=-version:refname)
if ($LASTEXITCODE -ne 0) {
    throw 'Unable to read Git tags for the build version.'
}

foreach ($tag in $tags) {
    if ($tag -match '^release\/(v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$') {
        $version = $Matches[1]
        if ($Format -eq 'Numeric' -and $version -match '^v(\d+\.\d+\.\d+)') {
            Write-Output $Matches[1]
        } else {
            Write-Output $version
        }
        exit 0
    }
}

if ($Format -eq 'Numeric') {
    Write-Output '0.0.0'
} else {
    Write-Output 'dev'
}
