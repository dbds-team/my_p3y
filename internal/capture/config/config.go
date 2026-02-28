package config

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

// CaptureConfig 聚合采集相关配置，便于模块迁移复用。
type CaptureConfig struct {
	Enabled bool         `yaml:"enabled"`
	Store   StoreConfig  `yaml:"store"`
	Filter  FilterConfig `yaml:"filter"`
	View    ViewConfig   `yaml:"view"`
}

type StoreConfig struct {
	DBPath string `yaml:"db_path"`
}

type FilterConfig struct {
	StaticExtensions     []string `yaml:"static_extensions"`
	StaticBypassContains string   `yaml:"static_bypass_contains"`
	PerURLKeepLatest     int      `yaml:"per_url_keep_latest"`
	AttackContains       []string `yaml:"attack_contains"`
	AttackRegex          []string `yaml:"attack_regex"`
}

type ViewConfig struct {
	Path     string `yaml:"path"`
	APIPath  string `yaml:"api_path"`
	PageSize int    `yaml:"page_size"`
}

func Default() CaptureConfig {
	return CaptureConfig{
		Enabled: true,
		Store:   StoreConfig{DBPath: "./capture.db"},
		Filter: FilterConfig{
			StaticExtensions: []string{
				".js", ".css", ".html", ".htm", ".txt", ".json", ".xml", ".map",
				".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".ico", ".bmp",
				".pdf", ".zip", ".rar", ".7z", ".gz", ".tar", ".exe", ".dll", ".bin",
				".woff", ".woff2", ".ttf", ".otf", ".eot", ".mp3", ".mp4", ".avi", ".mov",
			},
			StaticBypassContains: ";",
			PerURLKeepLatest:     10,
			AttackContains: []string{
				"union select", "<script", "../", "..\\", " xp_cmdshell", " or 1=1",
				"drop table", "sleep(", "benchmark(", "<img", "javascript:",
			},
			AttackRegex: []string{
				`(?i)(?:\bunion\b\s+\bselect\b)`,
				`(?i)(?:\bor\b\s+\d+=\d+)`,
				`(?i)<script[^>]*>`,
				`(?i)(?:\.\./|\.\.\\)`,
				`(?i)(?:\bselect\b.+\bfrom\b)`,
			},
		},
		View: ViewConfig{
			Path:     "/captures",
			APIPath:  "/api/captures",
			PageSize: 30,
		},
	}
}

func normalize(cfg *CaptureConfig) {
	if cfg.Store.DBPath == "" {
		cfg.Store.DBPath = "./capture.db"
	}
	if cfg.Filter.StaticBypassContains == "" {
		cfg.Filter.StaticBypassContains = ";"
	}
	if cfg.Filter.PerURLKeepLatest <= 0 {
		cfg.Filter.PerURLKeepLatest = 10
	}
	if cfg.View.Path == "" {
		cfg.View.Path = "/captures"
	}
	if cfg.View.APIPath == "" {
		cfg.View.APIPath = "/api/captures"
	}
	if cfg.View.PageSize <= 0 {
		cfg.View.PageSize = 30
	}
}

func Load(path string) (CaptureConfig, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read capture config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse capture config: %w", err)
	}
	normalize(&cfg)
	return cfg, nil
}

// Manager 提供线程安全配置读取与热更新。
type Manager struct {
	mu      sync.RWMutex
	cfg     CaptureConfig
	path    string
	lastMod time.Time
}

func NewManager(path string) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, path: path}
	if path != "" {
		if st, err := os.Stat(path); err == nil {
			m.lastMod = st.ModTime()
		}
	}
	return m, nil
}

func (m *Manager) Current() CaptureConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) ReloadIfChanged() (bool, error) {
	if m.path == "" {
		return false, nil
	}
	st, err := os.Stat(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	m.mu.RLock()
	last := m.lastMod
	m.mu.RUnlock()
	if !st.ModTime().After(last) {
		return false, nil
	}

	cfg, err := Load(m.path)
	if err != nil {
		return false, err
	}

	m.mu.Lock()
	m.cfg = cfg
	m.lastMod = st.ModTime()
	m.mu.Unlock()
	return true, nil
}
