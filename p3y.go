package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tjfoc/gmsm/gmtls"
	"github.com/txn2/n2proxy/sec"
	"github.com/yourusername/p3y/internal/capture/collector"
	captureConfig "github.com/yourusername/p3y/internal/capture/config"
	"github.com/yourusername/p3y/internal/capture/store"
	"github.com/yourusername/p3y/internal/capture/viewer"
	"go.uber.org/zap"
)

var Version = "0.0.0"

// Config 统一的配置结构（遵循 SRP 原则）
type Config struct {
	Backend        string
	IP             string
	Port           string
	MetricsIP      string
	MetricsPort    string
	CaptureIP      string
	CapturePort    string
	Username       string
	Password       string
	TLS            bool
	TLSMode        string
	TLSCfgFile     string
	Certificate    string
	Key            string
	SkipVerify     bool
	LogOutput      string
	CaptureCfgFile string
	CaptureCfgPoll int
}

// Metrics 封装 Prometheus 指标（遵循 SRP 原则）
type Metrics struct {
	Requests  prometheus.Counter
	Latency   prometheus.Summary
	AuthFails prometheus.Counter
}

// ProxyHandler 代理处理器接口（遵循 ISP 原则）
type ProxyHandler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// Proxy 反向代理实现
type Proxy struct {
	target    *url.URL
	proxy     *httputil.ReverseProxy
	logger    *zap.Logger
	metrics   *Metrics
	auth      *BasicAuthMiddleware
	collector *collector.Collector
}

// BasicAuthMiddleware 认证中间件（遵循 SRP 原则）
type BasicAuthMiddleware struct {
	username string
	password string
	metrics  *Metrics
}

// NewMetrics 创建 Prometheus 指标
func NewMetrics() *Metrics {
	return &Metrics{
		Requests: promauto.NewCounter(prometheus.CounterOpts{
			Name: "p3y_total_requests",
			Help: "Total number of requests received.",
		}),
		AuthFails: promauto.NewCounter(prometheus.CounterOpts{
			Name: "p3y_total_authentication_failures",
			Help: "Total number of authentication failures.",
		}),
		Latency: promauto.NewSummary(prometheus.SummaryOpts{
			Name: "p3y_response_time",
			Help: "Response latency.",
		}),
	}
}

// NewBasicAuthMiddleware 创建认证中间件
func NewBasicAuthMiddleware(username, password string, metrics *Metrics) *BasicAuthMiddleware {
	return &BasicAuthMiddleware{
		username: username,
		password: password,
		metrics:  metrics,
	}
}

// Middleware 认证中间件实现（使用标准库的 BasicAuth）
func (m *BasicAuthMiddleware) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.username == "" {
			next.ServeHTTP(w, r)
			return
		}

		username, password, ok := r.BasicAuth()
		if !ok || username != m.username || password != m.password {
			m.metrics.AuthFails.Inc()
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// NewProxy 创建代理实例
func NewProxy(targetURL *url.URL, logger *zap.Logger, metrics *Metrics, skipVerify bool, captureCollector *collector.Collector) *Proxy {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	if skipVerify {
		proxy.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return &Proxy{
		target:    targetURL,
		proxy:     proxy,
		logger:    logger,
		metrics:   metrics,
		collector: captureCollector,
	}
}

// SetAuth 设置认证中间件（遵循 DIP 原则）
func (p *Proxy) SetAuth(auth *BasicAuthMiddleware) {
	p.auth = auth
}

// ServeHTTP 实现 ProxyHandler 接口
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.metrics.Requests.Inc()
	start := time.Now()
	reqPath := r.URL.Path
	reqMethod := r.Method

	var captured *collector.CapturedRequest
	if p.collector != nil {
		captured = p.collector.Extract(r)
	}

	r.Host = p.target.Host
	statusWriter := newStatusRecorder(w)
	p.proxy.ServeHTTP(statusWriter, r)
	if p.collector != nil {
		p.collector.Record(captured, statusWriter.StatusCode())
	}

	latency := time.Since(start)
	p.metrics.Latency.Observe(float64(latency))
	p.logger.Info("request_completed",
		zap.String("method", reqMethod),
		zap.String("path", reqPath),
		zap.Duration("latency", latency),
	)
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (w *statusRecorder) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) StatusCode() int {
	return w.statusCode
}

func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijacker not supported")
}

func (w *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// ConfigFromEnvAndFlags 从环境变量和命令行参数读取配置（遵循 SRP 原则）
func ConfigFromEnvAndFlags() *Config {
	cfg := &Config{
		Backend:        getEnv("BACKEND", "http://example.com:80"),
		IP:             getEnv("IP", "0.0.0.0"),
		MetricsIP:      getEnv("METRICS_IP", ""),
		Port:           getEnv("PORT", "8080"),
		MetricsPort:    getEnv("METRICS_PORT", "2112"),
		CaptureIP:      getEnv("CAPTURE_IP", "127.0.0.1"),
		CapturePort:    getEnv("CAPTURE_PORT", "26001"),
		Username:       getEnv("USERNAME", ""),
		Password:       getEnv("PASSWORD", ""),
		TLS:            parseBool(getEnv("TLS", "false")),
		TLSMode:        getEnv("TLS_MODE", "standard"),
		TLSCfgFile:     getEnv("TLSCFG", ""),
		Certificate:    getEnv("CRT", "./example.crt"),
		Key:            getEnv("KEY", "./example.key"),
		SkipVerify:     parseBool(getEnv("SKIP_VERIFY", "false")),
		LogOutput:      getEnv("LOGOUT", "stdout"),
		CaptureCfgFile: getEnv("CAPTURECFG", "./capture.yaml"),
		CaptureCfgPoll: parseInt(getEnv("CAPTURECFG_POLL_SEC", "5"), 5),
	}

	if cfg.MetricsIP == "" {
		cfg.MetricsIP = cfg.IP
	}

	flag.StringVar(&cfg.IP, "ip", cfg.IP, "Server IP address to bind to.")
	flag.StringVar(&cfg.MetricsIP, "metrics_ip", cfg.MetricsIP, "Metrics server IP address to bind to.")
	flag.StringVar(&cfg.CaptureIP, "capture_ip", cfg.CaptureIP, "Capture viewer server IP address to bind to.")
	flag.StringVar(&cfg.Port, "port", cfg.Port, "Server port.")
	flag.StringVar(&cfg.MetricsPort, "metrics_port", cfg.MetricsPort, "Metrics server port.")
	flag.StringVar(&cfg.CapturePort, "capture_port", cfg.CapturePort, "Capture viewer server port (set 0 to disable).")
	flag.StringVar(&cfg.Backend, "backend", cfg.Backend, "Backend server URL.")
	flag.StringVar(&cfg.Username, "username", cfg.Username, "BasicAuth username.")
	flag.StringVar(&cfg.Password, "password", cfg.Password, "BasicAuth password.")
	flag.BoolVar(&cfg.TLS, "tls", cfg.TLS, "Enable TLS (requires crt and key).")
	flag.StringVar(&cfg.TLSMode, "tls_mode", cfg.TLSMode, "TLS mode for frontend listener: standard|gm")
	flag.StringVar(&cfg.TLSCfgFile, "tlsCfg", cfg.TLSCfgFile, "TLS config file path.")
	flag.StringVar(&cfg.Certificate, "crt", cfg.Certificate, "Path to cert file.")
	flag.StringVar(&cfg.Key, "key", cfg.Key, "Path to private key file.")
	flag.BoolVar(&cfg.SkipVerify, "skip-verify", cfg.SkipVerify, "Skip backend TLS verification.")
	flag.StringVar(&cfg.LogOutput, "logout", cfg.LogOutput, "Log output (stdout or file path).")
	flag.StringVar(&cfg.CaptureCfgFile, "captureCfg", cfg.CaptureCfgFile, "Request capture config file path (includes capture routes).")
	flag.IntVar(&cfg.CaptureCfgPoll, "captureCfgPollSec", cfg.CaptureCfgPoll, "Capture config reload interval in seconds.")

	version := flag.Bool("version", false, "Display version.")
	flag.Parse()

	if *version {
		fmt.Printf("Version: %s\n", Version)
		os.Exit(0)
	}
	cfg.TLSMode = normalizeTLSMode(cfg.TLSMode)

	return cfg
}

// InitLogger 初始化日志（遵循 SRP 原则）
func InitLogger(output string) (*zap.Logger, error) {
	zapCfg := zap.NewProductionConfig()
	zapCfg.DisableCaller = true
	zapCfg.DisableStacktrace = true
	zapCfg.OutputPaths = []string{output}

	return zapCfg.Build()
}

// StartMetricsServer 启动指标服务器（遵循 SRP 原则）
func StartMetricsServer(ctx context.Context, addr string, logger *zap.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("starting_metrics_server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics_server_error", zap.Error(err))
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("metrics_server_shutdown_error", zap.Error(err))
		}
	}()

	return srv
}

func StartCaptureServer(ctx context.Context, addr string, captureViewer *viewer.Viewer, logger *zap.Logger) *http.Server {
	if captureViewer == nil {
		return nil
	}
	mux := http.NewServeMux()
	captureViewer.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("starting_capture_server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("capture_server_error", zap.Error(err))
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("capture_server_shutdown_error", zap.Error(err))
		}
	}()

	return srv
}

func printStartupSummary(cfg *Config, captureCfg captureConfig.CaptureConfig, logger *zap.Logger) {
	proxyScheme := "http"
	if cfg.TLS {
		proxyScheme = "https"
	}

	logger.Info("startup_routes",
		zap.String("proxy_bind", cfg.IP+":"+cfg.Port),
		zap.String("proxy_example", fmt.Sprintf("%s://<host>:%s/", proxyScheme, cfg.Port)),
		zap.String("metrics_bind", cfg.MetricsIP+":"+cfg.MetricsPort),
		zap.String("metrics_route", "/metrics"),
		zap.String("capture_bind", cfg.CaptureIP+":"+cfg.CapturePort),
		zap.String("capture_html_route", captureCfg.View.Path),
		zap.String("capture_api_route", captureCfg.View.APIPath),
	)

	fmt.Printf("Proxy:   %s://<host>:%s (bind %s:%s)\n", proxyScheme, cfg.Port, cfg.IP, cfg.Port)
	fmt.Printf("Metrics: http://%s:%s/metrics\n", cfg.MetricsIP, cfg.MetricsPort)
	if cfg.CapturePort == "0" {
		fmt.Printf("Capture: disabled (capture_port=0)\n")
		return
	}
	fmt.Printf("Capture HTML: http://%s:%s%s\n", cfg.CaptureIP, cfg.CapturePort, captureCfg.View.Path)
	fmt.Printf("Capture API:  http://%s:%s%s\n", cfg.CaptureIP, cfg.CapturePort, captureCfg.View.APIPath)
}

func StartCaptureConfigReloader(ctx context.Context, pollSec int, manager *captureConfig.Manager, logger *zap.Logger) {
	if manager == nil || pollSec <= 0 {
		return
	}
	interval := time.Duration(pollSec) * time.Second
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				changed, err := manager.ReloadIfChanged()
				if err != nil {
					logger.Warn("capture_config_reload_failed", zap.Error(err))
					continue
				}
				if changed {
					logger.Info("capture_config_reloaded")
				}
			}
		}
	}()
}

// StartProxyServer 启动代理服务器（遵循 SRP 原则）
func StartProxyServer(ctx context.Context, cfg *Config, proxy *Proxy, auth *BasicAuthMiddleware, logger *zap.Logger) error {
	mux := http.NewServeMux()

	handler := http.HandlerFunc(proxy.ServeHTTP)
	if auth != nil {
		handler = auth.Middleware(handler)
	}
	mux.Handle("/", handler)

	srv := &http.Server{
		Addr:    cfg.IP + ":" + cfg.Port,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		logger.Info("shutting_down_proxy_server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("proxy_server_shutdown_error", zap.Error(err))
		}
	}()

	logger.Info("starting_proxy_server",
		zap.String("addr", srv.Addr),
		zap.String("backend", cfg.Backend),
		zap.Bool("tls", cfg.TLS),
		zap.String("tls_mode", cfg.TLSMode),
	)

	if !cfg.TLS {
		return srv.ListenAndServe()
	}

	if cfg.TLSMode == "gm" {
		gmCert, err := gmtls.LoadGMX509KeyPair(cfg.Certificate, cfg.Key)
		if err != nil {
			return fmt.Errorf("failed to load GM cert/key pair: %w", err)
		}
		gmCfg := &gmtls.Config{
			GMSupport:    gmtls.NewGMSupport(),
			Certificates: []gmtls.Certificate{gmCert},
		}
		ln, err := gmtls.Listen("tcp", srv.Addr, gmCfg)
		if err != nil {
			return fmt.Errorf("failed to start GM TLS listener: %w", err)
		}
		return srv.Serve(ln)
	}

	tlsCfg := sec.GenericTLSConfig()
	if cfg.TLSCfgFile != "" {
		logger.Info("loading_tls_config", zap.String("file", cfg.TLSCfgFile))
		var err error
		tlsCfg, err = sec.NewTLSCfgFromYaml(cfg.TLSCfgFile, logger)
		if err != nil {
			return fmt.Errorf("failed to load TLS config: %w", err)
		}
	} else {
		logger.Warn("using_default_tls_config")
	}

	srv.TLSConfig = tlsCfg
	srv.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))

	err := srv.ListenAndServeTLS(cfg.Certificate, cfg.Key)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unsupported elliptic curve") {
		return fmt.Errorf("failed to load TLS cert/key: unsupported elliptic curve for standard mode; try -tls_mode gm for SM2 certificates: %w", err)
	}
	return err
}

func main() {
	cfg := ConfigFromEnvAndFlags()

	logger, err := InitLogger(cfg.LogOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	targetURL, err := url.Parse(cfg.Backend)
	if err != nil {
		logger.Fatal("invalid_backend_url", zap.Error(err))
	}

	metrics := NewMetrics()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartMetricsServer(ctx, cfg.MetricsIP+":"+cfg.MetricsPort, logger)

	captureCfgManager, err := captureConfig.NewManager(cfg.CaptureCfgFile)
	if err != nil {
		logger.Fatal("capture_config_init_failed", zap.Error(err))
	}
	StartCaptureConfigReloader(ctx, cfg.CaptureCfgPoll, captureCfgManager, logger)

	captureStore, err := store.NewSQLiteStore(captureCfgManager.Current().Store.DBPath)
	if err != nil {
		logger.Fatal("capture_store_init_failed", zap.Error(err))
	}
	defer captureStore.Close()

	captureCollector := collector.New(captureCfgManager, captureStore, logger)
	captureViewer, err := viewer.New(captureStore, captureCfgManager)
	if err != nil {
		logger.Fatal("capture_viewer_init_failed", zap.Error(err))
	}
	printStartupSummary(cfg, captureCfgManager.Current(), logger)
	if cfg.CapturePort != "0" {
		StartCaptureServer(ctx, cfg.CaptureIP+":"+cfg.CapturePort, captureViewer, logger)
	}

	proxy := NewProxy(targetURL, logger, metrics, cfg.SkipVerify, captureCollector)

	var auth *BasicAuthMiddleware
	if cfg.Username != "" {
		auth = NewBasicAuthMiddleware(cfg.Username, cfg.Password, metrics)
		proxy.SetAuth(auth)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received_signal", zap.String("signal", sig.String()))
		cancel()
	}()

	if err := StartProxyServer(ctx, cfg, proxy, auth, logger); err != nil && err != http.ErrServerClosed {
		logger.Fatal("proxy_server_error", zap.Error(err))
	}

	logger.Info("server_stopped")
}

// getEnv 获取环境变量，如果不存在则返回默认值（遵循 DRY 原则）
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// parseBool 解析布尔值字符串（遵循 DRY 原则，消除重复的布尔转换逻辑）
func parseBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

func parseInt(s string, fallback int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func normalizeTLSMode(s string) string {
	mode := strings.ToLower(strings.TrimSpace(s))
	switch mode {
	case "", "standard":
		return "standard"
	case "gm":
		return "gm"
	default:
		return "standard"
	}
}
