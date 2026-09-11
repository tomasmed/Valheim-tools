# Valheim Tools & Telemetry Shippers 🛡️⚡

[![License: MIT](https://img.shields.io/badge/License-MIT-amber.svg)](https://opensource.org/licenses/MIT)
[![Valheim 1.0 Ready](https://img.shields.io/badge/Valheim-1.0%20Ready-emerald.svg)](https://valheim-dash.vercel.app)
[![Zero Binaries](https://img.shields.io/badge/Zero%20Binaries-100%25%20Plaintext-blue.svg)](https://github.com/tomasmed/Valheim-tools)

Official open-source log shippers, synchronization scripts, and community utilities for **[Valheim Mead Hall (ValheimDash)](https://valheim-dash.vercel.app)**.

---

## ⚡ What is this?

When hosting a dedicated Valheim server with **Crossplay enabled** (Xbox / Steam / PC Game Pass cross-compatibility), standard server pingers (Valve A2S UDP `2457`) are disabled by PlayFab. Furthermore, every time your server restarts, your 6-digit Join Code rotates!

These shippers solve that problem. They non-destructively monitor your server's log file in real time and push live server health, Join Codes, and login events directly to your Mead Hall dashboard and Discord channels.

- **100% Open-Source & Auditable**: Pure plaintext PowerShell and Python 3. No closed-source `.exe` binaries, no keyloggers, no telemetry bloat.
- **Non-Locking Shared Read**: Reads files using read-only non-exclusive streams so it never interferes with Valheim or locks disk I/O.
- **Zero Mods Required**: Works on pure vanilla Valheim servers as well as modded (BepInEx / ValheimPlus).

---

## 🚀 Quickstart: Choose Your Setup

### Option A: Windows Host or Client (PowerShell)
Works on dedicated Windows server hosts **or** locally on your gaming PC (for managed hosts like Valhost, DatHost, GPortal):

```powershell
# Run the PowerShell shipper
powershell -ExecutionPolicy Bypass -File .\valheim-shipper.ps1 -DashboardUrl "https://valheim-dash.vercel.app" -Secret "YOUR_SERVER_TOKEN"
```

*Note: If running on your local PC while playing on a managed host, it automatically detects your `%USERPROFILE%\AppData\LocalLow\IronGate\Valheim\Player.log`!*

---

### Option B: Linux VPS / Docker / systemd (Python 3)
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
