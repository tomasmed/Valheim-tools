# RFC: Drakkar Universal Log Shipper (Go + Docker) 🛶

**Document ID**: `RFC-2026-09-DRAKKAR-SHIPPER`  
**Repository**: `tomasmed/Valheim-tools` (Public Shippers & Companions)  
**Status**: `Proposed / Priority 1`  
**Companion Core**: `tomasmed/ValheimDash` (Private Platform Core)

---

## 1. Executive Summary

**Drakkar** is an open-source, ultra-lightweight log shipping daemon for Valheim dedicated servers. Named after the legendary Norse longships that carried warriors across perilous seas, Drakkar tails server log files and ships events (Join Codes, player connections, saves, combat) to the Valheim Mead Hall dashboard (`/api/telemetry`).

### The Core Problems Solved:
1. **Zero Host Runtime Dependencies**: Replaces Python and PowerShell script requirements with a single compiled static Go binary (Windows `.exe`, Linux `x86_64`, and Linux `arm64`).
2. **Minimal Resource Overhead**: Operates under **<15 MB RAM** and **<0.2% CPU**, eliminating any impact on game server tick rate.
3. **Bursty Write Protection (Debounced Batching)**: Automatically flushes logs every **25 lines** OR every **3 seconds**, preventing high-concurrency request storms against the dashboard API.
4. **Log Rotation Resilience**: Detects file truncation or server restarts and resets cursor offsets cleanly.
5. **Turnkey Docker Sidecar**: Packages the Go binary into a tiny ~12MB container (`ghcr.io/tomasmed/valheim-drakkar:latest`) ready for Unraid, TrueNAS, and Docker Compose.

---

## 2. Technical Architecture

```
[ Valheim Server (output_log.txt / Player.log) ]
                     │
                     ▼
           [ 1. File Tailer ]  (Polls / inotify with .drakkar.cursor offset)
                     │
                     ▼
           [ 2. In-Memory Channel Buffer ]
                     │
                     ▼
           [ 3. Debounce & Batcher ]
                     ├── Buffer >= 25 lines  ────┐
                     └── Timer >= 3 seconds  ────┴──► Flush Batch
                                                             │
                                                             ▼
                                                [ 4. HTTP POST /api/telemetry ]
                                                (Bearer token + backoff retry)
```

### 2.1 File Tailing & Rotation Handling
- Reads from the specified log file path using standard Go I/O buffers.
- Persists read byte position in a local `.drakkar.cursor` file.
- If the file size drops below the cursor offset (indicating server reboot or log wipe), Drakkar resets the cursor to byte `0`.

### 2.2 Debounced Batching
- Log lines enter a buffered Go channel.
- A worker flushes the accumulated lines as a JSON array payload to `/api/telemetry` when:
  - **Batch Threshold**: Accumulated count reaches `25 lines`.
  - **Time Threshold**: `3 seconds` elapse since the last flush.
- If network fails or the dashboard returns HTTP 500/504, the batch is held in an in-memory ring buffer and retried using exponential backoff with jitter (max 5 retries).

---

## 3. Configuration & CLI Interface

Drakkar can be configured via environment variables or CLI flags:

### Flags & Environment Variables

| CLI Flag | Env Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `--url` | `DRAKKAR_DASHBOARD_URL` | `https://valheim-dash.vercel.app` | Target Mead Hall endpoint |
| `--secret` | `DRAKKAR_SECRET` | *(Required)* | Secret Realm Key from dashboard |
| `--log` | `DRAKKAR_LOG_PATH` | `./Player.log` | Path to server log file |
| `--batch-size` | `DRAKKAR_BATCH_SIZE` | `25` | Maximum lines per HTTP flush |
| `--batch-time` | `DRAKKAR_BATCH_TIME_SEC` | `3` | Maximum seconds before forced flush |
| `--cursor` | `DRAKKAR_CURSOR_PATH` | `./.drakkar.cursor` | Location of cursor offset file |

### Usage Examples

#### Windows Standalone
```powershell
.\drakkar-windows-amd64.exe --secret "VALHEIM_TOKEN_ABC123" --log "C:\Valheim\server.log"
```

#### Linux Standalone
```bash
./drakkar-linux-amd64 --secret "VALHEIM_TOKEN_ABC123" --log "/opt/valheim/server.log"
```

---

## 4. Docker Deployment

### Multi-Stage Dockerfile (Alpine/Scratch Base)
Produces a lean ~12MB container image:

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o drakkar .

# Final image
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /
COPY --from=builder /app/drakkar /drakkar
ENTRYPOINT ["/drakkar"]
```

### Docker Compose Sidecar Example
Mounts server logs read-only alongside popular Valheim server images:

```yaml
services:
  valheim-server:
    image: lloesche/valheim-server:latest
    container_name: valheim-server
    volumes:
      - /opt/valheim/data:/config
    restart: unless-stopped

  drakkar-shipper:
    image: ghcr.io/tomasmed/valheim-drakkar:latest
    container_name: valheim-drakkar
    restart: unless-stopped
    volumes:
      - /opt/valheim/data/logs:/logs:ro
    environment:
      - DRAKKAR_DASHBOARD_URL=https://valheim-dash.vercel.app
      - DRAKKAR_SECRET=YOUR_REALM_SECRET_TOKEN
      - DRAKKAR_LOG_PATH=/logs/valheim_server.log
```

---

## 5. Tiered Shipper Strategy

To maintain maximum compatibility without maintenance fragmentation:

1. **Tier 1 (Universal Recommended)**:
   - Go Static Binary (`.exe` on Windows, ELF on Linux/ARM).
   - Docker container image on GitHub Container Registry (`ghcr.io`).
2. **Tier 2 (Transparent Windows Fallback)**:
   - Retain a clean, 35-line `valheim-shipper.ps1` for users who prefer reading raw PowerShell over downloading compiled binaries.
3. **Deprecations**:
   - Retire the vendored Python setup to eliminate `pip` and dynamic link library dependency issues.

---

## 6. Implementation Checklist
- [x] Initialize `drakkar/` package in `tomasmed/Valheim-tools`.
- [x] Implement file tailer with cursor offset tracking.
- [x] Implement debounced channel batching and HTTP client with backoff.
- [x] Add GitHub Actions workflow for cross-compilation release binaries.
- [x] Add GitHub Container Registry action for automated multi-arch Docker image builds.
- [x] Update repository `README.md` with Drakkar quickstart guides.
