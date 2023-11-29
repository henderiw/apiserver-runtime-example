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
	"context"
	"fmt"
	"sync"

	"github.com/henderiw/apiserver-runtime-example/pkg/store"
)

const (
	// errors
	NotFound = "not found"
)

func NewStore[T1 any]() store.Storer[T1] {
	return &mem[T1]{
		db: map[store.Key]T1{},
		//callbackFn: []ResourceCallbackFn{},
	}
}

type mem[T1 any] struct {
	m  sync.RWMutex
	db map[store.Key]T1
	//callbackFn []ResourceCallbackFn
}

// Get return the type
func (r *mem[T1]) Get(ctx context.Context, key store.Key) (T1, error) {
	r.m.RLock()
	defer r.m.RUnlock()

	x, ok := r.db[key]
	if !ok {
		return *new(T1), fmt.Errorf("%s, nsn: %s", NotFound, key.String())
	}
	return x, nil
}

func (r *mem[T1]) List(ctx context.Context, visitorFunc func(ctx context.Context, key store.Key, obj T1)) {
	r.m.RLock()
	defer r.m.RUnlock()

	for key, obj := range r.db {
		if visitorFunc != nil {
			visitorFunc(ctx, key, obj)
		}
	}
}

func (r *mem[T1]) Create(ctx context.Context, key store.Key, data T1) error {
	// if an error is returned the entry already exists
	if _, err := r.Get(ctx, key); err == nil {
		return fmt.Errorf("duplicate entry %v", key.String())
	}
	// update the cache before calling the callback since the cb fn will use this data
	r.update(ctx, key, data)

	/*
		// call callback if data got changed or if no data exists
		for _, cb := range r.callbackFn {
			cb(ctx, AddAction, nsn, newd)
		}
	*/
	return nil
}

// Upsert creates or updates the entry in the cache
func (r *mem[T1]) Update(ctx context.Context, key store.Key, data T1) error {
	/*
		exists := true
		oldd, err := r.Get(ctx, nsn)
		if err != nil {
			exists = false
		}
	*/
	// update the cache before calling the callback since the cb fn will use this data
	r.update(ctx, key, data)

	// call callback if data got changed or if no data exists
	/*
		if exists {
			if !reflect.DeepEqual(oldd, newd) {
				for _, cb := range r.callbackFn {
					cb(ctx, UpsertAction, nsn, newd)
				}
			}
		} else {
			for _, cb := range r.callbackFn {
				cb(ctx, AddAction, nsn, newd)
			}
		}
	*/
	return nil
}

func (r *mem[T1]) update(ctx context.Context, key store.Key, newd T1) {
	r.m.Lock()
	defer r.m.Unlock()
	r.db[key] = newd
}

func (r *mem[T1]) delete(ctx context.Context, key store.Key) {
	r.m.Lock()
	defer r.m.Unlock()
	delete(r.db, key)
}

// Delete deletes the entry in the cache
func (r *mem[T1]) Delete(ctx context.Context, key store.Key) error {
	// only if an exisitng object gets deleted we
	// call the registered callbacks
	exists := true
	_, err := r.Get(ctx, key)
	if err != nil {
		exists = false
	}
	// if exists call the callback
	if exists {
		/*
			for _, cb := range r.callbackFn {
				cb(ctx, DeleteAction, nsn, d)
			}
		*/
	}
	// delete the entry to ensure the cb uses the proper data
	r.delete(ctx, key)
	return nil
}

/*
func (r *mem[T1]) AddWatch(fn ResourceCallbackFn) {
	r.m.Lock()
	defer r.m.Unlock()
	found := false
	for _, cb := range r.callbackFn {
		if reflect.ValueOf(cb).Pointer() == reflect.ValueOf(fn).Pointer() {
			found = true
		}
	}
	if !found {
		r.callbackFn = append(r.callbackFn, fn)
	}
}
*/
