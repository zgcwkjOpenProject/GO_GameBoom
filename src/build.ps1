#Requires -Version 7.0
<#
.SYNOPSIS
构建炸弹人的 Windows 发布目录

.EXAMPLE
.\build.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dist = Join-Path $Root 'dist'
$FrontendSource = Join-Path $Root 'frontend'
$Utf8 = [System.Text.UTF8Encoding]::new($false)

# 平台打包脚本共用函数

# 去掉 JS 注释（跳过字符串/模板字面量，避免误删 URL 等），用于打包时精简源码
function Remove-JsComments([string]$Text) {
    $sb = [System.Text.StringBuilder]::new()
    $i = 0
    $len = $Text.Length
    $state = 'code'   # code / line / block / sq / dq / tpl / regex
    $lastCodeChar = [char]0
    $regexClass = $false
    while ($i -lt $len) {
        $c = $Text[$i]
        $nx = if ($i + 1 -lt $len) { $Text[$i + 1] } else { '' }
        switch ($state) {
            'line' {
                if ($c -eq "`n" -or $c -eq "`r") { $state = 'code'; [void]$sb.Append($c) }
                $i++
            }
            'block' {
                if ($c -eq '*' -and $nx -eq '/') { $state = 'code'; $i += 2 }
                else { $i++ }
            }
            'sq' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq "'") { $state = 'code'; $i++ }
                else { $i++ }
            }
            'dq' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq '"') { $state = 'code'; $i++ }
                else { $i++ }
            }
            'tpl' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq '`') { $state = 'code'; $lastCodeChar = $c; $i++ }
                else { $i++ }
            }
            'regex' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq '[') { $regexClass = $true; $i++ }
                elseif ($c -eq ']') { $regexClass = $false; $i++ }
                elseif ($c -eq '/' -and -not $regexClass) { $state = 'code'; $lastCodeChar = $c; $i++ }
                else { $i++ }
            }
            default {
                if ($c -eq '/' -and $nx -eq '/') { $state = 'line'; $i += 2 }
                elseif ($c -eq '/' -and $nx -eq '*') { $state = 'block'; $i += 2 }
                elseif ($c -eq "'") { $state = 'sq'; [void]$sb.Append($c); $i++ }
                elseif ($c -eq '"') { $state = 'dq'; [void]$sb.Append($c); $i++ }
                elseif ($c -eq '`') { $state = 'tpl'; [void]$sb.Append($c); $i++ }
                elseif ($c -eq '/' -and (Test-JsRegexStart $lastCodeChar)) {
                    $state = 'regex'
                    $regexClass = $false
                    [void]$sb.Append($c)
                    $i++
                }
                else {
                    [void]$sb.Append($c)
                    if (-not [char]::IsWhiteSpace($c)) { $lastCodeChar = $c }
                    $i++
                }
            }
        }
    }
    return $sb.ToString()
}

# 判断折叠空白时两个字符之间是否必须保留分隔
function Test-JsSpaceNeeded([char]$Left, [char]$Right) {
    $l = [string]$Left
    $r = [string]$Right
    return ($l -match '[A-Za-z0-9_$]' -and $r -match '[A-Za-z0-9_$]')
}

# 判断当前位置的 / 是否更可能是正则字面量起点
function Test-JsRegexStart([char]$Left) {
    if ($Left -eq [char]0) { return $true }
    return '([{=,:;!&|?+-*~^<>'.Contains([string]$Left)
}

# 判断代码区换行是否必须改为分号，保留无分号源码的语句边界
function Test-JsLineBreakNeedsSemicolon([System.Text.StringBuilder]$Text, [char]$Right) {
    if ($Text.Length -eq 0) { return $false }

    $left = $Text[$Text.Length - 1]
    $canEndStatement = [char]::IsLetterOrDigit($left) -or '_$)]}'.Contains([string]$left) -or $left -eq "'" -or $left -eq '"' -or $left -eq [char]96
    if (($left -eq '+' -or $left -eq '-') -and $Text.Length -gt 1 -and $Text[$Text.Length - 2] -eq $left) {
        $canEndStatement = $true
    }
    $continuesExpression = ',.;:)]}([+-*/%&|=!<>?'.Contains([string]$Right)
    return $canEndStatement -and -not $continuesExpression
}

# 压缩 JS：去注释并折叠为单行，保留自动分号插入的语义
function Minify-JsText([string]$Text) {
    $Text = Remove-JsComments $Text
    $sb = [System.Text.StringBuilder]::new()
    $i = 0
    $len = $Text.Length
    $state = 'code'   # code / sq / dq / tpl / regex
    $pendingSpace = $false
    $pendingNewline = $false
    $lastCodeChar = [char]0
    $regexClass = $false

    while ($i -lt $len) {
        $c = $Text[$i]
        $nx = if ($i + 1 -lt $len) { $Text[$i + 1] } else { [char]0 }

        switch ($state) {
            'sq' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq "'") { $state = 'code'; $lastCodeChar = $c; $i++ }
                else { $i++ }
            }
            'dq' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq '"') { $state = 'code'; $lastCodeChar = $c; $i++ }
                else { $i++ }
            }
            'tpl' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq '`') { $state = 'code'; $lastCodeChar = $c; $i++ }
                else { $i++ }
            }
            'regex' {
                [void]$sb.Append($c)
                if ($c -eq '\' -and $nx) { [void]$sb.Append($nx); $i += 2 }
                elseif ($c -eq '[') { $regexClass = $true; $i++ }
                elseif ($c -eq ']') { $regexClass = $false; $i++ }
                elseif ($c -eq '/' -and -not $regexClass) { $state = 'code'; $lastCodeChar = $c; $i++ }
                else { $i++ }
            }
            default {
                if ($c -eq "`r" -or $c -eq "`n") {
                    $pendingSpace = $false
                    $pendingNewline = $true
                    $i++
                    continue
                }
                if ([char]::IsWhiteSpace($c)) {
                    $pendingSpace = $true
                    $i++
                    continue
                }
                if ($pendingNewline -and (Test-JsLineBreakNeedsSemicolon $sb $c)) {
                    [void]$sb.Append(';')
                }
                elseif ($pendingSpace -and $lastCodeChar -ne [char]0 -and (Test-JsSpaceNeeded $lastCodeChar $c)) {
                    [void]$sb.Append(' ')
                }
                $pendingSpace = $false
                $pendingNewline = $false
                [void]$sb.Append($c)
                if ($c -eq "'") { $state = 'sq' }
                elseif ($c -eq '"') { $state = 'dq' }
                elseif ($c -eq '`') { $state = 'tpl' }
                elseif ($c -eq '/' -and (Test-JsRegexStart $lastCodeChar)) {
                    $state = 'regex'
                    $regexClass = $false
                }
                $lastCodeChar = $c
                $i++
            }
        }
    }

    return ($sb.ToString() -replace "`r", '' -replace "`n", '').Trim()
}

# 压缩 CSS：去注释、折叠空白、清理常见分隔符周围的多余空格
function Minify-CssText([string]$Text) {
    $Text = [regex]::Replace($Text, '(?s)/\*.*?\*/', '')
    $Text = [regex]::Replace($Text, '\s+', ' ')
    $Text = [regex]::Replace($Text, '\s*([{}:;,>+~])\s*', '$1')
    $Text = [regex]::Replace($Text, ';}', '}')
    return $Text.Trim()
}

# 压缩 HTML：去注释、折叠标签间空白，适合本仓库这种静态页面
function Minify-HtmlText([string]$Text) {
    $Text = [regex]::Replace($Text, '(?s)<!--.*?-->', '')
    $Text = [regex]::Replace($Text, '>\s+<', '><')
    $Text = [regex]::Replace($Text, '\s{2,}', ' ')
    return $Text.Trim()
}

# 只处理单个文本资源，按后缀决定是否压缩
function Optimize-TextAsset([string]$Path) {
    if (-not (Test-Path $Path)) { return }

    $ext = [System.IO.Path]::GetExtension($Path).ToLowerInvariant()
    $content = [System.IO.File]::ReadAllText($Path)
    $next = $null

    switch ($ext) {
        '.js' {
            $next = Minify-JsText $content
        }
        '.css' {
            $next = Minify-CssText $content
        }
        '.html' {
            $next = Minify-HtmlText $content
        }
        default {
            return
        }
    }

    if ($null -ne $next) {
        [System.IO.File]::WriteAllText($Path, $next, [System.Text.UTF8Encoding]::new($false))
    }
}

# 递归处理目录内的文本资源，供打包脚本直接调用
function Optimize-TextAssets([string]$Root) {
    if (-not (Test-Path $Root)) { return }
    Get-ChildItem -LiteralPath $Root -Recurse -File | ForEach-Object {
        Optimize-TextAsset $_.FullName
    }
}

# Get-AssetVersion 生成本次构建的静态资源版本号
function Get-AssetVersion {
    return "v$(Get-Date -Format 'yyMMddHHmmss')"
}

# Add-AssetVersion 为发布入口和 ES Module 相对导入追加同一缓存版本
function Add-AssetVersion([string]$Root, [string]$Version) {
    $htmlPattern = '(href|src)=(["''])((?!https?:|//|about:|data:)[^"'']*?\.(?:css|js))(?:\?[^"'']*)?'
    Get-ChildItem -LiteralPath $Root -Recurse -File -Filter '*.html' | ForEach-Object {
        $content = [System.IO.File]::ReadAllText($_.FullName)
        $next = [regex]::Replace($content, $htmlPattern, "`$1=`$2`$3?v=$Version")
        if ($next -ne $content) {
            [System.IO.File]::WriteAllText($_.FullName, $next, $Utf8)
        }
    }

    $modulePattern = '((?:from|import)\s*["''])(\./|\.\./)([^"'']*?\.js)(?:\?[^"'']*)?'
    Get-ChildItem -LiteralPath $Root -Recurse -File -Filter '*.js' | ForEach-Object {
        $content = [System.IO.File]::ReadAllText($_.FullName)
        $next = [regex]::Replace($content, $modulePattern, "`$1`$2`$3?v=$Version")
        if ($next -ne $content) {
            [System.IO.File]::WriteAllText($_.FullName, $next, $Utf8)
        }
    }
}

# Add-BuildVersion 把发布版本写进页面和供浏览器轮询的版本文件
function Add-BuildVersion([string]$Root, [string]$Version) {
    $indexPath = Join-Path $Root 'index.html'
    $content = [System.IO.File]::ReadAllText($indexPath)
    $next = $content -replace '(<meta name="app-version" content=")[^"]*', ('$1' + $Version)
    if ($next -ne $content) {
        [System.IO.File]::WriteAllText($indexPath, $next, $Utf8)
    }
    [System.IO.File]::WriteAllText((Join-Path $Root 'version.json'), ('{"version":"' + $Version + '"}'), $Utf8)
}

# Invoke-Checked 执行外部命令并在失败时中止构建
function Invoke-Checked([string]$Name, [scriptblock]$Action) {
    Write-Host "[build] $Name" -ForegroundColor Cyan
    & $Action
    if ($LASTEXITCODE -ne 0) {
        throw "$Name 失败，退出码 $LASTEXITCODE"
    }
}

Push-Location $Root
try {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw '未找到 Go 工具链'
    }
    if (-not (Test-Path -LiteralPath $FrontendSource -PathType Container)) {
        throw '未找到 frontend 目录'
    }

    $assetVersion = Get-AssetVersion
    Write-Host "[version] 发布版本: $assetVersion" -ForegroundColor Green
    Invoke-Checked '检查 Go 工具链' { go version }
    Invoke-Checked '运行测试' { go test ./... }
    Invoke-Checked '运行静态检查' { go vet ./... }

    if (Test-Path -LiteralPath $Dist) {
        Remove-Item -LiteralPath $Dist -Recurse -Force
    }
    New-Item -ItemType Directory -Path $Dist -Force | Out-Null
    $frontendOut = Join-Path $Dist 'frontend'
    Copy-Item -LiteralPath $FrontendSource -Destination $frontendOut -Recurse -Force
    Add-BuildVersion $frontendOut $assetVersion
    Optimize-TextAssets $frontendOut
    Add-AssetVersion $frontendOut $assetVersion
    Copy-Item -LiteralPath (Join-Path $Root 'config.json') -Destination (Join-Path $Dist 'config.json') -Force

    Invoke-Checked '编译服务端' { go build '-ldflags=-s -w' -trimpath -o (Join-Path $Dist 'bomberman.exe') . }
    Write-Host "[done] 发布目录: $Dist" -ForegroundColor Green
}
finally {
    Pop-Location
}
