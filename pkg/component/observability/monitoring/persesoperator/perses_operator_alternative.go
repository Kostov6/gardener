// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package persesoperator

import (
	"context"

	"github.com/gardener/gardener/imagevector"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/gardener/gardener/pkg/component"
	gardenletconfigv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewResources returns a Resources renderer for perses-operator using the component builder pattern.
func NewResources(namespace string, values Values) component.Resources {
	return &resources{namespace: namespace, values: values}
}

type resources struct {
	namespace string
	values    Values
}

func (r *resources) All(ctx context.Context) ([]component.Bundle, error) { //nolint:revive,unused
	// Reuse the existing object constructors via the legacy type.
	p := &persesOperator{namespace: r.namespace, values: r.values}

	objs := []client.Object{
		p.serviceAccount(),
		p.deployment(),
		p.vpa(),
		p.clusterRole(),
		p.clusterRoleBinding(),
	}

	// Bundle with care label so ManagedResource is tracked under ObservabilityComponentsHealthy.
	return []component.Bundle{
		{
			Name:    managedResourceName,
			Objects: objs,
			Labels:  map[string]string{v1beta1constants.LabelCareConditionType: v1beta1constants.ObservabilityComponentsHealthy},
		},
	}, nil
}

func NewBuilder() *component.Builder {
	image, err := imagevector.Containers().FindImage(imagevector.ContainerImageNamePersesOperator)
	if err != nil {
		return nil
	}

	return component.NewBuilder("perses-operator").
		SeedComponent(func(_ *gardencorev1beta1.Seed, _ *gardenletconfigv1alpha1.GardenletConfiguration) (component.Resources, bool) {
			return NewResources(v1beta1constants.GardenNamespace, Values{
				Image:             image.String(),
				PriorityClassName: v1beta1constants.PriorityClassNameSeedSystem600,
			}), true
		}).
		GardenComponent(func(_ *v1alpha1.Garden) (component.Resources, bool) {
			return NewResources(v1beta1constants.GardenNamespace, Values{
				Image:             image.String(),
				PriorityClassName: v1beta1constants.PriorityClassNameGardenSystem100,
			}), true
		})
}
