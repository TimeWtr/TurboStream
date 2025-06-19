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
	"runtime"
	"sync"
	"time"
	"unsafe"
)

const (
	mediumPoolTTL     = 2 * time.Minute
	initializeBufSize = 512
)

// LifeCycleManager cache pool life cycle manager, used to manage small, medium and large sizes
// Object buffer pool, small object buffer pool directly calls sync.Pool, medium object buffer pool
// with expiration time. The background program regularly clears expired objects, and the large object
// manager stores the reference count of each object. The manager does not perform implicit deletion
// in the background, but provides methods for explicit deletion to the caller.
type LifeCycleManager struct {
	// Small object pool, storing information with a size less than 1024 bytes.
	SmallPool sync.Pool
	// Medium object pool, storing information with a size between 1024 bytes - 32 * 1024 bytes.
	MediumPool *MediumPool
	// Large object pool, storing information with a size larger than 32 * 1024 bytes.
	BigDataPool *BigDataPool
}

func NewLifeCycleManager() *LifeCycleManager {
	return &LifeCycleManager{
		SmallPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, initializeBufSize)
			},
		},
		MediumPool: &MediumPool{
			cache: make(map[uintptr]time.Time),
			mu:    sync.RWMutex{},
			stop:  make(chan struct{}),
		},
		BigDataPool: &BigDataPool{
			pool: make(map[uintptr]BigDataEntry),
			mu:   sync.RWMutex{},
		},
	}
}

func (l *LifeCycleManager) Preload(capacity int32) {
	const preLoadPercent = 0.2
	preloadSize := int(float64(capacity) * preLoadPercent)
	for i := 0; i < preloadSize; i++ {
		l.SmallPool.Put(l.SmallPool.Get())
	}
}

func (l *LifeCycleManager) Cleanup() {
	l.MediumPool.cleanup()
	l.BigDataPool.cleanup()
}

type MediumPool struct {
	cache map[uintptr]time.Time // the relationship of object uintptr and cache time
	mu    sync.RWMutex          // read-write lock
	stop  chan struct{}         // stop signal
}

func (m *MediumPool) Put(ptr uintptr, t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache[ptr] = t
}

// IsValid Determine whether ptr is legal.
func (m *MediumPool) IsValid(ptr uintptr) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.cache[ptr]
	if !ok {
		return false
	}

	return time.Since(t) < mediumPoolTTL
}

// Release the method to release source.
func (m *MediumPool) Release(ptr uintptr) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.cache, ptr)
}

func (m *MediumPool) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.cache) == 0 {
		return
	}

	const maxCleanCounts = 200
	count := 0
	for ptr, t := range m.cache {
		if time.Since(t) > time.Minute {
			delete(m.cache, ptr)
			count++
			if count > maxCleanCounts {
				return
			}
		}
	}
}

type BigDataPool struct {
	pool        map[uintptr]BigDataEntry // the relationship of object uintptr and Entry
	mu          sync.RWMutex             // read-write lock
	currentSize int
	maxSize     int
}

func NewBigDataPool(maxSize int) *BigDataPool {
	return &BigDataPool{
		pool:    make(map[uintptr]BigDataEntry),
		maxSize: maxSize,
	}
}

func (b *BigDataPool) cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.pool) == 0 {
		return
	}

	const maxCleanCounts = 200
	count := 0
	for ptr, bd := range b.pool {
		if bd.ref != 0 {
			continue
		}

		delete(b.pool, ptr)
		bd.owner = 0
		count++
		if count > maxCleanCounts {
			return
		}
	}
}

func (b *BigDataPool) Put(ptr uintptr, size int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.currentSize += size
	b.pool[ptr] = BigDataEntry{
		owner: ptr,
		size:  size,
		ref:   1,
	}
}

// Release the source and reduce counter for this uintptr.
func (b *BigDataPool) Release(ptr uintptr) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exist := b.pool[ptr]
	if !exist {
		return
	}
	entry.ref--
	b.pool[ptr] = entry
}

func (b *BigDataPool) Free(ptr unsafe.Pointer, size int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry, exist := b.pool[uintptr(ptr)]; exist {
		runtime.SetFinalizer((*byte)(ptr), nil)
		entry.ref = 0
		entry.owner = 0
		b.currentSize -= size
		delete(b.pool, uintptr(ptr))
	}
}

type BigDataEntry struct {
	size  int     // data size
	owner uintptr // original data reference
	ref   int     // data reference counter
}
