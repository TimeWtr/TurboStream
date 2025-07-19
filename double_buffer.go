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

package core

import (
	"context"
	"errors"
	"log"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	ts "github.com/TimeWtr/TurboStream"
	"github.com/TimeWtr/TurboStream/config"
	"github.com/TimeWtr/TurboStream/core/component"
	metrics2 "github.com/TimeWtr/TurboStream/core/metrics"
	"github.com/TimeWtr/TurboStream/errorx"
)

const (
	SmallDataThreshold      = 1024            // Small data threshold (<1KB)
	LargeDataThreshold      = 32 * 1024       // Big data threshold (>32KB)
	MediumDataCacheDuration = 5 * time.Second // Cache time for medium-sized data
	SwitchCheckInterval     = 5 * time.Millisecond
)

type Options func(buffer *DoubleBuffer) error

// WithMetrics Enable indicator collection and specify the collector type
func WithMetrics(collector ts.CollectorType) Options {
	return func(buffer *DoubleBuffer) error {
		if !collector.Validate() {
			return errors.New("invalid metrics collector")
		}

		buffer.enableMetrics = true
		switch collector {
		case ts.PrometheusCollector:
			buffer.mc = metrics2.NewBatchCollector(metrics2.NewPrometheus())
		case ts.OpenTelemetryCollector:
		}

		return nil
	}
}

// DoubleBuffer Double buffer design
type DoubleBuffer struct {
	// The active buffer is used to receive written data in real time. When the channel switching
	// condition is met, it will switch to the asynchronous processing buffer.
	active *component.SmartBuffer
	// The asynchronous read buffer is used to process data asynchronously. The buffer is placed
	// in the blocking minimum heap for sorting and waiting for asynchronous goroutine processing.
	// When the channel is switched, the active buffer is switched to the asynchronous read buffer.
	// The asynchronous read buffer is assigned a new buffer and switched to the active buffer to
	// receive write data in real time.
	passive *component.SmartBuffer
	// write data lock
	writeLock sync.Mutex
	// The lock to protect channels, only to swap channel.
	switchLock sync.RWMutex
	// The buffer should to read data.
	currentBuffer *component.SmartBuffer
	// Turn off buffer signal
	stop chan struct{}
	// Buffer capacity
	size int32
	// The number of data entries written to the current active buffer
	count atomic.Int32
	// Buffer state
	state int32
	// The time of last switch, in milliseconds
	lastSwitch atomic.Int64
	// buffer object pool
	pool sync.Pool
	//// Buffer for safe batch reads
	//readq chan [][]byte
	// Synchronous control
	wg sync.WaitGroup
	// A globally monotonically increasing unique sequence number used to perform sequential operations
	// on passive
	sequence atomic.Int64
	// The passive sequence number currently being processed by the asynchronous program, concurrent
	// security updates
	currentSequence atomic.Int64
	// The minimum heap is used to sort multiple passives to be processed. When switching channels,
	// the active channel will be converted to a passive channel.
	// Put it into the taskQueue for asynchronous processing. Because the taskQueue has a capacity limit,
	// if the taskQueue is full, this passive needs to block and wait, which will affect subsequent channel
	// switching and data writing. To solve this problem, use sequence+the minimum heap finds the passive
	// corresponding to the smallest sequence. The passive that needs to be switched is directly written to
	// the minimum heap for sorting. Each time it gets the passive that needs to be processed first, there
	// is no need to block and wait.
	pendingHeap *WrapHeap
	// Used to determine whether to enable indicator monitoring.
	enableMetrics bool
	// Batch indicator collector for receiving indicator data from double-buffered channels in real time.
	mc metrics2.BatchCollector
	// The config to switch channel.
	sc config.SwitchConfig
	// The condition to switch channels.
	scd      *config.SwitchCondition
	scNotify <-chan struct{}
	// The strategy switching channels.
	sw SwitchStrategy
	// The read mode, include zero copy read and safe read.
	readMode *ts.ReadMode
	// Waiters manager
	wm *WaiterManager
	// Life cycle manager
	pm *LifeCycleManager
}

func NewDoubleBuffer(size int32, sc *config.SwitchCondition, opts ...Options) (*DoubleBuffer, error) {
	pm := NewLifeCycleManager()

	d := &DoubleBuffer{
		active:      component.NewSmartBuffer(size),
		passive:     component.NewSmartBuffer(size),
		stop:        make(chan struct{}),
		size:        size,
		scd:         sc,
		sc:          sc.GetConfig(),
		scNotify:    sc.Register(),
		pendingHeap: NewWrapHeap(),
		sw:          NewDefaultStrategy(),
		mc:          metrics2.NewBatchCollector(metrics2.NewPrometheus()),
		wg:          sync.WaitGroup{},
		wm:          newWaiterManager(),
		state:       ts.WritingState,
		pm:          pm,
	}

	const firstSequence = 1
	d.currentSequence.Store(firstSequence)
	d.lastSwitch.Store(time.Now().UnixMilli())

	for _, opt := range opts {
		if err := opt(d); err != nil {
			return nil, err
		}
	}

	d.pool = sync.Pool{
		New: func() interface{} {
			return component.NewSmartBuffer(size)
		},
	}

	d.active, _ = d.pool.Get().(*component.SmartBuffer)
	d.passive, _ = d.pool.Get().(*component.SmartBuffer)

	const workers = 2
	d.wg.Add(workers)
	go d.swapMonitor()
	go d.asyncRecycleWorker()

	return d, nil
}

func (d *DoubleBuffer) Write(p []byte) error {
	if atomic.LoadInt32(&d.state) == ts.ClosedState {
		return errorx.ErrBufferClose
	}

	header := (*reflect.SliceHeader)(unsafe.Pointer(&p))
	bufferItem := component.BufferItem{
		Ptr:  unsafe.Pointer(header.Data),
		Size: int32(header.Len),
	}

	switch {
	case bufferItem.Size < SmallDataThreshold:
		buf, _ := d.pm.SmallPool.Get().([]byte)
		if int32(cap(buf)) < bufferItem.Size {
			buf = make([]byte, 0, bufferItem.Size)
		}
		buf = buf[:bufferItem.Size]
		copy(buf, p)
		bufferItem.Ptr = unsafe.Pointer(&buf[0])
	case bufferItem.Size > LargeDataThreshold:
		d.pm.BigDataPool.Put(uintptr(bufferItem.Ptr), int(bufferItem.Size))
	default:
		d.pm.MediumPool.Put(uintptr(bufferItem.Ptr), time.Now())
	}

	// fast path
	if atomic.LoadInt32(&d.state) == ts.WritingState {
		if err := d.active.Write(bufferItem); err == nil {
			_ = d.count.Add(1)
			return nil
		}
	}

	// low path
	d.writeLock.Lock()
	defer d.writeLock.Unlock()
	switch d.active.GetState() {
	case ts.ReadOnly:
		// try switch channel.
		d.switchChannel()
	case ts.Switching:
		// waiting other switch channel
		runtime.Gosched()
	default:
	}

	err := d.active.Write(bufferItem)
	if err == nil {
		_ = d.count.Add(1)
		return nil
	}

	return err
}

// needSwitch determines whether a channel switch needs to be executed. The switching conditions
// are as follows:
// 1. The comprehensive factor exceeds the threshold
// 2. The size of the current active buffer exceeds the buffer capacity
// 3. The time interval between the current time and the last switch exceeds the specified time window
// If any of the conditions is met, the channel switch needs to be executed immediately.
func (d *DoubleBuffer) needSwitch() bool {
	currentCount := d.count.Load()
	lastSwitch := time.UnixMilli(d.lastSwitch.Load())
	elapsed := time.Since(lastSwitch)
	select {
	case <-d.scNotify:
		d.sc = d.scd.GetConfig()
	default:
	}

	return d.sw.needSwitch(currentCount, d.size, elapsed, d.sc.TimeThreshold)
}

// switchChannel Perform channel switching
func (d *DoubleBuffer) switchChannel() {
	// set switch flag
	if !atomic.CompareAndSwapInt32(&d.state, ts.WritingState, ts.SwitchingState) {
		// TODO add skip switch metrics
		return
	}

	d.switchLock.Lock()
	defer func() {
		atomic.StoreInt32(&d.state, ts.WritingState)
		d.switchLock.Unlock()
	}()

	d.active.SetState(ts.ReadOnly)
	newBuf, _ := d.pool.Get().(*component.SmartBuffer)
	buf := d.swapChannels(newBuf)
	d.count.Store(0)
	d.lastSwitch.Store(time.Now().UnixMilli())

	go func() {
		item := MinHeapItem{sequence: d.sequence.Add(1), buf: buf}
		d.pendingHeap.Push(&item)
		log.Printf("current heap length: %d", d.pendingHeap.Len())

		go d.wm.notify(buf.Len())
	}()
}

func (d *DoubleBuffer) swapChannels(newBuf *component.SmartBuffer) *component.SmartBuffer {
	oldActive := d.active
	d.active, d.passive = d.passive, newBuf
	return oldActive
}

func (d *DoubleBuffer) swapMonitor() {
	defer d.wg.Done()

	ticker := time.NewTicker(SwitchCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			if d.needSwitch() {
				d.switchChannel()
			}
		}
	}
}

func (d *DoubleBuffer) pickBufferFromHeap() bool {
	const timeSleep = 10 * time.Millisecond
	counter := 0
	maxRetires := 3
	for counter < maxRetires {
		bufferItem := d.pendingHeap.Pick()
		if bufferItem == nil {
			return false
		}

		if bufferItem.sequence == d.currentSequence.Load() {
			d.currentBuffer = bufferItem.buf
			d.currentSequence.Add(1)
			break
		}

		d.pendingHeap.Push(bufferItem)
		counter++
		time.Sleep(timeSleep)
	}

	return d.currentBuffer != nil
}

func (d *DoubleBuffer) RegisterReadMode(readMode ts.ReadMode) error {
	if !readMode.Validate() {
		return errors.New("invalid read mode")
	}

	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&d.readMode)),
		unsafe.Pointer(&readMode))
	return nil
}

func (d *DoubleBuffer) createByteSliceFromPointer(ptr unsafe.Pointer, size int32) []byte {
	bytePtr := (*byte)(ptr)
	return unsafe.Slice(bytePtr, int(size))
}

// recycle releases resources, three different sizes.
// 1. SmallData: Get the []byte corresponding to ptr, reset and put it back into the buffer pool
// 2. MediumData: Delete the mapping relationship between ptr and time
// 3. LargeData: -1 the reference count of ptr in the pool
func (d *DoubleBuffer) recycle(ptr unsafe.Pointer, size int32) {
	if ptr == nil || size <= 0 {
		return
	}

	switch {
	case size < SmallDataThreshold:
	case size > LargeDataThreshold:
		ptrVal := uintptr(ptr)
		d.pm.BigDataPool.Release(ptrVal)
	default:
		ptrVal := uintptr(ptr)
		d.pm.MediumPool.Release(ptrVal)
	}
}

func (d *DoubleBuffer) asyncRecycleWorker() {
	defer d.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.pm.Cleanup()
		}
	}
}

// safeRead Read data, secure API, return default copy.
func (d *DoubleBuffer) safeRead() (DataChunk, bool) {
	ptr, size := d.currentBuffer.Pop()
	if size == 0 {
		return DataChunk{}, false
	}

	res := d.createByteSliceFromPointer(ptr, size)
	switch {
	case size < SmallDataThreshold:
		buf, _ := d.pm.SmallPool.Get().([]byte)
		if int32(cap(buf)) < size {
			buf = make([]byte, 0, size)
		}
		buf = buf[:size]
		copy(buf, res)
		return DataChunk{
			data: buf,
			free: func() {
				buf = buf[:0]
				d.pm.SmallPool.Put(buf)
			},
		}, true
	case size > LargeDataThreshold:
		// Big data returns a copy (safe default)
		return DataChunk{
			data: res,
			free: func() {
				d.recycle(ptr, size)
			},
		}, true
	default:
		if d.pm.MediumPool.IsValid(uintptr(ptr)) {
			// Data within the validity period (zero copy return)
			return DataChunk{
				data: res,
				free: func() {
					d.recycle(ptr, size)
				},
			}, true
		}

		// Cache invalidation, return a copy (safe default)
		data := make([]byte, size)
		copy(data, res)
		return DataChunk{
			data: data,
			free: func() {
				d.recycle(ptr, size)
			},
		}, true
	}
}

// zeroCopyRead is a non-safe API. When using this API, you must ensure that the data is not modified
// after Write. Otherwise, the zero-copy data will be wrong.
func (d *DoubleBuffer) zeroCopyRead() (DataChunk, bool) {
	ptr, size := d.currentBuffer.Pop()
	if ptr == nil {
		return DataChunk{}, false
	}

	res := d.createByteSliceFromPointer(ptr, size)

	if size > LargeDataThreshold {
		ptrVal := uintptr(ptr)
		d.pm.BigDataPool.Release(ptrVal)
	}

	return DataChunk{
		data: res,
		free: func() {
			d.pm.BigDataPool.Free(ptr, int(size))
		},
	}, true
}

// BlockingRead Blocking reading requires passing in a Context with timeout control.
// If there is data, read the data immediately and return. If there is no data, wait
// for new data in a blocking manner until the context times out or the channel is closed.
// It will not block forever.
func (d *DoubleBuffer) BlockingRead(ctx context.Context) (DataChunk, error) {
	data, err := d.tryRead()
	if err == nil {
		return data, nil
	}

	id, ch := d.wm.register()
	defer d.wm.unregister(id)

	select {
	case <-ch:
		return d.tryRead()
	case <-ctx.Done():
		return DataChunk{}, ctx.Err()
	case <-d.stop:
		return DataChunk{}, errorx.ErrBufferClose
	}
}

func (d *DoubleBuffer) tryRead() (DataChunk, error) {
	if d.currentBuffer == nil || d.currentBuffer.Len() == 0 {
		if d.currentBuffer != nil {
			uselessBuffer := d.currentBuffer
			uselessBuffer.Reset()
			d.currentBuffer = nil
			d.pool.Put(uselessBuffer)
		}

		if !d.pickBufferFromHeap() {
			return DataChunk{}, errorx.ErrNoBuffer
		}
	}

	readMode := *(*ts.ReadMode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&d.readMode))))
	switch readMode {
	case ts.SafeRead:
		res, ok := d.safeRead()
		if ok {
			return res, nil
		}
	case ts.ZeroCopyRead:
		res, ok := d.zeroCopyRead()
		if ok {
			return res, nil
		}
	default:
		return DataChunk{}, errorx.ErrReadMode
	}

	return DataChunk{}, errorx.ErrRead
}

func (d *DoubleBuffer) RegisterCallback(_ DataCallBack) UnregisterFunc {
	return func() {}
}

func (d *DoubleBuffer) BatchRead(_ context.Context, _ int) [][]byte {
	return nil
}

func (d *DoubleBuffer) drainProcessor() {
	for d.pendingHeap.Len() > 0 {
		item := d.pendingHeap.Pick()
		if item.buf != nil {
			d.pool.Put(item.buf)
		}
	}
}

func (d *DoubleBuffer) Close() {
	if !atomic.CompareAndSwapInt32(&d.state, ts.WritingState, ts.ClosedState) {
		return
	}

	close(d.stop)
	if d.active != nil {
		d.pool.Put(d.active)
	}

	if d.passive != nil {
		d.pool.Put(d.passive)
	}
	d.wg.Wait()

	d.drainProcessor()
}
