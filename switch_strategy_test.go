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
	"testing"
	"time"
)

const (
	bufferSize = 100
	interval   = 10 * time.Millisecond
)

func TestDefaultStrategy(t *testing.T) {
	tests := []struct {
		name         string
		currentCount int32
		elapsed      time.Duration
		want         bool
	}{
		{"buffer_exactly_full", 100, 0, true},
		{"buffer_over_capacity", 101, 0, true},
		{"buffer_full_low_time", 100, 1 * time.Millisecond, true},
		{"time_exactly_interval", 50, 10 * time.Millisecond, true},
		{"time_over_interval", 50, 11 * time.Millisecond, true},
		{"low_buffer_time_over", 0, 11 * time.Millisecond, true},
		{
			name:         "combined_factor_high",
			currentCount: 85, // 85/100 = 0.85
			elapsed:      10 * time.Millisecond,
			want:         true, // (0.6 * 1.0 + 0.4 * 1.0) = 1.0 > 0.85
		},
		{
			name:         "combined_factor_mid",
			currentCount: 70, // 70/100 = 0.7
			elapsed:      10 * time.Millisecond,
			want:         true, // (0.6 * 0.7 + 0.4 * 1.0) = 0.42+0.4 = 0.82 < 0.85? 但这里elapsed=10ms, factor=1.0
		},
		{
			name:         "combined_factor_low",
			currentCount: 50,
			elapsed:      5 * time.Millisecond,
			want:         false, // (0.6 * 0.5 + 0.4 * 0.5) = 0.3 + 0.2 = 0.5 < 0.85
		},

		{"all_low", 0, 0, false},
		{"low_count_mid_time", 50, 5 * time.Millisecond, false},
		{"mid_count_low_time", 70, 1 * time.Millisecond, false},
		{"zero_buffer", 0, 10 * time.Millisecond, true},
		{"max_interval", 50, 10 * time.Millisecond, true},
	}

	strategy := &DefaultStrategy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.needSwitch(tt.currentCount, bufferSize, tt.elapsed, interval)
			if got != tt.want {
				t.Errorf("%s: needSwitch(%d, %d, %v, %v) = %v, want %v",
					tt.name, tt.currentCount, bufferSize, tt.elapsed, interval, got, tt.want)
			}
		})
	}

	t.Run("combined_factor_precision", func(t *testing.T) {
		// 0.84 * 0.6 + 1.0 * 0.4 = 0.504 + 0.4 = 0.904 > 0.85 (should be true)
		got := strategy.needSwitch(84, bufferSize, 10*time.Millisecond, interval)
		if !got {
			t.Error("needSwitch(84, 100, 10ms) should be true")
		}

		// 0.84 * 0.6 + 0.0 * 0.4 = 0.504 < 0.85 (should be false)
		got = strategy.needSwitch(84, bufferSize, 0, interval)
		if got {
			t.Error("needSwitch(84, 100, 0) should be false")
		}
	})
}

func TestSizeOnlyStrategy(t *testing.T) {
	tests := []struct {
		name         string
		currentCount int32
		want         bool
	}{
		{"exactly_full", 100, true},
		{"over_capacity", 101, true},
		{"high_capacity", 99, false},
		{"mid_capacity", 50, false},
		{"empty", 0, false},
		{"negative", -1, false},
	}

	strategy := &SizeOnlyStrategy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.needSwitch(tt.currentCount, bufferSize, 1*time.Hour, 1*time.Second)
			if got != tt.want {
				t.Errorf("needSwitch(%d) = %v, want %v", tt.currentCount, got, tt.want)
			}

			gotZeroTime := strategy.needSwitch(tt.currentCount, bufferSize, 0, 0)
			if got != gotZeroTime {
				t.Errorf("Time parameters affected result: %v vs %v", got, gotZeroTime)
			}
		})
	}
}

func TestTimeWindowOnlyStrategy(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		want    bool
	}{
		{"exact_interval", 10 * time.Millisecond, true},
		{"over_interval", 11 * time.Millisecond, true},
		{"just_under", 9 * time.Millisecond, false},
		{"half_interval", 5 * time.Millisecond, false},
		{"zero_time", 0, false},
		{"negative_time", -1 * time.Millisecond, false},
	}

	strategy := &TimeWindowOnlyStrategy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.needSwitch(100, bufferSize, tt.elapsed, interval)
			if got != tt.want {
				t.Errorf("needSwitch(%v) = %v, want %v", tt.elapsed, got, tt.want)
			}

			gotZeroCount := strategy.needSwitch(0, bufferSize, tt.elapsed, interval)
			if got != gotZeroCount {
				t.Errorf("Count parameters affected result: %v vs %v", got, gotZeroCount)
			}
		})
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("zero_time_interval", func(t *testing.T) {
		strategy := &DefaultStrategy{}

		got := strategy.needSwitch(50, bufferSize, 1, 0)
		if !got {
			t.Error("Should switch when interval is 0")
		}

		sTime := &TimeWindowOnlyStrategy{}
		got = sTime.needSwitch(50, bufferSize, 1, 0)
		if !got {
			t.Error("TimeWindow should switch when interval is 0")
		}
	})

	t.Run("negative_values", func(t *testing.T) {
		strategy := &DefaultStrategy{}

		tests := []struct {
			count    int32
			size     int32
			elapsed  time.Duration
			interval time.Duration
			want     bool
		}{
			{-1, 100, 0, interval, false},
			{50, -100, 0, interval, true},
			{50, 100, -1, interval, false},
			{50, 100, 0, -1, true},
		}

		for i, tt := range tests {
			got := strategy.needSwitch(tt.count, tt.size, tt.elapsed, tt.interval)
			if got != tt.want {
				t.Errorf("Test %d: needSwitch(%d,%d,%v,%v) = %v, want %v",
					i+1, tt.count, tt.size, tt.elapsed, tt.interval, got, tt.want)
			}
		}
	})
}
