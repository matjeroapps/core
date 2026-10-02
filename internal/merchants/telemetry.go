package merchants

import (
	"log"
	"sync/atomic"
)

type MetricsCounters struct {
	AuthRequestsTotal         uint64
	AuthShadowMatchesTotal    uint64
	AuthShadowMismatchesTotal uint64
}

type Telemetry struct {
	counters MetricsCounters
}

func NewTelemetry() *Telemetry {
	return &Telemetry{}
}

func (t *Telemetry) RecordAuthRequest(mode AuthMode, success bool) {
	atomic.AddUint64(&t.counters.AuthRequestsTotal, 1)
}

func (t *Telemetry) RecordShadowComparison(match bool, details string) {
	if match {
		atomic.AddUint64(&t.counters.AuthShadowMatchesTotal, 1)
	} else {
		atomic.AddUint64(&t.counters.AuthShadowMismatchesTotal, 1)
		log.Printf("[ALERT] [MERCHANT_AUTH_SHADOW_MISMATCH] Shadow comparison failure: %s", details)
	}
}

func (t *Telemetry) GetMetrics() MetricsCounters {
	return MetricsCounters{
		AuthRequestsTotal:         atomic.LoadUint64(&t.counters.AuthRequestsTotal),
		AuthShadowMatchesTotal:    atomic.LoadUint64(&t.counters.AuthShadowMatchesTotal),
		AuthShadowMismatchesTotal: atomic.LoadUint64(&t.counters.AuthShadowMismatchesTotal),
	}
}
