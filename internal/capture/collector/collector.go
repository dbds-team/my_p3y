package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/yourusername/p3y/internal/capture/config"
	"github.com/yourusername/p3y/internal/capture/filter"
	"github.com/yourusername/p3y/internal/capture/store"
	"go.uber.org/zap"
)

type Collector struct {
	cfgManager *config.Manager
	store      *store.SQLiteStore
	logger     *zap.Logger
	mu         sync.RWMutex
	engine     *filter.Engine
	engineHash string
}

func New(cfgManager *config.Manager, st *store.SQLiteStore, logger *zap.Logger) *Collector {
	return &Collector{cfgManager: cfgManager, store: st, logger: logger}
}

func (c *Collector) Extract(r *http.Request) *CapturedRequest {
	rd, err := extractRequestData(r)
	if err != nil {
		c.logger.Warn("capture_extract_failed", zap.Error(err))
		return nil
	}
	return &rd
}

func (c *Collector) Record(rd *CapturedRequest, statusCode int) {
	if rd == nil {
		return
	}

	cfg := c.cfgManager.Current()
	if !cfg.Enabled {
		return
	}

	engine, err := c.getEngine(cfg.Filter)
	if err != nil {
		c.logger.Warn("capture_filter_init_failed", zap.Error(err))
		return
	}

	decision := engine.Evaluate(filter.RequestData{
		Path:       rd.Path,
		URL:        rd.URL,
		Query:      rd.GetParams,
		PostParams: rd.PostParams,
		UniqueKey:  rd.UniqueKey,
	}, statusCode)
	if !decision.Keep {
		c.logger.Debug("capture_filtered", zap.String("reason", decision.Reason), zap.String("url", rd.URL), zap.Int("status", statusCode))
		return
	}

	if err := c.store.Save(store.Record{
		URL:        rd.URL,
		Path:       rd.Path,
		GetParams:  rd.GetParams,
		PostParams: rd.PostParams,
		UniqueKey:  rd.UniqueKey,
	}, cfg.Filter.PerURLKeepLatest); err != nil {
		c.logger.Warn("capture_store_failed", zap.Error(err), zap.String("url", rd.URL))
		return
	}
}

func (c *Collector) getEngine(filterCfg config.FilterConfig) (*filter.Engine, error) {
	raw, err := json.Marshal(filterCfg)
	if err != nil {
		return nil, err
	}
	hash := string(raw)

	c.mu.RLock()
	if c.engine != nil && c.engineHash == hash {
		engine := c.engine
		c.mu.RUnlock()
		return engine, nil
	}
	c.mu.RUnlock()

	engine, err := filter.NewEngine(filterCfg, c.store)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.engine = engine
	c.engineHash = hash
	c.mu.Unlock()
	return engine, nil
}

type CapturedRequest struct {
	URL        string
	Path       string
	GetParams  string
	PostParams string
	UniqueKey  string
}

func extractRequestData(r *http.Request) (CapturedRequest, error) {
	fullURL := r.URL.Path
	if r.URL.RawQuery != "" {
		fullURL += "?" + r.URL.RawQuery
	}

	getParams := normalizeValues(r.URL.Query())

	postParams := ""
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		if strings.Contains(ct, "application/x-www-form-urlencoded") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return CapturedRequest{}, fmt.Errorf("read form body: %w", err)
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			vals, err := url.ParseQuery(string(body))
			if err != nil {
				return CapturedRequest{}, fmt.Errorf("parse form body: %w", err)
			}
			postParams = normalizeValues(vals)
		} else {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return CapturedRequest{}, fmt.Errorf("read body: %w", err)
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			trimmed := strings.TrimSpace(string(body))
			if trimmed != "" {
				postParams = trimmed
			}
		}
	}

	keyInput := r.URL.Path + "|" + getParams + "|" + postParams
	sum := sha256.Sum256([]byte(keyInput))

	return CapturedRequest{
		URL:        fullURL,
		Path:       r.URL.Path,
		GetParams:  getParams,
		PostParams: postParams,
		UniqueKey:  hex.EncodeToString(sum[:]),
	}, nil
}

func normalizeValues(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vals := append([]string(nil), values[k]...)
		sort.Strings(vals)
		parts = append(parts, fmt.Sprintf("%s=%s", k, strings.Join(vals, ",")))
	}
	return strings.Join(parts, "&")
}
