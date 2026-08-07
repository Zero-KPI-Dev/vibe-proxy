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

const defaultRetentionSweepInterval = time.Hour

// App is a running gateway and the resources it owns.
type App struct {
	server      *http.Server
	adminServer *http.Server
	database    *store.SQLite
	recorder    *telemetry.AsyncRecorder

	retentionCancel context.CancelFunc
	retentionDone   chan struct{}
	retentionPolicy *retentionPolicy

	ready         chan struct{}
	done          chan struct{}
	serveDone     chan struct{}
	serveDoneOnce sync.Once

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
	serveRemaining  int
	serveStopped    bool
	handlersDrained chan struct{}
	handlersOnce    sync.Once
}

type retentionPolicy struct {
	mu                sync.RWMutex
	summaryDays       int
	contentRetention  time.Duration
	payloadQuotaBytes int64
}

func newRetentionPolicy(cfg *config.RuntimeConfig) *retentionPolicy {
	policy := &retentionPolicy{}
	policy.update(cfg)
	return policy
}

func (p *retentionPolicy) update(cfg *config.RuntimeConfig) (time.Duration, int64, bool, bool) {
	if p == nil || cfg == nil {
		return 0, 0, false, false
	}
	contentRetention := time.Duration(cfg.Observability.Retention.ContentDays) * 24 * time.Hour
	payloadQuotaBytes := int64(cfg.Observability.Retention.MaxContentStorageMB) << 20
	p.mu.Lock()
	payloadPolicyChanged := p.contentRetention != contentRetention || p.payloadQuotaBytes != payloadQuotaBytes
	summaryPolicyChanged := p.summaryDays != cfg.Observability.Retention.SummariesDays
	p.summaryDays = cfg.Observability.Retention.SummariesDays
	p.contentRetention = contentRetention
	p.payloadQuotaBytes = payloadQuotaBytes
	p.mu.Unlock()
	return contentRetention, payloadQuotaBytes, payloadPolicyChanged, summaryPolicyChanged
}

func (p *retentionPolicy) current() (int, int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.summaryDays, p.payloadQuotaBytes
}

// Start loads configuration, starts the data and control HTTP servers, and
// returns once both listeners have been bound.
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
	if err := db.Retain(cfg.Observability.Retention.SummariesDays); err != nil {
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

	policy := newRetentionPolicy(cfg)
	runtimeOptions := options.Runtime
	previousConfigApplied := runtimeOptions.OnConfigApplied
	runtimeOptions.OnConfigApplied = func(next *config.RuntimeConfig) {
		contentRetention, payloadQuotaBytes, payloadPolicyChanged, summaryPolicyChanged := policy.update(next)
		if payloadPolicyChanged {
			_ = db.ConfigurePayloadStorage(contentRetention, payloadQuotaBytes)
		}
		if summaryPolicyChanged {
			_ = db.Retain(next.Observability.Retention.SummariesDays)
		}
		if previousConfigApplied != nil {
			previousConfigApplied(next)
		}
	}

	prom := sharedPrometheus()
	recent := telemetry.NewRecentStore(200)
	recorder := telemetry.NewAsyncRecorder(db, telemetry.DefaultRecorderCapacity)
	sink := metrics.MultiSink{recorder, prom, recent}
	runtimeServer := runtime.NewWithOptions(options.ConfigPath, cfg, sink, prom, runtimeOptions)
	dataServer := &http.Server{
		Addr:         cfg.Server.Listen,
		ReadTimeout:  cfg.Server.ReadTimeout.Duration,
		WriteTimeout: cfg.Server.WriteTimeout.Duration,
		IdleTimeout:  cfg.Server.IdleTimeout.Duration,
	}
	if dataServer.ReadTimeout == 0 {
		dataServer.ReadTimeout = 30 * time.Second
	}
	if dataServer.IdleTimeout == 0 {
		dataServer.IdleTimeout = 120 * time.Second
	}
	adminServer := &http.Server{
		Addr:         cfg.Server.AdminListen,
		ReadTimeout:  dataServer.ReadTimeout,
		WriteTimeout: dataServer.WriteTimeout,
		IdleTimeout:  dataServer.IdleTimeout,
	}

	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	dataListener, err := listen("tcp", cfg.Server.Listen)
	if err != nil {
		closeStartupResources(recorder, db)
		return nil, startupError("data_listen", cfg.Server.Listen, err)
	}
	adminListener, err := listen("tcp", cfg.Server.AdminListen)
	if err != nil {
		_ = dataListener.Close()
		closeStartupResources(recorder, db)
		return nil, startupError("admin_listen", cfg.Server.AdminListen, err)
	}
	runtimeServer.SetEffectiveListenerAddresses(dataListener.Addr().String(), adminListener.Addr().String())

	app := &App{
		server:          dataServer,
		adminServer:     adminServer,
		database:        db,
		recorder:        recorder,
		ready:           make(chan struct{}),
		done:            make(chan struct{}),
		serveDone:       make(chan struct{}),
		serveRemaining:  2,
		handlersDrained: make(chan struct{}),
		retentionDone:   make(chan struct{}),
		retentionPolicy: policy,
		status: Status{
			State:        StateStarting,
			Address:      dataListener.Addr().String(),
			AdminAddress: adminListener.Addr().String(),
			StartedAt:    time.Now(),
		},
	}
	retentionContext, retentionCancel := context.WithCancel(context.Background())
	app.retentionCancel = retentionCancel
	retentionInterval := options.RetentionSweepInterval
	if retentionInterval <= 0 {
		retentionInterval = defaultRetentionSweepInterval
	}
	go app.runRetention(retentionContext, retentionInterval)
	dataServer.Handler = app.trackHandlers(runtimeServer.DataRoutes())
	adminServer.Handler = app.trackHandlers(runtimeServer.ControlRoutes())
	app.setState(StateRunning, nil)
	close(app.ready)
	go app.serve("data", dataServer, dataListener)
	go app.serve("control", adminServer, adminListener)
	return app, nil
}

func closeStartupResources(recorder *telemetry.AsyncRecorder, db *store.SQLite) {
	flushContext, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = recorder.Close(flushContext)
	cancel()
	_ = db.Close()
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

// AdminAddress returns the bound loopback control-plane listener address.
func (a *App) AdminAddress() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status.AdminAddress
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

func (a *App) serve(plane string, server *http.Server, listener net.Listener) {
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	a.mu.Lock()
	if err != nil && a.serveErr == nil {
		a.serveErr = fmt.Errorf("%s server: %w", plane, err)
	}
	shuttingDown := a.shuttingDown
	if !shuttingDown {
		a.finalizing = true
	}
	a.mu.Unlock()
	if !shuttingDown {
		_ = a.server.Close()
		_ = a.adminServer.Close()
	}
	allStopped := a.markServeStopped()
	if allStopped && !shuttingDown {
		a.finish()
	}
}

func (a *App) shutdown(ctx context.Context) {
	errorsDone := make(chan error, 2)
	go func() { errorsDone <- a.server.Shutdown(ctx) }()
	go func() { errorsDone <- a.adminServer.Shutdown(ctx) }()
	shutdownErr := errors.Join(<-errorsDone, <-errorsDone)
	if shutdownErr != nil {
		_ = a.server.Close()
		_ = a.adminServer.Close()
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
		if a.retentionCancel != nil {
			a.retentionCancel()
			<-a.retentionDone
		}
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

func (a *App) runRetention(ctx context.Context, interval time.Duration) {
	defer close(a.retentionDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Retention is best-effort maintenance. A transient busy database must
			// not terminate the gateway; the next sweep retries both policies.
			summaryDays, payloadQuotaBytes := a.retentionPolicy.current()
			_ = a.database.Retain(summaryDays)
			_ = a.database.PrunePayloads(time.Now().UTC(), payloadQuotaBytes)
		}
	}
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

func (a *App) markServeStopped() bool {
	a.handlerMu.Lock()
	if a.serveRemaining > 0 {
		a.serveRemaining--
	}
	allStopped := a.serveRemaining == 0
	if allStopped {
		a.serveStopped = true
	}
	shouldClose := allStopped && a.activeHandlers == 0
	a.handlerMu.Unlock()
	if shouldClose {
		a.handlersOnce.Do(func() { close(a.handlersDrained) })
	}
	if allStopped {
		a.serveDoneOnce.Do(func() { close(a.serveDone) })
	}
	return allStopped
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
