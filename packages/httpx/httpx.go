package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"core/packages/config"
)

type contextKey string

const (
	requestIDKey     contextKey = "request_id"
	correlationIDKey contextKey = "correlation_id"

	HeaderRequestID     = "X-Request-Id"
	HeaderCorrelationID = "X-Correlation-Id"
)

type App struct {
	Config Config
	Logger *slog.Logger
	Ready  func(context.Context) error
}

type Config struct {
	ServiceName     string
	Environment     string
	Addr            string
	ShutdownTimeout time.Duration
}

func ConfigFrom(cfg config.Config) Config {
	return Config{
		ServiceName:     cfg.ServiceName,
		Environment:     cfg.Environment,
		Addr:            cfg.HTTPAddr,
		ShutdownTimeout: cfg.ShutdownTimeout,
	}
}

func NewRouter(app App) chi.Router {
	if app.Ready == nil {
		app.Ready = func(context.Context) error { return nil }
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(correlationMiddleware)
	r.Use(recoverMiddleware(app.Logger))

	r.Get("/healthz", healthHandler(app.Config))
	r.Get("/readyz", readyHandler(app.Config, app.Ready))

	return r
}

func Run(ctx context.Context, cfg Config, logger *slog.Logger, handler http.Handler) error {
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", slog.String("addr", cfg.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		return shutdownServer(server, cfg.ShutdownTimeout)
	case err := <-errCh:
		return err
	}
}

func healthHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": cfg.ServiceName,
		})
	}
}

func readyHandler(cfg Config, ready func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := ready(r.Context()); err != nil {
			WriteError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "ready",
			"service": cfg.ServiceName,
		})
	}
}

func shutdownServer(server *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func CorrelationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationIDKey).(string)
	return value
}

func correlationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		correlationID := r.Header.Get(HeaderCorrelationID)
		if correlationID == "" {
			correlationID = requestID
		}

		w.Header().Set(HeaderRequestID, requestID)
		w.Header().Set(HeaderCorrelationID, correlationID)

		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		ctx = context.WithValue(ctx, correlationIDKey, correlationID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if logger != nil {
						logger.Error("panic recovered",
							slog.Any("panic", recovered),
							slog.String("stack", string(debug.Stack())),
						)
					}
					WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}
