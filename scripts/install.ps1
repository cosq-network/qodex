param(
  [string]$Version = "latest",
  [string]$InstallDir = "$env:LOCALAPPDATA\\Programs\\qodex"
)

$ErrorActionPreference = "Stop"

$repo = "benoybose/qodex"
$binary = "qodex.exe"

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
  "X64" { $arch = "x86_64" }
  "Arm64" { $arch = "arm64" }
  default { throw "Unsupported architecture: $($_)" }
}

if ($Version -eq "latest") {
  $url = "https://github.com/$repo/releases/latest/download/qodex_windows_$arch.zip"
} else {
  if ($Version.StartsWith("v")) {
    $tag = $Version
  } else {
    $tag = "v$Version"
  }
  $url = "https://github.com/$repo/releases/download/$tag/qodex_windows_$arch.zip"
}

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("qodex-" + [System.Guid]::NewGuid().ToString("N"))
$zipPath = Join-Path $tmpDir "qodex.zip"

New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

try {
  Invoke-WebRequest -Uri $url -OutFile $zipPath
  Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force
  Copy-Item -Path (Join-Path $tmpDir $binary) -Destination (Join-Path $InstallDir $binary) -Force

  $machinePath = [Environment]::GetEnvironmentVariable("Path", "User")
  $pathParts = @()
  if ($machinePath) {
    $pathParts = $machinePath -split ";"
  }
  if ($pathParts -notcontains $InstallDir) {
    $updatedPath = (($pathParts + $InstallDir) | Where-Object { $_ -ne "" }) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $updatedPath, "User")
    Write-Host "Added $InstallDir to your user PATH. Open a new terminal to use qodex."
  }

  Write-Host "Installed qodex to $(Join-Path $InstallDir $binary)"
} finally {
  Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
