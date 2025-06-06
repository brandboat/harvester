/*
Copyright 2025 Rancher Labs, Inc.

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

// Code generated by main. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/k8snetworkplumbingwg/whereabouts/pkg/api/whereabouts.cni.cncf.io/v1alpha1"
	"github.com/rancher/wrangler/v3/pkg/generic"
)

// IPPoolController interface for managing IPPool resources.
type IPPoolController interface {
	generic.ControllerInterface[*v1alpha1.IPPool, *v1alpha1.IPPoolList]
}

// IPPoolClient interface for managing IPPool resources in Kubernetes.
type IPPoolClient interface {
	generic.ClientInterface[*v1alpha1.IPPool, *v1alpha1.IPPoolList]
}

// IPPoolCache interface for retrieving IPPool resources in memory.
type IPPoolCache interface {
	generic.CacheInterface[*v1alpha1.IPPool]
}
