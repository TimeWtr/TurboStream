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

package metrics

import (
	ts "github.com/TimeWtr/TurboStream"
)

// Collector Indicator monitoring interface
type Collector interface {
	CollectSwitcher(enable bool) // 采集器开关
	WriteMetrics
	ReadMetrics
	PoolMetrics
	AsyncGoroutineMetrics
	ChannelMetrics
}

// WriteMetrics Write operation indicator
type WriteMetrics interface {
	// ObserveWrite Number of writes, size of writes, number of errors
	ObserveWrite(counts, bytes, errors float64)
}

// ReadMetrics Read data and write indicator data to the readq channel
type ReadMetrics interface {
	// ObserveRead Number of reads, size of writes, number of errors
	ObserveRead(counts, bytes, errors float64)
}

// PoolMetrics Cache pool metrics data
type PoolMetrics interface {
	// AllocInc Difference by which the allocated object count increases
	AllocInc(delta float64)
}

// AsyncGoroutineMetrics The number of goroutines that asynchronously read channel data
type AsyncGoroutineMetrics interface {
	// ObserveAsyncGoroutine Monitoring of the number of asynchronous goroutines,
	// increase/decrease corresponds to the respective number
	ObserveAsyncGoroutine(operation ts.OperationType, delta float64)
}

// ChannelMetrics Channel related indicator data
type ChannelMetrics interface {
	ChannelSwitchMetrics
	ActiveChannelMetrics
}

// ChannelSwitchMetrics Channel switching indicator data
type ChannelSwitchMetrics interface {
	SwitchWithLatency(status ts.SwitchStatus, counts float64, millSeconds float64)
}

// ActiveChannelMetrics Channel indicators of active buffers
type ActiveChannelMetrics interface {
	ObserveActive(counts, size float64)
}
