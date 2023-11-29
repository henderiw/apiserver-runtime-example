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
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	api "github.com/henderiw/apiserver-runtime-example/apis/config/v1alpha1"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/context/tctx"
	"github.com/henderiw/apiserver-runtime-example/pkg/storage/store"
	"github.com/henderiw/logger/log"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/prototext"
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

const (
	targetNameKey      = "targetName"
	targetNamespaceKey = "targetNamespace"
)

// TODO this is to be replaced by the metadata
//var targetKey = store.GetNSNKey(types.NamespacedName{Namespace: "default", Name: "dev1"})

func NewMemoryStorageProvider(obj resource.Object, store store.Storer[runtime.Object], targetStore store.Storer[tctx.Context]) builderrest.ResourceHandlerProvider {
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
			store,
			targetStore,
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
	store store.Storer[runtime.Object],
	targetStore store.Storer[tctx.Context],
	gr schema.GroupResource,
	//gvk schema.GroupVersionKind,
	codec runtime.Codec,
	isNamespaced bool,
	newFunc func() runtime.Object,
	newListFunc func() runtime.Object,
) rest.Storage {
	return &mem{
		store:          store,
		targetStore:    targetStore,
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
	store       store.Storer[runtime.Object]
	targetStore store.Storer[tctx.Context]

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
) (store.Key, error) {
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if namespaced != r.isNamespaced {
		return store.Key{}, fmt.Errorf("namespace mismatch got %t, want %t", namespaced, r.isNamespaced)
	}
	/*
		if typeMeta.GroupVersionKind() != r.gvk {
			return store.ResourceKey{}, fmt.Errorf("gvk mismatch got %s, want %s", typeMeta.GroupVersionKind(), r.gvk)
		}
	*/
	return store.Key{
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
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)

	newListObj := r.NewList()
	v, err := getListPrt(newListObj)
	if err != nil {
		return nil, err
	}

	r.store.List(ctx, func(ctx context.Context, key store.Key, obj runtime.Object) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.Error("cannot get meta from object", "error", err.Error())
			return
		}

		if namespaced && accessor.GetNamespace() == ns {
			appendItem(v, obj)
		} else {
			appendItem(v, obj)
		}

	})

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
	//log.Info("get", "ctx", ctx, "typeMeta", options.TypeMeta, "obj", runtimeObject)

	accessor, err := meta.Accessor(runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create", "labels accessor", accessor)

	// Get Key
	key, err := r.getKey(ctx, accessor.GetName())
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create", "key", key.String())

	// get the data of the runtime object
	newConfig, ok := runtimeObject.(*api.Config)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", runtimeObject))
	}
	log.Info("create", "labels object", newConfig.GetLabels())
	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create", "obj", string(newConfig.Spec.Config.Raw))
	// interact with the data server
	// get the target from the targetCache
	// set the
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("target %s does not exist, err: %s", targetKey.String(), err.Error()))
	}
	if tctx.DataStore == nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("datastore for target %s does not exist, err: %s", targetKey.String(), err.Error()))
	}
	// create candidate
	rsp, err := tctx.Client.CreateDataStore(ctx, &sdcpb.CreateDataStoreRequest{
		Name: targetKey.String(),
		Datastore: &sdcpb.DataStore{
			Type:     sdcpb.Type_CANDIDATE,
			Name:     "candidate", // to be changed to gvknsn
			Owner:    "candidate", // to be changed to gvknsn
			Priority: 10,
		},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return nil, apierrors.NewInternalError(fmt.Errorf("create candidate failed for target %s, err: %s", targetKey.String(), err.Error()))
		}
		log.Info("create candidate ds already exists", "rsp", prototext.Format(rsp), "error", err.Error())
	} else {
		log.Info("create candidate ds succeeded", "rsp", prototext.Format(rsp))
	}
	

	path, err := ParsePath("/")
	if err != nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("path %s invalid for %s, err: %s", "/", targetKey.String(), err.Error()))
	}

	req :=  &sdcpb.SetDataRequest{
		Name: targetKey.String(),
		Datastore: &sdcpb.DataStore{
			Type:     sdcpb.Type_CANDIDATE,
			Name:     "candidate", // to be changed to gvknsn
			Owner:    "candidate", // to be changed to gvknsn
			Priority: 10,
		},
		Update: []*sdcpb.Update{
			{
				Path: path,
				Value: &sdcpb.TypedValue{
					Value: &sdcpb.TypedValue_JsonVal{
						JsonVal: newConfig.Spec.Config.Raw,
					},
				},
			},
		},
	}
	setResp, err := tctx.Client.SetData(ctx, req)
	if err != nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("set data failed for target %s, err: %s", targetKey.String(), err.Error()))
	}
	log.Info("create set succeeded", "req", prototext.Format(req))
	log.Info("create set succeeded", "rsp", prototext.Format(setResp))

	commitRsp, err := tctx.Client.Commit(ctx, &sdcpb.CommitRequest{
		Name: targetKey.String(),
		Datastore: &sdcpb.DataStore{
			Type:     sdcpb.Type_CANDIDATE,
			Name:     "candidate", // to be changed to gvknsn
			Owner:    "candidate", // to be changed to gvknsn
			Priority: 10,
		},
		Rebase: true,
		Stay:   false,
	})
	if err != nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("commit failed for target %s, err: %s", targetKey.String(), err.Error()))
	}
	log.Info("commit succeeded", "rsp", prototext.Format(commitRsp))

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
		log.Info("update", "err", err.Error())
		if forceAllowCreate && strings.Contains(err.Error(), "not found") {
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

	// get the data of the runtime object
	newConfig, ok := newObj.(*api.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", newObj))
	}
	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("update", "key", key.String(), "targetKey", targetKey.String())
	log.Info("update", "obj", string(newConfig.Spec.Config.Raw))

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

	// get the data of the runtime object
	newConfig, ok := obj.(*api.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", obj))
	}
	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("delete", "targetKey", targetKey, "obj", string(newConfig.Spec.Config.Raw))

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

	r.store.List(ctx, func(ctx context.Context, key store.Key, obj runtime.Object) {
		// TODO delete
		appendItem(v, obj)
	})

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

func getTargetKey(labels map[string]string) (store.Key, error) {
	var targetName, targetNamespace string
	if labels != nil {
		targetName = labels[targetNameKey]
		targetNamespace = labels[targetNamespaceKey]
	}
	if targetName == "" || targetName == "" {
		return store.Key{}, fmt.Errorf(" target namespace and name is required got %s.%s", targetNamespace, targetName)
	}
	return store.Key{NamespacedName: types.NamespacedName{Namespace: targetNamespace, Name: targetName}}, nil
}

func getGVKNSName() {}

var errMalformedXPath = errors.New("malformed xpath")
var errMalformedXPathKey = errors.New("malformed xpath key")

var escapedBracketsReplacer = strings.NewReplacer(`\]`, `]`, `\[`, `[`)

func ParsePath(p string) (*sdcpb.Path, error) {
	lp := len(p)
	if lp == 0 {
		return &sdcpb.Path{}, nil
	}
	var origin string

	idx := strings.Index(p, ":")
	if idx >= 0 && p[0] != '/' && !strings.Contains(p[:idx], "/") &&
		// path == origin:/ || path == origin:
		((idx+1 < lp && p[idx+1] == '/') || (lp == idx+1)) {
		origin = p[:idx]
		p = p[idx+1:]
	}

	pes, err := toPathElems(p)
	if err != nil {
		return nil, err
	}
	return &sdcpb.Path{
		Origin: origin,
		Elem:   pes,
	}, nil
}

// toPathElems parses a xpath and returns a list of path elements
func toPathElems(p string) ([]*sdcpb.PathElem, error) {
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	buffer := make([]rune, 0)
	null := rune(0)
	prevC := rune(0)
	// track if the loop is traversing a key
	inKey := false
	for _, r := range p {
		switch r {
		case '[':
			if inKey && prevC != '\\' {
				return nil, errMalformedXPath
			}
			if prevC != '\\' {
				inKey = true
			}
		case ']':
			if !inKey && prevC != '\\' {
				return nil, errMalformedXPath
			}
			if prevC != '\\' {
				inKey = false
			}
		case '/':
			if !inKey {
				buffer = append(buffer, null)
				prevC = r
				continue
			}
		}
		buffer = append(buffer, r)
		prevC = r
	}
	if inKey {
		return nil, errMalformedXPath
	}
	stringElems := strings.Split(string(buffer), string(null))
	pElems := make([]*sdcpb.PathElem, 0, len(stringElems))
	for _, s := range stringElems {
		if s == "" {
			continue
		}
		pe, err := toPathElem(s)
		if err != nil {
			return nil, err
		}
		pElems = append(pElems, pe)
	}
	return pElems, nil
}

// toPathElem take a xpath formatted path element such as "elem1[k=v]" and returns the corresponding mgmt_server.PathElem
func toPathElem(s string) (*sdcpb.PathElem, error) {
	idx := -1
	prevC := rune(0)
	for i, r := range s {
		if r == '[' && prevC != '\\' {
			idx = i
			break
		}
		prevC = r
	}
	var kvs map[string]string
	if idx > 0 {
		var err error
		kvs, err = parseXPathKeys(s[idx:])
		if err != nil {
			return nil, err
		}
		s = s[:idx]
	}
	return &sdcpb.PathElem{Name: s, Key: kvs}, nil
}

// parseXPathKeys takes keys definition from an xpath, e.g [k1=v1][k2=v2] and return the keys and values as a map[string]string
func parseXPathKeys(s string) (map[string]string, error) {
	if len(s) == 0 {
		return nil, nil
	}
	kvs := make(map[string]string)
	inKey := false
	start := 0
	prevRune := rune(0)
	for i, r := range s {
		switch r {
		case '[':
			if prevRune == '\\' {
				prevRune = r
				continue
			}
			if inKey {
				return nil, errMalformedXPathKey
			}
			inKey = true
			start = i + 1
		case ']':
			if prevRune == '\\' {
				prevRune = r
				continue
			}
			if !inKey {
				return nil, errMalformedXPathKey
			}
			eq := strings.Index(s[start:i], "=")
			if eq < 0 {
				return nil, errMalformedXPathKey
			}
			k, v := s[start:i][:eq], s[start:i][eq+1:]
			if len(k) == 0 || len(v) == 0 {
				return nil, errMalformedXPathKey
			}
			kvs[escapedBracketsReplacer.Replace(k)] = escapedBracketsReplacer.Replace(v)
			inKey = false
		}
		prevRune = r
	}
	if inKey {
		return nil, errMalformedXPathKey
	}
	return kvs, nil
}

///////////

// ToStrings converts gnmi.Path to index strings. When index strings are generated,
// gnmi.Path will be irreversibly lost. Index strings will be built by using name field
// in gnmi.PathElem. If gnmi.PathElem has key field, values will be included in
// alphabetical order of the keys.
// E.g. <target>/<origin>/a/b[b:d, a:c]/e will be returned as <target>/<origin>/a/b/c/d/e
// If prefix parameter is set to true, <target> and <origin> fields of
// the gnmi.Path will be prepended in the index strings unless they are empty string.
// gnmi.Path.Element field is deprecated, but being gracefully handled by this function
// in the absence of gnmi.Path.Elem.
func ToStrings(p *sdcpb.Path, prefix, nokeys bool) []string {
	is := []string{}
	if p == nil {
		return is
	}
	if prefix {
		// add target to the list of index strings
		if t := p.GetTarget(); t != "" {
			is = append(is, t)
		}
		// add origin to the list of index strings
		if o := p.GetOrigin(); o != "" {
			is = append(is, o)
		}
	}
	for _, e := range p.GetElem() {
		is = append(is, e.GetName())
		if !nokeys {
			is = append(is, sortedVals(e.GetKey())...)
		}
	}

	return is
}

func sortedVals(m map[string]string) []string {
	// Special case single key lists.
	if len(m) == 1 {
		for _, v := range m {
			return []string{v}
		}
	}
	// Return deterministic ordering of multi-key lists.
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	vs := make([]string, 0, len(m))
	for _, k := range ks {
		vs = append(vs, m[k])
	}
	return vs
}

func CompletePath(prefix, path *sdcpb.Path) ([]string, error) {
	oPre, oPath := prefix.GetOrigin(), path.GetOrigin()

	var fullPrefix []string
	indexedPrefix := ToStrings(prefix, false, false)
	switch {
	case oPre != "" && oPath != "":
		return nil, errors.New("origin is set both in prefix and path")
	case oPre != "":
		fullPrefix = append(fullPrefix, oPre)
		fullPrefix = append(fullPrefix, indexedPrefix...)
	case oPath != "":
		if len(indexedPrefix) > 0 {
			return nil, errors.New("path elements in prefix are set even though origin is set in path")
		}
		fullPrefix = append(fullPrefix, oPath)
	default:
		// Neither prefix nor path specified an origin. Include the path elements in prefix.
		fullPrefix = append(fullPrefix, indexedPrefix...)
	}

	return append(fullPrefix, ToStrings(path, false, false)...), nil
}

func ToXPath(p *sdcpb.Path, noKeys bool) string {
	if p == nil {
		return ""
	}
	sb := strings.Builder{}
	if p.Origin != "" {
		sb.WriteString(p.Origin)
		sb.WriteString(":")
	}
	elems := p.GetElem()
	numElems := len(elems)
	for i, pe := range elems {
		sb.WriteString(pe.GetName())
		if !noKeys {
			for k, v := range pe.GetKey() {
				sb.WriteString("[")
				sb.WriteString(k)
				sb.WriteString("=")
				sb.WriteString(v)
				sb.WriteString("]")
			}
		}
		if i+1 != numElems {
			sb.WriteString("/")
		}
	}
	return sb.String()
}
