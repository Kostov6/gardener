package registry

import (
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/gardener/gardener/pkg/component"
	"github.com/gardener/gardener/pkg/component/autoscaling/clusterautoscaler"
	"github.com/gardener/gardener/pkg/component/observability/monitoring/persesoperator"
	"github.com/gardener/gardener/pkg/component/observability/opentelemetry/operator"
	"github.com/gardener/gardener/pkg/component/shoot/namespaces"
	gardenletconfigv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Registry struct {
	components []*component.Builder

	// Common configuration functions/objects applied to all builders
	client          client.Client
	namespace       string
	shoot           *gardencorev1beta1.Shoot
	seed            *gardencorev1beta1.Seed
	garden          *operatorv1alpha1.Garden
	gardenletConfig *gardenletconfigv1alpha1.GardenletConfiguration

	// Built components for the selected destination
	buildComponents map[string]component.DeployWaiter
}

func NewRegistry() *Registry {
	return &Registry{
		components: []*component.Builder{
			namespaces.NewBuilder(),
			clusterautoscaler.NewBuilder(),
			persesoperator.NewBuilder(),
			operator.NewBuilder(),
		},
	}
}

// Build configures all builders with the registry-supplied functions/objects and builds
// deployers for the given destination: "shoot", "seed", or "garden".
func (r *Registry) Build(componentType string) *Registry {
	r.buildComponents = make(map[string]component.DeployWaiter)

	for _, b := range r.components {
		b.Client(r.client)
		b.Namespace(r.namespace)

		if r.shoot != nil {
			b.WithShoot(r.shoot)
		}
		if r.seed != nil {
			b.WithSeed(r.seed)
		}
		if r.garden != nil {
			b.WithGarden(r.garden)
		}
		if r.gardenletConfig != nil {
			b.WithGardenletConfig(r.gardenletConfig)
		}
		if dw := b.Build(componentType); dw != nil {
			r.buildComponents[b.Name()] = dw
		}
	}
	return r
}

// SeedClient supplies the seed client lazily to all component builders.
func (r *Registry) Client(c client.Client) *Registry { r.client = c; return r }

// Namespace supplies the seed namespace lazily to all component builders.
func (r *Registry) Namespace(ns string) *Registry { r.namespace = ns; return r }

// WithShoot supplies a Shoot object to all component builders for mapper-based resource derivation.
func (r *Registry) WithShoot(shoot *gardencorev1beta1.Shoot) *Registry { r.shoot = shoot; return r }

// WithSeed supplies a Seed object to all component builders for mapper-based resource derivation.
func (r *Registry) WithSeed(seed *gardencorev1beta1.Seed) *Registry { r.seed = seed; return r }

// WithGardenletConfig supplies Gardenlet configuration for seed mappers across all builders.
func (r *Registry) WithGardenletConfig(cfg *gardenletconfigv1alpha1.GardenletConfiguration) *Registry {
	r.gardenletConfig = cfg
	return r
}

// WithGarden supplies a Garden object to all component builders for mapper-based resource derivation.
func (r *Registry) WithGarden(garden *operatorv1alpha1.Garden) *Registry { r.garden = garden; return r }

// Component returns the built component by name for the selected destination.
func (r *Registry) Component(name string) component.DeployWaiter { return r.buildComponents[name] }
