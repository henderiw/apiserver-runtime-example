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
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/henderiw/apiserver-runtime-example/apis/config/v1alpha1"
	"github.com/henderiw/apiserver-runtime-example/apis/generated/openapi"
	dsclient "github.com/henderiw/apiserver-runtime-example/pkg/dataserver/client"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/context/dsctx"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/context/tctx"
	"github.com/henderiw/apiserver-runtime-example/pkg/config"
	"github.com/henderiw/apiserver-runtime-example/pkg/store"
	memstore "github.com/henderiw/apiserver-runtime-example/pkg/store/memory"
	"github.com/henderiw/logger/log"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // register auth plugins
	"k8s.io/component-base/logs"
	"sigs.k8s.io/apiserver-runtime/pkg/builder"
)

const (
	dataServerAddress = "localhost:56000"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	l := log.NewLogger(&log.HandlerOptions{Name: "caas-logger", AddSource: false})
	slog.SetDefault(l)
	ctx := log.IntoContext(context.Background(), l)
	log := log.FromContext(ctx)

	configStore := memstore.NewStore[runtime.Object]()
	targetStore := memstore.NewStore[tctx.Context]()
	// dataServer
	dataServerStore := memstore.NewStore[dsctx.Context]()
	dsCtx, err := createDataServer(ctx, dataServerStore)
	if err != nil {
		log.Error("cannot create data server", "error", err.Error())
		os.Exit(1)
	}
	// fake reconciler
	r := reconciler{
		configStore:     configStore,
		targetStore:     targetStore,
		dataServerStore: dataServerStore,
	}

	time.Sleep(5 * time.Second)
	// target -> create on selected dataserver
	key := store.GetNSNKey(types.NamespacedName{Namespace: "default", Name: "dev1"})
	if err := r.createTarget(ctx, key, dsCtx); err != nil {
		log.Error("cannot create target", "key", key, "error", err.Error())
		os.Exit(1)
	}

	if err := builder.APIServer.
		WithOpenAPIDefinitions("Config", "v0.0.0", openapi.GetOpenAPIDefinitions).
		WithResourceAndHandler(&v1alpha1.Config{}, config.NewProvider(&v1alpha1.Config{}, configStore, targetStore)).
		WithoutEtcd().
		WithLocalDebugExtension().
		Execute(); err != nil {
		log.Info("cannot start caas")
	}
}

func createDataServer(ctx context.Context, dataServerStore store.Storer[dsctx.Context]) (*dsctx.Context, error) {
	log := log.FromContext(ctx)

	// TODO the population of the dataservers in the store should become dynamic, through a controller
	// right now it is static since all of this happens in the same pod and we dont have scaled out dataservers
	dsConfig := &dsclient.Config{
		Address: dataServerAddress,
	}
	dsClient, err := dsclient.New(dsConfig)
	if err != nil {
		log.Error("cannot create client to dataserver", "err", err)
		return nil, err
	}
	if err := dsClient.Start(ctx); err != nil {
		log.Error("cannot start client to dataserver", "err", err)
		return nil, err
	}
	dsCtx := dsctx.Context{
		Config:  dsConfig,
		Targets: sets.New[string](),
		Client:  dsClient,
	}
	if err := dataServerStore.Create(ctx, store.GetNameKey(dataServerAddress), dsCtx); err != nil {
		log.Error("cannot store dataserver", "err", err)
		return nil, err
	}
	log.Info("created dataserver")
	return &dsCtx, nil
}

type reconciler struct {
	configStore     store.Storer[runtime.Object]
	targetStore     store.Storer[tctx.Context]
	dataServerStore store.Storer[dsctx.Context]
}

func (r *reconciler) createTarget(ctx context.Context, key store.Key, dsCtx *dsctx.Context) error {
	log := log.FromContext(ctx)
	r.addTarget2DataServer(ctx, store.GetNameKey(dsCtx.Client.GetAddress()), key)
	// create the target in the target store
	if err := r.targetStore.Create(ctx, key, tctx.Context{
		Client: dsCtx.Client,
	}); err != nil {
		log.Info("cannot store target", "error", err.Error())
		return err
	}
	if err := r.updateDataStore(ctx, key); err != nil {
		log.Info("cannot update target datastore", "error", err.Error())
		return err
	}
	return nil
}

func (r *reconciler) updateDataStore(ctx context.Context, key store.Key) error {
	log := log.FromContext(ctx)
	// this should always succeed
	currentTargetCtx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		return err
	}
	if _, err := currentTargetCtx.Client.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()}); err != nil {
		if !strings.Contains(err.Error(), "unknown datastore") {
			return err
		}
		// datastore does not exist
	} else {
		// datastore exists -< validate changes and if so delete the datastore
		/*
			if r.validateDataStoreChanges(ctx, cr, getRsp) {
				rsp, err := currentTargetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: cr.Name})
				if err != nil {
					return err
				}
				log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
			}
		*/
	}
	// datastore does not exist -> create datastore
	req := getCreateDataStoreRequest(ctx, key)
	rsp, err := currentTargetCtx.Client.CreateDataStore(ctx, req)
	if err != nil {
		return err
	}
	currentTargetCtx.DataStore = req
	r.targetStore.Update(ctx, key, currentTargetCtx)
	log.Info("create datastore succeeded", "key", key, "resp", prototext.Format(rsp))
	return nil
}

func getCreateDataStoreRequest(ctx context.Context, key store.Key) *sdcpb.CreateDataStoreRequest {
	return &sdcpb.CreateDataStoreRequest{
		Name: key.String(),
		Target: &sdcpb.Target{
			Type:    "noop",
			Address: "1.1.1.1",
			Credentials: &sdcpb.Credentials{
				Username: "admin",      // TODO should come from the secret
				Password: "NokiaSrl1!", // TODO should come from the secret
			},
		},
		Sync: &sdcpb.Sync{ // TODO should come from the target profile
			Validate: true,
			Gnmi: []*sdcpb.GNMISync{
				{Name: "config", Path: []string{"/"}, Mode: sdcpb.SyncMode_SM_ON_CHANGE, Encoding: "45"},
			},
		},
		Schema: &sdcpb.Schema{
			Name:    "srl",     // TODO should come from the target profile
			Vendor:  "Nokia",   // TODO should come from the target profile
			Version: "23.10.1", // TODO should come from the target profile
		},
	}
}

func (r *reconciler) addTarget2DataServer(ctx context.Context, dsKey store.Key, targetKey store.Key) {
	log := log.FromContext(ctx)
	dsctx, err := r.dataServerStore.Get(ctx, dsKey)
	if err != nil {
		log.Info("AddTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}
	dsctx.Targets = dsctx.Targets.Insert(targetKey.String())
	if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
		log.Info("AddTarget2DataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
	}
}
