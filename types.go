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
	"context"

	ts "github.com/TimeWtr/TurboStream"
)

type (
	DataCallBack   func(data []byte)
	UnregisterFunc func()
)

type ReadBuffer interface {
	RegisterReadMode(readMode ts.ReadMode) error
	BlockingRead(ctx context.Context) ([]byte, error)
	RegisterCallback(cb DataCallBack) UnregisterFunc
	BatchRead(ctx context.Context, batchSize int) [][]byte
}
