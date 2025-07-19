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

import "sync"

type WaiterManager struct {
	ws        map[int]chan struct{}
	pool      sync.Pool
	mu        sync.Mutex
	currentID int
	closed    bool
}

func newWaiterManager() *WaiterManager {
	return &WaiterManager{
		ws: make(map[int]chan struct{}),
		pool: sync.Pool{
			New: func() interface{} {
				return make(chan struct{}, 1)
			},
		},
	}
}

func (w *WaiterManager) register() (id int, ch <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()

	id = w.currentID + 1
	w.currentID = id
	notify, _ := w.pool.Get().(chan struct{})
	w.ws[id] = notify

	return id, notify
}

func (w *WaiterManager) unregister(id int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	ch, exist := w.ws[id]
	if !exist {
		return
	}

	w.pool.Put(ch)
	delete(w.ws, id)
}

// notify is used to notify the API that blocks the call to obtain data when data exists
// Set 32 batch notifications each time, and the maximum allowed notification is 1024 to
// prevent unlimited occupancy.
// Fast path: skip notification if there is no notification waiting, or it has been closed.
// Slow path: use batch sending + non-blocking notification. When all notifications are
// completed/no notification waiting for the notification party to end the notification,
func (w *WaiterManager) notify(dataSize int) {
	const (
		MaxWaitersPerBatch = 64
		MaxTotalWaiters    = 1024
	)
	w.mu.Lock()
	defer w.mu.Unlock()

	// fast path
	if len(w.ws) == 0 || w.closed {
		return
	}

	// low path
	totalToNotify := _min(MaxWaitersPerBatch, dataSize, MaxTotalWaiters)
	if totalToNotify <= 0 {
		return
	}

	var shouldDelete []int
	for remaining := totalToNotify; remaining > 0; {
		batchSize := _min(MaxTotalWaiters, remaining)
		var batch []chan struct{}
		for id, notify := range w.ws {
			batch = append(batch, notify)
			shouldDelete = append(shouldDelete, id)

			if len(batch) >= batchSize {
				break
			}
		}

		for _, notify := range batch {
			select {
			case notify <- struct{}{}:
				remaining--
			default:
			}
		}

		for _, id := range shouldDelete {
			delete(w.ws, id)
		}

		if len(w.ws) == 0 {
			break
		}
	}
}

func (w *WaiterManager) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	for _, notify := range w.ws {
		close(notify)
	}
}

// _min calculate the max value.
func _min(values ...int) int {
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}

	return m
}
