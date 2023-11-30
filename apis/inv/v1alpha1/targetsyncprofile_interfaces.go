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
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func GetSyncProfile(syncProfile *TargetSyncProfile) *sdcpb.Sync {
	sync := &sdcpb.Sync{}
	sync.Validate = false
	if syncProfile.Spec.Validate != nil {
		sync.Validate = *syncProfile.Spec.Validate
	}
	if syncProfile.Spec.Buffer != nil {
		sync.Buffer = *syncProfile.Spec.Buffer
	}
	if syncProfile.Spec.Workers != nil {
		sync.WriteWorkers = *syncProfile.Spec.Workers
	}
	sync.Gnmi = make([]*sdcpb.GNMISync, 0, len(syncProfile.Spec.Sync))
	for _, syncConfig := range syncProfile.Spec.Sync {
		sync.Gnmi = append(sync.Gnmi, &sdcpb.GNMISync{
			Name:     syncConfig.Name,
			Path:     syncConfig.Paths,
			Mode:     getSyncMode(syncConfig.Mode),
			Encoding: getEncoding(syncConfig.Encoding),
			Interval: syncConfig.Interval,
		})
	}
	return sync
}

func getEncoding(e Encoding) string {
	switch e {
	case Encoding_Config:
		return "45"
	default:
		return string(e)
	}
}

func getSyncMode(mode SyncMode) sdcpb.SyncMode {
	switch mode {
	case SyncMode_OnChange:
		return sdcpb.SyncMode_SM_ON_CHANGE
	case SyncMode_Sample:
		return sdcpb.SyncMode_SM_SAMPLE
	case SyncMode_Once:
		return sdcpb.SyncMode_SM_ONCE
	default:
		return sdcpb.SyncMode_SM_ON_CHANGE
	}
}

// DefaultTargetSyncProfile returns a default TargetSyncProfile
func DefaultTargetSyncProfile() *TargetSyncProfile {
	return BuildTargetSyncProfile(
		metav1.ObjectMeta{
			Name:      "default",
			Namespace: "default",
		},
		TargetSyncProfileSpec{
			Validate: pointer.Bool(true),
			Sync: []TargetSyncProfileSync{
				{
					Name:     "config",
					Protocol: Protocol_GNMI,
					Paths:    []string{"/"},
					Mode:     SyncMode_OnChange,
				},
			},
		},
	)
}

// BuildTargetSyncProfile returns a TargetSyncProfile from a client Object a crName and
// an TargetSyncProfile Spec/Status
func BuildTargetSyncProfile(meta metav1.ObjectMeta, spec TargetSyncProfileSpec) *TargetSyncProfile {
	return &TargetSyncProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeBuilder.GroupVersion.Identifier(),
			Kind:       TargetSyncProfileKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}
