// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package prometheusoperator

import (
	"context"

	"github.com/gardener/gardener/imagevector"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/gardener/gardener/pkg/component"
	gardenletconfigv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewResources returns a Resources renderer for prometheus-operator using the component builder pattern.
func NewResources(namespace string, values Values) component.Resources {
	return &resources{namespace: namespace, values: values}
}

type resources struct {
	namespace string
	values    Values
}

func (r *resources) All(ctx context.Context) ([]component.Bundle, error) { //nolint:revive,unused
	// Reuse existing object constructors via the legacy type.
	p := &prometheusOperator{namespace: r.namespace, values: r.values}

	objs := []client.Object{
		p.serviceAccount(),
		p.service(),
		p.deployment(),
		p.vpa(),
		p.clusterRole(),
		p.clusterRoleBinding(),
		p.clusterRolePrometheus(),
		p.rolePrometheusShoot(),
	}

	// Bundle with care label so ManagedResource is tracked under ObservabilityComponentsHealthy.
	return []component.Bundle{{
		Name:    ManagedResourceName,
		Objects: objs,
		Labels:  map[string]string{v1beta1constants.LabelCareConditionType: v1beta1constants.ObservabilityComponentsHealthy},
	}}, nil
}

// NewBuilder provides mapping-based construction for Seed and Garden contexts.
func NewBuilder() *component.Builder {
	operatorImage, err := imagevector.Containers().FindImage(imagevector.ContainerImageNamePrometheusOperator)
	if err != nil {
		return nil
	}
	reloaderImage, err := imagevector.Containers().FindImage(imagevector.ContainerImageNameConfigmapReloader)
	if err != nil {
		return nil
	}

	return component.NewBuilder("prometheus-operator").
		SeedComponent(func(_ *gardencorev1beta1.Seed, _ *gardenletconfigv1alpha1.GardenletConfiguration) (component.Resources, bool) {
			return NewResources(v1beta1constants.GardenNamespace, Values{
				Image:               operatorImage.String(),
				ImageConfigReloader: reloaderImage.String(),
				PriorityClassName:   v1beta1constants.PriorityClassNameSeedSystem600,
			}), true
		}).
		GardenComponent(func(_ *operatorv1alpha1.Garden) (component.Resources, bool) {
			return NewResources(v1beta1constants.GardenNamespace, Values{
				Image:               operatorImage.String(),
				ImageConfigReloader: reloaderImage.String(),
				PriorityClassName:   v1beta1constants.PriorityClassNameGardenSystem100,
			}), true
		})
}
