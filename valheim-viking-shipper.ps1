# Valheim Viking Hero Armory Shipper
# Safely inspects your local Valheim character (.fch) save file (including Steam Cloud saves)
# and syncs your viking's gear, biome era, skills, and battle stats to the Mead Hall Dashboard.

param(
  [string]$DashboardUrl = "https://valheim-dash.vercel.app",
  [string]$CharacterName = ""
)

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  Valheim Mead Hall - Viking Hero Armory Sync" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "Target Endpoint: $DashboardUrl/api/armory/sync" -ForegroundColor Gray
Write-Host ""

# Standard Valheim save search locations (Local AppData + Steam Cloud cache)
$searchPaths = @(
  "$env:USERPROFILE\AppData\LocalLow\IronGate\Valheim\characters",
  "$env:USERPROFILE\AppData\LocalLow\IronGate\Valheim\characters_local"
)

# Auto-detect Steam Cloud save cache location
$steamPath = (Get-ItemProperty -Path "HKCU:\Software\Valve\Steam" -ErrorAction SilentlyContinue).SteamPath
if (-not $steamPath) {
  $steamPath = "C:\Program Files (x86)\Steam"
}
if (Test-Path $steamPath) {
  $steamUserDirs = Get-ChildItem -Path "$steamPath\userdata" -Directory -ErrorAction SilentlyContinue
  foreach ($u in $steamUserDirs) {
    $cloudCharPath = Join-Path $u.FullName "892970\remote\characters"
    if (Test-Path $cloudCharPath) {
      $searchPaths += $cloudCharPath
    }
  }
}

$candidateFiles = @()
foreach ($p in $searchPaths) {
  if (Test-Path $p) {
    $files = Get-ChildItem -Path $p -Filter "*.fch" -File -ErrorAction SilentlyContinue | Where-Object { $_.BaseName -notmatch '_backup_' }
    if ($files) {
      $candidateFiles += $files
    }
  }
}

if ($candidateFiles.Count -eq 0) {
  Write-Warning "Could not find any Valheim character (.fch) save files in standard locations or Steam Cloud cache:"
  foreach ($p in $searchPaths) {
    Write-Host "  - $p" -ForegroundColor DarkGray
  }
  Write-Host "Make sure you have launched Valheim and created at least one Viking on this PC." -ForegroundColor Yellow
  return
}

# Filter by character name if supplied, else pick most recently modified save
$targetFile = $null
if (-not [string]::IsNullOrWhiteSpace($CharacterName)) {
  $targetFile = $candidateFiles | Where-Object { $_.BaseName -like "*$CharacterName*" } | Sort-Object LastWriteTime -Descending | Select-Object -First 1
}

if (-not $targetFile) {
  $targetFile = $candidateFiles | Sort-Object LastWriteTime -Descending | Select-Object -First 1
}

Write-Host "Discovered $($candidateFiles.Count) Viking Character(s):" -ForegroundColor Cyan
foreach ($f in ($candidateFiles | Sort-Object LastWriteTime -Descending)) {
  $ago = [math]::Round(((Get-Date) - $f.LastWriteTime).TotalMinutes)
  $isTarget = if ($f.FullName -eq $targetFile.FullName) { " [SYNC TARGET]" } else { "" }
  Write-Host "  - $($f.BaseName) (Saved $ago mins ago)$isTarget" -ForegroundColor DarkGray
}
Write-Host ""

$lastSavedAgo = [math]::Round(((Get-Date) - $targetFile.LastWriteTime).TotalMinutes)
Write-Host "[+] Selected Viking: $($targetFile.BaseName)" -ForegroundColor Green
Write-Host "    File Path:  $($targetFile.FullName)" -ForegroundColor DarkGray
Write-Host "    Last Saved: $lastSavedAgo minutes ago" -ForegroundColor DarkGray
Write-Host ""

Write-Host "Opening save in safe READ-ONLY non-locking mode..." -ForegroundColor Cyan

# Read raw bytes using non-locking shared read mode
try {
  $fileStream = [System.IO.FileStream]::new(
    $targetFile.FullName,
    [System.IO.FileMode]::Open,
    [System.IO.FileAccess]::Read,
    [System.IO.FileShare]::ReadWrite
  )
  $binaryReader = [System.IO.BinaryReader]::new($fileStream)
  $fileBytes = $binaryReader.ReadBytes([int]$fileStream.Length)
} catch {
  Write-Error "Failed to read character file: $_"
  return
} finally {
  if ($binaryReader) { $binaryReader.Close() }
  if ($fileStream) { $fileStream.Close() }
}

Write-Host "Syncing Viking to the Mead Hall..." -ForegroundColor Yellow

$targetEndpoint = "$($DashboardUrl.TrimEnd('/'))/api/armory/sync?name=$([Uri]::EscapeDataString($targetFile.BaseName))"
try {
  $response = Invoke-RestMethod -Uri $targetEndpoint -Method Post -Body $fileBytes -ContentType "application/octet-stream" -TimeoutSec 10
  
  if ($response -and $response.hero) {
    $hero = $response.hero
    Write-Host ""
    Write-Host "==========================================================" -ForegroundColor Green
    Write-Host " [SUCCESS] $($hero.name) has arrived in the Mead Hall!" -ForegroundColor Green
    Write-Host "==========================================================" -ForegroundColor Green
    Write-Host " Title:        $($hero.title)" -ForegroundColor Yellow
    Write-Host " Biome Era:    $($hero.biomeEra)" -ForegroundColor Cyan
    Write-Host " Top Mastery:  $($hero.topSkillName) (Lv. $($hero.topSkillLevel))" -ForegroundColor Magenta
    Write-Host " Battle Log:   $($hero.kills) kills / $($hero.deaths) deaths" -ForegroundColor Gray
    Write-Host " Synced At:    $($hero.syncedAt)" -ForegroundColor DarkGray
    Write-Host "==========================================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "View your warrior card now on your Mead Hall Dashboard!" -ForegroundColor Cyan
  } else {
    Write-Host "Save uploaded successfully!" -ForegroundColor Green
  }
} catch {
  Write-Error "Failed to reach Mead Hall Dashboard at ${targetEndpoint}: $_"
}
