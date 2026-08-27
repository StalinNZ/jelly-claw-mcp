package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var tvEpisodeRegex = regexp.MustCompile(`(?i)[sS](\d{2})[eE](\d{2})`)

var (
	mu    sync.RWMutex
	index map[string][]string
)

type ServiceConfig struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

type Config struct {
	Sonarr, Radarr, Readarr, Bazarr, Prowlarr, Jellyfin, Qbittorrent ServiceConfig
	TorrentPath   string            `json:"torrent_path"`
	ScanRoots     []string          `json:"scan_roots"`
	CatalogPath   string            `json:"catalog_path"`
	LogPath       string            `json:"log_path"`
	DryRun        bool              `json:"dry_run"`
	LibraryPaths  map[string]string `json:"library_paths"`
	ExecPaths     map[string]string `json:"exec_paths"`
	LocalAIModels map[string]string `json:"local_ai_models"`
}

type IndexEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func loadConfig() (*Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %v", err)
	}
	configPath := filepath.Join(filepath.Dir(exePath), "config.json")

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %v", err)
	}
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %v", err)
	}

	if cfg.CatalogPath == "" {
		cfg.CatalogPath = filepath.Join(filepath.Dir(exePath), "MASTER_CATALOG.json")
	}
	if cfg.LogPath == "" {
		cfg.LogPath = filepath.Join(filepath.Dir(exePath), "jelly-claw-audit.log")
	}
	return &cfg, nil
}

func apiAuth(req *http.Request, cfg ServiceConfig) {
	req.Header.Set("X-Api-Key", cfg.APIKey)
}

func updateIndex(cfg *Config) error {
	mu.Lock()
	defer mu.Unlock()

	index = make(map[string][]string)

	// Ensure catalog directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.CatalogPath), 0755); err != nil {
		return fmt.Errorf("failed to create catalog directory: %v", err)
	}

	// Walk each scan root
	for _, root := range cfg.ScanRoots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("walk error at %s: %v", path, err)
			}
			if info.IsDir() {
				return nil
			}
			// Skip hidden files and directories
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			// Skip non-media files
			ext := strings.ToLower(filepath.Ext(path))
			if !(ext == ".mkv" || ext == ".mp4" || ext == ".avi" || ext == ".mp3" || ext == ".flac" || ext == ".jpg" || ext == ".jpeg" || ext == ".png") {
				return nil
			}
			// Add to index
			key := strings.ToLower(filepath.Base(path))
			index[key] = append(index[key], path)
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk %s: %v", root, err)
		}
	}

	// Write catalog to disk
	file, err := os.Create(cfg.CatalogPath)
	if err != nil {
		return fmt.Errorf("failed to create catalog file: %v", err)
	}
	defer file.Close()

	entries := make([]IndexEntry, 0)
	for _, paths := range index {
		for _, path := range paths {
			entries = append(entries, IndexEntry{Path: path, Size: 0}) // Size not used
		}
	}

	if err := json.NewEncoder(file).Encode(entries); err != nil {
		return fmt.Errorf("failed to encode catalog: %v", err)
	}

	return nil
}

func searchIndex(query string) ([]string, error) {
	mu.RLock()
	defer mu.RUnlock()

	var results []string
	for key, paths := range index {
		if strings.Contains(key, strings.ToLower(query)) {
			results = append(results, paths...)
		}
	}
	return results, nil
}

func logAction(cfg *Config, action string, details string) error {
	logFile, err := os.OpenFile(cfg.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}
	defer logFile.Close()

	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("[%s] %s: %s\n", timestamp, action, details)
	if _, err := logFile.WriteString(logEntry); err != nil {
		return fmt.Errorf("failed to write to log file: %v", err)
	}
	return nil
}

func handleUpdateIndex(w http.ResponseWriter, r *http.Request, cfg *Config) {
	if err := updateIndex(cfg); err != nil {
		http.Error(w, fmt.Sprintf("failed to update index: %v", err), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func handleSearchIndex(w http.ResponseWriter, r *http.Request, cfg *Config) {
	query := r.URL.Query().Get("query")
	if query == "" {
		http.Error(w, "query parameter is required", http.StatusBadRequest)
		return
	}

	results, err := searchIndex(query)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to search index: %v", err), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(map[string]interface{}{"results": results}); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func handleEnsureFolders(w http.ResponseWriter, r *http.Request, cfg *Config) {
	for name, path := range cfg.LibraryPaths {
		if err := os.MkdirAll(path, 0755); err != nil {
			http.Error(w, fmt.Sprintf("failed to create folder %s: %v", name, err), http.StatusInternalServerError)
			return
		}
	}
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func handleStandardizeLibrary(w http.ResponseWriter, r *http.Request, cfg *Config) {
	// Trigger Jellyfin library refresh
	apiURL := fmt.Sprintf("%s/Library/Refresh", cfg.Jellyfin.URL)
	req, err := http.NewRequest("POST", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	apiAuth(req, cfg.Jellyfin)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to refresh library: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("jellyfin returned status %d", resp.StatusCode), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func handleAuditActiveTorrents(w http.ResponseWriter, r *http.Request, cfg *Config) {
	apiURL := fmt.Sprintf("%s/api/v2/torrents/info", cfg.Qbittorrent.URL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	apiAuth(req, cfg.Qbittorrent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch torrents: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("qbittorrent returned status %d", resp.StatusCode), http.StatusInternalServerError)
		return
	}

	var torrents []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode torrents: %v", err), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(torrents); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func handleReclaimExecute(w http.ResponseWriter, r *http.Request, cfg *Config) {
	// List active torrents
	apiURL := fmt.Sprintf("%s/api/v2/torrents/info", cfg.Qbittorrent.URL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	apiAuth(req, cfg.Qbittorrent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch torrents: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("qbittorrent returned status %d", resp.StatusCode), http.StatusInternalServerError)
		return
	}

	var torrents []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode torrents: %v", err), http.StatusInternalServerError)
		return
	}

	// Move completed torrents
	for _, torrent := range torrents {
		if torrent["progress"].(float64) == 1 && torrent["state"].(string) == "uploading" {
			name := torrent["name"].(string)
			hash := torrent["hash"].(string)
			savePath := torrent["save_path"].(string)
			contentPath := filepath.Join(savePath, name)

			// Determine target path
			targetPath := ""
			for lib, path := range cfg.LibraryPaths {
				if strings.Contains(strings.ToLower(name), strings.ToLower(lib)) {
					targetPath = filepath.Join(path, name)
					break
				}
			}
			if targetPath == "" {
				targetPath = filepath.Join(cfg.LibraryPaths["movies"], name) // Default to movies
			}

			// Move files
			if !cfg.DryRun {
				if err := os.Rename(contentPath, targetPath); err != nil {
					logAction(cfg, "reclaim_error", fmt.Sprintf("failed to move %s: %v", name, err))
					continue
				}
				// Remove torrent from qBittorrent
				removeURL := fmt.Sprintf("%s/api/v2/torrents/delete", cfg.Qbittorrent.URL)
				form := url.Values{}
				form.Add("hashes", hash)
				form.Add("deleteFiles", "false")
				req, _ := http.NewRequest("POST", removeURL, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				apiAuth(req, cfg.Qbittorrent)
				if _, err := client.Do(req); err != nil {
					logAction(cfg, "reclaim_error", fmt.Sprintf("failed to remove torrent %s: %v", name, err))
				}
			}
			logAction(cfg, "reclaim_success", fmt.Sprintf("moved %s to %s", name, targetPath))
		}
	}

	if err := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Load index from disk
	mu.Lock()
	defer mu.Unlock()
	index = make(map[string][]string)
	if file, err := os.Open(cfg.CatalogPath); err == nil {
		defer file.Close()
		var entries []IndexEntry
		if err := json.NewDecoder(file).Decode(&entries); err == nil {
			for _, entry := range entries {
				key := strings.ToLower(filepath.Base(entry.Path))
				index[key] = append(index[key], entry.Path)
			}
		}
	}

	// Register HTTP handlers
	http.HandleFunc("/update_index", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateIndex(w, r, cfg)
	})
	http.HandleFunc("/search_index", func(w http.ResponseWriter, r *http.Request) {
		handleSearchIndex(w, r, cfg)
	})
	http.HandleFunc("/ensure_folders", func(w http.ResponseWriter, r *http.Request) {
		handleEnsureFolders(w, r, cfg)
	})
	http.HandleFunc("/standardize_library", func(w http.ResponseWriter, r *http.Request) {
		handleStandardizeLibrary(w, r, cfg)
	})
	http.HandleFunc("/audit_active_torrents", func(w http.ResponseWriter, r *http.Request) {
		handleAuditActiveTorrents(w, r, cfg)
	})
	http.HandleFunc("/reclaim_execute", func(w http.ResponseWriter, r *http.Request) {
		handleReclaimExecute(w, r, cfg)
	})

	// Start HTTP server
	port := ":8080"
	fmt.Printf("jelly-claw MCP server listening on %s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("failed to start server: %v\n", err)
		os.Exit(1)
	}
}