/*
Copyright 2023 The Nephio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ctrlconfig

import (
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/context/dsctx"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/context/tctx"
	"github.com/henderiw/apiserver-runtime-example/pkg/store"
	"k8s.io/apimachinery/pkg/runtime"
)

type ControllerConfig struct {
	ConfigStore     store.Storer[runtime.Object]
	TargetStore     store.Storer[tctx.Context]
	DataServerStore store.Storer[dsctx.Context]
}
