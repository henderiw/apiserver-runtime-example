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
	"reflect"

	"github.com/henderiw/apiserver-runtime-example/pkg/storage/store"
	"github.com/henderiw/apiserver-runtime-example/pkg/storage/store/memory"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	builderrest "sigs.k8s.io/apiserver-runtime/pkg/builder/rest"
)

var tracer = otel.Tracer("apiserver")

func NewMemoryStorageProvider(obj resource.Object) builderrest.ResourceHandlerProvider {
	return func(scheme *runtime.Scheme, getter generic.RESTOptionsGetter) (rest.Storage, error) {
		gr := obj.GetGroupVersionResource().GroupResource()
		/*
			gvk := schema.GroupVersionKind{
				Group:   obj.GetGroupVersionResource().Group,
				Version: obj.GetGroupVersionResource().Version,
				Kind:    reflect.TypeOf(&obj).Name(),
			}
		*/
		codec, _, err := storage.NewStorageCodec(storage.StorageCodecConfig{
			StorageMediaType:  runtime.ContentTypeJSON,
			StorageSerializer: serializer.NewCodecFactory(scheme),
			StorageVersion:    scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
			MemoryVersion:     scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
			Config:            storagebackend.Config{}, // useless fields..
		})
		if err != nil {
			return nil, err
		}
		return NewMemoryREST(
			gr,
			//gvk,
			codec,
			obj.NamespaceScoped(),
			obj.New,
			obj.NewList,
		), nil
	}
}

func NewMemoryREST(
	gr schema.GroupResource,
	//gvk schema.GroupVersionKind,
	codec runtime.Codec,
	isNamespaced bool,
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
) rest.Storage {
	return &mem{
		store:          memory.NewStore[runtime.Object](),
		TableConvertor: rest.NewDefaultTableConvertor(gr),
		codec:          codec,
		gr:             gr,
		//gvk:            gvk,
		isNamespaced: isNamespaced,
		newFunc:      newFunc,
		newListFunc:  newListFunc,
		watchers:     NewMemWatchers(),
	}
}

type mem struct {
	store store.Storer[runtime.Object]

	rest.TableConvertor
	codec runtime.Codec
	//objRootPath  string
	gr schema.GroupResource
	//gvk          schema.GroupVersionKind
	isNamespaced bool

	watchers    *memWatchers
	newFunc     func() runtime.Object
	newListFunc func() runtime.Object
}

func (r *mem) Destroy() {}

func (r *mem) New() runtime.Object {
	return r.newFunc()
}

func (r *mem) NewList() runtime.Object {
	return r.newListFunc()
}

func (r *mem) NamespaceScoped() bool {
	return r.isNamespaced
}

func (r *mem) getKey(
	ctx context.Context,
	name string,
) (store.ResourceKey, error) {
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if namespaced != r.isNamespaced {
		return store.ResourceKey{}, fmt.Errorf("namespace mismatch got %t, want %t", namespaced, r.isNamespaced)
	}
	/*
		if typeMeta.GroupVersionKind() != r.gvk {
			return store.ResourceKey{}, fmt.Errorf("gvk mismatch got %s, want %s", typeMeta.GroupVersionKind(), r.gvk)
		}
	*/
	return store.ResourceKey{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: ns,
		},
		//GroupVersionKind: r.gvk,
	}, nil
}

func (r *mem) Get(
	ctx context.Context,
	name string,
	options *metav1.GetOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Get", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx).With("name", name)
	log.Info("get...")

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}

	obj, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, name)
	}
	return obj, nil
}

func getListPrt(listObj runtime.Object) (reflect.Value, error) {
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return reflect.Value{}, err
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return reflect.Value{}, fmt.Errorf("need ptr to slice: %v", err)
	}
	return v, nil
}

func appendItem(v reflect.Value, obj runtime.Object) {
	v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
}

func (r *mem) List(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::List", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, "")
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("list", "key", key.String())

	newListObj := r.NewList()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	objs, err := r.store.List(ctx, key, options.LabelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot list objs from store")
	}
	for _, obj := range objs {
		appendItem(v, obj)
	}

	return newListObj, nil
}

func (r *mem) Create(
	ctx context.Context,
	runtimeObject runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Create", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)
	log.Info("get", "ctx", ctx, "typeMeta", options.TypeMeta, "obj", runtimeObject)


	accessor, err := meta.Accessor(runtimeObject)
	if err != nil {
		return nil, err
	}

	// Get Key
	key, err := r.getKey(ctx, accessor.GetName())
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create", "key", key.String())

	if err := r.store.Create(ctx, key, runtimeObject); err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	return runtimeObject, nil
}

func (r *mem) Update(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Update", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("update", "key", key.String())

	// isCreate tracks whether this is an update that creates an object (this happens in server-side apply)
	isCreate := false

	oldObj, err := r.store.Get(ctx, key)
	if err != nil {
		if forceAllowCreate && apierrors.IsNotFound(err) {
			// For server-side apply, we can create the object here
			isCreate = true
		} else {
			return nil, false, err
		}
	}
	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		log.Info("update failed to construct UpdatedObject", "error", err.Error())
		return nil, false, err
	}

	if !isCreate {
		if err := r.store.Update(ctx, key, newObj); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newObj, false, nil
	} else {
		if err := r.store.Create(ctx, key, newObj); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newObj, true, nil
	}
}

func (r *mem) Delete(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Delete", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("delete", "key", key.String())

	obj, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, false, apierrors.NewNotFound(r.gr, name)
	}

	if err := r.store.Delete(ctx, key); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	return obj, true, nil
}

func (r *mem) DeleteCollection(
	ctx context.Context,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
	listOptions *metainternalversion.ListOptions,
) (runtime.Object, error) {

	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::DeleteCollection", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)
	log.Info("delete collection")

	// Get Key
	key, err := r.getKey(ctx, "")
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("delete collection", "key", key.String())

	newListObj := r.NewList()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	objs, err := r.store.List(ctx, key, listOptions.LabelSelector)
	if err != nil {
		return nil, errors.Wrap(err, "cannot list objs from store")
	}
	for _, obj := range objs {
		// TODO Delete
		//r.store.Delete(ctx, store.ResourceKey{Names})
		appendItem(v, obj)
	}

	return newListObj, nil
}

func (r *mem) Watch(
	ctx context.Context,
	options *metainternalversion.ListOptions,
) (watch.Interface, error) {
	// Start OTEL tracer
	ctx, span := tracer.Start(ctx, "configs::Watch", trace.WithAttributes())
	defer span.End()

	// logger
	log := log.FromContext(ctx)
	log.Info("watch", "options", *options)

	_, cancel := context.WithCancel(ctx)

	w := &memWatch{
		cancel:   cancel,
		resultCh: make(chan watch.Event, 64),
	}

	return w, nil
}

/*
func GetKey(obj runtime.Object) store.ResourceKey {
	return store.ResourceKey{
		NamespacedName: types.NamespacedName{
			Name: obj.
		},
		GroupVersionKind: typeMeta.GroupVersionKind(),
	}
}
*/
