$ErrorActionPreference = 'Stop'

$repository = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$tags = @(& git -C $repository tag --points-at HEAD --list 'release/v*' --sort=-version:refname)
if ($LASTEXITCODE -ne 0) {
    throw 'Unable to read Git tags for the build version.'
}

foreach ($tag in $tags) {
    if ($tag -match '^release\/(v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$') {
        Write-Output $Matches[1]
        exit 0
    }
}

Write-Output 'dev'
