package pricing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	LiteLLMURL       = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	cacheFileName    = "litellm-prices.json"
	overrideFileName = "prices.json"
	refreshAfter     = 24 * time.Hour
	fetchTimeout     = 10 * time.Second
	maxDownloadBytes = 64 << 20
)

type Options struct {
	CacheDir  string
	ConfigDir string
	URL       string
	Offline   bool
	Client    *http.Client
}

type Status struct {
	Source   string
	Fetched  time.Time
	Override string
	Warning  string
}

func DefaultOptions(offline bool) Options {
	cacheRoot, _ := os.UserCacheDir()
	home, _ := os.UserHomeDir()
	configRoot := os.Getenv("XDG_CONFIG_HOME")
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}
	return Options{
		CacheDir:  filepath.Join(cacheRoot, "agent-trail"),
		ConfigDir: filepath.Join(configRoot, "agent-trail"),
		URL:       LiteLLMURL,
		Offline:   offline,
		Client:    &http.Client{Timeout: fetchTimeout},
	}
}

func Load(ctx context.Context, opts Options) (Table, Status) {
	cachePath := filepath.Join(opts.CacheDir, cacheFileName)
	status := Status{Source: cachePath}
	raw, fetched, err := readCache(cachePath)
	switch {
	case opts.Offline && err != nil:
		status.Warning = "offline and no cached price list, costs not shown"
	case opts.Offline:
	case err != nil:
		fresh, fetchErr := download(ctx, opts)
		if fetchErr != nil {
			status.Warning = "prices unavailable, costs not shown: " + fetchErr.Error()
			break
		}
		if writeErr := writeCache(cachePath, fresh); writeErr != nil {
			status.Warning = "price cache not saved: " + writeErr.Error()
		}
		raw, fetched, err = fresh, time.Now(), nil
	case time.Since(fetched) > refreshAfter:
		go refreshCache(opts, cachePath)
	}
	prices := map[string]Price{}
	if err == nil {
		parsed, parseErr := parseTable(raw, sourceLiteLLM, firstPartyProviders)
		if parseErr != nil {
			status.Warning = "price list unreadable: " + parseErr.Error()
		}
		maps.Copy(prices, parsed)
		status.Fetched = fetched
	}
	overridePath := filepath.Join(opts.ConfigDir, overrideFileName)
	if overrides, readErr := os.ReadFile(overridePath); readErr == nil {
		parsed, parseErr := parseTable(overrides, sourceOverride, nil)
		if parseErr != nil {
			status.Warning = fmt.Sprintf("%s ignored: %v", overridePath, parseErr)
		} else {
			maps.Copy(prices, parsed)
			status.Override = overridePath
		}
	}
	return Table{prices: prices, FetchedAt: dateOrEmpty(status.Fetched)}, status
}

func dateOrEmpty(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format(time.DateOnly)
}

func readCache(path string) ([]byte, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	raw, err := os.ReadFile(path)
	return raw, info.ModTime(), err
}

func download(ctx context.Context, opts Options) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return nil, err
	}
	response, err := opts.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", opts.URL, response.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxDownloadBytes))
	if err != nil {
		return nil, err
	}
	if _, err := parseTable(raw, sourceLiteLLM, firstPartyProviders); err != nil {
		return nil, errors.New("downloaded price list is not valid JSON")
	}
	return raw, nil
}

func refreshCache(opts Options, path string) {
	if fresh, err := download(context.Background(), opts); err == nil {
		_ = writeCache(path, fresh)
	}
}

func writeCache(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
