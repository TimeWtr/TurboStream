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

package buffers

import "sync"

type Segment interface {
	Write(p []byte) error
	BatchWrite(p [][]byte) error
	Read(idx int) ([]byte, error)
	ReadAll() ([][]byte, error)
	Size() int
	MessageCount() int
	Capacity() int
	Release()
	IsFull() bool
	IsEmpty() bool
	CreateTime() int64
	LastWriteTime() int64
}

type BufferSegment struct {
	id         uint64
	data       []byte
	meta       []Metadata
	start      int
	end        int
	capacity   int
	count      int
	createTime int64
	lock       sync.RWMutex
	version    int64
}

type Metadata struct{}
