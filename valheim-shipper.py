#!/usr/bin/env python3
"""
Valheim Telemetry Shipper Daemon (Python 3)
Streams real-time server events from server.log or Player.log to Valheim Mead Hall.

Works on Linux dedicated servers (Ubuntu, Debian, AlmaLinux, Arch), Docker containers,
systemd services, macOS, and Windows.

Zero external dependencies - uses standard Python 3.7+ library.
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone

# Regex signatures matching Valheim Dedicated Server log output
RE_JOIN_CODE = re.compile(r'join code (\d+)|Join Code\s*[:=]?\s*(\d{5,8})', re.IGNORECASE)
RE_VERSION = re.compile(r'Valheim version:\s*([0-9\.]+)|Console:\s*Valheim\s*([0-9\.]+)', re.IGNORECASE)
RE_DAY = re.compile(r'(?:day|Day)\s*[:=]?\s*(\d+)|time\s*[:=]?\s*[\d\.]+\s*,\s*day\s*[:=]?\s*(\d+)', re.IGNORECASE)
RE_WORLD = re.compile(r'ZNet\.LoadWorld:\s*([^\s\(]+)|Get create world\s*([^\r\n]+)', re.IGNORECASE)
RE_ACTIVE_PLAYERS = re.compile(r'is active with (\d+) player\(s\)', re.IGNORECASE)
RE_PLAYER_LOGIN = re.compile(r'Got character ZDOID from (.+?)\s*:\s*(-?\d+:\d+)', re.IGNORECASE)
RE_PLAYER_LOGOUT = re.compile(r'Destroying abandoned non persistent zdo\s+(-?\d+:\d+)', re.IGNORECASE)
RE_WORLD_SAVE = re.compile(r'World save \(\d+/\d+\) done|World saved \( ([\d\.]+)ms \)|Save World Thread Started', re.IGNORECASE)
RE_SOCKET_CLOSED = re.compile(r'ZPlayFabSocket::Dispose\. State: CLOSED|RPC_Disconnect|Player connection lost|Closing socket (\d+)|Destroying player (\d+)', re.IGNORECASE)
RE_ZERO_PLAYERS = re.compile(r'now 0 player\(s\)', re.IGNORECASE)

def iso_now():
    return datetime.now(timezone.utc).isoformat()

def get_server_process_stats(log_path=None):
    """
    Locates the valheim_server process to extract:
    - uptimeSeconds (int)
    - memoryUsageMb (int)
    - startedAt (ISO-8601 UTC string)

    Supports Linux /proc, Windows/Unix ps, and falls back to server.log creation time.
    """
    stats = {}
    now_ts = time.time()

    # 1. Linux /proc inspection (zero dependencies)
    if os.path.exists("/proc"):
        try:
            for entry in os.scandir("/proc"):
                if not entry.name.isdigit():
                    continue
                pid = entry.name
                comm_path = f"/proc/{pid}/comm"
                cmdline_path = f"/proc/{pid}/cmdline"
                is_valheim = False

                try:
                    if os.path.exists(comm_path):
                        with open(comm_path, "r", encoding="utf-8", errors="ignore") as f:
                            comm = f.read().strip()
                            if "valheim_server" in comm:
                                is_valheim = True
                    if not is_valheim and os.path.exists(cmdline_path):
                        with open(cmdline_path, "r", encoding="utf-8", errors="ignore") as f:
                            cmdline = f.read()
                            if "valheim_server" in cmdline:
                                is_valheim = True
                except Exception:
                    continue

                if is_valheim:
                    # In Linux, mtime of /proc/<pid> is the process start time
                    try:
                        start_ts = os.path.getmtime(f"/proc/{pid}")
                        uptime_sec = max(0, int(now_ts - start_ts))
                        stats["uptimeSeconds"] = uptime_sec
                        stats["startedAt"] = datetime.fromtimestamp(start_ts, tz=timezone.utc).isoformat()
                    except Exception:
                        pass

                    # Memory RSS from /proc/<pid>/status
                    try:
                        status_path = f"/proc/{pid}/status"
                        if os.path.exists(status_path):
                            with open(status_path, "r", encoding="utf-8", errors="ignore") as f:
                                for sline in f:
                                    if sline.startswith("VmRSS:"):
                                        parts = sline.split()
                                        if len(parts) >= 2 and parts[1].isdigit():
                                            stats["memoryUsageMb"] = int(parts[1]) // 1024
                                        break
                    except Exception:
                        pass

                    if "uptimeSeconds" in stats:
                        return stats
        except Exception:
            pass

    # 2. Fallback: inspect log file metadata if available
    if log_path and os.path.exists(log_path):
        try:
            create_ts = os.path.getctime(log_path)
            mtime_ts = os.path.getmtime(log_path)
            start_ts = min(create_ts, mtime_ts)
            uptime_sec = max(0, int(now_ts - start_ts))
            stats["uptimeSeconds"] = uptime_sec
            stats["startedAt"] = datetime.fromtimestamp(start_ts, tz=timezone.utc).isoformat()
        except Exception:
            pass

    return stats


def send_telemetry(dashboard_url, secret, payload):
    url = f"{dashboard_url.rstrip('/')}/api/telemetry"
    headers = {"Content-Type": "application/json"}
    if secret:
        headers["Authorization"] = f"Bearer {secret}"
    
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=8) as response:
            if response.status == 200:
                print(f"[{datetime.now().strftime('%H:%M:%S')}] Telemetry pushed successfully.")
    except urllib.error.HTTPError as e:
        print(f"[{datetime.now().strftime('%H:%M:%S')}] HTTP Error {e.code}: {e.reason}", file=sys.stderr)
    except Exception as e:
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Failed to send telemetry: {e}", file=sys.stderr)

def resolve_default_log():
    # Check common Linux paths, then Windows
    home = os.path.expanduser("~")
    candidates = [
        # Linux dedicated server standard locations
        os.path.join(home, ".config/unity3d/IronGate/Valheim/server.log"),
        os.path.join(home, ".config/unity3d/IronGate/Valheim/Player.log"),
        "/opt/valheim/server.log",
        "/valheim/server.log",
        # Windows AppData
        os.path.expandvars(r"%USERPROFILE%\AppData\LocalLow\IronGate\Valheim\server.log"),
        os.path.expandvars(r"%USERPROFILE%\AppData\LocalLow\IronGate\Valheim\Player.log"),
    ]
    for path in candidates:
        if os.path.exists(path):
            return path
    return candidates[0]

def main():
    parser = argparse.ArgumentParser(description="Valheim Telemetry Shipper (Python 3)")
    parser.add_argument("--url", default="https://valheim-dash.vercel.app", help="Valheim Mead Hall Dashboard URL")
    parser.add_argument("--secret", default=os.environ.get("TELEMETRY_SECRET", ""), help="Realm Server Secret Token")
    parser.add_argument("--log", default="", help="Path to Valheim server.log or Player.log")
    parser.add_argument("--interval", type=float, default=1.5, help="Poll interval in seconds")
    parser.add_argument("--day", type=int, default=None, help="Initial in-game world day (override if server log omits day lines)")
    args = parser.parse_args()

    log_path = args.log if args.log else resolve_default_log()

    print("==========================================================")
    print(" Valheim Mead Hall - Python Telemetry Shipper")
    print(" Open-Source Tools: https://github.com/tomasmed/Valheim-tools")
    print("==========================================================")
    print(f"Dashboard:    {args.url}/api/telemetry")
    print(f"Log Target:   {log_path}")
    if args.secret:
        print(f"Security:     Authorization Bearer Enabled ({args.secret[:8]}...)")
    else:
        print("Security:     No Secret Token provided (local/public default)")
    print(f"Poll Rate:    {args.interval}s")
    if args.day is not None:
        print(f"Manual Day:   Day {args.day} (CLI override)")
    print("==========================================================")

    while not os.path.exists(log_path):
        print(f"Waiting for log file to appear at {log_path}...", file=sys.stderr)
        time.sleep(3)

    print("Log file found. Attaching stream in shared read-only mode...")

    active_players = {}
    discovered_join_code = None
    discovered_version = None
    discovered_day = args.day
    discovered_save = None
    discovered_world = None

    # Catch-up phase
    with open(log_path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue

            m_code = RE_JOIN_CODE.search(line)
            if m_code:
                discovered_join_code = m_code.group(1) or m_code.group(2)

            m_ver = RE_VERSION.search(line)
            if m_ver:
                discovered_version = m_ver.group(1) or m_ver.group(2)

            m_day = RE_DAY.search(line)
            if m_day:
                discovered_day = int(m_day.group(1) or m_day.group(2))

            m_world = RE_WORLD.search(line)
            if m_world:
                discovered_world = (m_world.group(1) or m_world.group(2)).strip()

            if RE_WORLD_SAVE.search(line):
                discovered_save = iso_now()

            m_login = RE_PLAYER_LOGIN.search(line)
            if m_login:
                p_name = m_login.group(1).strip()
                zdoid = m_login.group(2).strip()
                active_players[p_name] = {
                    "id": f"p-{p_name}",
                    "name": p_name,
                    "characterZdoId": zdoid,
                    "connectedAt": iso_now(),
                    "isOnline": True
                }

            m_logout = RE_PLAYER_LOGOUT.search(line)
            if m_logout:
                zdoid = m_logout.group(1).strip()
                for name, p_data in active_players.items():
                    if p_data.get("characterZdoId") == zdoid:
                        p_data["isOnline"] = False
                        p_data["disconnectedAt"] = iso_now()

    server_init = {
        "isOnline": True,
        "currentPlayers": sum(1 for p in active_players.values() if p.get("isOnline"))
    }
    if discovered_join_code:
        server_init["joinCode"] = discovered_join_code
        print(f"  Discovered Join Code: {discovered_join_code}")
    if discovered_version:
        server_init["version"] = discovered_version
        print(f"  Discovered Version:   v{discovered_version}")
    if discovered_day is not None:
        server_init["dayCount"] = discovered_day
        print(f"  Discovered Day:       Day {discovered_day}")
    if discovered_world:
        server_init["worldName"] = discovered_world
        print(f"  Discovered World:     {discovered_world}")
    if discovered_save:
        server_init["lastSavedAt"] = discovered_save

    proc_stats = get_server_process_stats(log_path)
    if "uptimeSeconds" in proc_stats:
        server_init["uptimeSeconds"] = proc_stats["uptimeSeconds"]
        uptime_hours = round(proc_stats["uptimeSeconds"] / 3600, 1)
        mem_str = f" ({proc_stats['memoryUsageMb']} MB)" if "memoryUsageMb" in proc_stats else ""
        print(f"  Process Uptime:       {uptime_hours} hours{mem_str}")
    if "memoryUsageMb" in proc_stats:
        server_init["memoryUsageMb"] = proc_stats["memoryUsageMb"]
    if "startedAt" in proc_stats:
        server_init["startedAt"] = proc_stats["startedAt"]

    for p in active_players.values():
        status = "ONLINE" if p.get("isOnline") else "offline"
        print(f"  Player History: {p['name']} [{status}]")

    send_telemetry(args.url, args.secret, {
        "server": server_init,
        "players": list(active_players.values())
    })
    print("Initial synchronization complete. Now tailing live stream... (Ctrl+C to quit)\n")

    # Live tail phase
    last_heartbeat = time.time()
    with open(log_path, "r", encoding="utf-8", errors="replace") as f:
        f.seek(0, os.SEEK_END)
        while True:
            line = f.readline()
            if line:
                line = line.strip()
                if not line:
                    continue

                m_code = RE_JOIN_CODE.search(line)
                if m_code:
                    code = m_code.group(1) or m_code.group(2)
                    print(f"\n[JOIN CODE] PlayFab Code: {code}")
                    send_telemetry(args.url, args.secret, {
                        "server": {"joinCode": code, "isOnline": True},
                        "players": list(active_players.values()),
                        "events": [{
                            "id": f"ev-{int(time.time()*1000)}",
                            "timestamp": iso_now(),
                            "category": "system",
                            "level": "info",
                            "message": f"PlayFab Join Code: {code}"
                        }]
                    })

                m_login = RE_PLAYER_LOGIN.search(line)
                if m_login:
                    p_name = m_login.group(1).strip()
                    zdoid = m_login.group(2).strip()
                    print(f"\n[WARRIOR ARRIVED] {p_name} ({zdoid})")
                    active_players[p_name] = {
                        "id": f"p-{p_name}",
                        "name": p_name,
                        "characterZdoId": zdoid,
                        "connectedAt": iso_now(),
                        "isOnline": True
                    }
                    online_count = sum(1 for p in active_players.values() if p.get("isOnline"))
                    send_telemetry(args.url, args.secret, {
                        "server": {"currentPlayers": online_count, "isOnline": True},
                        "players": list(active_players.values()),
                        "events": [{
                            "id": f"ev-{int(time.time()*1000)}",
                            "timestamp": iso_now(),
                            "category": "player",
                            "level": "info",
                            "message": f"Player '{p_name}' connected to the server."
                        }]
                    })

                m_logout = RE_PLAYER_LOGOUT.search(line)
                if m_logout:
                    zdoid = m_logout.group(1).strip()
                    found = None
                    for name, p_data in active_players.items():
                        if p_data.get("characterZdoId") == zdoid and p_data.get("isOnline"):
                            p_data["isOnline"] = False
                            p_data["disconnectedAt"] = iso_now()
                            found = name
                            break
                    if found:
                        print(f"\n[WARRIOR DEPARTED] {found}")
                        online_count = sum(1 for p in active_players.values() if p.get("isOnline"))
                        send_telemetry(args.url, args.secret, {
                            "server": {"currentPlayers": online_count},
                            "players": list(active_players.values()),
                            "events": [{
                                "id": f"ev-{int(time.time()*1000)}",
                                "timestamp": iso_now(),
                                "category": "player",
                                "level": "info",
                                "message": f"Player '{found}' left the realm."
                            }]
                        })

                if RE_SOCKET_CLOSED.search(line) or RE_ZERO_PLAYERS.search(line):
                    online = [p for p in active_players.values() if p.get("isOnline")]
                    if len(online) <= 1:
                        for p in online:
                            p["isOnline"] = False
                            p["disconnectedAt"] = iso_now()
                        send_telemetry(args.url, args.secret, {
                            "server": {"currentPlayers": 0},
                            "players": list(active_players.values())
                        })

                if RE_WORLD_SAVE.search(line):
                    now = iso_now()
                    print(f"\n[WORLD SAVED] at {now}")
                    send_telemetry(args.url, args.secret, {
                        "server": {"lastSavedAt": now},
                        "events": [{
                            "id": f"ev-{int(time.time()*1000)}",
                            "timestamp": now,
                            "category": "save",
                            "level": "success",
                            "message": "World saved successfully."
                        }]
                    })

                m_day = RE_DAY.search(line)
                if m_day:
                    day = int(m_day.group(1) or m_day.group(2))
                    print(f"\n[DAY TICK] Day {day}")
                    send_telemetry(args.url, args.secret, {
                        "server": {"dayCount": day}
                    })

            else:
                # Periodic heartbeat every 30s
                if time.time() - last_heartbeat > 30:
                    last_heartbeat = time.time()
                    hb_server = {"isOnline": True}
                    stats = get_server_process_stats(log_path)
                    if "uptimeSeconds" in stats:
                        hb_server["uptimeSeconds"] = stats["uptimeSeconds"]
                    if "memoryUsageMb" in stats:
                        hb_server["memoryUsageMb"] = stats["memoryUsageMb"]
                    if "startedAt" in stats:
                        hb_server["startedAt"] = stats["startedAt"]
                    send_telemetry(args.url, args.secret, {
                        "server": hb_server
                    })
                time.sleep(args.interval)

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nShipper stopped by user.")
        sys.exit(0)
