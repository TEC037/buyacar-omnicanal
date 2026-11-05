package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config define los parámetros de configuración para el servidor web embebido.
type Config struct {
	Port            int    `json:"port"`
	Host            string `json:"host"`
	OperatingMode   string `json:"operating_mode"`
	EnableDashboard bool   `json:"enable_dashboard"`
	EnableMetrics   bool   `json:"enable_metrics"`
}

// DefaultConfig retorna la configuración base para el servidor local.
func DefaultConfig() Config {
	return Config{
		Port:            8080,
		Host:            "127.0.0.1",
		OperatingMode:   "SIMULATION",
		EnableDashboard: true,
		EnableMetrics:   true,
	}
}

// Server define el contrato para el ciclo de vida del servidor web embebido (PROJECT.md § M4).
type Server interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// HTTPServer implementa Server utilizando net/http nativo y embed.FS sin dependencias externas.
type HTTPServer struct {
	cfg        Config
	httpServer *http.Server
	listener   net.Listener
	dashboard  *DashboardHandler
	metrics    *MetricsExporter
	logger     *slog.Logger

	mu        sync.Mutex
	running   bool
	boundAddr string
}

// NewServer construye una nueva instancia de HTTPServer con sus manejadores integrados.
func NewServer(cfg Config, dashboard *DashboardHandler, metrics *MetricsExporter) *HTTPServer {
	if cfg.Port < 0 {
		cfg.Port = 8080
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.OperatingMode == "" {
		cfg.OperatingMode = "SIMULATION"
	}

	mux := http.NewServeMux()

	if cfg.EnableMetrics && metrics != nil {
		mux.Handle("/metrics", metrics)
	}

	if cfg.EnableDashboard && dashboard != nil {
		mux.Handle("/", dashboard)
	}

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &HTTPServer{
		cfg:        cfg,
		httpServer: httpSrv,
		dashboard:  dashboard,
		metrics:    metrics,
		logger:     slog.Default(),
	}
}

// SetLogger configura un logger estructurado para el servidor.
func (s *HTTPServer) SetLogger(l *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l != nil {
		s.logger = l
	}
}

// Addr retorna la dirección en la que el servidor está escuchando (ej. "127.0.0.1:8080").
func (s *HTTPServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
}

// URL retorna la URL base accesible del servidor HTTP (ej. "http://127.0.0.1:8080").
func (s *HTTPServer) URL() string {
	return "http://" + s.Addr()
}

// Dashboard devuelve el DashboardHandler asociado.
func (s *HTTPServer) Dashboard() *DashboardHandler {
	return s.dashboard
}

// Metrics devuelve el MetricsExporter asociado.
func (s *HTTPServer) Metrics() *MetricsExporter {
	return s.metrics
}

// Start arranca el servidor HTTP escuchando en la dirección configurada.
// La ejecución del servicio ocurre de manera concurrente y se apaga limpiamente si ctx se cancela.
func (s *HTTPServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("server is already running")
	}

	bindTarget := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	ln, err := net.Listen("tcp", bindTarget)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to bind server to %s: %w", bindTarget, err)
	}

	s.listener = ln
	s.boundAddr = ln.Addr().String()
	s.running = true
	s.mu.Unlock()

	s.logger.Info("embedded local web server started",
		"address", s.boundAddr,
		"operating_mode", s.cfg.OperatingMode,
		"dashboard_enabled", s.cfg.EnableDashboard,
		"metrics_enabled", s.cfg.EnableMetrics,
	)

	errChan := make(chan error, 1)
	go func() {
		if serveErr := s.httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errChan <- serveErr
		}
		close(errChan)
	}()

	// Monitorear cancelación de contexto para apagado ordenado
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errChan:
		return err
	}
}

// Shutdown realiza el apagado ordenado (graceful shutdown) del servidor.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.logger.Info("shutting down embedded local web server...")
	return s.httpServer.Shutdown(ctx)
}
