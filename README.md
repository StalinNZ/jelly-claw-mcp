# Jelly-Claw MCP

UTU Media Stack MCP server for **Jellyfin**, **Servarr** (Sonarr/Radarr/Readarr/Bazarr/Prowlarr), and **qBittorrent** orchestration.

## Purpose
- **Index media libraries** (`update_index`, `search_index`)
- **Standardize folder structure** (`ensure_folders`)
- **Trigger Jellyfin library refreshes** (`standardize_library`)
- **Audit active torrents** (`audit_active_torrents`)
- **Reclaim completed torrents** (`reclaim_execute`)

## Security
⚠️ **API keys and paths are loaded from `config.json` at runtime.**
⚠️ **Never commit `config.json` to Git.**
⚠️ **Never hardcode secrets in source code.**

## Setup
### 1. Clone the Repo
```bash
# HTTPS (auth required)
git clone https://github.com/StalinNZ/jelly-claw-mcp.git
cd jelly-claw-mcp

# GitHub CLI (auth required)
gh repo clone StalinNZ/jelly-claw-mcp
```

### 2. Create `config.json`
Create a `config.json` file in the same directory as the binary:
```json
{
  "sonarr": {
    "url": "http://localhost:8989",
    "api_key": "your_sonarr_api_key"
  },
  "radarr": {
    "url": "http://localhost:7878",
    "api_key": "your_radarr_api_key"
  },
  "readarr": {
    "url": "http://localhost:8787",
    "api_key": "your_readarr_api_key"
  },
  "bazarr": {
    "url": "http://localhost:6767",
    "api_key": "your_bazarr_api_key"
  },
  "prowlarr": {
    "url": "http://localhost:9696",
    "api_key": "your_prowlarr_api_key"
  },
  "jellyfin": {
    "url": "http://localhost:8096",
    "api_key": "your_jellyfin_api_key"
  },
  "qbittorrent": {
    "url": "http://localhost:8080",
    "api_key": "your_qbittorrent_api_key"
  },
  "torrent_path": "/path/to/torrents",
  "scan_roots": [
    "/path/to/media/library"
  ],
  "catalog_path": "MASTER_CATALOG.json",
  "log_path": "jelly-claw-audit.log",
  "dry_run": false,
  "library_paths": {
    "movies": "/path/to/movies",
    "tv": "/path/to/tv",
    "music": "/path/to/music",
    "books": "/path/to/books"
  }
}
```

### 3. Build & Run
```bash
# Build
go build -o jelly-claw-go.exe .

# Run
./jelly-claw-go.exe
```

### 4. `.gitignore`
Ensure `config.json` and local files are excluded:
```gitignore
# Secrets
config.json

# Local data
MASTER_CATALOG.json
jelly-claw-audit.log

# Binaries
*.exe
*.bin
```

## Tools
| Tool | Description | Endpoint |
|------|-------------|----------|
| `update_index` | Rebuild media catalog | `POST /update_index` |
| `search_index` | Query catalog by filename | `GET /search_index?query=...` |
| `ensure_folders` | Create standard media directories | `POST /ensure_folders` |
| `standardize_library` | Trigger Jellyfin library refresh | `POST /standardize_library` |
| `audit_active_torrents` | List qBittorrent torrents | `GET /audit_active_torrents` |
| `reclaim_execute` | Move completed torrents to library | `POST /reclaim_execute` |

## Audit
- [Security, Error Handling, and Concurrency Audit Findings](https://github.com/StalinNZ/jelly-claw-mcp/issues/1)
- All findings addressed in [commit 9e3d661](https://github.com/StalinNZ/jelly-claw-mcp/commit/9e3d661).

## License
Private. All rights reserved.