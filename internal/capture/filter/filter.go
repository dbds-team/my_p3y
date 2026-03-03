package filter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yourusername/p3y/internal/capture/config"
)

type RequestData struct {
	Path       string
	URL        string
	Query      string
	PostParams string
	UniqueKey  string
}

type Decision struct {
	Keep   bool
	Reason string
}

type Deduper interface {
	SeenBefore(key string) bool
}

type Engine struct {
	extensions    map[string]struct{}
	bypassMark    string
	pathIncludes  []string
	statusExclude map[int]struct{}
	contains      []string
	patterns      []*regexp.Regexp
	deduper       Deduper
}

func NewEngine(cfg config.FilterConfig, deduper Deduper) (*Engine, error) {
	extensions := make(map[string]struct{}, len(cfg.StaticExtensions))
	for _, ext := range cfg.StaticExtensions {
		ext := strings.TrimSpace(strings.ToLower(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extensions[ext] = struct{}{}
	}

	contains := make([]string, 0, len(cfg.AttackContains))
	for _, item := range cfg.AttackContains {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			contains = append(contains, item)
		}
	}

	patterns := make([]*regexp.Regexp, 0, len(cfg.AttackRegex))
	for _, raw := range cfg.AttackRegex {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		re, err := regexp.Compile(raw)
		if err != nil {
			return nil, fmt.Errorf("compile attack regex %q: %w", raw, err)
		}
		patterns = append(patterns, re)
	}

	statusExclude := make(map[int]struct{}, len(cfg.ResponseStatusExclude))
	for _, sc := range cfg.ResponseStatusExclude {
		if sc > 0 {
			statusExclude[sc] = struct{}{}
		}
	}

	pathIncludes := make([]string, 0, len(cfg.PathIncludeKeywords))
	for _, kw := range cfg.PathIncludeKeywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" {
			pathIncludes = append(pathIncludes, kw)
		}
	}

	return &Engine{
		extensions:    extensions,
		bypassMark:    cfg.StaticBypassContains,
		pathIncludes:  pathIncludes,
		statusExclude: statusExclude,
		contains:      contains,
		patterns:      patterns,
		deduper:       deduper,
	}, nil
}

func (e *Engine) Evaluate(d RequestData, statusCode int) Decision {
	if _, excluded := e.statusExclude[statusCode]; excluded {
		return Decision{Keep: false, Reason: "response_status_excluded"}
	}

	if e.isStaticPath(d.Path, d.URL) {
		return Decision{Keep: false, Reason: "static_resource"}
	}
	if !e.pathIncluded(d.Path) {
		return Decision{Keep: false, Reason: "path_not_included"}
	}

	if e.containsAttackPayload(d.URL + " " + d.Query + " " + d.PostParams) {
		return Decision{Keep: false, Reason: "attack_pattern"}
	}

	if d.UniqueKey != "" && e.deduper != nil && e.deduper.SeenBefore(d.UniqueKey) {
		return Decision{Keep: false, Reason: "duplicate_request"}
	}

	return Decision{Keep: true, Reason: "accepted"}
}

func (e *Engine) isStaticPath(path, rawURL string) bool {
	if e.bypassMark != "" && strings.Contains(rawURL, e.bypassMark) {
		return false
	}
	path = strings.ToLower(path)
	for ext := range e.extensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func (e *Engine) containsAttackPayload(payload string) bool {
	lower := strings.ToLower(payload)
	for _, key := range e.contains {
		if strings.Contains(lower, key) {
			return true
		}
	}
	for _, re := range e.patterns {
		if re.MatchString(payload) {
			return true
		}
	}
	return false
}

func (e *Engine) pathIncluded(path string) bool {
	if len(e.pathIncludes) == 0 {
		return true
	}
	path = strings.ToLower(path)
	for _, kw := range e.pathIncludes {
		if strings.Contains(path, kw) {
			return true
		}
	}
	return false
}
