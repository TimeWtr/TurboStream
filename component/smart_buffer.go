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

package component

import (
	"errors"
	"runtime"
	"sync/atomic"
	"unsafe"

	ts "github.com/TimeWtr/TurboStream"
	"github.com/TimeWtr/TurboStream/errorx"
)

type BufferItem struct {
	Ptr  unsafe.Pointer
	Size int32
}

type SmartBuffer struct {
	buf      []BufferItem  // stores the header information corresponding to []byte
	head     int32         // Write index
	tail     int32         // read index
	count    int32         // the number of data currently written
	capacity int32         // smart buffer capacity setting
	state    int32         // smart buffer state
	sem      chan struct{} // closed signal
}

func NewSmartBuffer(capacity int32) *SmartBuffer {
	s := &SmartBuffer{
		buf:      make([]BufferItem, capacity),
		head:     -1,
		tail:     -1,
		capacity: capacity,
		state:    ts.WriteOnly,
		sem:      make(chan struct{}),
	}
	atomic.StoreInt32(&s.count, 0)

	return s
}

func (s *SmartBuffer) SetState(state int32) {
	atomic.StoreInt32(&s.state, state)
}

func (s *SmartBuffer) GetState() int32 {
	return atomic.LoadInt32(&s.state)
}

func (s *SmartBuffer) Len() int {
	return int(atomic.LoadInt32(&s.count))
}

func (s *SmartBuffer) Write(bufferItem BufferItem) error {
	if s.state == ts.ClosedState {
		return errorx.ErrBufferClose
	}

	return s.Push(bufferItem)
}

func (s *SmartBuffer) calcPos(index int32) int32 {
	if s.capacity <= 0 {
		return 0
	}

	pos := index % s.capacity
	if pos < 0 {
		pos += s.capacity
	}
	return pos % s.capacity
}

// Push The method that actually executes the data writing
func (s *SmartBuffer) Push(sli BufferItem) error {
	for {
		if atomic.LoadInt32(&s.state) == ts.ReadOnly {
			return errors.New("buffer is read-only")
		}

		currentCount := atomic.LoadInt32(&s.count)
		if currentCount >= s.capacity {
			atomic.CompareAndSwapInt32(&s.state, ts.WriteOnly, ts.ReadOnly)
			return errors.New("buffer is read-only")
		}

		head := atomic.LoadInt32(&s.head)
		newHead := head + 1
		if atomic.CompareAndSwapInt32(&s.head, head, newHead) {
			pos := s.calcPos(head)

			const maxRetry = 3
			for i := 0; i < maxRetry; i++ {
				if s.buf[pos].Ptr == nil {
					break
				}

				//if i < maxRetry-1 {
				//	runtime.Gosched()
				//}
			}

			s.buf[pos] = sli
			atomic.AddInt32(&s.count, 1)
			return nil
		}
	}
}

func (s *SmartBuffer) Reset() {
	s.state = ts.WriteOnly
	s.count = 0
	s.head, s.tail = -1, -1
	select {
	case <-s.sem:
	default:
	}
}

func (s *SmartBuffer) Close() {
	close(s.sem)
}

// Pop Execute data acquisition and return pointer and data length
func (s *SmartBuffer) Pop() (ptr unsafe.Pointer, size int32) {
	for {
		currentCount := atomic.LoadInt32(&s.count)
		if currentCount == 0 {
			return nil, 0
		}

		tail := atomic.LoadInt32(&s.tail)
		newTail := tail + 1
		if atomic.CompareAndSwapInt32(&s.tail, tail, newTail) {
			pos := s.calcPos(tail)

			const maxRetry = 3
			var item BufferItem
			for i := 0; i < maxRetry; i++ {
				item = s.buf[pos]
				if item.Ptr != nil {
					break
				}

				if i < maxRetry-1 {
					runtime.Gosched()
				}
			}

			s.buf[pos] = BufferItem{}
			atomic.AddInt32(&s.count, -1)

			return item.Ptr, item.Size
		}

		runtime.Gosched()
	}
}
