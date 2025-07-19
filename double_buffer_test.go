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
	"encoding/binary"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	ts "github.com/TimeWtr/TurboStream"
	"github.com/TimeWtr/TurboStream/config"
	"github.com/TimeWtr/TurboStream/core/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/net/context"
)

func fillRealisticData(data []byte) []byte {
	timestamp := uint64(time.Now().UnixNano())
	binary.BigEndian.PutUint64(data[0:8], timestamp)

	rand.Read(data[8:32])
	copy(data[32:40], "HEADER")
	copy(data[40:100], "Lorem ipsum dolor sit amet, consectetur adipiscing elit.")
	rand.Read(data[100:])
	return data
}

func TestSmartBuffer_BasicOperations(t *testing.T) {
	t.Run("Write and safe read small data", func(t *testing.T) {
		sb := component.NewSmartBuffer(10)

		data := []byte("hello")
		err := sb.Write(component.BufferItem{
			Ptr:  unsafe.Pointer(&data[0]),
			Size: int32(len(data)),
		})
		assert.NoError(t, err)

		ptr, _ := sb.Pop()
		assert.NotNil(t, ptr)
		assert.Equal(t, 0, sb.Len())
	})

	t.Run("Write and zero copy read small data", func(t *testing.T) {
		sb := component.NewSmartBuffer(10)

		data := []byte("hello")
		err := sb.Write(component.BufferItem{
			Ptr: unsafe.Pointer(&data),
		})
		assert.NoError(t, err)

		time.Sleep(time.Millisecond * 10)
		ptr, _ := sb.Pop()
		assert.NotNil(t, ptr)
		assert.Equal(t, 0, sb.Len())
	})

	t.Run("Write and zero-copy read large data(64KB)", func(t *testing.T) {
		sb := component.NewSmartBuffer(10)

		data := make([]byte, 64*1024)
		data = fillRealisticData(data)
		for i := 0; i < 10; i++ {
			rand.Read(data)
			err := sb.Write(component.BufferItem{
				Ptr: unsafe.Pointer(&i),
			})
			assert.NoError(t, err)
		}

		for i := 1; i <= 10; i++ {
			ptr, _ := sb.Pop()
			assert.NotNil(t, ptr)
			assert.Equal(t, 10-i, sb.Len())
		}
	})

	t.Run("Write and zero-copy read large data(128KB)", func(t *testing.T) {
		sb := component.NewSmartBuffer(200)

		data := make([]byte, 128*1024)
		data = fillRealisticData(data)
		for i := 0; i < 200; i++ {
			rand.Read(data)
			err := sb.Write(component.BufferItem{
				Ptr: unsafe.Pointer(&i),
			})
			assert.NoError(t, err)
		}

		for i := 1; i <= 200; i++ {
			ptr, _ := sb.Pop()
			assert.NotNil(t, ptr)
			assert.Equal(t, 200-i, sb.Len())
		}
	})
}

func TestDoubleBuffer_BlockingRead(t *testing.T) {
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    time.Second * 5,
	})
	require.NoError(t, err)

	db, err := NewDoubleBuffer(1000, sc)
	require.NoError(t, err)
	defer db.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()

		template := "this is a template, seq: %d"
		for i := 0; i < 20000; i++ {
			err = db.Write([]byte(fmt.Sprintf(template, i)))
			require.NoError(t, err)
		}
	}()

	go func() {
		defer wg.Done()

		_ = db.RegisterReadMode(ts.SafeRead)

		count := 0
		for {
			if count >= 20000 {
				t.Log("received message count: ", count)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			chunk, err1 := db.BlockingRead(ctx)
			cancel()
			if err1 != nil {
				t.Log(err1)
				continue
			}
			t.Log("received chunk data: ", string(chunk.Bytes()))
			chunk.Release()
			count++
		}
	}()

	wg.Wait()
}

func TestDoubleBuffer_SwitchConditions(t *testing.T) {
	t.Run("Switch by capacity", func(t *testing.T) {
		sc, err := config.NewSwitchCondition(config.SwitchConfig{
			PercentThreshold: 80,
			TimeThreshold:    5 * time.Second,
		})
		require.NoError(t, err)

		db, err := NewDoubleBuffer(20, sc)
		require.NoError(t, err)
		defer db.Close()

		for i := 0; i < 50; i++ {
			err = db.Write([]byte{byte(i)})
			require.NoError(t, err)
		}
	})

	t.Run("Switch by time", func(t *testing.T) {
		sc, err := config.NewSwitchCondition(config.SwitchConfig{
			PercentThreshold: 80,
			TimeThreshold:    time.Millisecond * 60,
		})
		require.NoError(t, err)

		db, err := NewDoubleBuffer(20, sc)
		require.NoError(t, err)
		defer db.Close()

		_ = db.Write([]byte("test"))

		// Wait for time-based switch
		time.Sleep(100 * time.Millisecond)

		assert.Equal(t, 1, db.pendingHeap.Len(), "expected one item in heap")
	})

	t.Run("Switch by combined factors", func(t *testing.T) {
		sc, err := config.NewSwitchCondition(config.SwitchConfig{
			PercentThreshold: 80,
			TimeThreshold:    time.Millisecond * 70,
		})
		require.NoError(t, err)

		db, err := NewDoubleBuffer(20, sc)
		require.NoError(t, err)
		defer db.Close()

		// Write enough data to be above threshold but below capacity
		for i := 0; i < 10; i++ {
			_ = db.Write([]byte{byte(i)})
		}

		// Should trigger switch after time passes
		time.Sleep(100 * time.Millisecond)

		assert.Equal(t, 1, db.pendingHeap.Len(), "expected one item in heap")
	})
}

const (
	TestBufferSize    = 10000
	TestItems         = 1000000
	_128BytesDataSize = 128
	_64KBDataSize     = 64 * 1024
	_1024KBDataSize   = 1024 * 1024
)

func BenchmarkBlockingRead_Throughput(b *testing.B) {
	defer goleak.VerifyNone(b,
		goleak.IgnoreCurrent(),
	)

	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    time.Second * 5,
	})
	assert.NoError(b, err)

	db, err := NewDoubleBuffer(TestBufferSize, sc)
	assert.NoError(b, err)
	defer db.Close()

	err = db.RegisterReadMode(ts.ZeroCopyRead)
	assert.NoError(b, err)

	go func() {
		data := make([]byte, _128BytesDataSize)
		for i := 0; i < b.N; i++ {
			if err = db.Write(data); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err1 := db.BlockingRead(ctx)
		if err1 != nil {
			b.Fatalf("BlockingRead failed: %v", err1)
		}
	}

	b.StopTimer()
}

func BenchmarkBlockingRead_PerfMetrics(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Configuration
	const (
		TestBufferSize = 1024 * 1024 // 1MB buffer
		DataSize       = 1024        // 1KB messages
		WarmupMessages = 1000000     // Pre-fill buffer
	)

	// Initialize
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    time.Second * 5,
	})
	if err != nil {
		b.Fatal(err)
	}

	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}

	// Pre-fill buffer
	warmupData := make([]byte, DataSize)
	for i := 0; i < WarmupMessages; i++ {
		if err := db.Write(warmupData); err != nil {
			b.Fatal(err)
		}
	}

	// Writer goroutine (runs for entire benchmark duration)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		data := make([]byte, DataSize)
		for ctx.Err() == nil {
			if err := db.Write(data); err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()

		// Operation we're benchmarking
		data, err := db.BlockingRead(ctx)
		if err != nil {
			b.Fatal(err)
		}
		_ = data // Prevent optimization

		// Report custom metrics
		latency := time.Since(start)
		b.ReportMetric(float64(latency.Nanoseconds()), "ns/msg")

		// Get memory stats
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		b.ReportMetric(float64(m.Mallocs), "allocs/msg")
		b.ReportMetric(float64(m.TotalAlloc), "bytes/msg")
	}

	// Report throughput at the end
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/s")
}

func BenchmarkBlockingRead_PerfMetrics_64KB(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Configuration
	const (
		TestBufferSize = 1024 * 1024 // 1MB buffer
		DataSize       = 1024 * 64   // 1KB messages
		WarmupMessages = 10000       // Pre-fill buffer
	)

	// Initialize
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    time.Second * 5,
	})
	if err != nil {
		b.Fatal(err)
	}

	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err = db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}

	// Pre-fill buffer
	warmupData := make([]byte, DataSize)
	for i := 0; i < WarmupMessages; i++ {
		if err = db.Write(warmupData); err != nil {
			b.Fatal(err)
		}
	}

	// Writer goroutine (runs for entire benchmark duration)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		data := make([]byte, DataSize)
		for ctx.Err() == nil {
			if err = db.Write(data); err != nil {
				return
			}
		}
	}()

	// Benchmark loop - this is what go test -bench measures
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()

		// This is the operation we're benchmarking
		chunk, err := db.BlockingRead(ctx)
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
		if len(chunk.Bytes()) != DataSize {
			b.Fatalf("Unexpected data size: got %d, want %d", len(chunk.Bytes()), DataSize)
		}
		chunk.Release()

		// Report custom metrics per operation
		b.ReportMetric(float64(time.Since(start).Nanoseconds()), "ns/op")

		// Memory stats (optional)
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		b.ReportMetric(float64(m.Mallocs), "allocs/op")
		b.ReportMetric(float64(m.TotalAlloc), "bytes/op")
	}
	b.StopTimer()
}

func BenchmarkBlockingRead_Detailed(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}

	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _128BytesDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
				// Small delay to prevent overwhelming the buffer
				time.Sleep(1 * time.Microsecond)
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		// Start memory stats
		var memBefore, memAfter runtime.MemStats
		runtime.ReadMemStats(&memBefore)
		start := time.Now()

		// Perform read operation
		chunk, err1 := db.BlockingRead(context.Background())
		if err1 != nil {
			b.Fatalf("BlockingRead failed: %v", err1)
		}
		if len(chunk.Bytes()) != _128BytesDataSize {
			b.Fatalf("Unexpected data size: got %d, want %d", len(chunk.Bytes()), _128BytesDataSize)
		}
		chunk.Release()

		// End memory stats
		runtime.ReadMemStats(&memAfter)
		latency := time.Since(start)

		// Report custom metrics
		b.ReportMetric(float64(latency.Nanoseconds()), "ns/op")
		b.ReportMetric(float64(memAfter.Mallocs-memBefore.Mallocs), "allocs/op")
		b.ReportMetric(float64(memAfter.TotalAlloc-memBefore.TotalAlloc), "bytes/op")
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

func BenchmarkBlockingRead_Throughput_Zero_Copy_128Bytes(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}

	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _128BytesDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		_, err = db.BlockingRead(context.Background())
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

func BenchmarkBlockingRead_Throughput_Safe_Read_128Bytes(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.SafeRead); err != nil {
		b.Fatal(err)
	}

	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _128BytesDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		_, err = db.BlockingRead(context.Background())
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

func BenchmarkBlockingRead_Throughput_Safe_Read_64KB(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.SafeRead); err != nil {
		b.Fatal(err)
	}

	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _64KBDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		_, err = db.BlockingRead(context.Background())
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

func BenchmarkBlockingRead_Throughput_Zero_Copy_64KB(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}

	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _64KBDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		_, err = db.BlockingRead(context.Background())
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

func BenchmarkBlockingRead_Throughput_Zero_Copy_64KB_10Core(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}
	b.SetParallelism(10)
	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _64KBDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		_, err = db.BlockingRead(context.Background())
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

func BenchmarkBlockingRead_Throughput_Zero_Copy_64KB_1Core(b *testing.B) {
	defer goleak.VerifyNone(b, goleak.IgnoreCurrent())

	// Setup switch config
	sc, err := config.NewSwitchCondition(config.SwitchConfig{
		PercentThreshold: 80,
		TimeThreshold:    5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create buffer
	db, err := NewDoubleBuffer(TestBufferSize, sc)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Register read mode
	if err = db.RegisterReadMode(ts.ZeroCopyRead); err != nil {
		b.Fatal(err)
	}
	b.SetParallelism(10)
	// Writer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data := make([]byte, _64KBDataSize)
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				if err = db.Write(data); err != nil {
					b.Logf("Write error: %v", err)
					return
				}
			}
		}
	}()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark iterations
	for i := 0; i < b.N; i++ {
		_, err = db.BlockingRead(context.Background())
		if err != nil {
			b.Fatalf("BlockingRead failed: %v", err)
		}
	}

	// Stop timer and clean up
	b.StopTimer()
	cancel()
	wg.Wait()
}

//func TestNewDoubleBuffer_Check_Heap_Small_Bytes(t *testing.T) {
//	sc, err := config.NewSwitchCondition(config.SwitchConfig{
//		PercentThreshold: 80,
//		TimeThreshold:    time.Second,
//	})
//	if err != nil {
//		panic(err)
//	}
//
//	bf, err := NewDoubleBuffer(1000, sc)
//	if err != nil {
//		panic(err)
//	}
//
//	err = bf.RegisterReadMode(ts.SafeRead)
//	if err != nil {
//		panic(err)
//	}
//
//	exitChan := make(chan struct{}, 1)
//	var wg sync.WaitGroup
//	wg.Add(2)
//	go func() {
//		defer wg.Done()
//
//		var wg1 sync.WaitGroup
//		for i := 0; i < 5; i++ {
//			wg1.Add(1)
//			go func() {
//				defer wg1.Done()
//
//				for {
//					select {
//					case <-exitChan:
//						return
//					default:
//					}
//
//					ctx, cancel := context.WithTimeout(context.Background()(), time.Second)
//					chunk, err1 := bf.BlockingRead(ctx)
//					cancel()
//					if err1 != nil {
//						fmt.Printf("blocking read error, cause: %v\n", err1)
//						continue
//					}
//					fmt.Printf("blocking read success: %v\n", string(chunk.Bytes()))
//					chunk.Release()
//				}
//			}()
//		}
//
//		wg1.Wait()
//	}()
//
//	go func() {
//		defer wg.Done()
//		defer bf.Close()
//
//		template := "2025-06-07 12:12:00 [Info] test log write，current sequence: %d\n"
//		count := 0
//		var wg2 sync.WaitGroup
//		for i := 0; i < 5; i++ {
//			wg2.Add(1)
//			go func() {
//				defer wg2.Done()
//				for {
//					select {
//					case <-exitChan:
//						fmt.Println("end write!")
//						return
//					default:
//					}
//
//					count++
//					err = bf.Write([]byte(fmt.Sprintf(template, count)))
//					if err != nil {
//						fmt.Printf("write log error, cause：%s\n", err.Error())
//						continue
//					}
//				}
//			}()
//		}
//		wg2.Wait()
//	}()
//
//	go func() {
//		err = statsviz.RegisterDefault()
//		if err != nil {
//			panic(err)
//		}
//		err = http.ListenAndServe(":8081", nil)
//		if err != nil {
//			panic(err)
//		}
//	}()
//
//	wg.Wait()
//	fmt.Println("Write Success!")
//}
//
//func TestNewDoubleBuffer_Check_Heap_64KB(t *testing.T) {
//	sc, err := config.NewSwitchCondition(config.SwitchConfig{
//		PercentThreshold: 80,
//		TimeThreshold:    time.Second,
//	})
//	if err != nil {
//		panic(err)
//	}
//
//	bf, err := NewDoubleBuffer(500, sc)
//	if err != nil {
//		panic(err)
//	}
//
//	err = bf.RegisterReadMode(ts.ZeroCopyRead)
//	if err != nil {
//		panic(err)
//	}
//
//	exitChan := make(chan struct{}, 1)
//	var wg sync.WaitGroup
//	wg.Add(2)
//	go func() {
//		defer wg.Done()
//
//		for {
//			select {
//			case <-exitChan:
//				return
//			default:
//			}
//
//			ctx, cancel := context.WithTimeout(context.Background()(), time.Second)
//			chunk, err1 := bf.BlockingRead(ctx)
//			cancel()
//			if err1 != nil {
//				fmt.Printf("blocking read error, cause: %v\n", err1)
//				continue
//			}
//
//			//fmt.Println(chunk.Bytes()[:50])
//			chunk.Release()
//		}
//	}()
//
//	go func() {
//		defer wg.Done()
//		defer bf.Close()
//
//		//template := "2025-06-07 12:12:00 [Info] test log write，current sequence: %d\n"
//		for {
//			select {
//			case <-exitChan:
//				fmt.Println("end write!")
//				return
//			default:
//			}
//
//			data := make([]byte, _64KBDataSize)
//			data = fillRealisticData(data)
//			if len(data) < _64KBDataSize {
//				log.Panicf("unexpect data: %s\n", string(data))
//			}
//
//			err = bf.Write(data)
//			if err != nil {
//				log.Panicf("write log error, cause：%s\n", err.Error())
//			}
//		}
//	}()
//
//	go func() {
//		err = statsviz.RegisterDefault()
//		if err != nil {
//			panic(err)
//		}
//		err = http.ListenAndServe(":8081", nil)
//		if err != nil {
//			panic(err)
//		}
//	}()
//
//	wg.Wait()
//	fmt.Println("Write Success!")
//}
//
//func TestNewDoubleBuffer_Check_Heap_64Bytes(t *testing.T) {
//	sc, err := config.NewSwitchCondition(config.SwitchConfig{
//		PercentThreshold: 80,
//		TimeThreshold:    time.Second,
//	})
//	if err != nil {
//		panic(err)
//	}
//
//	bf, err := NewDoubleBuffer(500, sc)
//	if err != nil {
//		panic(err)
//	}
//
//	err = bf.RegisterReadMode(ts.ZeroCopyRead)
//	if err != nil {
//		panic(err)
//	}
//
//	exitChan := make(chan struct{}, 1)
//	var wg sync.WaitGroup
//	wg.Add(2)
//	go func() {
//		defer wg.Done()
//
//		for {
//			select {
//			case <-exitChan:
//				return
//			default:
//			}
//
//			ctx, cancel := context.WithTimeout(context.Background()(), time.Second)
//			chunk, err1 := bf.BlockingRead(ctx)
//			cancel()
//			if err1 != nil {
//				fmt.Printf("blocking read error, cause: %v\n", err1)
//				continue
//			}
//
//			//fmt.Println(chunk.Bytes()[:50])
//			chunk.Release()
//		}
//	}()
//
//	go func() {
//		defer wg.Done()
//		defer bf.Close()
//
//		//template := "2025-06-07 12:12:00 [Info] test log write，current sequence: %d\n"
//		for {
//			select {
//			case <-exitChan:
//				fmt.Println("end write!")
//				return
//			default:
//			}
//
//			data := make([]byte, 64)
//			binary.BigEndian.PutUint64(data, uint64(time.Now().Unix()))
//			rand.Read(data)
//			err = bf.Write(data)
//			if err != nil {
//				t.Logf("write log error, cause：%s\n", err.Error())
//			}
//		}
//	}()
//
//	go func() {
//		err = statsviz.RegisterDefault()
//		if err != nil {
//			panic(err)
//		}
//		err = http.ListenAndServe(":8081", nil)
//		if err != nil {
//			panic(err)
//		}
//	}()
//
//	wg.Wait()
//	fmt.Println("Write Success!")
//}
