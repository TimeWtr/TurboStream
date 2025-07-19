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
	"container/list"
	"sync"
	"unsafe"

	"github.com/TimeWtr/TurboStream/utils/atomicx"
	"golang.org/x/sys/unix"
)

type slabSizeLevel int

const (
	_4KBSizeLevel   slabSizeLevel = 4 * 1024
	_8KBSizeLevel   slabSizeLevel = 8 * 1024
	_16KBSizeLevel  slabSizeLevel = 16 * 1024
	_32KBSizeLevel  slabSizeLevel = 32 * 1024
	_maxSizeLeve    slabSizeLevel = _32KBSizeLevel
	_groupSizeLevel slabSizeLevel = _32KBSizeLevel * 2
)

func (sl slabSizeLevel) int() int {
	return int(sl)
}

type slabAllocator struct {
	blockSize   slabSizeLevel
	chunkSize   slabSizeLevel
	chunks      []chunk
	freeList    *list.List
	partialList *list.List
	fullList    *list.List
	lock        sync.Mutex
	states      slabState
	numaNode    int
}

func newSlabAllocator(chunkSize, blockSize slabSizeLevel) *slabAllocator {
	return &slabAllocator{
		blockSize:   blockSize,
		chunkSize:   chunkSize,
		freeList:    list.New(),
		partialList: list.New(),
		fullList:    list.New(),
	}
}

func (s *slabAllocator) initChunk() (*chunk, error) {
	addr, err := unix.Mmap(-1, 0, s.chunkSize.int(),
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}

	blockCount := s.chunkSize.int() / s.blockSize.int()
	c := &chunk{
		start:    uintptr(unsafe.Pointer(&addr[0])),
		size:     s.chunkSize.int(),
		freeList: list.New(),
		blocks:   make([]*memoryBlock, 0, blockCount),
	}

	for i := 0; i < blockCount; i++ {
		blockAddr := c.start + uintptr(i*s.blockSize.int())
		block := &memoryBlock{
			start:  blockAddr,
			size:   s.blockSize.int(),
			isFree: true,
			chunk:  c,
		}
		c.blocks = append(c.blocks, block)
		c.freeList.PushBack(block)
	}

	return c, nil
}

func (s *slabAllocator) Alloc() ([]byte, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for e := s.partialList.Front(); e != nil; e = e.Next() {
		// c := e.Value.(*memoryBlock)
	}

	return nil, nil
}

type slabState struct{}

type chunk struct {
	start     uintptr
	size      int
	freeCount atomicx.Int32
	blocks    []*memoryBlock
	freeList  *list.List
	slab      *slabAllocator
}

func (c *chunk) allocBlock() *memoryBlock {
	if c.freeList.Len() == 0 {
		return nil
	}

	e := c.freeList.Front()
	c.freeList.Remove(e)
	block, _ := e.Value.(*memoryBlock)
	block.isFree = false
	block.element = nil
	c.freeCount.Add(-1)

	if c.freeCount.Load() == 0 {
		c.slab.lock.Lock()
		defer c.slab.lock.Unlock()

		for es := c.slab.partialList.Front(); es != nil; es = es.Next() {
			if v, _ := es.Value.(*chunk); v == c {
				c.slab.partialList.Remove(es)
				c.slab.fullList.PushBack(es)
				break
			}
		}
	}

	return block
}

func (c *chunk) free() {}

type memoryBlock struct {
	start   uintptr
	size    int
	isFree  bool
	chunk   *chunk
	element *list.Element
}
