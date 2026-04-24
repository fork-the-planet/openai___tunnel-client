$ErrorActionPreference = "Stop"

$PluginRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$TunnelClientBin = $null
$Attempts = New-Object System.Collections.Generic.List[string]

function Add-Attempt {
    param([string]$Message)
    $script:Attempts.Add("- $Message")
}

function Test-ExecutableFile {
    param([string]$PathValue)
    if ([string]::IsNullOrWhiteSpace($PathValue)) {
        return $false
    }
    return (Test-Path -LiteralPath $PathValue -PathType Leaf)
}

function Find-AdjacentBinary {
    param([string]$Root)
    $Current = (Resolve-Path -LiteralPath $Root).Path
    while ($true) {
        foreach ($Candidate in @(
            (Join-Path $Current "tunnel-client.exe"),
            (Join-Path $Current "tunnel-client"),
            (Join-Path (Join-Path $Current "bin") "tunnel-client.exe"),
            (Join-Path (Join-Path $Current "bin") "tunnel-client"),
            (Join-Path (Join-Path (Join-Path (Join-Path $Current "bazel-bin") "cmd") "client") "client.exe"),
            (Join-Path (Join-Path (Join-Path (Join-Path $Current "bazel-bin") "cmd") "client") "client"),
            (Join-Path (Join-Path (Join-Path (Join-Path (Join-Path (Join-Path $Current "bazel-bin") "api") "tunnel-client") "cmd") "client") "client.exe"),
            (Join-Path (Join-Path (Join-Path (Join-Path (Join-Path (Join-Path $Current "bazel-bin") "api") "tunnel-client") "cmd") "client") "client")
        )) {
            if (Test-ExecutableFile $Candidate) {
                return $Candidate
            }
        }
        $Parent = Split-Path -Parent $Current
        if ($Parent -eq $Current -or [string]::IsNullOrWhiteSpace($Parent)) {
            break
        }
        $Current = $Parent
    }
    return $null
}

if ($args.Count -ge 1 -and $args[0] -eq "--tunnel-client-bin") {
    if ($args.Count -lt 2) {
        [Console]::Error.WriteLine("error: --tunnel-client-bin requires a value")
        exit 2
    }
    if (Test-ExecutableFile $args[1]) {
        $TunnelClientBin = $args[1]
    } else {
        Add-Attempt "--tunnel-client-bin: $($args[1]) was not an executable file"
    }
    $args = $args[2..($args.Count - 1)]
} else {
    Add-Attempt "--tunnel-client-bin: not provided"
}

if (-not $TunnelClientBin -and $env:TUNNEL_CLIENT_BIN) {
    if (Test-ExecutableFile $env:TUNNEL_CLIENT_BIN) {
        $TunnelClientBin = $env:TUNNEL_CLIENT_BIN
    } else {
        Add-Attempt "TUNNEL_CLIENT_BIN: $($env:TUNNEL_CLIENT_BIN) was not an executable file"
    }
} elseif (-not $TunnelClientBin) {
    Add-Attempt "TUNNEL_CLIENT_BIN: not set"
}

$HintPath = Join-Path $PluginRoot ".tunnel-client-bin"
if (-not $TunnelClientBin -and (Test-Path -LiteralPath $HintPath -PathType Leaf)) {
    $HintedBin = (Get-Content -LiteralPath $HintPath -TotalCount 1).Trim()
    if (Test-ExecutableFile $HintedBin) {
        $TunnelClientBin = $HintedBin
    } else {
        Add-Attempt "installed .tunnel-client-bin hint: $HintedBin was not an executable file"
    }
} elseif (-not $TunnelClientBin) {
    Add-Attempt "installed .tunnel-client-bin hint: not present"
}

if (-not $TunnelClientBin) {
    $AdjacentBin = Find-AdjacentBinary $PluginRoot
    if ($AdjacentBin) {
        $TunnelClientBin = $AdjacentBin
    } else {
        Add-Attempt "adjacent build outputs: no executable tunnel-client binary found next to the plugin"
    }
}

if (-not $TunnelClientBin) {
    $PathCandidate = $null
    foreach ($Name in @("tunnel-client.exe", "tunnel-client")) {
        $Command = Get-Command $Name -ErrorAction SilentlyContinue
        if ($Command) {
            $PathCandidate = $Command.Source
            break
        }
    }
    if ($PathCandidate) {
        $TunnelClientBin = $PathCandidate
    } else {
        Add-Attempt "PATH: no tunnel-client executable found"
    }
}

if ($args.Count -eq 0 -or $args[0] -in @("-h", "--help")) {
    @"
Usage: tunnel_mcp <command> [args]

Routes to native tunnel-client commands:
  create|connect|list|status|stop|disconnect|rm
  admin-profiles <subcommand>

All routed commands default to --json.
"@ | Write-Output
    exit 0
}

$RoutedArgs = @()
switch ($args[0]) {
    "admin-profiles" {
        $RoutedArgs = @("admin-profiles") + $args[1..($args.Count - 1)]
    }
    "create" { $RoutedArgs = @("runtimes", "create") + $args[1..($args.Count - 1)] }
    "connect" { $RoutedArgs = @("runtimes", "connect") + $args[1..($args.Count - 1)] }
    "list" { $RoutedArgs = @("runtimes", "list") + $args[1..($args.Count - 1)] }
    "status" { $RoutedArgs = @("runtimes", "status") + $args[1..($args.Count - 1)] }
    "stop" { $RoutedArgs = @("runtimes", "stop") + $args[1..($args.Count - 1)] }
    "disconnect" { $RoutedArgs = @("runtimes", "disconnect") + $args[1..($args.Count - 1)] }
    "rm" { $RoutedArgs = @("runtimes", "rm") + $args[1..($args.Count - 1)] }
    "remove" { $RoutedArgs = @("runtimes", "rm") + $args[1..($args.Count - 1)] }
    default {
        [Console]::Error.WriteLine("unsupported tunnel_mcp command; use create, connect, list, status, stop, disconnect, rm, or admin-profiles")
        exit 2
    }
}

if (-not ($RoutedArgs -contains "--json")) {
    $RoutedArgs += "--json"
}

if (-not $TunnelClientBin) {
    $AttemptLines = $Attempts -join [Environment]::NewLine
    @"
error: tunnel-client was not found.

Discovery methods tried:
$AttemptLines

Next steps:
- Download a release binary from https://github.com/openai/tunnel-client/releases/latest
- Or clone and build from source from https://github.com/openai/tunnel-client:
  git clone https://github.com/openai/tunnel-client.git
  cd tunnel-client
  go build -o bin/tunnel-client ./cmd/client
  # Windows: go build -o bin/tunnel-client.exe ./cmd/client
- Then point the plugin at the binary with one of:
  - set TUNNEL_CLIENT_BIN to the full path to tunnel-client.exe
  - rerun with --tunnel-client-bin C:\path\to\tunnel-client.exe
- Or reinstall the plugin with --tunnel-client-bin C:\path\to\tunnel-client.exe

Executable naming guidance:
- macOS/Linux: tunnel-client
- Windows: tunnel-client.exe

This plugin does not auto-download, auto-clone, or auto-run remote tunnel-client binaries.
If the user explicitly asks Codex to set up tunnel-client, Codex may clone and build it from the public repo commands above.
"@ | Write-Error
    exit 2
}

& $TunnelClientBin @RoutedArgs
exit $LASTEXITCODE
