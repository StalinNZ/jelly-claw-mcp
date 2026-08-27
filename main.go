# jelly-claw

UTU Media Stack MCP server for Jellyfin/Servarr/qBittorrent orchestration.

## Tools

- update_index - Scan library roots and rebuild MASTER_CATALOG.json
- search_index - Query catalog by filename/path
- ensure_folders - Create standard media directories
- standardize_library - Trigger Jellyfin library refresh
- audit_active_torrents - List qBittorrent torrents with state/progress
- reclaim_execute - Move completed torrents from /downloads to library paths

## Config

Requires config.json in the binary directory with Servarr/Jellyfin/qBittorrent API endpoints and keys.

## Build

`ash
go build -o jelly-claw-go.exe .
`
"@ | Out-File -FilePath "C:\!A-UTU\LLMS-300000-!A-LLM-MCP-set\LLMS-310000-MCP-Tool-Kit\jelly-claw-mcp\README.md" -Encoding ascii

# main.go
@"
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
	"time"
)

var tvEpisodeRegex = regexp.MustCompile((?i)[sS](\\d{2})[eE](\\d{2}))

type ServiceConfig struct {
	URL    string json:"url"
	APIKey string json:"api_key"
}

type Config struct {
	Sonarr, Radarr, Readarr, Bazarr, Prowlarr, Jellyfin, Qbittorrent ServiceConfig
	TorrentPath   string            json:"torrent_path"
	ScanRoots     []string          json:"scan_roots"
	CatalogPath   string            json:"catalog_path"
	LogPath       string            json:"log_path"
	DryRun        bool              json:"dry_run"
	LibraryPaths  map[string]string json:"library_paths"
	ExecPaths     map[string]string json:"exec_paths"
	LocalAIModels map[string]string json:"local_ai_models"
}

type IndexEntry struct {
	Path     string    json:"path"
	Size     int64     json:"size"
	Modified time.Time json:"modified"
	Category string    json:"category"
}

type MessageContent struct {
	Type string json:"type"
	Text string json:"text"
}

type ToolResult struct {
	Content []MessageContent json:"content"
}

type JSONRPCResponse struct {
	JSONRPC string      json:"jsonrpc"
	ID      interface{} json:"id"
	Result  interface{} json:"result,omitempty"
	Error   interface{} json:"error,omitempty"
}

type JSONRPCRequest struct {
	ID     interface{}     json:"id"
	Method string          json:"method
	Params json.RawMessage json:"params"
}

var cfg Config

func loadConfig() error {
	exe, _ := os.Executable()
	cfgPath := filepath.Join(filepath.Dir(exe), "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &cfg)
}

func logAction(msg string) {
	logPath := cfg.LogPath
	if logPath == "" {
		exe, _ := os.Executable()
		logPath = filepath.Join(filepath.Dir(exe), "audit.log")
	}
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().Format(time.RFC3339), msg))
		f.Close()
	}
}

func apiAuth(req *http.Request, service ServiceConfig) {
	q := req.URL.Query()
	q.Add("apikey", service.APIKey)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-API-Key", service.APIKey)
}

func apiGet(service ServiceConfig, endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", service.URL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	apiAuth(req, service)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func apiPost(service ServiceConfig, endpoint string, body interface{}) ([]byte, error) {
	var buf io.Reader
	contentType := "application/json"
	if s, ok := body.(string); ok {
		buf = strings.NewReader(s)
		contentType = "application/x-www-form-urlencoded"
	} else {
		jb, _ := json.Marshal(body)
		buf = bytes.NewBuffer(jb)
	}
	req, err := http.NewRequest("POST", service.URL+endpoint, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	apiAuth(req, service)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func apiPut(service ServiceConfig, endpoint string, body interface{}) ([]byte, error) {
	jb, _ := json.Marshal(body)
	req, err := http.NewRequest("PUT", service.URL+endpoint, bytes.NewBuffer(jb))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	apiAuth(req, service)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func ensureFolders() string {
	created := 0
	folders := []string{"movies", "tv", "books", "downloads"}
	for _, f := range folders {
		p := filepath.Join(cfg.TorrentPath, f)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			os.MkdirAll(p, 0755)
			logAction(fmt.Sprintf("CREATED FOLDER: %s", p))
			created++
		}
	}
	return fmt.Sprintf("Verified folders. Created %d missing directories.", created)
}

func updateIndex() string {
	var catalog []IndexEntry
	for _, root := range cfg.ScanRoots {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			cat := "unknown"
			lowerPath := strings.ToLower(path)
			if strings.Contains(lowerPath, "\\tv") {
				cat = "tv"
			} else if strings.Contains(lowerPath, "\\movies") {
				cat = "movies"
			} else if strings.Contains(lowerPath, "\\books") {
				cat = "books"
			}
			catalog = append(catalog, IndexEntry{
				Path: path, Size: info.Size(), Modified: info.ModTime(), Category: cat,
			})
			return nil
		})
	}
	f, _ := os.Create(cfg.CatalogPath)
	json.NewEncoder(f).Encode(map[string]interface{}{"filesystem": catalog, "updated": time.Now()})
	f.Close()
	logAction(fmt.Sprintf("INDEX-UPDATE: Catalog built with %d entries.", len(catalog)))
	return fmt.Sprintf("Index Complete. Cataloged %d files.", len(catalog))
}

func searchIndex(query string) string {
	f, err := os.Open(cfg.CatalogPath)
	if err != nil {
		return "Catalog missing. Run update_index first."
	}
	defer f.Close()
	var data struct {
		Filesystem []IndexEntry json:"filesystem"
	}
	json.NewDecoder(f).Decode(&data)
	var results []string
	query = strings.ToLower(query)
	for _, entry := range data.Filesystem {
		if strings.Contains(strings.ToLower(entry.Path), query) {
			results = append(results, fmt.Sprintf("[%s] %s (%.2f MB)", entry.Category, entry.Path, float64(entry.Size)/(1024*1024)))
		}
		if len(results) >= 50 {
			break
		}
	}
	if len(results) == 0 {
		return "No matches in index."
	}
	return "--- INDEX SEARCH RESULTS ---\n" + strings.Join(results, "\n")
}

func executeTool(name string, argsRaw json.RawMessage) string {
	var args struct {
		Query   string json:"query"
		Service string json:"service"
	}
	json.Unmarshal(argsRaw, &args)
	switch name {
	case "update_index":
		return updateIndex()
	case "search_index":
		return searchIndex(args.Query)
	case "ensure_folders":
		return ensureFolders()
	case "standardize_library":
		apiPost(cfg.Jellyfin, "/Library/Refresh", nil)
		return "UI Refresh Triggered."
	case "audit_active_torrents":
		data, err := apiGet(cfg.Qbittorrent, "/api/v2/torrents/info")
		if err != nil {
			return "qBit error."
		}
		var torrents []map[string]interface{}
		json.Unmarshal(data, &torrents)
		var report strings.Builder
		report.WriteString("--- TORRENT AUDIT ---\n")
		for _, t := range torrents {
			state := strings.ToUpper(fmt.Sprint(t["state"]))
			var prog float64
			switch p := t["progress"].(type) {
			case float64:
				prog = p * 100
			case int:
				prog = float64(p) * 100
			}
			report.WriteString(fmt.Sprintf("[%s] %5.1f%% | %s\n", state, prog, t["name"]))
		}
		return report.String()
	case "reclaim_execute":
		data, err := apiGet(cfg.Qbittorrent, "/api/v2/torrents/info")
		if err != nil {
			return "qBit error."
		}
		var torrents []map[string]interface{}
		json.Unmarshal(data, &torrents)
		count := 0
		staging := filepath.Clean(filepath.Join(cfg.TorrentPath, "downloads"))
		for _, t := range torrents {
			savePath := filepath.Clean(fmt.Sprint(t["save_path"]))
			state := fmt.Sprint(t["state"])
			progress, _ := t["progress"].(float64)
			isStalledOrMissing := state == "missingFiles" || strings.Contains(strings.ToLower(state), "stalledup")
			if (progress == 1.0 || isStalledOrMissing) && strings.EqualFold(savePath, staging) {
				hash := fmt.Sprint(t["hash"])
				category := strings.ToLower(fmt.Sprint(t["category"]))
				if category == "" {
					name := strings.ToLower(fmt.Sprint(t["name"]))
					tvPatterns := []string{"s0", "s1", "s2", "season", "hdtv", "web-dl", "webrip"}
					isTV := false
					for _, p := range tvPatterns {
						if strings.Contains(name, p) {
							isTV = true
							break
						}
					}
					if isTV {
						category = "tv"
					} else {
						category = "movies"
					}
				}
				newPath := cfg.LibraryPaths[category]
				if newPath != "" {
					apiPost(cfg.Qbittorrent, "/api/v2/torrents/setLocation", "hashes="+hash+"&location="+url.QueryEscape(newPath))
					count++
				}
			}
		}
		return fmt.Sprintf("Reclaimed %d finished torrents.", count)
	}
	return "Unknown tool."
}

func main() {
	loadConfig()
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes( \n)
		if err != nil {
			break
		}
		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]string{"name": "jelly-claw", "version": "3.1.7"},
				"capabilities":    map[string]interface{}{},
			}
		case "tools/list":
			emptySchema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			searchSchema := map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query"},
				},
				"required": []string{"query"},
			}
			result = map[string]interface{}{"tools": []map[string]interface{}{
				{"name": "update_index", "inputSchema": emptySchema},
				{"name": "search_index", "inputSchema": searchSchema},
				{"name": "ensure_folders", "inputSchema": emptySchema},
				{"name": "standardize_library", "inputSchema": emptySchema},
				{"name": "audit_active_torrents", "inputSchema": emptySchema},
				{"name": "reclaim_execute", "inputSchema": emptySchema},
			}}
		case "tools/call":
			var params struct {
				Name      string          json:"name"
				Arguments json.RawMessage json:"arguments"
			}
			json.Unmarshal(req.Params, &params)
			text := executeTool(params.Name, params.Arguments)
			result = ToolResult{Content: []MessageContent{{Type: "text", Text: text}}}
		}
		if result != nil {
			resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
			rb, _ := json.Marshal(resp)
			fmt.Printf("%s\n", rb)
		}
	}
}
