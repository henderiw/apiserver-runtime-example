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
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/henderiw/apiserver-runtime-example/apis/inv/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// TargetConnectionProfileLister helps list TargetConnectionProfiles.
// All objects returned here must be treated as read-only.
type TargetConnectionProfileLister interface {
	// List lists all TargetConnectionProfiles in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.TargetConnectionProfile, err error)
	// TargetConnectionProfiles returns an object that can list and get TargetConnectionProfiles.
	TargetConnectionProfiles(namespace string) TargetConnectionProfileNamespaceLister
	TargetConnectionProfileListerExpansion
}

// targetConnectionProfileLister implements the TargetConnectionProfileLister interface.
type targetConnectionProfileLister struct {
	indexer cache.Indexer
}

// NewTargetConnectionProfileLister returns a new TargetConnectionProfileLister.
func NewTargetConnectionProfileLister(indexer cache.Indexer) TargetConnectionProfileLister {
	return &targetConnectionProfileLister{indexer: indexer}
}

// List lists all TargetConnectionProfiles in the indexer.
func (s *targetConnectionProfileLister) List(selector labels.Selector) (ret []*v1alpha1.TargetConnectionProfile, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.TargetConnectionProfile))
	})
	return ret, err
}

// TargetConnectionProfiles returns an object that can list and get TargetConnectionProfiles.
func (s *targetConnectionProfileLister) TargetConnectionProfiles(namespace string) TargetConnectionProfileNamespaceLister {
	return targetConnectionProfileNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// TargetConnectionProfileNamespaceLister helps list and get TargetConnectionProfiles.
// All objects returned here must be treated as read-only.
type TargetConnectionProfileNamespaceLister interface {
	// List lists all TargetConnectionProfiles in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.TargetConnectionProfile, err error)
	// Get retrieves the TargetConnectionProfile from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.TargetConnectionProfile, error)
	TargetConnectionProfileNamespaceListerExpansion
}

// targetConnectionProfileNamespaceLister implements the TargetConnectionProfileNamespaceLister
// interface.
type targetConnectionProfileNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all TargetConnectionProfiles in the indexer for a given namespace.
func (s targetConnectionProfileNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.TargetConnectionProfile, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.TargetConnectionProfile))
	})
	return ret, err
}

// Get retrieves the TargetConnectionProfile from the indexer for a given namespace and name.
func (s targetConnectionProfileNamespaceLister) Get(name string) (*v1alpha1.TargetConnectionProfile, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("targetconnectionprofile"), name)
	}
	return obj.(*v1alpha1.TargetConnectionProfile), nil
}
