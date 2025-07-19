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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaiters_RegisterUnregister(t *testing.T) {
	w := newWaiterManager()

	id, _ := w.register()
	if len(w.ws) != 1 {
		t.Errorf("Expected 1 waiter, got %d", len(w.ws))
	}

	w.unregister(id)
	if len(w.ws) != 0 {
		t.Error("Waiter should be removed after unregister")
	}

	w.unregister(id)
}

func TestWaiters_Notify(t *testing.T) {
	t.Run("no waiters", func(_ *testing.T) {
		w := newWaiterManager()
		w.notify(10)
	})

	t.Run("single notification", func(t *testing.T) {
		w := newWaiterManager()
		_, ch := w.register()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ch:
			case <-time.After(100 * time.Millisecond):
				t.Error("Notification not received")
			}
		}()

		time.Sleep(10 * time.Millisecond) // 确保goroutine启动
		w.notify(1)
		wg.Wait()
	})

	t.Run("batch notifications", func(t *testing.T) {
		const (
			numWaiters  = 50
			notifyCount = 30
			timeout     = 500 * time.Millisecond // 更长的超时时间
		)

		w := newWaiterManager()
		var (
			received int32
			wg       sync.WaitGroup
		)

		for i := 0; i < numWaiters; i++ {
			_, ch := w.register()
			wg.Add(1)
			go func(c <-chan struct{}) {
				defer wg.Done()
				select {
				case <-c:
					atomic.AddInt32(&received, 1)
				case <-time.After(timeout):
				}
			}(ch)
		}

		time.Sleep(20 * time.Millisecond)
		w.notify(notifyCount)
		wg.Wait()

		if got := atomic.LoadInt32(&received); got != int32(notifyCount) {
			t.Errorf("Expected %d notifications, got %d", notifyCount, got)
		}
	})
}

func TestWaiters_Close(t *testing.T) {
	w := newWaiterManager()
	_, ch := w.register()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case _, ok := <-ch:
			if ok {
				t.Error("Channel should be closed")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("Close not propagated")
		}
	}()

	time.Sleep(10 * time.Millisecond)
	w.Close()
	wg.Wait()

	id, ch := w.register()
	select {
	case <-ch:
		t.Error("New registrations after close should get closed channel")
	default:
	}
	w.unregister(id)
}

func TestWaiters_EdgeCases(t *testing.T) {
	t.Run("zero dataSize", func(t *testing.T) {
		w := newWaiterManager()
		w.register()
		w.notify(0)
		if len(w.ws) != 1 {
			t.Error("Waiter should not be removed")
		}
	})

	t.Run("large dataSize", func(t *testing.T) {
		w := newWaiterManager()
		for i := 0; i < 2000; i++ {
			w.register()
		}
		w.notify(2000)
		count := 0
		for _, ch := range w.ws {
			select {
			case <-ch:
				count++
			default:
			}
		}
		if count > 0 {
			t.Errorf("Expected at most 1024 notifications, got %d", 2000-len(w.ws))
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		w := newWaiterManager()
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				id, _ := w.register()
				w.unregister(id)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				w.notify(10)
			}
		}()

		wg.Wait()
		if w.closed {
			t.Error("Unexpected close during concurrent access")
		}
	})
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		name     string
		values   []int
		expected int
	}{
		{"single value", []int{5}, 5},
		{"multiple values", []int{3, 1, 4}, 1},
		{"negative values", []int{-1, -5, -3}, -5},
		{"mixed values", []int{10, 5, 15}, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := _min(tt.values...); got != tt.expected {
				t.Errorf("_min() = %v, want %v", got, tt.expected)
			}
		})
	}
}
