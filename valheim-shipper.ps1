<#
.SYNOPSIS
  Valheim Log Shipper & Telemetry Daemon
  Monitors server.log and ships real-time server events to the Valheim Mead Hall Dashboard.

.EXAMPLE
  # Local Development:
  powershell -ExecutionPolicy Bypass -File .\scripts\valheim-shipper.ps1 -DashboardUrl "http://localhost:3000"

  # Production Vercel Deployment (Protected):
  powershell -ExecutionPolicy Bypass -File .\scripts\valheim-shipper.ps1 -DashboardUrl "https://your-dash.vercel.app" -Secret "YOUR_TELEMETRY_SECRET"
#>

param(
  [string]$DashboardUrl = "http://localhost:3000",
  [string]$LogPath = "",
  [string]$Secret = "",
  [int]$PollIntervalMs = 1500
)

# Resolve default log location (check server.log first, then Player.log)
if ([string]::IsNullOrWhiteSpace($LogPath)) {
  $defaultServerLog = "$env:USERPROFILE\AppData\LocalLow\IronGate\Valheim\server.log"
  $defaultPlayerLog = "$env:USERPROFILE\AppData\LocalLow\IronGate\Valheim\Player.log"

  if (Test-Path $defaultServerLog) {
    $LogPath = $defaultServerLog
  } elseif (Test-Path $defaultPlayerLog) {
    $LogPath = $defaultPlayerLog
  } else {
    $LogPath = $defaultServerLog
  }
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Valheim Mead Hall - Server Telemetry Shipper" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "Dashboard Endpoint: $DashboardUrl/api/telemetry" -ForegroundColor Gray
Write-Host "Log Target:         $LogPath" -ForegroundColor Gray
if (-not [string]::IsNullOrWhiteSpace($Secret)) {
  Write-Host "Security:           Authorization Bearer Enabled" -ForegroundColor Green
}
Write-Host "Poll Interval:      $PollIntervalMs ms" -ForegroundColor Gray
Write-Host ""

if (-not (Test-Path $LogPath)) {
  Write-Warning "Log file was not found at $LogPath"
  Write-Warning "Waiting for Valheim Dedicated Server to start and create the log..."
  while (-not (Test-Path $LogPath)) {
    Start-Sleep -Seconds 3
  }
}

Write-Host "Log file detected. Attaching stream..." -ForegroundColor Green

# Use non-locking FileStream to read without conflicting with Valheim
$fileStream = New-Object System.IO.FileStream(
  $LogPath,
  [System.IO.FileMode]::Open,
  [System.IO.FileAccess]::Read,
  [System.IO.FileShare]::ReadWrite
)
$reader = New-Object System.IO.StreamReader($fileStream)

$activePlayers = @{}

function Get-ServerProcessStats {
  $proc = Get-Process -Name valheim_server -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($proc) {
    return @{
      uptimeSeconds = [int]((Get-Date) - $proc.StartTime).TotalSeconds
      memoryUsageMb = [int][math]::Round($proc.WorkingSet64 / 1MB)
    }
  }
  return @{}
}

function Send-Telemetry([hashtable]$payload) {
  try {
    $json = $payload | ConvertTo-Json -Depth 5
    $target = "$DashboardUrl/api/telemetry"
    $headers = @{ "Content-Type" = "application/json" }
    if (-not [string]::IsNullOrWhiteSpace($Secret)) {
      $headers["Authorization"] = "Bearer $Secret"
    }
    $null = Invoke-RestMethod -Uri $target -Method Post -Body $json -Headers $headers -TimeoutSec 5
    Write-Host "  Telemetry pushed to dashboard." -ForegroundColor DarkGray
  } catch {
    Write-Warning "Failed to contact dashboard at $DashboardUrl"
  }
}

Write-Host "Scanning log history for current server state..." -ForegroundColor Cyan

$discoveredJoinCode = $null
$discoveredVersion = $null
$discoveredDay = $null
$discoveredSave = $null
$discoveredWorld = $null

# Catch-up phase: scan existing lines
while ($null -ne ($line = $reader.ReadLine())) {
  $line = $line.Trim()
  if ([string]::IsNullOrWhiteSpace($line)) { continue }

  if ($line -match 'join code (\d+)' -or $line -match 'Join Code\s*[:=]?\s*(\d{5,8})') {
    $discoveredJoinCode = $matches[1]
  }
  if ($line -match 'Valheim version:\s*([0-9\.]+)' -or $line -match 'Console:\s*Valheim\s*([0-9\.]+)') {
    $discoveredVersion = $matches[1]
  }
  if ($line -match 'day:(\d+)' -or $line -match 'Day (\d+)') {
    $discoveredDay = [int]$matches[1]
  }
  if ($line -match 'World save \(\d+/\d+\) done' -or $line -match 'World saved \( ([\d\.]+)ms \)' -or $line -match 'Save World Thread Started') {
    $timeMatch = $line -match '^(\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2})'
    if ($timeMatch) {
      try { $discoveredSave = (Get-Date $matches[1]).ToString("o") } catch { $discoveredSave = (Get-Date).ToString("o") }
    } else {
      $discoveredSave = (Get-Date).ToString("o")
    }
  }
  if ($line -match 'ZNet\.LoadWorld:\s*([^\s\(]+)' -or $line -match 'Get create world\s*([^\r\n]+)') {
    $discoveredWorld = $matches[1].Trim()
  }

  # Character Login
  if ($line -match 'Got character ZDOID from ([\w\s]+) : ([\d\:]+)') {
    $playerName = $matches[1].Trim()
    $zdoid = $matches[2].Trim()
    $timeMatch = $line -match '^(\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2})'
    $connTime = if ($timeMatch) { (Get-Date $matches[1]).ToString("o") } else { (Get-Date).ToString("o") }

    $activePlayers[$playerName] = @{
      id = "p-$playerName"
      name = $playerName
      characterZdoId = $zdoid
      connectedAt = $connTime
      isOnline = $true
    }
  }

  # Match Player Disconnect by Character ZDOID (Quit Game / Logout)
  if ($line -match 'Destroying abandoned non persistent zdo ([\d\:]+)') {
    $zdoid = $matches[1]
    $timeMatch = $line -match '^(\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2})'
    $discoTime = if ($timeMatch) { (Get-Date $matches[1]).ToString("o") } else { (Get-Date).ToString("o") }

    foreach ($name in $activePlayers.Keys) {
      if ($activePlayers[$name].characterZdoId -eq $zdoid) {
        $activePlayers[$name].isOnline = $false
        $activePlayers[$name].disconnectedAt = $discoTime
      }
    }
  }

  # Player connection lost / socket closed / now 0 player(s)
  if ($line -match 'Player connection lost.*now 0 player\(s\)' -or $line -match 'now 0 player\(s\)' -or $line -match 'ZPlayFabSocket::Dispose\. State: CLOSED') {
    $timeMatch = $line -match '^(\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2})'
    $discoTime = if ($timeMatch) { (Get-Date $matches[1]).ToString("o") } else { (Get-Date).ToString("o") }

    $onlineWarriors = @($activePlayers.Values | Where-Object { $_.isOnline })
    if ($onlineWarriors.Count -le 1) {
      foreach ($k in $activePlayers.Keys) {
        if ($activePlayers[$k].isOnline) {
          $activePlayers[$k].isOnline = $false
          $activePlayers[$k].disconnectedAt = $discoTime
        }
      }
    }
  }
}

# Initial synchronization
$activeCount = ($activePlayers.Values | Where-Object { $_.isOnline }).Count
$initialServer = @{
  isOnline = $true
  currentPlayers = $activeCount
}
if ($discoveredJoinCode) { $initialServer["joinCode"] = $discoveredJoinCode; Write-Host "  Discovered Join Code: $discoveredJoinCode" -ForegroundColor Yellow }
if ($discoveredVersion) { $initialServer["version"] = $discoveredVersion; Write-Host "  Discovered Version:   v$discoveredVersion" -ForegroundColor Cyan }
if ($discoveredDay) { $initialServer["dayCount"] = $discoveredDay; Write-Host "  Discovered Day:       Day $discoveredDay" -ForegroundColor Yellow }
if ($discoveredSave) { $initialServer["lastSavedAt"] = $discoveredSave; Write-Host "  Discovered Save:      $discoveredSave" -ForegroundColor DarkYellow }
if ($discoveredWorld) { $initialServer["worldName"] = $discoveredWorld; Write-Host "  Discovered World:     $discoveredWorld" -ForegroundColor Green }

foreach ($p in $activePlayers.Values) {
  if ($p.isOnline) {
    Write-Host "  Discovered Active Warrior: $($p.name) (ZDOID: $($p.characterZdoId))" -ForegroundColor Green
  } else {
    Write-Host "  Discovered Past Warrior:   $($p.name)" -ForegroundColor DarkGray
  }
}

$procStats = Get-ServerProcessStats
if ($procStats.ContainsKey("uptimeSeconds")) {
  $initialServer["uptimeSeconds"] = $procStats["uptimeSeconds"]
  $initialServer["memoryUsageMb"] = $procStats["memoryUsageMb"]
  $uptimeHours = [math]::Round($procStats["uptimeSeconds"] / 3600, 1)
  Write-Host "  Process Uptime:       $uptimeHours hours ($($procStats['memoryUsageMb']) MB)" -ForegroundColor DarkCyan
}

Send-Telemetry @{
  server = $initialServer
  players = @($activePlayers.Values)
}
Write-Host "Initial telemetry synchronized. Attached to live stream... (Ctrl+C to exit)`n" -ForegroundColor Green

$lastHeartbeat = [DateTime]::UtcNow

try {
  while ($true) {
    $line = $reader.ReadLine()
    if ($null -ne $line) {
      $line = $line.Trim()
      if ([string]::IsNullOrWhiteSpace($line)) { continue }

      # Match Join Code
      if ($line -match 'join code (\d+)' -or $line -match 'Join Code\s*[:=]?\s*(\d{5,8})') {
        $joinCode = $matches[1]
        Write-Host "PLAYFAB JOIN CODE: $joinCode" -ForegroundColor Yellow
        Send-Telemetry @{
          server = @{ joinCode = $joinCode; isOnline = $true }
          players = @($activePlayers.Values)
          events = @(@{
            id = [guid]::NewGuid().ToString()
            timestamp = (Get-Date).ToString("o")
            category = "system"
            level = "info"
            message = "PlayFab Join Code: $joinCode"
          })
        }
      }

      # Match Valheim Version
      if ($line -match 'Valheim version:\s*([0-9\.]+)' -or $line -match 'Console:\s*Valheim\s*([0-9\.]+)') {
        $version = $matches[1]
        Write-Host "VALHEIM VERSION: v$version" -ForegroundColor Cyan
        Send-Telemetry @{
          server = @{ version = $version }
        }
      }

      # Match Day Count
      if ($line -match 'day:(\d+)' -or $line -match 'Day (\d+)') {
        $day = [int]$matches[1]
        Write-Host "DAY TICK: Day $day" -ForegroundColor Yellow
        Send-Telemetry @{
          server = @{ dayCount = $day }
        }
      }

      # Match Server Ready & Active
      if ($line -match 'is active with (\d+) player\(s\)') {
        $count = [int]$matches[1]
        Write-Host "SERVER STATUS: Active ($count warriors online)" -ForegroundColor Cyan
        Send-Telemetry @{
          server = @{ currentPlayers = $count; isOnline = $true }
          players = @($activePlayers.Values)
        }
      }

      # Match Player Entered
      if ($line -match 'Got character ZDOID from ([\w\s]+) : ([\d\:]+)') {
        $playerName = $matches[1].Trim()
        $zdoid = $matches[2].Trim()
        Write-Host "WARRIOR ARRIVED: $playerName (ZDOID: $zdoid)" -ForegroundColor Green
        
        $activePlayers[$playerName] = @{
          id = "p-$playerName"
          name = $playerName
          characterZdoId = $zdoid
          connectedAt = (Get-Date).ToString("o")
          isOnline = $true
        }

        Send-Telemetry @{
          players = @($activePlayers.Values)
          events = @(@{
            id = [guid]::NewGuid().ToString()
            timestamp = (Get-Date).ToString("o")
            category = "player"
            level = "info"
            message = "Player '$playerName' connected to the server."
          })
        }
      }

      # Match Player Disconnect by Character ZDOID (Quit Game / Logout)
      if ($line -match 'Destroying abandoned non persistent zdo ([\d\:]+)') {
        $zdoid = $matches[1]
        $foundPlayer = $null
        foreach ($name in $activePlayers.Keys) {
          if ($activePlayers[$name].characterZdoId -eq $zdoid -and $activePlayers[$name].isOnline) {
            $activePlayers[$name].isOnline = $false
            $activePlayers[$name].disconnectedAt = (Get-Date).ToString("o")
            $foundPlayer = $name
            break
          }
        }

        if ($foundPlayer) {
          Write-Host "WARRIOR DEPARTED: $foundPlayer (Quit Game)" -ForegroundColor Magenta
          $activeCount = ($activePlayers.Values | Where-Object { $_.isOnline }).Count
          Send-Telemetry @{
            server = @{ currentPlayers = $activeCount }
            players = @($activePlayers.Values)
            events = @(@{
              id = [guid]::NewGuid().ToString()
              timestamp = (Get-Date).ToString("o")
              category = "player"
              level = "info"
              message = "Player '$foundPlayer' left the realm."
            })
          }
        }
      }

      # Match Crossplay Socket Closure & Connection Lost
      if ($line -match 'ZPlayFabSocket::Dispose\. State: CLOSED' -or $line -match 'RPC_Disconnect' -or $line -match 'Player connection lost' -or $line -match 'Closing socket (\d+)' -or $line -match 'Destroying player (\d+)') {
        $onlineWarriors = @($activePlayers.Values | Where-Object { $_.isOnline })
        if ($onlineWarriors.Count -eq 1) {
          $departed = $onlineWarriors[0]
          $departedName = $departed.name
          $activePlayers[$departedName].isOnline = $false
          $activePlayers[$departedName].disconnectedAt = (Get-Date).ToString("o")

          Write-Host "WARRIOR DISCONNECTED: $departedName (Socket Closed)" -ForegroundColor Magenta
          Send-Telemetry @{
            server = @{ currentPlayers = 0 }
            players = @($activePlayers.Values)
            events = @(@{
              id = [guid]::NewGuid().ToString()
              timestamp = (Get-Date).ToString("o")
              category = "player"
              level = "info"
              message = "Player '$departedName' disconnected."
            })
          }
        } elseif ($line -match 'now 0 player\(s\)') {
          foreach ($k in $activePlayers.Keys) {
            $activePlayers[$k].isOnline = $false
            $activePlayers[$k].disconnectedAt = (Get-Date).ToString("o")
          }
          Send-Telemetry @{
            server = @{ currentPlayers = 0 }
            players = @($activePlayers.Values)
          }
        }
      }

      # Match World Save
      if ($line -match 'World save \(\d+/\d+\) done' -or $line -match 'World saved \( ([\d\.]+)ms \)' -or $line -match 'Save World Thread Started') {
        $saveTime = (Get-Date).ToString("o")
        Write-Host "WORLD SAVED: at $saveTime" -ForegroundColor DarkYellow
        Send-Telemetry @{
          server = @{ lastSavedAt = $saveTime }
          events = @(@{
            id = [guid]::NewGuid().ToString()
            timestamp = $saveTime
            category = "save"
            level = "success"
            message = "World saved successfully."
          })
        }
      }
    } else {
      # Heartbeat periodic update
      $now = [DateTime]::UtcNow
      if (($now - $lastHeartbeat).TotalSeconds -ge 30) {
        $lastHeartbeat = $now
        $stats = Get-ServerProcessStats
        if ($stats.ContainsKey("uptimeSeconds")) {
          Send-Telemetry @{
            server = @{
              uptimeSeconds = $stats["uptimeSeconds"]
              memoryUsageMb = $stats["memoryUsageMb"]
              isOnline = $true
            }
          }
        }
      }

      Start-Sleep -Milliseconds $PollIntervalMs
    }
  }
} finally {
  $reader.Close()
  $fileStream.Close()
  Write-Host "Telemetry stream stopped." -ForegroundColor Yellow
}
