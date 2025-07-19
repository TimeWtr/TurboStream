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
	"container/heap"
	"sync"

	"github.com/TimeWtr/TurboStream/core/component"
)

type MinHeapItem struct {
	// Passive monotonically increasing global sequence unique number, used to ensure passive global community
	sequence int64
	// Passive buffer
	buf *component.SmartBuffer
	// the index in heap
	index int
}

type MinHeap []*MinHeapItem

func (m MinHeap) Len() int {
	return len(m)
}

func (m MinHeap) Less(i, j int) bool {
	return m[i].sequence < m[j].sequence
}

func (m MinHeap) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
	m[i].index = i
	m[j].index = j
}

func (m *MinHeap) Push(x interface{}) {
	n := len(*m)
	item, _ := x.(*MinHeapItem)
	item.index = n
	*m = append(*m, item)
}

func (m *MinHeap) Pop() interface{} {
	old := *m
	n := len(old)
	item := old[n-1]
	item.index = -1
	*m = old[0 : n-1]
	return item
}

type WrapHeap struct {
	heap MinHeap
	mu   sync.RWMutex
}

func NewWrapHeap() *WrapHeap {
	heap.Init(&MinHeap{})
	return &WrapHeap{
		heap: MinHeap{},
		mu:   sync.RWMutex{},
	}
}

func (h *WrapHeap) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.heap.Len()
}

func (h *WrapHeap) Push(item *MinHeapItem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	heap.Push(&h.heap, item)
}

func (h *WrapHeap) Pick() *MinHeapItem {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.heap.Len() == 0 {
		return nil
	}

	item, _ := heap.Pop(&h.heap).(*MinHeapItem)
	return item
}

func (h *WrapHeap) PeekFirst() *MinHeapItem {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.heap[0]
}
