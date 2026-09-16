<#
.SYNOPSIS
    快速修改 file-cleaner 的版本号（0.1.0 这种）。

.DESCRIPTION
    单次改动三处，保持本地版本与前端包版本一致：
      VERSION                     —— 以后端二进制/发版为准的版本源
      web/package.json            —— 前端包版本
      web/package-lock.json       —— 锁文件里的根包版本
    可选 -Commit / -Tag 顺手提交、打 tag；-DryRun 只看不改。

.PARAMETER Version
    目标版本号，形如 0.1.0（带不带前导 v 都行）。

.PARAMETER Bump
    在当前版本上自增：major / minor / patch。

.EXAMPLE
    .\set-version.ps1 0.1.0

.EXAMPLE
    .\set-version.ps1 -Bump patch -Commit

.EXAMPLE
    .\set-version.ps1 1.0.0 -DryRun
#>
[CmdletBinding(DefaultParameterSetName = 'Set')]
param(
    [Parameter(ParameterSetName = 'Set', Position = 0)]
    [string]$Version,

    [Parameter(ParameterSetName = 'Bump', Mandatory = $true)]
    [ValidateSet('major', 'minor', 'patch')]
    [string]$Bump,

    # 提交 VERSION / package.json / package-lock.json
    [switch]$Commit,

    # 提交后打一个 vX.Y.Z 的 tag（不会自动 push）
    [switch]$Tag,

    # 允许把版本号写成和当前一样
    [switch]$Force,

    # 只预览不落盘
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

$root = $PSScriptRoot
if (-not $root) { $root = (Get-Location).Path }

$versionFile = Join-Path $root 'VERSION'
$packageJson = Join-Path (Join-Path $root 'web') 'package.json'
$packageLock = Join-Path (Join-Path $root 'web') 'package-lock.json'

function Read-Text([string]$path) {
    return [System.IO.File]::ReadAllText($path, [System.Text.Encoding]::UTF8)
}

function Write-Text([string]$path, [string]$text) {
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($path, $text, $utf8NoBom)
}

function Get-CurrentVersion {
    if (Test-Path -LiteralPath $versionFile) {
        $v = (Read-Text $versionFile).Trim()
        if ($v) { return $v }
    }
    if (Test-Path -LiteralPath $packageJson) {
        $m = [regex]::Match((Read-Text $packageJson), '"version"\s*:\s*"([^"]+)"')
        if ($m.Success) { return $m.Groups[1].Value }
    }
    return '0.0.0'
}

function Get-NextVersion([string]$current, [string]$part) {
    if ($current -notmatch '^\d+\.\d+\.\d+') {
        throw "当前版本号 '$current' 不能自增，请直接给目标版本号，例如：.\set-version.ps1 0.1.0"
    }
    $core = ($current -split '[-+]')[0]
    $nums = @($core.Split('.') | ForEach-Object { [int]$_ })
    while ($nums.Count -lt 3) { $nums += 0 }
    $major = $nums[0]; $minor = $nums[1]; $patch2 = $nums[2]
    switch ($part) {
        'major' { $major = $major + 1; $minor = 0; $patch2 = 0 }
        'minor' { $minor = $minor + 1; $patch2 = 0 }
        default { $patch2 = $patch2 + 1 }
    }
    return "$major.$minor.$patch2"
}

# ---------- 目标版本 ----------
$current = Get-CurrentVersion

if ($PSCmdlet.ParameterSetName -eq 'Bump') {
    $target = Get-NextVersion $current $Bump
} else {
    $target = $Version
}

if (-not $target) {
    throw '请给一个版本号，例如：.\set-version.ps1 0.1.0（或者 .\set-version.ps1 -Bump patch）'
}

$target = $target.Trim().TrimStart('v', 'V')
if ($target -notmatch '^\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$') {
    throw "版本号格式不对：'$target'，应形如 0.1.0"
}
if ($target -eq $current -and -not $Force) {
    throw "版本号已经是 $current 了，确实要重写请加 -Force"
}

Write-Host ''
Write-Host ("当前版本 : {0}" -f $current)
Write-Host ("目标版本 : {0}" -f $target) -ForegroundColor Cyan
Write-Host ''

# ---------- 计算改动 ----------
$versionText = "$target`n"

$pkgChanged = $false
$pkgNew = $null
if (Test-Path -LiteralPath $packageJson) {
    $pkgText = Read-Text $packageJson
    $pkgPattern = [regex]'(?m)^(\s*)"version"\s*:\s*"[^"]*"'
    $pkgNew = $pkgPattern.Replace($pkgText, ('$1"version": "' + $target + '"'), 1)
    $pkgChanged = ($pkgNew -ne $pkgText)
}

$lockMatches = 0
$lockNew = $null
if (Test-Path -LiteralPath $packageLock) {
    $lockText = Read-Text $packageLock
    $lockPattern = [regex]'(?m)^(\s*)"version"\s*:\s*"[^"]*"'
    $lockMatches = $lockPattern.Matches($lockText).Count
    if ($lockMatches -gt 0) {
        $lockNew = $lockPattern.Replace($lockText, ('$1"version": "' + $target + '"'), 2)
    }
}

if (Test-Path -LiteralPath $versionFile) { $versionResult = "改写为 $target" } else { $versionResult = "新建为 $target" }
if ($pkgChanged) { $pkgResult = "version → $target" } else { $pkgResult = '未找到 version 字段，跳过（先确认文件存在、且里面确实有 version 字段）' }
if ($lockMatches -gt 0) {
    $lockResult = "替换 $([Math]::Min($lockMatches, 2)) 处根包 version → $target"
} else {
    $lockResult = '未找到根包 version，跳过'
}

$rows = @(
    [pscustomobject]@{ File = 'VERSION'; Result = $versionResult },
    [pscustomobject]@{ File = 'web/package.json'; Result = $pkgResult },
    [pscustomobject]@{ File = 'web/package-lock.json'; Result = $lockResult }
)
($rows | Format-Table -AutoSize | Out-String -Width 200) | Write-Host

# ---------- 落盘 ----------
if ($DryRun) {
    Write-Host '（-DryRun：没有写入任何文件）' -ForegroundColor Yellow
} else {
    Write-Text $versionFile $versionText
    if ($pkgChanged) { Write-Text $packageJson $pkgNew }
    if ($lockMatches -gt 0) { Write-Text $packageLock $lockNew }
    Write-Host ("已写入版本号 {0}" -f $target) -ForegroundColor Green
}

# ---------- 可选提交 / 打 tag ----------
if ($Commit -or $Tag) {
    if ($DryRun) {
        Write-Host '（-DryRun：跳过 git 操作）' -ForegroundColor Yellow
    } else {
        Push-Location $root
        try {
            git rev-parse --git-dir | Out-Null
            git add -- VERSION web/package.json web/package-lock.json
            if ($Commit) {
                git commit -m ("chore(release): v{0}" -f $target)
            }
            if ($Tag) {
                git tag -a ("v{0}" -f $target) -m ("Release v{0}" -f $target)
            }
        } finally {
            Pop-Location
        }
    }
}

# ---------- 下一步 ----------
Write-Host ''
Write-Host '下一步：' -ForegroundColor Cyan
if (-not $Commit) {
    Write-Host ('  1) 提交：git add -A; git commit -m "chore(release): v{0}"' -f $target)
}
Write-Host ('  2) 推送分支：git push origin main')
Write-Host ('  3) 到 GitHub → Actions → Release → Run workflow，版本号填 {0}' -f $target)
Write-Host '     （workflow 会用 main 分支自动建 vX.Y.Z tag，别手动重复推同一个 tag）'
Write-Host ('  4) 跑完后可用：docker run -d --name file-cleaner -p 6888:6888 -v /mnt:/mnt ghcr.io/<owner>/<repo>:{0}' -f $target)
