// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package namespaces

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	v1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/component"
)

// resources implements component.Resources for the shoot core namespaces.
// It renders the kube-system Namespace with HA metadata derived from worker pools.
type resources struct {
	workerPools []gardencorev1beta1.Worker
}

// NewResources returns a renderer for the shoot namespaces resources.
// The returned component.Resources can be passed to component.NewBuilder().
func NewResources(workerPools []gardencorev1beta1.Worker) component.Resources {
	return &resources{workerPools: workerPools}
}

// All renders all objects as a single bundle backed by a ManagedResource.
func (r *resources) All(ctx context.Context) ([]component.Bundle, error) { //nolint:revive,unused
	zones := sets.New[string]()
	for _, pool := range r.workerPools {
		if v1beta1helper.SystemComponentsAllowed(&pool) {
			zones.Insert(pool.Zones...)
		}
	}

	kubeSystemNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: metav1.NamespaceSystem,
			Labels: map[string]string{
				"source":                         "builder",
				v1beta1constants.GardenerPurpose: metav1.NamespaceSystem,
				resourcesv1alpha1.HighAvailabilityConfigConsider: "true",
			},
			Annotations: map[string]string{
				resourcesv1alpha1.HighAvailabilityConfigZones: strings.Join(sets.List(zones), ","),
			},
		},
	}

	return []component.Bundle{{
		Name:        managedResourceName,
		Objects:     []client.Object{kubeSystemNamespace},
		Destination: "shoot",
	}}, nil
}

func NewBuilder() *component.Builder {
	return component.NewBuilder().
		ShootComponent(func(shoot *gardencorev1beta1.Shoot) (component.Resources, bool) {
			return NewResources(shoot.Spec.Provider.Workers), true
		})
}
