// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package clusterautoscaler

import (
	"context"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"

	"github.com/gardener/gardener/pkg/component"
	machinev1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// bootstrapResources is a Resources implementation for the cluster-autoscaler
// bootstrapper. It renders the cluster-scoped RBAC needed by the autoscaler.
type bootstrapResources struct{}

// All returns the full set of objects for the bootstrapper as a single bundle.
func (br *bootstrapResources) All(ctx context.Context) ([]component.Bundle, error) {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleControlName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{machinev1alpha1.GroupName},
				Resources: []string{"machineclasses", "machinedeployments", "machines", "machinesets"},
				Verbs:     []string{"create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	return []component.Bundle{{
		Name:    managedResourceControlName,
		Objects: []client.Object{clusterRole},
	}}, nil
}

// NewBootstrapResources returns a renderer for the cluster-autoscaler bootstrap resources.
// It exposes the unexported implementation via the public component.Resources interface.
func NewBootstrapResources() component.Resources { return &bootstrapResources{} }

func NewBuilder() *component.Builder {
	return component.NewBuilder().
		SeedComponent(func(_ *gardencorev1beta1.Seed) component.Resources {
			return NewBootstrapResources()
		})
}
