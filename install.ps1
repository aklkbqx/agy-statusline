$ErrorActionPreference = "Stop"

$Repo = "aklkbqx/agy-statusline"
$InstallDir = if ($env:AGY_STATUSLINE_INSTALL_DIR) { $env:AGY_STATUSLINE_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\agy-statusline" }
$Version = $env:AGY_STATUSLINE_VERSION

$Arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "Arm64" { "arm64" }
  "X64"   { "amd64" }
  default { throw "Unsupported architecture: $_" }
}

if (-not $Version) {
  $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
  $Version = ($release.tag_name -replace '^v', '')
}

$Archive = "agy-statusline_${Version}_windows_${Arch}.zip"
$BaseUrl = "https://github.com/$Repo/releases/download/v$Version"
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("agy-statusline-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TmpDir | Out-Null

try {
  Write-Host "Downloading agy-statusline v$Version (windows/$Arch)..."
  $ArchivePath = Join-Path $TmpDir $Archive
  $SumsPath = Join-Path $TmpDir "SHA256SUMS"
  Invoke-WebRequest -Uri "$BaseUrl/$Archive" -OutFile $ArchivePath
  Invoke-WebRequest -Uri "$BaseUrl/SHA256SUMS" -OutFile $SumsPath

  $ExpectedLine = Get-Content $SumsPath | Where-Object { $_ -match "\b$([regex]::Escape($Archive))$" } | Select-Object -First 1
  if (-not $ExpectedLine) {
    throw "Checksum entry for $Archive not found in SHA256SUMS"
  }
  $Expected = ($ExpectedLine -split '\s+')[0].ToLowerInvariant()
  $Actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($Actual -ne $Expected) {
    throw "Checksum mismatch (expected $Expected, got $Actual)"
  }

  $ExtractDir = Join-Path $TmpDir "extract"
  Expand-Archive -Path $ArchivePath -DestinationPath $ExtractDir -Force
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item -Path (Join-Path $ExtractDir "agy-statusline.exe") -Destination (Join-Path $InstallDir "agy-statusline.exe") -Force

  $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($UserPath -split ';') -notcontains $InstallDir) {
    $NewPath = if ($UserPath) { "$UserPath;$InstallDir" } else { $InstallDir }
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    $env:Path = "$env:Path;$InstallDir"
  }

  Write-Host "Installed $(Join-Path $InstallDir 'agy-statusline.exe')"
  Write-Host "Run: agy-statusline install"
} finally {
  Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
