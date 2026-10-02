package merchants

import (
	"log"
	"sync/atomic"
)

type MetricsCounters struct {
	AuthRequestsTotal            uint64
	AuthAllowedTotal             uint64
	AuthDeniedTotal              uint64
	AuthShadowMatchesTotal       uint64
	AuthShadowMismatchesTotal    uint64
	AuthCrossTenantRejections    uint64
	AuthMissingCapabilityDenials uint64
	AuthMissingMembershipDenials uint64
	AuthFallbackActivationsTotal uint64
	BackfillScannedTotal         uint64
	BackfillMappedTotal          uint64
	BackfillQuarantinedTotal     uint64
	BackfillFailedTotal          uint64
	BackfillRetriedTotal         uint64
	OutboxWriteFailuresTotal     uint64
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

func (t *Telemetry) RecordAuthDecision(mode AuthMode, success bool, reason string) {
	atomic.AddUint64(&t.counters.AuthRequestsTotal, 1)
	if success {
		atomic.AddUint64(&t.counters.AuthAllowedTotal, 1)
		return
	}
	atomic.AddUint64(&t.counters.AuthDeniedTotal, 1)
	switch reason {
	case ErrCapabilitySuspended.Error():
		atomic.AddUint64(&t.counters.AuthMissingCapabilityDenials, 1)
	case ErrMembershipNotFound.Error():
		atomic.AddUint64(&t.counters.AuthMissingMembershipDenials, 1)
	case ErrOwnerMismatch.Error():
		atomic.AddUint64(&t.counters.AuthCrossTenantRejections, 1)
	}
}

func (t *Telemetry) RecordShadowComparison(match bool, details string) {
	if match {
		atomic.AddUint64(&t.counters.AuthShadowMatchesTotal, 1)
	} else {
		atomic.AddUint64(&t.counters.AuthShadowMismatchesTotal, 1)
		log.Printf("[ALERT] [MERCHANT_AUTH_SHADOW_MISMATCH] Shadow comparison failure: %s", details)
	}
}

func (t *Telemetry) RecordFallbackActivation(reason string) {
	atomic.AddUint64(&t.counters.AuthFallbackActivationsTotal, 1)
	log.Printf("[ALERT] [MERCHANT_AUTH_FALLBACK_ACTIVE] Merchant authorization fallback activated: %s", reason)
}

func (t *Telemetry) RecordBackfill(scanned, mapped, quarantined, failed, retried uint64) {
	atomic.AddUint64(&t.counters.BackfillScannedTotal, scanned)
	atomic.AddUint64(&t.counters.BackfillMappedTotal, mapped)
	atomic.AddUint64(&t.counters.BackfillQuarantinedTotal, quarantined)
	atomic.AddUint64(&t.counters.BackfillFailedTotal, failed)
	atomic.AddUint64(&t.counters.BackfillRetriedTotal, retried)
}

func (t *Telemetry) RecordOutboxWriteFailure() {
	atomic.AddUint64(&t.counters.OutboxWriteFailuresTotal, 1)
}

func (t *Telemetry) GetMetrics() MetricsCounters {
	return MetricsCounters{
		AuthRequestsTotal:            atomic.LoadUint64(&t.counters.AuthRequestsTotal),
		AuthAllowedTotal:             atomic.LoadUint64(&t.counters.AuthAllowedTotal),
		AuthDeniedTotal:              atomic.LoadUint64(&t.counters.AuthDeniedTotal),
		AuthShadowMatchesTotal:       atomic.LoadUint64(&t.counters.AuthShadowMatchesTotal),
		AuthShadowMismatchesTotal:    atomic.LoadUint64(&t.counters.AuthShadowMismatchesTotal),
		AuthCrossTenantRejections:    atomic.LoadUint64(&t.counters.AuthCrossTenantRejections),
		AuthMissingCapabilityDenials: atomic.LoadUint64(&t.counters.AuthMissingCapabilityDenials),
		AuthMissingMembershipDenials: atomic.LoadUint64(&t.counters.AuthMissingMembershipDenials),
		AuthFallbackActivationsTotal: atomic.LoadUint64(&t.counters.AuthFallbackActivationsTotal),
		BackfillScannedTotal:         atomic.LoadUint64(&t.counters.BackfillScannedTotal),
		BackfillMappedTotal:          atomic.LoadUint64(&t.counters.BackfillMappedTotal),
		BackfillQuarantinedTotal:     atomic.LoadUint64(&t.counters.BackfillQuarantinedTotal),
		BackfillFailedTotal:          atomic.LoadUint64(&t.counters.BackfillFailedTotal),
		BackfillRetriedTotal:         atomic.LoadUint64(&t.counters.BackfillRetriedTotal),
		OutboxWriteFailuresTotal:     atomic.LoadUint64(&t.counters.OutboxWriteFailuresTotal),
	}
}
