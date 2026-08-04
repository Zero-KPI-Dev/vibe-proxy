package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

type Prometheus struct {
	requests *prometheus.CounterVec
	ttft     *prometheus.HistogramVec
	tokens   *prometheus.CounterVec
	tpot     *prometheus.HistogramVec
	tps      *prometheus.HistogramVec
}

func New() *Prometheus {
	p := &Prometheus{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vibe_proxy_requests_total", Help: "Total gateway requests."}, []string{"model", "channel", "status"}),
		ttft:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vibe_proxy_ttft_seconds", Help: "Time to first token.", Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}}, []string{"model", "channel"}),
		tokens:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vibe_proxy_tokens_total", Help: "Token counts."}, []string{"model", "channel", "kind"}),
		tpot:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vibe_proxy_tpot_seconds", Help: "Average time per output token."}, []string{"model", "channel"}),
		tps:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vibe_proxy_tps", Help: "Output token throughput."}, []string{"model", "channel"}),
	}
	prometheus.MustRegister(p.requests, p.ttft, p.tokens, p.tpot, p.tps)
	return p
}

func (p *Prometheus) Handler() http.Handler            { return promhttp.Handler() }
func (p *Prometheus) RequestStarted(e telemetry.Event) {}
func (p *Prometheus) Token(e telemetry.Event)          {}

func (p *Prometheus) RequestFinished(e telemetry.Event) {
	status := strconv.Itoa(e.StatusCode)
	p.requests.WithLabelValues(e.VirtualModel, e.ChannelID, status).Inc()
	if e.TTFTMillis > 0 {
		p.ttft.WithLabelValues(e.VirtualModel, e.ChannelID).Observe(float64(e.TTFTMillis) / 1000.0)
	}
	if e.TPOTMillis > 0 {
		p.tpot.WithLabelValues(e.VirtualModel, e.ChannelID).Observe(e.TPOTMillis / 1000.0)
	}
	if e.TPS > 0 {
		p.tps.WithLabelValues(e.VirtualModel, e.ChannelID).Observe(e.TPS)
	}
	p.tokens.WithLabelValues(e.VirtualModel, e.ChannelID, "prompt").Add(float64(e.Usage.PromptTokens))
	p.tokens.WithLabelValues(e.VirtualModel, e.ChannelID, "completion").Add(float64(e.Usage.CompletionTokens))
	p.tokens.WithLabelValues(e.VirtualModel, e.ChannelID, "cache_read").Add(float64(e.Usage.CacheReadTokens))
	p.tokens.WithLabelValues(e.VirtualModel, e.ChannelID, "cache_write").Add(float64(e.Usage.CacheWriteTokens))
}

type MultiSink []telemetry.EventSink

func (m MultiSink) RequestStarted(e telemetry.Event) {
	for _, s := range m {
		if s != nil {
			s.RequestStarted(e)
		}
	}
}
func (m MultiSink) RequestFinished(e telemetry.Event) {
	for _, s := range m {
		if s != nil {
			s.RequestFinished(e)
		}
	}
}
func (m MultiSink) Token(e telemetry.Event) {
	for _, s := range m {
		if s != nil {
			s.Token(e)
		}
	}
}

func (m MultiSink) RecordPayload(snapshot telemetry.PayloadSnapshot) error {
	var firstErr error
	for _, sink := range m {
		if recorder, ok := sink.(telemetry.PayloadRecorder); ok {
			if err := recorder.RecordPayload(snapshot); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m MultiSink) RecordObservation(observation telemetry.TraceObservation) error {
	var firstErr error
	for _, sink := range m {
		if recorder, ok := sink.(telemetry.ObservationRecorder); ok {
			if err := recorder.RecordObservation(observation); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m MultiSink) RecentStore() *telemetry.RecentStore {
	for _, s := range m {
		if r, ok := s.(*telemetry.RecentStore); ok {
			return r
		}
		if provider, ok := s.(interface{ RecentStore() *telemetry.RecentStore }); ok {
			if r := provider.RecentStore(); r != nil {
				return r
			}
		}
	}
	return nil
}

func (m MultiSink) ObservabilityReader() telemetry.ObservabilityReader {
	for _, s := range m {
		if reader, ok := s.(telemetry.ObservabilityReader); ok {
			return reader
		}
		if provider, ok := s.(telemetry.ObservabilityReaderProvider); ok {
			if reader := provider.ObservabilityReader(); reader != nil {
				return reader
			}
		}
	}
	return nil
}
