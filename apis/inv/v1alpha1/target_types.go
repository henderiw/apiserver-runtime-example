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

package v1alpha1

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TargetSpec defines the desired state of Target
type TargetSpec struct {
	// Provider specifies the provider using this target.
	Provider Provider `json:"provider" yaml:"provider"`
	// Address defines the address of the mgmt ip address of the target
	Address *string `json:"address,omitempty" yaml:"address,omitempty"`
	// SecretName defines the name of the secret to connect to the target
	SecretName string `json:"secretName" yaml:"secretName"`
	// TLSSecretName defines the name of the secret to connect to the target
	TLSSecretName *string `json:"tlsSecretName,omitempty" yaml:"tlsSecretName,omitempty"`

	ConnectionProfile string `json:"connectionProfile" yaml:"connectionProfile"`
	SyncProfile       string `json:"syncProfile" yaml:"syncProfile"`
	// ParametersRef points to the vendor or implementation specific params for the
	// target.
	// +optional
	ParametersRef *corev1.ObjectReference `json:"parametersRef,omitempty" yaml:"parametersRef,omitempty"`
}

type Provider struct {
	Name    string `json:"name" yaml:"name"`
	Vendor  string `json:"vendor" yaml:"vendor"`
	Version string `json:"version" yaml:"version"`
}

// TargetStatus defines the observed state of Target
type TargetStatus struct {
	// ConditionedStatus provides the status of the Target using conditions
	// 2 conditions are used:
	// - a condition for the reconcilation status
	// - a condition for the ready status
	// if both are true the other attributes in the status are meaningful
	ConditionedStatus `json:",inline" yaml:",inline"`
}

// +kubebuilder:object:root=true
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:resource:categories={nephio,inv}
// Target is the Schema for the Target API
// +k8s:openapi-gen=true
type Target struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   TargetSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status TargetStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true
// TargetList contains a list of Targets
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TargetList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []Target `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&Target{}, &TargetList{})
}

var (
	TargetKind             = reflect.TypeOf(Target{}).Name()
	TargetGroupKind        = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: TargetKind}.String()
	TargetKindAPIVersion   = TargetKind + "." + SchemeGroupVersion.String()
	TargetGroupVersionKind = SchemeGroupVersion.WithKind(TargetKind)
)
