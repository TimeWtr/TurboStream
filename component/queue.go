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
	"container/list"
	"time"
)

type EvictContext struct {
	QueueSize     int           // current queue size
	Queue         *list.List    // all items in queue
	CurrentMemory uint64        // current used memory
	CPUUsage      float32       // current cpu usage
	LatencyP99    time.Duration // p99 latency
}

type QueueItem struct {
	ID       int64
	Buf      *SmartBuffer
	PushTime int64
}
