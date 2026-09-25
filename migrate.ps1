# Migrate an existing komari-agent install to Lite-agent.
# Keeps the original endpoint, node token, and launch flags, then
# uninstalls komari-agent after Lite-agent is running.

function Log-Info { param([string]$Message) Write-Host "$Message" -ForegroundColor Cyan }
function Log-Success { param([string]$Message) Write-Host "$Message" -ForegroundColor Green }
function Log-Warning { param([string]$Message) Write-Host "[WARNING] $Message" -ForegroundColor Yellow }
function Log-Error { param([string]$Message) Write-Host "[ERROR] $Message" -ForegroundColor Red }
function Log-Step { param([string]$Message) Write-Host "$Message" -ForegroundColor Magenta }
function Log-Config { param([string]$Message) Write-Host "- $Message" -ForegroundColor White }

$LegacyServiceName = "komari-agent"
$GitHubRepo = "nuomiiiii/Lite-agent"
$GitHubProxy = ""
$InstallVersion = ""
$EndpointOverride = ""
$Collected = New-Object System.Collections.Generic.List[string]
$SourceDirs = New-Object System.Collections.Generic.List[string]
$InstallDir = Join-Path $Env:ProgramFiles "Lite"
$LegacyDir = Join-Path $Env:ProgramFiles "Komari"

if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)) {
    Log-Error "Please run this script as Administrator."
    exit 1
}

for ($i = 0; $i -lt $args.Count; $i++) {
    switch ($args[$i]) {
        "--install-ghproxy" {
            if (($i + 1) -ge $args.Count -or [string]::IsNullOrWhiteSpace($args[$i + 1]) -or $args[$i + 1].StartsWith("-")) {
                Log-Error "--install-ghproxy requires a value"
                exit 1
            }
            $GitHubProxy = $args[$i + 1]; $i++; continue
        }
        { $_ -like "--install-ghproxy=*" } {
            $GitHubProxy = $_.Substring("--install-ghproxy=".Length)
            if ([string]::IsNullOrWhiteSpace($GitHubProxy)) {
                Log-Error "--install-ghproxy requires a value"
                exit 1
            }
            continue
        }
        "--install-version" {
            if (($i + 1) -ge $args.Count -or [string]::IsNullOrWhiteSpace($args[$i + 1]) -or $args[$i + 1].StartsWith("-")) {
                Log-Error "--install-version requires a value"
                exit 1
            }
            $InstallVersion = $args[$i + 1]; $i++; continue
        }
        { $_ -like "--install-version=*" } {
            $InstallVersion = $_.Substring("--install-version=".Length)
            if ([string]::IsNullOrWhiteSpace($InstallVersion)) {
                Log-Error "--install-version requires a value"
                exit 1
            }
            continue
        }
        "--install-dir" {
            Log-Error "Migration always uses the default Lite-agent directory and service name."
            Log-Error "Run this script without --install-dir so komari-agent can be uninstalled afterward."
            exit 1
        }
        "--install-service-name" {
            Log-Error "Migration always uses the default Lite-agent directory and service name."
            exit 1
        }
        "-e" {
            if (($i + 1) -ge $args.Count -or [string]::IsNullOrWhiteSpace($args[$i + 1]) -or $args[$i + 1].StartsWith("-")) {
                Log-Error "-e requires a panel URL"
                exit 1
            }
            $EndpointOverride = $args[$i + 1]; $i++; continue
        }
        "--endpoint" {
            if (($i + 1) -ge $args.Count -or [string]::IsNullOrWhiteSpace($args[$i + 1]) -or $args[$i + 1].StartsWith("-")) {
                Log-Error "--endpoint requires a panel URL"
                exit 1
            }
            $EndpointOverride = $args[$i + 1]; $i++; continue
        }
        { $_ -like "--endpoint=*" } {
            $EndpointOverride = $_.Substring("--endpoint=".Length)
            if ([string]::IsNullOrWhiteSpace($EndpointOverride)) {
                Log-Error "--endpoint requires a panel URL"
                exit 1
            }
            continue
        }
        "-t" {
            Log-Error "Do not pass a new token. This script keeps the existing komari-agent key."
            exit 1
        }
        "--token" {
            Log-Error "Do not pass a new token. This script keeps the existing komari-agent key."
            exit 1
        }
        { $_ -like "--token=*" } {
            Log-Error "Do not pass a new token. This script keeps the existing komari-agent key."
            exit 1
        }
        Default {
            Log-Error "Unknown argument: $($args[$i])"
            Log-Error "Usage: migrate.ps1 [--endpoint URL] [--install-ghproxy URL] [--install-version VER]"
            exit 1
        }
    }
}

function Add-SourceDir {
    param([string]$Dir)
    if ([string]::IsNullOrWhiteSpace($Dir) -or -not (Test-Path $Dir)) { return }
    $full = [System.IO.Path]::GetFullPath($Dir)
    foreach ($existing in $SourceDirs) {
        if ([string]::Equals($existing, $full, [System.StringComparison]::OrdinalIgnoreCase)) { return }
    }
    $SourceDirs.Add($full) | Out-Null
}

function ConvertFrom-ArgString {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $clean = $Text -replace "`0", ""
    return @($clean.Trim() -split '\s+' | Where-Object { $_ -ne '' })
}

function ConvertFrom-QuotedCommand {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $clean = ($Text -replace "`0", "").Trim()
    $rest = ""
    if ($clean -match '^"([^"]+)"\s*(.*)$') {
        Add-SourceDir ([System.IO.Path]::GetDirectoryName($matches[1]))
        $rest = $matches[2]
    }
    elseif ($clean -match '^(\S+)\s*(.*)$') {
        Add-SourceDir ([System.IO.Path]::GetDirectoryName($matches[1]))
        $rest = $matches[2]
    }
    else {
        return @()
    }
    return @(ConvertFrom-ArgString $rest)
}

function Test-HasNamedFlag {
    param([string]$Name, [string]$Short = "")
    $items = @($Collected)
    foreach ($item in $items) {
        if ($Short -and $item -eq "-$Short") { return $true }
        if ($item -eq "--$Name" -or $item -like "--$Name=*") { return $true }
    }
    return $false
}

function Get-NamedFlagValue {
    param([string]$Name, [string]$Short = "")
    $items = @($Collected)
    for ($i = 0; $i -lt $items.Count; $i++) {
        $item = $items[$i]
        if ($Short -and $item -eq "-$Short") {
            if (($i + 1) -lt $items.Count) { return $items[$i + 1] }
            return ""
        }
        if ($item -eq "--$Name") {
            if (($i + 1) -lt $items.Count) { return $items[$i + 1] }
            return ""
        }
        if ($item.StartsWith("--$Name=")) {
            return $item.Substring("--$Name=".Length)
        }
    }
    return ""
}

function Set-NamedFlag {
    param([string]$Name, [string]$Short, [string]$Value)
    $items = @($Collected)
    $out = New-Object System.Collections.Generic.List[string]
    $skip = $false
    foreach ($item in $items) {
        if ($skip) { $skip = $false; continue }
        if ($Short -and $item -eq "-$Short") { $skip = $true; continue }
        if ($item -eq "--$Name") { $skip = $true; continue }
        if ($item -like "--$Name=*") { continue }
        $out.Add($item) | Out-Null
    }
    if ($Short) {
        $out.Add("-$Short") | Out-Null
        $out.Add($Value) | Out-Null
    }
    else {
        $out.Add("--$Name") | Out-Null
        $out.Add($Value) | Out-Null
    }
    $script:Collected = $out
}

function Remove-NamedFlag {
    param([string]$Name)
    $items = @($Collected)
    $out = New-Object System.Collections.Generic.List[string]
    $skip = $false
    foreach ($item in $items) {
        if ($skip) { $skip = $false; continue }
        if ($item -eq "--$Name") { $skip = $true; continue }
        if ($item -like "--$Name=*") { continue }
        $out.Add($item) | Out-Null
    }
    $script:Collected = $out
}

function Redact-AgentArgs {
    param([string[]]$ArgList)
    $out = New-Object System.Collections.Generic.List[string]
    $hideNext = $false
    foreach ($item in @($ArgList)) {
        if ($hideNext) {
            $out.Add("***") | Out-Null
            $hideNext = $false
            continue
        }
        if ($item -eq "-t" -or $item -eq "--token" -or $item -eq "--cf-access-client-secret") {
            $out.Add($item) | Out-Null
            $hideNext = $true
            continue
        }
        if ($item -like "--token=*") { $out.Add("--token=***") | Out-Null; continue }
        if ($item -like "--cf-access-client-secret=*") { $out.Add("--cf-access-client-secret=***") | Out-Null; continue }
        $out.Add($item) | Out-Null
    }
    return ($out -join " ")
}

function Read-JsonField {
    param([string]$Path, [string]$Name)
    if (-not (Test-Path $Path)) { return "" }
    try {
        $json = Get-Content -LiteralPath $Path -Raw -ErrorAction Stop | ConvertFrom-Json
        $value = $json.$Name
        if ($null -eq $value) { return "" }
        return [string]$value
    }
    catch {
        return ""
    }
}

function Find-Nssm {
    $cmd = Get-Command nssm -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($candidate in @(
            (Join-Path $LegacyDir "nssm.exe"),
            (Join-Path $InstallDir "nssm.exe")
        )) {
        if (Test-Path $candidate) { return $candidate }
    }
    return ""
}

function Adopt-EnvKey {
    param([string]$Key, [string]$Value)
    if ([string]::IsNullOrWhiteSpace($Value)) { return }
    switch ($Key) {
        "AGENT_TOKEN" {
            if (-not (Test-HasNamedFlag "token" "t")) {
                $Collected.Add("-t") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
        "AGENT_ENDPOINT" {
            if (-not (Test-HasNamedFlag "endpoint" "e")) {
                $Collected.Add("-e") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
        "AGENT_MONTH_ROTATE" {
            if (-not (Test-HasNamedFlag "month-rotate")) {
                $Collected.Add("--month-rotate") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
        "AGENT_INTERVAL" {
            if (-not (Test-HasNamedFlag "interval")) {
                $Collected.Add("--interval") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
        "AGENT_INCLUDE_NICS" {
            if (-not (Test-HasNamedFlag "include-nics")) {
                $Collected.Add("--include-nics") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
        "AGENT_EXCLUDE_NICS" {
            if (-not (Test-HasNamedFlag "exclude-nics")) {
                $Collected.Add("--exclude-nics") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
        "AGENT_CONFIG_FILE" {
            if (-not (Test-HasNamedFlag "config")) {
                $Collected.Add("--config") | Out-Null
                $Collected.Add($Value) | Out-Null
            }
        }
    }
}

function Test-ServiceExists {
    param([string]$Name)
    $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
    return [bool]$svc
}

$legacyFound = $false
$nssmPath = Find-Nssm
if (Test-ServiceExists $LegacyServiceName) {
    $legacyFound = $true
    if ($nssmPath) {
        $params = & $nssmPath get $LegacyServiceName AppParameters 2>$null | Out-String
        if ($params -notmatch "Can't open service" -and $params -notmatch "does not exist") {
            foreach ($item in @(ConvertFrom-ArgString $params)) { $Collected.Add($item) | Out-Null }
        }
        $appDir = & $nssmPath get $LegacyServiceName AppDirectory 2>$null | Out-String
        Add-SourceDir ($appDir.Trim())
        $appPath = & $nssmPath get $LegacyServiceName Application 2>$null | Out-String
        if ($appPath) { Add-SourceDir ([System.IO.Path]::GetDirectoryName($appPath.Trim())) }
        foreach ($envName in @("AppEnvironmentExtra", "AppEnvironment")) {
            $envExtra = & $nssmPath get $LegacyServiceName $envName 2>$null | Out-String
            foreach ($line in @(($envExtra -replace "`0", "`n") -split '[\r\n]+')) {
                $line = $line.Trim()
                if ($line -match '^(AGENT_[A-Z0-9_]+)=(.*)$') {
                    Adopt-EnvKey -Key $matches[1] -Value $matches[2].Trim().Trim('"')
                }
            }
        }
    }
    else {
        $svc = Get-CimInstance Win32_Service -Filter "Name='$LegacyServiceName'" -ErrorAction SilentlyContinue
        if ($svc -and $svc.PathName) {
            foreach ($item in @(ConvertFrom-QuotedCommand $svc.PathName)) { $Collected.Add($item) | Out-Null }
        }
    }
}

Add-SourceDir $LegacyDir

if (-not $legacyFound) {
    Log-Error "No running komari-agent install was found."
    Log-Error "This script reads the existing komari-agent service and keeps its node token."
    exit 1
}

$sidecarToken = ""
$sidecarEndpoint = ""
$sidecarUuid = ""
foreach ($dir in @($SourceDirs)) {
    if (-not $sidecarToken) { $sidecarToken = Read-JsonField (Join-Path $dir "auto-discovery.json") "token" }
    if (-not $sidecarUuid) { $sidecarUuid = Read-JsonField (Join-Path $dir "auto-discovery.json") "uuid" }
    if (-not $sidecarToken) { $sidecarToken = Read-JsonField (Join-Path $dir "node.json") "token" }
    if (-not $sidecarEndpoint) { $sidecarEndpoint = Read-JsonField (Join-Path $dir "auto-discovery.json") "endpoint" }
    if (-not $sidecarEndpoint) { $sidecarEndpoint = Read-JsonField (Join-Path $dir "node.json") "endpoint" }
}

if (-not (Test-HasNamedFlag "token" "t")) {
    if ($sidecarToken) {
        $Collected.Add("-t") | Out-Null
        $Collected.Add($sidecarToken) | Out-Null
    }
    else {
        Log-Error "Could not find the existing node token in komari-agent arguments or identity files."
        Log-Error "Migration will not create a new Lite node. Keep the original key in place and try again."
        exit 1
    }
}

if ($EndpointOverride) {
    Set-NamedFlag -Name "endpoint" -Short "e" -Value $EndpointOverride
}
elseif (-not (Test-HasNamedFlag "endpoint" "e")) {
    if ($sidecarEndpoint) {
        $Collected.Add("-e") | Out-Null
        $Collected.Add($sidecarEndpoint) | Out-Null
    }
    else {
        Log-Error "Could not find the existing panel address. Pass --endpoint with your Lite URL."
        exit 1
    }
}

Remove-NamedFlag "auto-discovery"

if (Test-HasNamedFlag "config") {
    $cfg = Get-NamedFlagValue -Name "config"
    if ($cfg -and (Test-Path $cfg)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $dest = Join-Path $InstallDir ([System.IO.Path]::GetFileName($cfg))
        if ($cfg -ne $dest -and -not (Test-Path $dest)) {
            Log-Info "Copying config $([System.IO.Path]::GetFileName($cfg)) to $InstallDir"
            Copy-Item $cfg $dest -Force
        }
        if (Test-Path $dest) {
            Set-NamedFlag -Name "config" -Short "" -Value $dest
        }
    }
}

Log-Step "Copying sidecar files from the detected komari-agent directories..."
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
foreach ($dir in @($SourceDirs)) {
    if ($dir -eq $InstallDir) { continue }
    foreach ($name in @("auto-discovery.json", "net_static.json", "net_static.json.bak", "nssm.exe", "node.json", "remote-control.state", "config.json")) {
        $src = Join-Path $dir $name
        $dst = Join-Path $InstallDir $name
        if ((Test-Path $src) -and -not (Test-Path $dst)) {
            Log-Info "Copying $name from $dir"
            Copy-Item $src $dst -Force
        }
    }
}

if (-not (Test-HasNamedFlag "config") -and (Test-Path (Join-Path $InstallDir "config.json"))) {
    Set-NamedFlag -Name "config" -Short "" -Value (Join-Path $InstallDir "config.json")
}

Write-Host "==========================================="
Write-Host "    Lite Agent Migration Script"
Write-Host "==========================================="
Log-Config "Keeping the original node token and installing Lite-agent"
Log-Config "Endpoint: $(Get-NamedFlagValue -Name endpoint -Short e)"
if ($sidecarUuid) { Log-Config "Saved UUID: $sidecarUuid" }
Log-Config "Arguments: $(Redact-AgentArgs @($Collected))"

$installer = ""
if ($PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot "install.ps1"))) {
    $installer = Join-Path $PSScriptRoot "install.ps1"
}
else {
    $rawUrl = "https://raw.githubusercontent.com/$GitHubRepo/main/install.ps1"
    if ($GitHubProxy) {
        $rawUrl = "$($GitHubProxy.TrimEnd('/'))/https://raw.githubusercontent.com/$GitHubRepo/main/install.ps1"
    }
    $installer = Join-Path $env:TEMP "lite-agent-install.ps1"
    Log-Step "Downloading Lite-agent install.ps1..."
    try {
        Invoke-WebRequest -Uri $rawUrl -OutFile $installer -UseBasicParsing
    }
    catch {
        Log-Error "Failed to download install.ps1: $_"
        exit 1
    }
    if (-not (Test-Path $installer) -or (Get-Item $installer).Length -le 0) {
        Log-Error "Downloaded install.ps1 is empty"
        exit 1
    }
}

$installArgs = New-Object System.Collections.Generic.List[string]
if ($GitHubProxy) {
    $installArgs.Add("--install-ghproxy") | Out-Null
    $installArgs.Add($GitHubProxy) | Out-Null
}
if ($InstallVersion) {
    $installArgs.Add("--install-version") | Out-Null
    $installArgs.Add($InstallVersion) | Out-Null
}
foreach ($item in @($Collected)) { $installArgs.Add($item) | Out-Null }

Log-Step "Installing Lite-agent with the original node key..."
& $installer @($installArgs)
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
Log-Success "Migration finished. Confirm the node is online in Lite."
