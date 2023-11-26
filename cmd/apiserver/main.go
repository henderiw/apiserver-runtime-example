/*
Copyright 2017 The Kubernetes Authors.

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

//go:generate apiserver-runtime-gen
package main

import (
	"log/slog"

	"github.com/henderiw/apiserver-runtime-example/apis/config/v1alpha1"
	"github.com/henderiw/apiserver-runtime-example/apis/generated/openapi"
	"github.com/henderiw/apiserver-runtime-example/pkg/storage/memory"
	"github.com/henderiw/logger/log"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	log := log.NewLogger(&log.HandlerOptions{Name: "caas-logger", AddSource: false})
	slog.SetDefault(log)

	err := builder.APIServer.
		WithOpenAPIDefinitions("Config", "v0.0.0", openapi.GetOpenAPIDefinitions).
		WithResourceAndHandler(&v1alpha1.Config{}, memory.NewMemoryStorageProvider(&v1alpha1.Config{})).
		WithoutEtcd().
		WithLocalDebugExtension().
		Execute()
	if err != nil {
		log.Info("cannot start caas")
	}
}
