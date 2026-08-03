// Package gatewayapp owns the reusable gateway startup and shutdown lifecycle.
package gatewayapp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/runtime"
	"github.com/a448582655/vibe-proxy/internal/store"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

var (
	prometheusOnce sync.Once
	prometheusSink *metrics.Prometheus
)

// App is a running gateway and the resources it owns.
type App struct {
	server   *http.Server
	database *store.SQLite
	recorder *telemetry.AsyncRecorder

	ready     chan struct{}
	done      chan struct{}
	serveDone chan struct{}

	shutdownOnce sync.Once
	finishOnce   sync.Once

	mu           sync.RWMutex
	status       Status
	serveErr     error
	shutdownErr  error
	shuttingDown bool
	finalizing   bool

	handlerMu       sync.Mutex
	activeHandlers  int
	serveStopped    bool
	handlersDrained chan struct{}
	handlersOnce    sync.Once
}

// Start loads configuration, starts the HTTP gateway, and returns once its
// listener has been bound.
func Start(_ context.Context, options Options) (*App, error) {
	cfg, err := config.LoadRuntime(options.ConfigPath)
	if err != nil {
		return nil, startupError("config", "", err)
	}
	if issues := config.ValidateRuntime(cfg); config.HasErrors(issues) {
		return nil, startupError("validate", cfg.Server.Listen, fmt.Errorf("%+v", issues))
	}

	db, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return nil, startupError("database", cfg.Server.Listen, err)
	}
	if err := db.Retain(cfg.Storage.RetentionDays); err != nil {
		_ = db.Close()
		return nil, startupError("database", cfg.Server.Listen, err)
	}
	if err := db.ConfigurePayloadStorage(
		time.Duration(cfg.Observability.Retention.ContentDays)*24*time.Hour,
		int64(cfg.Observability.Retention.MaxContentStorageMB)<<20,
	); err != nil {
		_ = db.Close()
		return nil, startupError("database", cfg.Server.Listen, err)
	}

	prom := sharedPrometheus()
	recent := telemetry.NewRecentStore(200)
	recorder := telemetry.NewAsyncRecorder(db, telemetry.DefaultRecorderCapacity)
	sink := metrics.MultiSink{recorder, prom, recent}
	runtimeServer := runtime.NewWithOptions(options.ConfigPath, cfg, sink, prom, options.Runtime)
	httpServer := &http.Server{
		Addr:         cfg.Server.Listen,
		ReadTimeout:  cfg.Server.ReadTimeout.Duration,
		WriteTimeout: cfg.Server.WriteTimeout.Duration,
		IdleTimeout:  cfg.Server.IdleTimeout.Duration,
	}
	if httpServer.ReadTimeout == 0 {
		httpServer.ReadTimeout = 30 * time.Second
	}
	if httpServer.IdleTimeout == 0 {
		httpServer.IdleTimeout = 120 * time.Second
	}

	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", cfg.Server.Listen)
	if err != nil {
		flushContext, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = recorder.Close(flushContext)
		cancel()
		_ = db.Close()
		return nil, startupError("listen", cfg.Server.Listen, err)
	}

	app := &App{
		server:          httpServer,
		database:        db,
		recorder:        recorder,
		ready:           make(chan struct{}),
		done:            make(chan struct{}),
		serveDone:       make(chan struct{}),
		handlersDrained: make(chan struct{}),
		status: Status{
			State:     StateStarting,
			Address:   listener.Addr().String(),
			StartedAt: time.Now(),
		},
	}
	httpServer.Handler = app.trackHandlers(runtimeServer.Routes())
	app.setState(StateRunning, nil)
	close(app.ready)
	go app.serve(listener)
	return app, nil
}

// Ready closes once the listener has been bound and the app is running.
func (a *App) Ready() <-chan struct{} { return a.ready }

// Done closes once HTTP serving has stopped and owned resources are closed.
func (a *App) Done() <-chan struct{} { return a.done }

// Address returns the bound listener address.
func (a *App) Address() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status.Address
}

// Status returns an immutable lifecycle snapshot.
func (a *App) Status() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// Wait blocks until the app has finished and returns the terminal error, if any.
func (a *App) Wait() error {
	<-a.done
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.shutdownErr != nil {
		return a.shutdownErr
	}
	return a.serveErr
}

// Shutdown stops HTTP serving and releases owned resources. It is idempotent.
func (a *App) Shutdown(ctx context.Context) error {
	select {
	case <-a.done:
		return a.Wait()
	default:
	}

	a.shutdownOnce.Do(func() {
		a.mu.Lock()
		if a.finalizing {
			a.mu.Unlock()
			return
		}
		a.shuttingDown = true
		a.status.State = StateStopping
		a.mu.Unlock()
		go a.shutdown(ctx)
	})

	select {
	case <-a.done:
		return a.Wait()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) serve(listener net.Listener) {
	err := a.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	a.mu.Lock()
	a.serveErr = err
	shuttingDown := a.shuttingDown
	if !shuttingDown {
		a.finalizing = true
	}
	a.mu.Unlock()
	a.markServeStopped()
	close(a.serveDone)
	if !shuttingDown {
		a.finish()
	}
}

func (a *App) shutdown(ctx context.Context) {
	shutdownErr := a.server.Shutdown(ctx)
	if shutdownErr != nil {
		_ = a.server.Close()
	}
	<-a.serveDone

	a.mu.Lock()
	a.shutdownErr = shutdownErr
	a.mu.Unlock()
	a.finish()
}

func (a *App) finish() {
	a.finishOnce.Do(func() {
		<-a.handlersDrained
		var recorderErr error
		if a.recorder != nil {
			flushContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			recorderErr = a.recorder.Close(flushContext)
			cancel()
		}
		closeErr := errors.Join(recorderErr, a.database.Close())
		a.mu.Lock()
		if a.shutdownErr == nil && closeErr != nil {
			a.shutdownErr = closeErr
		}
		switch {
		case a.shutdownErr != nil:
			a.status.State = StateFailed
			a.status.LastError = a.shutdownErr.Error()
		case a.serveErr != nil:
			a.status.State = StateFailed
			a.status.LastError = a.serveErr.Error()
		default:
			a.status.State = StateStopped
		}
		a.mu.Unlock()
		close(a.done)
	})
}

func (a *App) trackHandlers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.handlerStarted() {
			http.Error(w, "gateway is shutting down", http.StatusServiceUnavailable)
			return
		}
		defer a.handlerFinished()
		next.ServeHTTP(w, r)
	})
}

func (a *App) handlerStarted() bool {
	a.handlerMu.Lock()
	defer a.handlerMu.Unlock()
	if a.serveStopped {
		return false
	}
	a.activeHandlers++
	return true
}

func (a *App) handlerFinished() {
	a.handlerMu.Lock()
	a.activeHandlers--
	shouldClose := a.serveStopped && a.activeHandlers == 0
	a.handlerMu.Unlock()
	if shouldClose {
		a.handlersOnce.Do(func() { close(a.handlersDrained) })
	}
}

func (a *App) markServeStopped() {
	a.handlerMu.Lock()
	a.serveStopped = true
	shouldClose := a.activeHandlers == 0
	a.handlerMu.Unlock()
	if shouldClose {
		a.handlersOnce.Do(func() { close(a.handlersDrained) })
	}
}

func (a *App) setState(state State, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.State = state
	if err != nil {
		a.status.LastError = err.Error()
	}
}

func sharedPrometheus() *metrics.Prometheus {
	prometheusOnce.Do(func() {
		prometheusSink = metrics.New()
	})
	return prometheusSink
}

func startupError(stage, address string, err error) error {
	return &StartupError{Stage: stage, Address: address, Err: err}
}
