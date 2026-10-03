package integration

import (
	"sync/atomic"
)

// IntegrationMetricsCounters carries the observability counters required by
// the Track I1 rollback & observability plan. They follow the same in-process
// atomic-counter pattern as the merchants authorization telemetry.
type IntegrationMetricsCounters struct {
	ConnectionsCreatedTotal       uint64
	ConnectionsStatusChangedTotal uint64
	MigrationRunsTotal            uint64
	MigrationScannedTotal         uint64
	MigrationMappedTotal          uint64
	MigrationReusedTotal          uint64
	MigrationQuarantinedTotal     uint64
	MigrationFailedTotal          uint64
}

// Telemetry records Merchant-owned integration connection and migration
// evidence. Live connection counts derive from the
// merchant_integration_connections table itself; no hot-path COUNT query is
// spent reproducing it in memory.
type Telemetry struct {
	counters IntegrationMetricsCounters
}

func NewTelemetry() *Telemetry {
	return &Telemetry{}
}

func (t *Telemetry) RecordConnectionCreated() {
	atomic.AddUint64(&t.counters.ConnectionsCreatedTotal, 1)
}

func (t *Telemetry) RecordConnectionStatusChanged() {
	atomic.AddUint64(&t.counters.ConnectionsStatusChangedTotal, 1)
}

func (t *Telemetry) RecordMigrationRun(scanned, mapped, reused, quarantined, failed int) {
	atomic.AddUint64(&t.counters.MigrationRunsTotal, 1)
	atomic.AddUint64(&t.counters.MigrationScannedTotal, uint64(scanned))
	atomic.AddUint64(&t.counters.MigrationMappedTotal, uint64(mapped))
	atomic.AddUint64(&t.counters.MigrationReusedTotal, uint64(reused))
	atomic.AddUint64(&t.counters.MigrationQuarantinedTotal, uint64(quarantined))
	atomic.AddUint64(&t.counters.MigrationFailedTotal, uint64(failed))
}

func (t *Telemetry) GetMetrics() IntegrationMetricsCounters {
	return IntegrationMetricsCounters{
		ConnectionsCreatedTotal:       atomic.LoadUint64(&t.counters.ConnectionsCreatedTotal),
		ConnectionsStatusChangedTotal: atomic.LoadUint64(&t.counters.ConnectionsStatusChangedTotal),
		MigrationRunsTotal:            atomic.LoadUint64(&t.counters.MigrationRunsTotal),
		MigrationScannedTotal:         atomic.LoadUint64(&t.counters.MigrationScannedTotal),
		MigrationMappedTotal:          atomic.LoadUint64(&t.counters.MigrationMappedTotal),
		MigrationReusedTotal:          atomic.LoadUint64(&t.counters.MigrationReusedTotal),
		MigrationQuarantinedTotal:     atomic.LoadUint64(&t.counters.MigrationQuarantinedTotal),
		MigrationFailedTotal:          atomic.LoadUint64(&t.counters.MigrationFailedTotal),
	}
}
