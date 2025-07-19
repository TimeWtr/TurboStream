// Copyright 2025 TimeWtr
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"net/http"

	ts "github.com/TimeWtr/TurboStream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	mc       *Prometheus
	registry *prometheus.Registry // Indicator registry
)

// GetHandler Return HTTP handler for docking with various frameworks
func GetHandler() http.Handler {
	return promhttp.HandlerFor(
		registry,
		promhttp.HandlerOpts{EnableOpenMetrics: true},
	)
}

var _ Collector = (*Prometheus)(nil)

type Prometheus struct {
	enabled                 bool                   // Whether to enable indicator collection
	writeCounter            *prometheus.CounterVec // The total number of entries written
	writeSizes              prometheus.Counter     // total size written
	writeErrors             prometheus.Counter     // Write failure error count
	readCounter             *prometheus.CounterVec // The total number of write and read channels
	readSizes               prometheus.Counter     // The total size written to the read channel
	readErrors              prometheus.Counter     // Write read channel error count
	switchCounts            prometheus.Counter     // Buffer switching times
	switchLatency           prometheus.Histogram   // switching delay
	skipSwitchCounts        prometheus.Counter     // The number of times the scheduled task skips channel switching
	activeChannelDataCounts prometheus.Gauge       // The number of data written in the active channel
	activeChannelDataSizes  prometheus.Gauge       // The size of data written in the active channel
	asyncWorkers            prometheus.Gauge       // Number of asynchronous processing coroutines
	poolAlloc               prometheus.Counter     // Object pool allocation times
}

func NewPrometheus() *Prometheus {
	mc = &Prometheus{}
	registry = prometheus.NewRegistry()
	return mc.register()
}

func (p *Prometheus) register() *Prometheus {
	const namespace = "turbo stream"
	p.writeCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "write_counts_total",
		Help:      "Number of metrics written by write.",
	}, []string{"result"})
	registry.MustRegister(p.writeCounter)

	p.writeSizes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "write_sizes_total",
		Help:      "Number of metrics written by write sizes.",
	})
	registry.MustRegister(p.writeSizes)

	p.writeErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "write_errors_total",
		Help:      "Number of errors encountered by write.",
	})
	registry.MustRegister(p.writeErrors)

	p.readCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "read_counts_total",
		Help:      "Number of metrics read.",
	}, []string{"result"})
	registry.MustRegister(p.readCounter)

	p.readSizes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "read_metrics_sizes_total",
		Help:      "Number of metrics read sizes.",
	})
	registry.MustRegister(p.readSizes)

	p.readErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "read_metrics_errors_total",
		Help:      "Number of read errors.",
	})
	registry.MustRegister(p.readErrors)

	p.poolAlloc = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "pool_alloc_total",
		Help:      "Number of pool allocations.",
	})
	registry.MustRegister(p.poolAlloc)

	p.asyncWorkers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "async_workers",
		Help:      "Number of async workers.",
	})
	registry.MustRegister(p.asyncWorkers)

	p.switchCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "switch_counts_total",
		Help:      "Number of switches.",
	})
	registry.MustRegister(p.switchCounts)

	p.activeChannelDataCounts = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "active_channel_data_counts",
		Help:      "Number of active channels.",
	})
	registry.MustRegister(p.activeChannelDataCounts)

	p.activeChannelDataSizes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "active_channel_data_sizes",
		Help:      "Number of active channel sizes.",
	})
	registry.MustRegister(p.activeChannelDataSizes)

	p.switchLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "switch_latency",
		Help:      "Latency of switches.",
	})
	registry.MustRegister(p.switchLatency)

	p.skipSwitchCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "skip_switch_counts_total",
		Help:      "Number of skip switches.",
	})
	registry.MustRegister(p.skipSwitchCounts)

	return p
}

func (p *Prometheus) CollectSwitcher(enable bool) {
	p.enabled = enable
}

func (p *Prometheus) ObserveWrite(counts, bytes, errors float64) {
	if !p.enabled {
		return
	}

	p.writeCounter.With(prometheus.Labels{"result": "success"}).Add(counts)
	p.writeSizes.Add(bytes)
	p.writeErrors.Add(errors)
}

func (p *Prometheus) ObserveRead(counts, bytes, errors float64) {
	if !p.enabled {
		return
	}

	p.readCounter.With(prometheus.Labels{"result": "success"}).Add(counts)
	p.readSizes.Add(bytes)
	p.readErrors.Add(errors)
}

func (p *Prometheus) AllocInc(delta float64) {
	if !p.enabled {
		return
	}

	p.poolAlloc.Add(delta)
}

func (p *Prometheus) ObserveAsyncGoroutine(operation ts.OperationType, counts float64) {
	if !p.enabled {
		return
	}

	if operation == ts.MetricsIncOp {
		p.asyncWorkers.Add(counts)
	} else {
		p.asyncWorkers.Add(-counts)
	}
}

func (p *Prometheus) SwitchWithLatency(status ts.SwitchStatus, counts, millSeconds float64) {
	if !p.enabled {
		return
	}

	switch status {
	case ts.SwitchSuccess:
		p.switchCounts.Add(counts)
		p.switchLatency.Observe(millSeconds)
	case ts.SwitchFailure:
	case ts.SwitchSkip:
		p.skipSwitchCounts.Inc()
	}
}

func (p *Prometheus) ObserveActive(counts, size float64) {
	if !p.enabled {
		return
	}

	if size == 0 && counts == 0 {
		p.activeChannelDataCounts.Set(0)
		p.activeChannelDataSizes.Set(0)
	}

	p.activeChannelDataCounts.Add(counts)
	p.activeChannelDataSizes.Add(size)
}
