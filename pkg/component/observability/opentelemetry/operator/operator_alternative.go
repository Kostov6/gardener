// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"

	"github.com/gardener/gardener/imagevector"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/gardener/gardener/pkg/component"
	gardenletconfigv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	gardenlethelper "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1/helper"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewResources returns a Resources renderer for OpenTelemetry operator using the component builder pattern.
func NewResources(namespace string, values Values) component.Resources {
	return &resources{namespace: namespace, values: values}
}

type resources struct {
	namespace string
	values    Values
}

func (r *resources) All(ctx context.Context) ([]component.Bundle, error) { //nolint:revive,unused
	// Reuse the existing object constructors via the legacy type.
	o := &openTelemetryOperator{namespace: r.namespace, values: r.values}

	objs := []client.Object{
		o.serviceAccount(),
		o.clusterRole(),
		o.clusterRoleBinding(),
		o.role(),
		o.roleBinding(),
		o.deployment(),
		o.vpa(),
	}

	// Bundle with care label so ManagedResource is tracked under ObservabilityComponentsHealthy.
	return []component.Bundle{
		{
			Name:    OperatorManagedResourceName,
			Objects: objs,
			Labels:  map[string]string{v1beta1constants.LabelCareConditionType: v1beta1constants.ObservabilityComponentsHealthy},
		},
	}, nil
}

// NewBuilder provides mapping-based construction for Seed and Garden contexts.
func NewBuilder() *component.Builder {
	image, err := imagevector.Containers().FindImage(imagevector.ContainerImageNameOpentelemetryOperator)
	if err != nil {
		return nil
	}

	return component.NewBuilder().
		SeedComponent(func(_ *gardencorev1beta1.Seed, config *gardenletconfigv1alpha1.GardenletConfiguration) (component.Resources, bool) {
			return NewResources(v1beta1constants.GardenNamespace, Values{
				Image:             image.String(),
				PriorityClassName: v1beta1constants.PriorityClassNameSeedSystem600,
			}), gardenlethelper.IsLoggingEnabled(config)
		}).
		GardenComponent(func(_ *operatorv1alpha1.Garden) (component.Resources, bool) {
			return NewResources(v1beta1constants.GardenNamespace, Values{
				Image:             image.String(),
				PriorityClassName: v1beta1constants.PriorityClassNameGardenSystem100,
			}), true
		})
}
