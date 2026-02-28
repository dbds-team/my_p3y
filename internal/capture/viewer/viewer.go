package viewer

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"

	"github.com/yourusername/p3y/internal/capture/config"
	"github.com/yourusername/p3y/internal/capture/store"
)

type Viewer struct {
	store      *store.SQLiteStore
	cfgManager *config.Manager
	tpl        *template.Template
}

type pageData struct {
	Records  []store.Record
	Path     string
	Limit    int
	Offset   int
	NextPage int
	PrevPage int
}

func New(st *store.SQLiteStore, cfgManager *config.Manager) (*Viewer, error) {
	tpl, err := template.New("captures").Parse(htmlTpl)
	if err != nil {
		return nil, err
	}
	return &Viewer{store: st, cfgManager: cfgManager, tpl: tpl}, nil
}

func (v *Viewer) RegisterRoutes(mux *http.ServeMux) {
	// 严格完整路径匹配，避免短路径或前缀误命中。
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cfg := v.cfgManager.Current()
		switch r.URL.Path {
		case cfg.View.Path:
			v.handleHTML(w, r)
		case cfg.View.APIPath:
			v.handleAPI(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func (v *Viewer) handleHTML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := v.cfgManager.Current()
	limit, offset, path := parseQuery(r, cfg.View.PageSize)

	records, err := v.store.List(limit, offset, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pd := pageData{
		Records:  records,
		Path:     path,
		Limit:    limit,
		Offset:   offset,
		NextPage: offset + limit,
		PrevPage: max(offset-limit, 0),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := v.tpl.Execute(w, pd); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (v *Viewer) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := v.cfgManager.Current()
	limit, offset, path := parseQuery(r, cfg.View.PageSize)

	records, err := v.store.List(limit, offset, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"limit":   limit,
		"offset":  offset,
		"path":    path,
		"records": records,
	})
}

func parseQuery(r *http.Request, defaultPageSize int) (limit, offset int, path string) {
	limit = defaultPageSize
	offset = 0
	path = r.URL.Query().Get("path")

	if v := r.URL.Query().Get("limit"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 200 {
			limit = p
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 0 {
			offset = p
		}
	}
	return
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

const htmlTpl = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Captured Requests</title>
  <style>
    body { font-family: "Segoe UI", sans-serif; margin: 24px; background: #f6f8fb; color: #111; }
    h1 { margin: 0 0 12px; }
    form { margin-bottom: 12px; }
    input, button { padding: 8px; margin-right: 8px; }
    table { border-collapse: collapse; width: 100%; background: #fff; }
    th, td { border: 1px solid #ddd; padding: 8px; vertical-align: top; }
    th { background: #f1f3f8; text-align: left; }
    .small { color: #666; font-size: 12px; }
    .pager { margin-top: 12px; }
  </style>
</head>
<body>
  <h1>Captured Requests</h1>
  <form method="get">
    <label>Path: <input name="path" value="{{.Path}}" /></label>
    <label>Limit: <input name="limit" value="{{.Limit}}" size="4" /></label>
    <label>Offset: <input name="offset" value="{{.Offset}}" size="4" /></label>
    <button type="submit">Search</button>
  </form>

  <table>
    <thead>
      <tr>
        <th>ID</th>
        <th>Time</th>
        <th>URL</th>
        <th>GET Params</th>
        <th>POST Params</th>
      </tr>
    </thead>
    <tbody>
    {{range .Records}}
      <tr>
        <td>{{.ID}}</td>
        <td>{{.CreatedAt}}</td>
        <td>{{.URL}}<div class="small">{{.Path}}</div></td>
        <td><pre>{{.GetParams}}</pre></td>
        <td><pre>{{.PostParams}}</pre></td>
      </tr>
    {{else}}
      <tr><td colspan="5">No records</td></tr>
    {{end}}
    </tbody>
  </table>

  <div class="pager">
    <a href="?path={{.Path}}&limit={{.Limit}}&offset={{.PrevPage}}">Prev</a>
    |
    <a href="?path={{.Path}}&limit={{.Limit}}&offset={{.NextPage}}">Next</a>
  </div>
</body>
</html>`
