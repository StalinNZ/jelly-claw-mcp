# Security, Error Handling, and Concurrency Audit Findings

## Security Issues

1. **API Key in URL Query Parameters (`apiAuth` function):**
   - The `apiAuth` function adds the API key to the URL query string (`req.URL.RawQuery = q.Encode()`). URLs are often logged by servers, proxies, and network monitors, leading to secret exposure.
   - **Fix:** Remove the API key from the query string and rely solely on the `X-Api-Key` or `Authorization` HTTP header.

2. **Insecure Path Joining (`updateIndex`, `searchIndex`):**
   - No path sanitization when working with `cfg.ScanRoots` or `cfg.CatalogPath`. It's possible to traverse directories if inputs are not trusted.
   - **Fix:** Use `filepath.Clean` and ensure paths are bounded, though mostly this depends on trusted config.

## Missing Error Handling

1. **`loadConfig()` unchecked in `main()`:**
   - `loadConfig()` returns an error, but `main()` completely ignores it. If `config.json` is missing or invalid, the app runs with empty configurations, potentially causing panics or incorrect behavior.
   - **Fix:** Check the error returned by `loadConfig()` and `log.Fatal` if it fails.

2. **Unchecked `os.Executable()`:**
   - `os.Executable()` errors are ignored in `loadConfig` and `logAction`.
   - **Fix:** Check for errors when resolving the executable path.

3. **Unchecked `json.Encoder.Encode`:**
   - `json.NewEncoder(w).Encode(...)` errors are ignored in HTTP handlers.
   - **Fix:** Check and log errors from `Encode`.

4. **Unchecked `os.Open`:**
   - `os.Open` errors are ignored in `updateIndex` and `searchIndex`.
   - **Fix:** Check and log errors from `os.Open`.

5. **Unchecked `filepath.Walk`:**
   - `filepath.Walk` errors are ignored in `updateIndex`.
   - **Fix:** Check and log errors from `filepath.Walk`.

6. **Unchecked `http.NewRequest`:**
   - `http.NewRequest` errors are ignored in `apiAuth`.
   - **Fix:** Check and log errors from `http.NewRequest`.

7. **Unchecked `http.DefaultClient.Do`:**
   - `http.DefaultClient.Do` errors are ignored in `apiAuth`.
   - **Fix:** Check and log errors from `http.DefaultClient.Do`.

## Concurrency Issues

1. **Shared State Without Mutex:**
   - The `index` and `config` variables are accessed concurrently by HTTP handlers without synchronization.
   - **Fix:** Add a `sync.RWMutex` to protect shared state.

2. **No Context Cancellation:**
   - HTTP requests in `apiAuth` do not use `context.Context` for cancellation or timeouts.
   - **Fix:** Use `req.WithContext(ctx)` and pass a context with timeout.

3. **No HTTP Timeouts:**
   - `http.DefaultClient` has no timeouts, risking resource exhaustion.
   - **Fix:** Configure `http.Client` with `Timeout`, `IdleConnTimeout`, and `MaxIdleConns`.

## Suggested Fixes

### API Key Exposure
```go
// Before:
q := req.URL.Query()
q.Add("api_key", cfg.ApiKey)
req.URL.RawQuery = q.Encode()

// After:
req.Header.Set("X-Api-Key", cfg.ApiKey)
```

### Path Sanitization
```go
// Before:
path.Join(cfg.ScanRoots[0], relPath)

// After:
cleanPath := filepath.Clean(path.Join(cfg.ScanRoots[0], relPath))
if !strings.HasPrefix(cleanPath, filepath.Clean(cfg.ScanRoots[0])) {
    return fmt.Errorf("path traversal detected")
}
```

### Error Handling
```go
// Before:
json.NewEncoder(w).Encode(result)

// After:
if err := json.NewEncoder(w).Encode(result); err != nil {
    http.Error(w, err.Error(), http.StatusInternalServerError)
}
```

### Concurrency
```go
var (
    mu    sync.RWMutex
    index map[string][]string
)

// In handlers:
mu.RLock()
defer mu.RUnlock()
// Access index
```

### HTTP Timeouts
```go
client := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        IdleConnTimeout: 90 * time.Second,
        MaxIdleConns:    100,
    },
}
```

### Context Cancellation
```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
req = req.WithContext(ctx)
```