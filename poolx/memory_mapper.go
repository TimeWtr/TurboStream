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

import "golang.org/x/sys/unix"

type memoryMapper interface {
	Mmap(fd string, offset int64, length int, prot int, flags int) ([]byte, error)
	UnMmap(p []byte) error
	Sync(p []byte, flags int) error
	Lock(addr []byte) error
	Advise(addr []byte, advise int) error
}

type linuxMemoryMapper struct{}

func (l *linuxMemoryMapper) Mmap(fd, length, prot, flags int, offset int64) ([]byte, error) {
	return unix.Mmap(fd, offset, length, prot, flags)
}

func (l *linuxMemoryMapper) UnMmap(p []byte) error {
	return unix.Munmap(p)
}

func (l *linuxMemoryMapper) Sync(p []byte, flags int) error {
	return unix.Msync(p, flags)
}

func (l *linuxMemoryMapper) Lock(addr []byte) error {
	return unix.Mlock(addr)
}

func (l *linuxMemoryMapper) Advise(addr []byte, advise int) error {
	return unix.Madvise(addr, advise)
}
