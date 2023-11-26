// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memory

import (
	"sync"

	"k8s.io/apimachinery/pkg/watch"
)

func NewMemWatchers() *memWatchers {
	return &memWatchers{
		watchers: make(map[int]*memWatch, 10),
	}
}

type memWatchers struct {
	m        sync.RWMutex
	watchers map[int]*memWatch
}

type memWatch struct {
	r  *memWatchers
	id int
	ch chan watch.Event
}

func (w *memWatch) Stop() {
	w.r.m.Lock()
	defer w.r.m.Unlock()
	delete(w.r.watchers, w.id)
}

func (w *memWatch) ResultChan() <-chan watch.Event {
	return w.ch
}