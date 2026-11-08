$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repoRef = if ($env:KERN_BRANCH) { $env:KERN_BRANCH } else { 'main' }
if ($repoRef -notmatch '^[A-Za-z0-9._/-]+$') {
    throw "Invalid KERN_BRANCH value: $repoRef"
}

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Convert-WindowsPathToBashLiteral([string]$Path, [string]$DistroName) {
    $wslPath = (& wsl.exe -d $DistroName -- wslpath -a $Path 2>$null | Out-String).Trim()
    if (-not $wslPath -or $LASTEXITCODE -ne 0) {
        throw "Could not map installer path into WSL: $Path"
    }
    return "'" + $wslPath.Replace("'", "'\''") + "'"
}

$distros = @(& wsl.exe --list --quiet 2>$null | ForEach-Object {
    ($_ -replace "`0", '').Trim()
} | Where-Object { $_ -match '^Ubuntu($|-)' })

if ($distros.Count -eq 0) {
    if (-not (Test-Administrator)) {
        throw 'No Ubuntu WSL distribution is installed. Open PowerShell as Administrator and run: wsl --install -d Ubuntu. Reboot if asked, finish Ubuntu first-run setup, then run this installer command again.'
    }

    Write-Host 'Installing Ubuntu in WSL. Windows may require a reboot.' -ForegroundColor Cyan
    & wsl.exe --install -d Ubuntu --no-launch
    if ($LASTEXITCODE -ne 0) {
        throw 'WSL could not install Ubuntu. Reboot if requested, finish `wsl --install -d Ubuntu`, then run this installer command again.'
    }
    $registered = @(& wsl.exe --list --quiet 2>$null | ForEach-Object {
        ($_ -replace "`0", '').Trim()
    } | Where-Object { $_ -eq 'Ubuntu' })
    if ($registered.Count -eq 0) {
        throw 'WSL setup requires a reboot before Ubuntu can be registered. Reboot, finish Ubuntu first-run setup, then rerun this installer command.'
    }
    $Distro = 'Ubuntu'
    & wsl.exe -d $Distro
    if ($LASTEXITCODE -ne 0) {
        throw 'Ubuntu first-run setup did not complete. Finish creating the Linux user, then run this installer command again.'
    }
} else {
    $Distro = ($distros | Where-Object { $_ -eq 'Ubuntu' } | Select-Object -First 1)
    if (-not $Distro) {
        $Distro = ($distros | Select-Object -First 1)
    }
}

& wsl.exe --set-version $Distro 2
if ($LASTEXITCODE -ne 0) {
    throw "Could not ensure WSL2 for '$Distro'. Enable Virtual Machine Platform and reboot, then rerun this command."
}

$installer = Join-Path $env:TEMP "kern-install-$PID.sh"
try {
    $url = "https://raw.githubusercontent.com/druxuss/Kern/$repoRef/install.sh"
    Write-Host "Downloading kern installer for WSL distribution '$Distro' ($repoRef)..." -ForegroundColor Cyan
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $installer
    $bashPath = Convert-WindowsPathToBashLiteral $installer $Distro
    $linuxCommand = "KERN_BRANCH='$repoRef' bash $bashPath"
    & wsl.exe -d $Distro -- bash -lc $linuxCommand
    if ($LASTEXITCODE -ne 0) {
        throw "The Linux installer failed inside WSL with exit code $LASTEXITCODE."
    }
    try {
        $desktop = [Environment]::GetFolderPath('Desktop')
        if ($desktop -and (Test-Path -LiteralPath $desktop)) {
            $shell = New-Object -ComObject WScript.Shell
            $shortcut = $shell.CreateShortcut((Join-Path $desktop 'kern (WSL2).lnk'))
            $shortcut.TargetPath = Join-Path $env:WINDIR 'System32\wsl.exe'
            $shortcut.Arguments = "-d $Distro"
            $shortcut.Description = 'Open the Ubuntu WSL environment with kern installed'
            $shortcut.Save()
        }
    } catch {
        Write-Warning "Could not create the optional WSL desktop shortcut: $_"
    }
    Write-Host 'kern is installed in WSL. Open Ubuntu and run: kern --help' -ForegroundColor Green
} finally {
    Remove-Item -LiteralPath $installer -Force -ErrorAction SilentlyContinue
}
