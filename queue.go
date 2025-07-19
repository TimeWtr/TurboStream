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
	"container/list"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TimeWtr/TurboStream/core/component"
	"github.com/TimeWtr/TurboStream/errorx"
)

// EvictPolicy defines an interface for eviction policies.
// Eviction policies are used to determine when and how to evict items from a data structure,
// such as a cache or a queue. Implementations of this interface should provide methods
// to check if eviction is necessary, perform the eviction, return the policy's name,
// and provide metrics related to the eviction process.
//
//go:generate mockgen -destination=./mocks/queue/evictpolicy_mock.go -package queue_mocks github.com/TimeWtr/TurboStream/core EvictPolicy
type EvictPolicy interface {
	// ShouldEvict checks if eviction should be performed.
	// It returns true if eviction is necessary, false otherwise.
	ShouldEvict(ctx *component.EvictContext) bool

	// Evict performs the eviction operation.
	Evict() int

	// Name returns the name of the eviction policy.
	// This can be used for logging, monitoring, or identification purposes.
	Name() string

	// Metrics returns a map of metrics related to the eviction policy.
	// The keys are metric names, and the values are floating-point numbers representing the metric values.
	Metrics() map[string]float64
}

type PolicyConfig struct {
	MaxSize       int
	HighWaterMark float64
	LowWaterMark  float64
	MinEvictCount int
	MaxEvictCount int
	IdleTimeout   time.Duration
}
type FixedThreshold struct{}

//go:generate mockgen -destination=./mocks/queue/bufferqueuemanager_mock.go -package queue_mocks github.com/TimeWtr/TurboStream/core BufferQueueManager
type BufferQueueManager interface {
	Push(s *component.QueueItem) error
	Pick() (*component.QueueItem, error)
	SetEvictPolicy(ep EvictPolicy)
	Reset()
	Close()
}

// BufferQueue the queue based on double link list
type BufferQueue struct {
	q               *list.List
	mu              sync.RWMutex
	ep              EvictPolicy
	evictCount      int
	maxSize         int
	enableAutoEvict bool
	stop            chan struct{}
	state           atomic.Int32
}

func NewBufferQueue(ep EvictPolicy, maxSize int, enableAutoEvict bool) BufferQueueManager {
	bq := &BufferQueue{
		q:               list.New(),
		maxSize:         maxSize,
		ep:              ep,
		stop:            make(chan struct{}),
		enableAutoEvict: enableAutoEvict,
	}

	if bq.enableAutoEvict {
		go bq.autoEvict()
	}

	return bq
}

// len calculates and returns the number of elements currently in the channel of the BufferQueue.
//
//nolint:unused  // this method will use
func (q *BufferQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.q.Len()
}

func (q *BufferQueue) Push(s *component.QueueItem) error {
	if q.state.Load() == 1 {
		return errorx.ErrQueueClosed
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.q.Len() < q.maxSize {
		_ = q.q.PushBack(s)
		return nil
	}

	if q.ep.ShouldEvict(&component.EvictContext{}) {
		evictNum := q.ep.Evict()
		q.evictCount += evictNum
	}
	q.q.PushBack(s)
	return nil
}

func (q *BufferQueue) Pick() (*component.QueueItem, error) {
	if q.state.Load() == 1 {
		return nil, errorx.ErrQueueClosed
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.q.Len() == 0 {
		return nil, errorx.ErrQueueEmpty
	}

	e := q.q.Front()
	q.q.Remove(e)
	buf, _ := e.Value.(*component.QueueItem)
	return buf, nil
}

func (q *BufferQueue) SetEvictPolicy(ep EvictPolicy) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ep = ep
}

func (q *BufferQueue) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.evictCount = 0
	q.q = list.New()
}

func (q *BufferQueue) autoEvict() {
	const evictInterval = time.Millisecond * 100
	ticker := time.NewTicker(evictInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx := &component.EvictContext{
				Queue: q.q,
			}
			q.mu.Lock()
			if !q.ep.ShouldEvict(ctx) {
				q.mu.Unlock()
				continue
			}

			evictNum := q.ep.Evict()
			q.evictCount += evictNum
			q.mu.Unlock()
		case <-q.stop:
			return
		}
	}
}

func (q *BufferQueue) Close() {
	if !q.state.CompareAndSwap(0, 1) {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	close(q.stop)
}
