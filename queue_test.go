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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/TimeWtr/TurboStream/core/component"
	qm "github.com/TimeWtr/TurboStream/core/mocks/queue"
	"github.com/TimeWtr/TurboStream/errorx"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestBufferQueue_Evict_Policy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ep := qm.NewMockEvictPolicy(ctrl)
	bq := NewBufferQueue(ep, 10, false)
	newPolicy := qm.NewMockEvictPolicy(ctrl)
	bq.SetEvictPolicy(newPolicy)
}

func TestBufferQueue_Should_Evict(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp := qm.NewMockEvictPolicy(ctrl)
	q := NewBufferQueue(mockEp, 2, false)
	defer q.Close()

	mockEp.EXPECT().ShouldEvict(gomock.Any()).Return(true)
	mockEp.EXPECT().Evict().Return(1)

	err := q.Push(&component.QueueItem{ID: 1})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	err = q.Push(&component.QueueItem{ID: 2})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	err = q.Push(&component.QueueItem{ID: 3})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
}

func TestBufferQueue_PopBuffer_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp := qm.NewMockEvictPolicy(ctrl)
	q := NewBufferQueue(mockEp, 5, false)
	_, err := q.Pick()
	if err == nil {
		t.Fatal("Expected error when popping from empty queue")
	}

	if !errors.Is(err, errorx.ErrQueueEmpty) {
		t.Fatalf("Expected ErrQueueEmpty, got %v", err)
	}
}

func TestBufferQueue_PopBuffer_Normal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp := qm.NewMockEvictPolicy(ctrl)
	q := NewBufferQueue(mockEp, 2, false)
	defer q.Close()

	pushTime := time.Now().UnixMilli()
	err := q.Push(&component.QueueItem{ID: 1, PushTime: pushTime})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	item, err := q.Pick()
	assert.NoError(t, err)
	assert.Equal(t, &component.QueueItem{ID: 1, PushTime: pushTime}, item)
}

func TestBufferQueue_SetEvictPolicy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp1 := qm.NewMockEvictPolicy(ctrl)
	mockEp2 := qm.NewMockEvictPolicy(ctrl)

	q := NewBufferQueue(mockEp1, 5, false)
	q.SetEvictPolicy(mockEp2)
}

func TestBufferQueue_Reset(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp := qm.NewMockEvictPolicy(ctrl)

	q := NewBufferQueue(mockEp, 2, false)
	q.Reset()

	err := q.Push(&component.QueueItem{ID: 1, PushTime: time.Now().UnixMilli()})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	err = q.Push(&component.QueueItem{ID: 2, PushTime: time.Now().UnixMilli()})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
}

func TestBufferQueue_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp := qm.NewMockEvictPolicy(ctrl)
	q := NewBufferQueue(mockEp, 2, false)
	q.Close()
}

func TestBufferQueue_Concurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEp := qm.NewMockEvictPolicy(ctrl)
	mockEp.EXPECT().ShouldEvict(gomock.Any()).Return(false).AnyTimes()

	q := NewBufferQueue(mockEp, 100, false)

	var wg sync.WaitGroup
	workers := 10
	itemsPerWorker := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < itemsPerWorker; j++ {
				item := &component.QueueItem{ID: 1}
				if err := q.Push(item); err != nil {
					t.Errorf("Push error: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < itemsPerWorker; j++ {
				if _, err := q.Pick(); err != nil {
					t.Errorf("Pick error: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	_, err := q.Pick()
	if err == nil {
		t.Error("Queue should be empty after all pops")
	}
}

func TestBufferQueue_AutoEvict(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pushTime := time.Now().UnixMilli()
	mockEp := qm.NewMockEvictPolicy(ctrl)
	mockEp.EXPECT().ShouldEvict(gomock.Any()).Return(true).AnyTimes()
	mockEp.EXPECT().Evict().Return(2).AnyTimes()

	q := NewBufferQueue(mockEp, 1, true)

	err := q.Push(&component.QueueItem{ID: 1, PushTime: pushTime})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	err = q.Push(&component.QueueItem{ID: 2, PushTime: pushTime})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	err = q.Push(&component.QueueItem{ID: 3, PushTime: pushTime + 1})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	time.Sleep(1 * time.Second)
	q.Close()
}
