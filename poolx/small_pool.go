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

package poolx

import (
	"github.com/TimeWtr/TurboStream/utils/atomicx"
)

type sizePool struct {
	size  int
	count *atomicx.Int64
	pool  chan []byte
	max   int
}

func newSizePool(size, maxSize int) *sizePool {
	return &sizePool{
		size:  size,
		max:   maxSize,
		pool:  make(chan []byte, maxSize),
		count: atomicx.NewInt64(0),
	}
}

func (p *sizePool) Alloc() []byte {
	select {
	case buf := <-p.pool:
		p.count.Add(-1)
		return buf
	default:
		return make([]byte, 0, p.size)
	}
}

func (p *sizePool) Free(buf []byte) error {
	if cap(buf) != p.size {
		return ErrBufferSize
	}

	if p.count.Load() >= int64(p.max) {
		// discard buffer
		return nil
	}

	select {
	case p.pool <- buf:
		p.count.Add(1)
	default:
		// discard buffer
	}

	return nil
}

type sizeLevel int

const (
	_64Bytes   sizeLevel = 64
	_128Bytes  sizeLevel = 128
	_256Bytes  sizeLevel = 256
	_512Bytes  sizeLevel = 512
	_1024Bytes sizeLevel = 1024
)

func (sl sizeLevel) int() int {
	return int(sl)
}

type smallPool struct {
	pools map[sizeLevel]*sizePool
	sizes []sizeLevel
}

func newSmallPool() *smallPool {
	sizes := []sizeLevel{
		_64Bytes,
		_128Bytes,
		_256Bytes,
		_512Bytes,
		_1024Bytes,
	}

	const maxSize = 1024
	pools := make(map[sizeLevel]*sizePool)
	for _, sl := range sizes {
		pools[sl] = newSizePool(sl.int(), maxSize)
	}

	return &smallPool{
		pools: pools,
		sizes: sizes,
	}
}

func (p *smallPool) Alloc(size int) []byte {
	for _, sl := range p.sizes {
		if size <= sl.int() {
			return p.pools[sl].Alloc()
		}
	}

	return nil
}

func (p *smallPool) Free(buf []byte) {
	for _, sl := range p.sizes {
		if cap(buf) == sl.int() {
			_ = p.pools[sl].Free(buf)
		}
	}
}
