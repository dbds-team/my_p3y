package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/txn2/n2proxy/sec"
	"go.uber.org/zap"
)

var Version = "0.0.0"

// Config 统一的配置结构（遵循 SRP 原则）
type Config struct {
	Backend     string
	IP          string
	Port        string
	MetricsPort string
	Username    string
	Password    string
	TLS         bool
	TLSCfgFile  string
	Certificate string
	Key         string
	SkipVerify  bool
	LogOutput   string
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
	target  *url.URL
	proxy   *httputil.ReverseProxy
	logger  *zap.Logger
	metrics *Metrics
	auth    *BasicAuthMiddleware
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
		// 如果没有配置认证，直接通过
		if m.username == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 使用标准库的 BasicAuth 方法（遵循 KISS 原则）
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
func NewProxy(targetURL *url.URL, logger *zap.Logger, metrics *Metrics, skipVerify bool) *Proxy {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	if skipVerify {
		proxy.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return &Proxy{
		target:  targetURL,
		proxy:   proxy,
		logger:  logger,
		metrics: metrics,
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

	// 修改请求的 Host
	r.Host = p.target.Host

	// 转发请求
	p.proxy.ServeHTTP(w, r)

	// 记录延迟
	latency := time.Since(start)
	p.metrics.Latency.Observe(float64(latency))

	// 结构化日志
	p.logger.Info("request_completed",
		zap.String("method", reqMethod),
		zap.String("path", reqPath),
		zap.Duration("latency", latency),
	)
}

// ConfigFromEnvAndFlags 从环境变量和命令行参数读取配置（遵循 SRP 原则）
func ConfigFromEnvAndFlags() *Config {
	cfg := &Config{
		Backend:     getEnv("BACKEND", "http://example.com:80"),
		IP:          getEnv("IP", "0.0.0.0"),
		Port:        getEnv("PORT", "8080"),
		MetricsPort: getEnv("METRICS_PORT", "2112"),
		Username:    getEnv("USERNAME", ""),
		Password:    getEnv("PASSWORD", ""),
		TLS:         parseBool(getEnv("TLS", "false")),
		TLSCfgFile:  getEnv("TLSCFG", ""),
		Certificate: getEnv("CRT", "./example.crt"),
		Key:         getEnv("KEY", "./example.key"),
		SkipVerify:  parseBool(getEnv("SKIP_VERIFY", "false")),
		LogOutput:   getEnv("LOGOUT", "stdout"),
	}

	// 命令行参数覆盖环境变量
	flag.StringVar(&cfg.IP, "ip", cfg.IP, "Server IP address to bind to.")
	flag.StringVar(&cfg.Port, "port", cfg.Port, "Server port.")
	flag.StringVar(&cfg.MetricsPort, "metrics_port", cfg.MetricsPort, "Metrics server port.")
	flag.StringVar(&cfg.Backend, "backend", cfg.Backend, "Backend server URL.")
	flag.StringVar(&cfg.Username, "username", cfg.Username, "BasicAuth username.")
	flag.StringVar(&cfg.Password, "password", cfg.Password, "BasicAuth password.")
	flag.BoolVar(&cfg.TLS, "tls", cfg.TLS, "Enable TLS (requires crt and key).")
	flag.StringVar(&cfg.TLSCfgFile, "tlsCfg", cfg.TLSCfgFile, "TLS config file path.")
	flag.StringVar(&cfg.Certificate, "crt", cfg.Certificate, "Path to cert file.")
	flag.StringVar(&cfg.Key, "key", cfg.Key, "Path to private key file.")
	flag.BoolVar(&cfg.SkipVerify, "skip-verify", cfg.SkipVerify, "Skip backend TLS verification.")
	flag.StringVar(&cfg.LogOutput, "logout", cfg.LogOutput, "Log output (stdout or file path).")

	version := flag.Bool("version", false, "Display version.")
	flag.Parse()

	if *version {
		fmt.Printf("Version: %s\n", Version)
		os.Exit(0)
	}

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

	// 优雅关闭
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

// StartProxyServer 启动代理服务器（遵循 SRP 原则）
func StartProxyServer(ctx context.Context, cfg *Config, proxy *Proxy, auth *BasicAuthMiddleware, logger *zap.Logger) error {
	mux := http.NewServeMux()

	// 应用中间件
	handler := http.HandlerFunc(proxy.ServeHTTP)
	if auth != nil {
		handler = auth.Middleware(handler)
	}

	mux.Handle("/", handler)

	srv := &http.Server{
		Addr:    cfg.IP + ":" + cfg.Port,
		Handler: mux,
	}

	// 优雅关闭
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
	)

	// 启动服务器
	if !cfg.TLS {
		return srv.ListenAndServe()
	}

	// TLS 模式
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

	return srv.ListenAndServeTLS(cfg.Certificate, cfg.Key)
}

func main() {
	// 读取配置
	cfg := ConfigFromEnvAndFlags()

	// 初始化日志
	logger, err := InitLogger(cfg.LogOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// 解析后端 URL
	targetURL, err := url.Parse(cfg.Backend)
	if err != nil {
		logger.Fatal("invalid_backend_url", zap.Error(err))
	}

	// 创建指标
	metrics := NewMetrics()

	// 创建上下文用于优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动指标服务器
	StartMetricsServer(ctx, cfg.IP+":"+cfg.MetricsPort, logger)

	// 创建代理
	proxy := NewProxy(targetURL, logger, metrics, cfg.SkipVerify)

	// 设置认证（如果配置了）
	var auth *BasicAuthMiddleware
	if cfg.Username != "" {
		auth = NewBasicAuthMiddleware(cfg.Username, cfg.Password, metrics)
		proxy.SetAuth(auth)
	}

	// 监听系统信号实现优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received_signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// 启动代理服务器
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
