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
	"sync/atomic"
	"time"

	ts "github.com/TimeWtr/TurboStream"
)

// BatchCollector Collector for reporting indicator data in batches,
// abstracted to provide interface to the caller
type BatchCollector interface {
	Controller
	Recorder
}

// Recorder Interface provided to the caller
type Recorder interface {
	RecordWrite(size int64, err error)                         // Report write data
	RecordRead(count, size int64, err error)                   // Report reading data
	RecordSwitch(status ts.SwitchStatus, latencySeconds int64) // Report channel switching data
	ObserveAsyncWorker(op ts.OperationType)                    // Report asynchronous goroutine data
	RecordPoolAlloc()                                          // Report pool object creation data
}

// Controller Batch update controller
type Controller interface {
	Start() // Start asynchronous batch update
	Stop()  // Stop asynchronous batch updates
	Flush() // Force immediate batch update
}

// Write Indicators for writing data
type Write struct {
	writeCounts             int64 // The total number of entries written
	writeSizes              int64 // total size written
	writeErrors             int64 // Write failure error count
	activeChannelDataCounts int64 // The number of data written in the active channel
	activeChannelDataSizes  int64 // The size of data written in the active channel
}

func (w *Write) Reset() {
	atomic.StoreInt64(&w.activeChannelDataCounts, 0)
	atomic.StoreInt64(&w.activeChannelDataSizes, 0)
	atomic.StoreInt64(&w.writeSizes, 0)
	atomic.StoreInt64(&w.writeCounts, 0)
	atomic.StoreInt64(&w.writeErrors, 0)
}

// Read Indicators for reading data
type Read struct {
	readCounts int64 // The total number of write and read channels
	readSizes  int64 // The total size written to the read channel
	readErrors int64 // Write read channel error count
}

func (r *Read) Reset() {
	atomic.StoreInt64(&r.readCounts, 0)
	atomic.StoreInt64(&r.readSizes, 0)
	atomic.StoreInt64(&r.readErrors, 0)
}

type Supporting struct {
	asyncWorkerIncCounts int64 // The number of asynchronous processing coroutines increased
	asyncWorkerDecCounts int64 // Asynchronous processing coroutine reduction
	poolAlloc            int64 // Object pool allocation times
	switchCounts         int64 // Buffer switching times
	switchLatency        int64 // switching delay
	skipSwitchCounts     int64 // The number of times the scheduled task skips channel switching
}

func (s *Supporting) Reset() {
	atomic.StoreInt64(&s.asyncWorkerIncCounts, 0)
	atomic.StoreInt64(&s.asyncWorkerDecCounts, 0)
	atomic.StoreInt64(&s.switchCounts, 0)
	atomic.StoreInt64(&s.switchLatency, 0)
	atomic.StoreInt64(&s.skipSwitchCounts, 0)
	atomic.StoreInt64(&s.poolAlloc, 0)
}

var _ Recorder = (*BatchCollectImpl)(nil)

// BatchCollectImpl Batch indicator collector, encapsulates the underlying collector,
// and adds scheduled tasks regularly write indicator data to the underlying collector
type BatchCollectImpl struct {
	w   *Write        // Write data indicator
	r   *Read         // Read data indicator
	sp  *Supporting   // Supporting indicators
	mc  Collector     // Bottom-level indicator collector
	t   *time.Ticker  // timer
	sem chan struct{} // shutdown the timer
}

func NewBatchCollector(mc Collector) *BatchCollectImpl {
	const duration = time.Second * 5
	b := &BatchCollectImpl{
		w:   &Write{},
		r:   &Read{},
		sp:  &Supporting{},
		mc:  mc,
		t:   time.NewTicker(duration),
		sem: make(chan struct{}),
	}

	b.mc.CollectSwitcher(true)

	return b
}

func (b *BatchCollectImpl) RecordWrite(size int64, err error) {
	if err != nil {
		atomic.AddInt64(&b.w.writeErrors, 1)
		return
	}

	atomic.AddInt64(&b.w.writeCounts, 1)
	atomic.AddInt64(&b.w.writeSizes, size)
	atomic.AddInt64(&b.w.activeChannelDataCounts, 1)
	atomic.AddInt64(&b.w.activeChannelDataSizes, size)
}

func (b *BatchCollectImpl) RecordRead(count, size int64, err error) {
	if err != nil {
		atomic.AddInt64(&b.r.readErrors, 1)
		return
	}

	atomic.AddInt64(&b.r.readCounts, count)
	atomic.AddInt64(&b.r.readSizes, size)
}

func (b *BatchCollectImpl) RecordSwitch(status ts.SwitchStatus, latencySeconds int64) {
	switch status {
	case ts.SwitchSkip:
		atomic.AddInt64(&b.sp.skipSwitchCounts, 1)
	case ts.SwitchSuccess:
		atomic.AddInt64(&b.sp.switchCounts, 1)
		atomic.StoreInt64(&b.sp.switchLatency, latencySeconds)
	case ts.SwitchFailure:
	}
}

func (b *BatchCollectImpl) ObserveAsyncWorker(op ts.OperationType) {
	if op == ts.MetricsIncOp {
		atomic.AddInt64(&b.sp.asyncWorkerIncCounts, 1)
		return
	}

	atomic.AddInt64(&b.sp.asyncWorkerDecCounts, 1)
}

func (b *BatchCollectImpl) RecordPoolAlloc() {
	atomic.AddInt64(&b.sp.poolAlloc, 1)
}

func (b *BatchCollectImpl) Start() {
	go b.asyncWorker()
}

func (b *BatchCollectImpl) Stop() {
	close(b.sem)
}

func (b *BatchCollectImpl) Flush() {
	b.report()
}

func (b *BatchCollectImpl) asyncWorker() {
	for {
		select {
		case <-b.sem:
			return
		case <-b.t.C:
			b.report()
		}
	}
}

// report 同步一次指标数据
func (b *BatchCollectImpl) report() {
	b.mc.ObserveWrite(float64(atomic.LoadInt64(&b.w.writeCounts)),
		float64(atomic.LoadInt64(&b.w.writeSizes)),
		float64(atomic.LoadInt64(&b.w.writeErrors)))
	b.mc.ObserveActive(float64(atomic.LoadInt64(&b.w.activeChannelDataCounts)),
		float64(atomic.LoadInt64(&b.w.activeChannelDataSizes)))
	b.w.Reset()

	b.mc.ObserveRead(float64(atomic.LoadInt64(&b.r.readCounts)),
		float64(atomic.LoadInt64(&b.r.readSizes)),
		float64(atomic.LoadInt64(&b.r.readErrors)))
	b.r.Reset()

	b.mc.ObserveAsyncGoroutine(ts.MetricsIncOp, float64(atomic.LoadInt64(&b.sp.asyncWorkerIncCounts)))
	b.mc.ObserveAsyncGoroutine(ts.MetricsDecOp, float64(atomic.LoadInt64(&b.sp.asyncWorkerDecCounts)))
	b.mc.AllocInc(float64(atomic.LoadInt64(&b.sp.poolAlloc)))
	b.mc.SwitchWithLatency(ts.SwitchSuccess,
		float64(atomic.LoadInt64(&b.sp.switchCounts)),
		float64(atomic.LoadInt64(&b.sp.switchLatency)))
	b.mc.SwitchWithLatency(ts.SwitchSkip,
		float64(atomic.LoadInt64(&b.sp.skipSwitchCounts)), 0)
	b.sp.Reset()
}
