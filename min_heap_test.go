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
	"testing"
)

func TestSequenceHeap_DuplicateSequences(t *testing.T) {
	t.Run("DuplicateSequences", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		items := []*MinHeapItem{
			{sequence: 100},
			{sequence: 100},
			{sequence: 100},
		}

		for _, item := range items {
			heap.Push(h, item)
		}

		verifyHeap(t, h, items)

		for i := 0; i < 3; i++ {
			item, _ := heap.Pop(h).(*MinHeapItem)
			if item.sequence != 100 {
				t.Errorf("Pop #%d: expected sequence 100, got %d", i+1, item.sequence)
			}
		}
	})
}

func TestSequenceHeap_MultipleItemsInOrder(t *testing.T) {
	t.Run("MultipleItemsInOrder", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		items := []*MinHeapItem{
			{sequence: 100},
			{sequence: 200},
			{sequence: 300},
		}

		for _, item := range items {
			heap.Push(h, item)
		}

		verifyHeap(t, h, items)

		expectedSequence := []uint64{100, 200, 300}
		for i, expected := range expectedSequence {
			item, _ := heap.Pop(h).(*MinHeapItem)
			if uint64(item.sequence) != expected {
				t.Errorf("Pop #%d: expected sequence %d, got %d", i+1, expected, item.sequence)
			}
		}
	})
}

func TestSequenceHeap_MultipleItemsRandomOrder(t *testing.T) {
	t.Run("MultipleItemsRandomOrder", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		items := []*MinHeapItem{
			{sequence: 500},
			{sequence: 100},
			{sequence: 300},
			{sequence: 200},
			{sequence: 400},
		}

		for _, item := range items {
			heap.Push(h, item)
		}

		verifyHeap(t, h, items)

		expectedSequence := []uint64{100, 200, 300, 400, 500}
		for i, expected := range expectedSequence {
			item, _ := heap.Pop(h).(*MinHeapItem)
			if uint64(item.sequence) != expected {
				t.Errorf("Pop #%d: expected sequence %d, got %d", i+1, expected, item.sequence)
			}
		}
	})
}

func TestSequenceHeap_EmptyHeap(t *testing.T) {
	t.Run("EmptyHeap", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		if h.Len() != 0 {
			t.Errorf("Expected empty heap, got length %d", h.Len())
		}
	})
}

func TestSequenceHeap_SingleItem(t *testing.T) {
	t.Run("SingleItem", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		item := &MinHeapItem{sequence: 100}
		heap.Push(h, item)

		if h.Len() != 1 {
			t.Errorf("Expected heap length 1, got %d", h.Len())
		}

		if item.index != 0 {
			t.Errorf("Expected index 0, got %d", item.index)
		}

		popped, _ := heap.Pop(h).(*MinHeapItem)
		if popped != item {
			t.Error("Popped item doesn't match pushed item")
		}

		if popped.index != -1 {
			t.Errorf("Expected index -1 after pop, got %d", popped.index)
		}

		if h.Len() != 0 {
			t.Errorf("Expected empty heap after pop, got length %d", h.Len())
		}
	})
}

func TestSequenceHeap_Pop(t *testing.T) {
	t.Run("HeapOperationsAfterPop", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		items := []*MinHeapItem{
			{sequence: 500},
			{sequence: 100},
			{sequence: 300},
			{sequence: 200},
			{sequence: 400},
		}

		for _, item := range items {
			heap.Push(h, item)
		}

		heap.Pop(h)
		heap.Pop(h)

		newItems := []*MinHeapItem{
			{sequence: 150},
			{sequence: 50},
		}

		for _, item := range newItems {
			heap.Push(h, item)
		}

		verifyHeap(t, h, append(items[2:], newItems...))
		expectedSequence := []uint64{50, 150, 300, 400, 500}
		for i, expected := range expectedSequence {
			item, _ := heap.Pop(h).(*MinHeapItem)
			if uint64(item.sequence) != expected {
				t.Errorf("Pop #%d: expected sequence %d, got %d", i+1, expected, item.sequence)
			}
		}
	})
}

func TestSequenceHeap(t *testing.T) {
	t.Run("MultipleItemsReverseOrder", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		items := []*MinHeapItem{
			{sequence: 300},
			{sequence: 200},
			{sequence: 100},
		}

		for _, item := range items {
			heap.Push(h, item)
		}

		verifyHeap(t, h, items)

		expectedSequence := []uint64{100, 200, 300}
		for i, expected := range expectedSequence {
			item, _ := heap.Pop(h).(*MinHeapItem)
			if uint64(item.sequence) != expected {
				t.Errorf("Pop #%d: expected sequence %d, got %d", i+1, expected, item.sequence)
			}
		}
	})

	t.Run("IndexMaintenance", func(t *testing.T) {
		h := &MinHeap{}
		heap.Init(h)

		items := []*MinHeapItem{
			{sequence: 300},
			{sequence: 200},
			{sequence: 100},
			{sequence: 400},
		}

		for _, item := range items {
			heap.Push(h, item)
		}

		for i, item := range *h {
			if item.index != i {
				t.Errorf("Item %d: expected index %d, got %d", item.sequence, i, item.index)
			}
		}

		popped, _ := heap.Pop(h).(*MinHeapItem)
		if popped.sequence != 100 {
			t.Errorf("Expected first pop to be 100, got %d", popped.sequence)
		}

		for i, item := range *h {
			if item.index != i {
				t.Errorf("After pop: item %d: expected index %d, got %d", item.sequence, i, item.index)
			}
		}

		if popped.index != -1 {
			t.Errorf("Popped item should have index -1, got %d", popped.index)
		}
	})
}

// verifyHeap 验证堆属性和索引正确性
func verifyHeap(t *testing.T, h *MinHeap, items []*MinHeapItem) {
	t.Helper()

	// 验证堆长度
	if h.Len() != len(items) {
		t.Errorf("Expected heap length %d, got %d", len(items), h.Len())
	}

	// 验证堆属性（最小堆）
	for i := 0; i < h.Len(); i++ {
		left := 2*i + 1
		right := 2*i + 2

		if left < h.Len() && (*h)[left].sequence < (*h)[i].sequence {
			t.Errorf("Heap property violated at index %d: %d > %d",
				i, (*h)[i].sequence, (*h)[left].sequence)
		}

		if right < h.Len() && (*h)[right].sequence < (*h)[i].sequence {
			t.Errorf("Heap property violated at index %d: %d > %d",
				i, (*h)[i].sequence, (*h)[right].sequence)
		}
	}

	// 验证索引正确性
	for i, item := range *h {
		if item.index != i {
			t.Errorf("Item %d: expected index %d, got %d", item.sequence, i, item.index)
		}
	}
}
