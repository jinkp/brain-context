# brain-context installer for Windows
# Usage: irm https://raw.githubusercontent.com/jinkp/brain-context/master/install.ps1 | iex
#
# Security: downloads SHA256 checksums from the release and verifies the
# binary before installing.  Set $env:BRAIN_SKIP_CHECKSUM=1 to bypass
# (not recommended).

param(
    [string]$InstallDir = "$env:LOCALAPPDATA\brain-context\bin",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$Repo = "jinkp/brain-context"
$BinaryName = "brain.exe"

function Write-Info    { param($msg) Write-Host "  -> $msg" -ForegroundColor Cyan }
function Write-Success { param($msg) Write-Host "  OK $msg" -ForegroundColor Green }
function Write-Warn    { param($msg) Write-Host "  !! $msg" -ForegroundColor Yellow }
function Write-Fail    { param($msg) Write-Host "  XX $msg" -ForegroundColor Red; exit 1 }

# ── Get latest release ────────────────────────────────────────────────────────
function Get-LatestVersion {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing
        return $release.tag_name
    } catch {
        Write-Warn "Could not detect latest version — using v0.1.0"
        return "v0.1.0"
    }
}

# ── Download and verify binary ─────────────────────────────────────────────────
function Install-Binary {
    param($Version)

    $Filename = "brain-windows-amd64.exe"
    $Url = "https://github.com/$Repo/releases/download/$Version/$Filename"
    $ChecksumsUrl = "https://github.com/$Repo/releases/download/$Version/checksums.txt"
    $TmpFile = Join-Path $env:TEMP "brain-install-$([System.IO.Path]::GetRandomFileName()).exe"

    Write-Info "Downloading brain-context $Version for Windows (amd64)..."

    try {
        Invoke-WebRequest -Uri $Url -OutFile $TmpFile -UseBasicParsing
    } catch {
        Write-Fail "Download failed: $Url`nError: $_"
    }

    # ── SHA256 verification ───────────────────────────────────────────────
    if ($env:BRAIN_SKIP_CHECKSUM -eq "1") {
        Write-Warn "Checksum verification skipped (BRAIN_SKIP_CHECKSUM=1)"
    } else {
        Write-Info "Verifying SHA256 checksum..."
        try {
            $checksumContent = (Invoke-WebRequest -Uri $ChecksumsUrl -UseBasicParsing).Content
            $line = ($checksumContent -split "`n") | Where-Object { $_ -match $Filename }

            if ($line) {
                $expectedHash = ($line -split "\s+")[0].Trim()
                $actualHash = (Get-FileHash -Path $TmpFile -Algorithm SHA256).Hash.ToLower()

                if ($actualHash -ne $expectedHash) {
                    Remove-Item -Path $TmpFile -Force -ErrorAction SilentlyContinue
                    Write-Fail "SHA256 checksum mismatch!`n  Expected: $expectedHash`n  Got:      $actualHash`n`nThe downloaded binary may have been tampered with. Aborting."
                }

                Write-Success "SHA256 checksum verified"
            } else {
                Write-Warn "Checksum entry not found for $Filename - skipping verification"
            }
        } catch {
            Write-Warn "checksums.txt not available for $Version - skipping verification"
            Write-Warn "Consider upgrading to a newer release that includes checksums"
        }
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    Copy-Item -Path $TmpFile -Destination "$InstallDir\$BinaryName" -Force
    Remove-Item -Path $TmpFile -Force -ErrorAction SilentlyContinue
}

# ── Add to PATH ───────────────────────────────────────────────────────────────
function Add-ToPath {
    $currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")

    if ($currentPath -like "*$InstallDir*") {
        return
    }

    [Environment]::SetEnvironmentVariable(
        "PATH",
        "$currentPath;$InstallDir",
        "User"
    )

    # Also update current session
    $env:PATH = "$env:PATH;$InstallDir"
    Write-Info "Added $InstallDir to user PATH"
}

# ── Main ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  brain-context installer" -ForegroundColor White
Write-Host "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor DarkGray
Write-Host ""

if (-not $Version) {
    $Version = Get-LatestVersion
}

Install-Binary -Version $Version
Add-ToPath

$BrainPath = "$InstallDir\$BinaryName"
Write-Host ""
Write-Success "brain-context $Version installed at $BrainPath"
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor White
Write-Host ""
Write-Host "  1. Login with your team token:"
Write-Host "     brain login --api https://your-api-url --token brn_tenant_xxx"
Write-Host ""
Write-Host "  2. Register your project:"
Write-Host "     brain register --project my-project --repo .\my-project ``"
Write-Host "       --embedder openai --model text-embedding-3-large --api-key `$env:OPENAI_KEY"
Write-Host ""
Write-Host "  3. Index your project:"
Write-Host "     brain index --project my-project"
Write-Host ""
Write-Host "  4. Configure your AI client:"
Write-Host "     brain tui                # Interactive setup"
Write-Host "     brain tui opencode       # OpenCode"
Write-Host "     brain tui claude         # Claude Code"
Write-Host "     brain tui cursor         # Cursor"
Write-Host "     brain tui all            # All at once"
Write-Host ""
Write-Host "  Note: Open a new terminal for PATH changes to take effect." -ForegroundColor Yellow
Write-Host ""
