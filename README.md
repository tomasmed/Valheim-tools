# Valheim Tools & Telemetry Shippers 🛡️⚡

[![License: MIT](https://img.shields.io/badge/License-MIT-amber.svg)](https://opensource.org/licenses/MIT)
[![Valheim 1.0 Ready](https://img.shields.io/badge/Valheim-1.0%20Ready-emerald.svg)](https://valheim-dash.vercel.app)
[![Drakkar Go](https://img.shields.io/badge/Drakkar-Go%20%2B%20Docker-blue.svg)](https://github.com/tomasmed/Valheim-tools/tree/main/drakkar)

Official open-source log shippers, synchronization scripts, and community utilities for **[Valheim Mead Hall (ValheimDash)](https://valheim-dash.vercel.app)**.

---

## ⚡ What is this?

When hosting a dedicated Valheim server with **Crossplay enabled** (Xbox / Steam / PC Game Pass cross-compatibility), standard server pingers (Valve A2S UDP `2457`) are disabled by PlayFab. Furthermore, every time your server restarts, your 6-digit Join Code rotates!

These shippers non-destructively monitor your server's log file in real time and push live server health, PlayFab Join Codes, and login events directly to your Mead Hall dashboard and Discord alerts.

- **Non-Locking Shared Read**: Reads files using read-only non-exclusive streams so it never locks disk I/O or interferes with the game server.
- **Zero Mods Required**: Works on pure vanilla Valheim servers as well as modded (BepInEx / ValheimPlus).
- **100% Open-Source & Auditable**: Distributed under the MIT license.

---

## 🛶 Tier 1: Drakkar Universal Shipper (Recommended)

**Drakkar** is the next-generation compiled Go daemon and turnkey Docker sidecar:
- **Zero Host Dependencies**: Single static binary without Python or runtime requirements.
- **Ultra-Lightweight**: Operates under **<15 MB RAM** and **<0.2% CPU**.
- **Debounced Batching**: Flushes batches every **25 lines** OR **3 seconds** to prevent HTTP request storms.
- **Rotation Resilience**: Detects file truncation or server restarts and resets cursor offsets cleanly.

### Option 1: Docker Sidecar (Unraid, TrueNAS, VPS)

Mount your Valheim server's log folder read-only into the Drakkar container:

```yaml
services:
  drakkar-shipper:
    image: ghcr.io/tomasmed/valheim-drakkar:latest
    container_name: valheim-drakkar
    restart: unless-stopped
    volumes:
      - /opt/valheim/data/logs:/logs:ro
      - ./drakkar-data:/data
    environment:
      - DRAKKAR_DASHBOARD_URL=https://valheim-dash.vercel.app
      - DRAKKAR_SECRET=YOUR_SERVER_TOKEN
      - DRAKKAR_LOG_PATH=/logs/valheim_server.log
      - DRAKKAR_CURSOR_PATH=/data/.drakkar.cursor
```

### Option 2: Standalone Static Binary

Download the compiled binary for your platform from GitHub Releases:

#### Windows Standalone
```powershell
.\drakkar-windows-amd64.exe --secret "YOUR_SERVER_TOKEN" --log "C:\Valheim\server.log"
```

#### Linux Standalone (Ubuntu, Debian, AlmaLinux, Arch)
```bash
chmod +x ./drakkar-linux-amd64
./drakkar-linux-amd64 --secret "YOUR_SERVER_TOKEN" --log "/opt/valheim/server.log"
```

---

## 📜 Tier 2: Plaintext Script Shippers

### Windows Host or Client (PowerShell)
Works on dedicated Windows server hosts **or** locally on your gaming PC (for managed hosts like Valhost, DatHost, GPortal):

```powershell
powershell -ExecutionPolicy Bypass -File .\valheim-shipper.ps1 -DashboardUrl "https://valheim-dash.vercel.app" -Secret "YOUR_SERVER_TOKEN"
```

*Note: If running on your gaming PC while connected to a hosted server, it automatically detects your `%USERPROFILE%\AppData\LocalLow\IronGate\Valheim\Player.log`!*

---

### Linux VPS / systemd (Python 3)
Works on Ubuntu, Debian, Arch, AlmaLinux, or any container with standard Python 3.7+ (zero external pip packages needed):

```bash
python3 valheim-shipper.py --url "https://valheim-dash.vercel.app" --secret "YOUR_SERVER_TOKEN" --log "/opt/valheim/server.log"
```

#### Run as a `systemd` Service (Linux VPS)
Create `/etc/systemd/system/valheim-shipper.service`:
```ini
[Unit]
Description=Valheim Mead Hall Telemetry Shipper
After=network.target valheim.service

[Service]
Type=simple
User=valheim
ExecStart=/usr/bin/python3 /opt/valheim-tools/valheim-shipper.py --url https://valheim-dash.vercel.app --secret YOUR_SERVER_TOKEN --log /home/valheim/.config/unity3d/IronGate/Valheim/server.log
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```
Then start and enable:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now valheim-shipper
```

---

## ⚔️ Viking Hero Armory Sync

Sync your active character gear, biome era (Meadows -> Ashlands), boss kills, and skill mastery to the Mead Hall Armory:

1. Double-click `sync-viking.bat` (or run `./valheim-viking-shipper.ps1`).
2. The script safely finds your local `.fch` character save (including Steam Cloud cache) and syncs it.

```powershell
powershell -ExecutionPolicy Bypass -File .\valheim-viking-shipper.ps1 -DashboardUrl "https://valheim-dash.vercel.app"
```

---

## 🔒 Security & Privacy Guarantees

We adhere to strict data minimization:
- **What We Stream**: Join Code, character names, connection/disconnection timestamps, world saves, in-game day counter.
- **What We NEVER Read**: Server passwords, admin passwords, Steam login tokens, player IP addresses, world files (`.db`/`.fwl`), or chat logs.

---

## 📜 License

Distributed under the **MIT License**. Free for personal and commercial community use. See `LICENSE` for details.
