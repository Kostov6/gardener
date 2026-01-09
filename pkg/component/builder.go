// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package component

import (
	"context"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gardenletconfigv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resources describes a pure rendering interface that returns all objects grouped in bundles.
// It is intentionally minimal; callers provide application semantics (ManagedResources etc.).
// Note: An identical interface may exist in component subpackages while we migrate; prefer this one.
type Resources interface {
	All(ctx context.Context) ([]Bundle, error)
}

// Bundle groups a set of objects under a logical name (typically the ManagedResource name).
// Scope and labels can be added later if needed.
type Bundle struct {
	Name    string
	Objects []client.Object
	Labels  map[string]string
	// Destination controls where the ManagedResource is created: "seed" or "shoot".
	Destination string
}

// simpleDeployWater is a simple DeployWaiter implementation that applies Bundles
// as seed-scoped ManagedResources and waits for their health/deletion.
// It purposely does not expose care labels or custom timeouts for the first iteration.
//
//nolint:revive // keeping name as requested
type simpleDeployWater struct {
	client    client.Client
	namespace string
	resources Resources
	log       logr.Logger
}

// Deploy renders resources and creates/updates seed ManagedResources for each bundle.
func (s *simpleDeployWater) Deploy(ctx context.Context) error {
	if s.log.GetSink() != nil {
		s.log.Info("Deploying bundles via ManagedResources")
	}
	bundles, err := s.resources.All(ctx)
	if err != nil {
		return err
	}
	for _, b := range bundles {
		if s.log.GetSink() != nil {
			s.log.Info("Applying ManagedResource bundle", "name", b.Name)
		}
		registry := managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer)
		data, err := registry.AddAllAndSerialize(b.Objects...)
		if err != nil {
			return err
		}
		if b.Destination == "shoot" {
			if len(b.Labels) > 0 {
				if err := managedresources.CreateForShootWithLabels(ctx, s.client, s.namespace, b.Name, managedresources.LabelValueGardener, false, b.Labels, data); err != nil {
					return err
				}
			} else {
				if err := managedresources.CreateForShoot(ctx, s.client, s.namespace, b.Name, managedresources.LabelValueGardener, false, data); err != nil {
					return err
				}
			}
		} else {
			if len(b.Labels) > 0 {
				if err := managedresources.CreateForSeedWithLabels(ctx, s.client, s.namespace, b.Name, false, b.Labels, data); err != nil {
					return err
				}
			} else {
				if err := managedresources.CreateForSeed(ctx, s.client, s.namespace, b.Name, false, data); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Destroy deletes the seed ManagedResources created during Deploy.
func (s *simpleDeployWater) Destroy(ctx context.Context) error {
	if s.log.GetSink() != nil {
		s.log.Info("Destroying ManagedResources for bundles")
	}
	bundles, err := s.resources.All(ctx)
	if err != nil {
		return err
	}
	for _, b := range bundles {
		if s.log.GetSink() != nil {
			s.log.Info("Deleting ManagedResource bundle", "name", b.Name)
		}
		if b.Destination == "shoot" {
			if err := managedresources.DeleteForShoot(ctx, s.client, s.namespace, b.Name); err != nil {
				return err
			}
		} else {
			if err := managedresources.DeleteForSeed(ctx, s.client, s.namespace, b.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Wait waits until all ManagedResources are healthy with a conservative default timeout.
func (s *simpleDeployWater) Wait(ctx context.Context) error {
	if s.log.GetSink() != nil {
		s.log.Info("Waiting for ManagedResources to become healthy")
	}
	bundles, err := s.resources.All(ctx)
	if err != nil {
		return err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, b := range bundles {
		if s.log.GetSink() != nil {
			s.log.Info("Waiting for health", "name", b.Name)
		}
		if err := managedresources.WaitUntilHealthy(timeoutCtx, s.client, s.namespace, b.Name); err != nil {
			return err
		}
	}
	return nil
}

// WaitCleanup waits until all ManagedResources are deleted with a conservative default timeout.
func (s *simpleDeployWater) WaitCleanup(ctx context.Context) error {
	if s.log.GetSink() != nil {
		s.log.Info("Waiting for ManagedResources to be deleted")
	}
	bundles, err := s.resources.All(ctx)
	if err != nil {
		return err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, b := range bundles {
		if s.log.GetSink() != nil {
			s.log.Info("Waiting for deletion", "name", b.Name)
		}
		if err := managedresources.WaitUntilDeleted(timeoutCtx, s.client, s.namespace, b.Name); err != nil {
			return err
		}
	}
	return nil
}

// Builder constructs a simple DeployWaiter using functional parameters.
// Scope is seed-only for the first iteration.
type Builder struct {
	seedClientFn func() client.Client
	namespaceFn  func() string
	resourcesFn  func() Resources
	loggerFn     func() logr.Logger
	// Optional shoot-aware mapping: if set, Build will derive Resources from the Shoot.
	shoot          *gardencorev1beta1.Shoot
	shootComponent func(*gardencorev1beta1.Shoot) (Resources, bool)
	// Optional seed-aware mapping: if set, Build will derive Resources from the Seed.
	seed            *gardencorev1beta1.Seed
	gardenletConfig *gardenletconfigv1alpha1.GardenletConfiguration
	seedComponent   func(*gardencorev1beta1.Seed, *gardenletconfigv1alpha1.GardenletConfiguration) (Resources, bool)
	// Optional garden-aware mapping: if set, Build will derive Resources from the Garden.
	garden          *operatorv1alpha1.Garden
	gardenComponent func(*operatorv1alpha1.Garden) (Resources, bool)
}

// NewBuilder returns a new Builder instance.
func NewBuilder() *Builder { return &Builder{} }

// SeedClient supplies the seed client lazily.
func (b *Builder) SeedClient(fn func() client.Client) *Builder { b.seedClientFn = fn; return b }

// Namespace supplies the seed namespace lazily.
func (b *Builder) Namespace(fn func() string) *Builder { b.namespaceFn = fn; return b }

// WithResources supplies the Resources producer.
func (b *Builder) WithResources(fn func() Resources) *Builder { b.resourcesFn = fn; return b }

// Logger supplies a logger lazily for simpleDeployWater to emit logs.
func (b *Builder) Logger(fn func() logr.Logger) *Builder { b.loggerFn = fn; return b }

// Note: enable/disable is controlled by mapper functions returning the boolean flag.

// WithShoot supplies a Shoot object for mapper-based resource derivation.
// If Map was configured, Build will call the mapper with this Shoot to obtain Resources.
func (b *Builder) WithShoot(shoot *gardencorev1beta1.Shoot) *Builder { b.shoot = shoot; return b }

// Map configures a function to derive Resources from a Shoot object.
// When set, and if WithResources was not provided, Build will use this mapper.
func (b *Builder) ShootComponent(fn func(*gardencorev1beta1.Shoot) (Resources, bool)) *Builder {
	b.shootComponent = fn
	return b
}

// WithSeed supplies a Seed object for mapper-based resource derivation.
func (b *Builder) WithSeed(seed *gardencorev1beta1.Seed) *Builder { b.seed = seed; return b }

// SeedComponent configures a function to derive Resources from a Seed object.
func (b *Builder) SeedComponent(fn func(*gardencorev1beta1.Seed, *gardenletconfigv1alpha1.GardenletConfiguration) (Resources, bool)) *Builder {
	b.seedComponent = fn
	return b
}

// WithGardenletConfig supplies Gardenlet configuration for mapper-based resource derivation in seed context.
func (b *Builder) WithGardenletConfig(cfg *gardenletconfigv1alpha1.GardenletConfiguration) *Builder {
	b.gardenletConfig = cfg
	return b
}

// WithGarden supplies a Garden object for mapper-based resource derivation.
func (b *Builder) WithGarden(garden *operatorv1alpha1.Garden) *Builder { b.garden = garden; return b }

// GardenComponent configures a function to derive Resources from a Garden object.
func (b *Builder) GardenComponent(fn func(*operatorv1alpha1.Garden) (Resources, bool)) *Builder {
	b.gardenComponent = fn
	return b
}

// Build creates the DeployWaiter.
func (b *Builder) Build(componentType string) DeployWaiter {
	var (
		c  client.Client
		ns string
		rs Resources
		lg logr.Logger
	)
	if b.seedClientFn != nil {
		c = b.seedClientFn()
	}
	if b.namespaceFn != nil {
		ns = b.namespaceFn()
	}
	// Prefer explicit WithResources; otherwise select by declared componentType when provided.
	mappedEnabled := true
	if b.resourcesFn != nil {
		rs = b.resourcesFn()
	} else {
		switch componentType {
		case "shoot":
			if b.shootComponent != nil {
				var enabled bool
				rs, enabled = b.shootComponent(b.shoot)
				mappedEnabled = enabled
			}
		case "seed":
			if b.seedComponent != nil {
				var enabled bool
				rs, enabled = b.seedComponent(b.seed, b.gardenletConfig)
				mappedEnabled = enabled
			}
		case "garden":
			if b.gardenComponent != nil {
				var enabled bool
				rs, enabled = b.gardenComponent(b.garden)
				mappedEnabled = enabled
			}
		}
		// Fallback to previous precedence if componentType was empty or no mapper was configured.
		if rs == nil {
			if b.shootComponent != nil {
				var enabled bool
				rs, enabled = b.shootComponent(b.shoot)
				mappedEnabled = enabled
			} else if b.seedComponent != nil {
				var enabled bool
				rs, enabled = b.seedComponent(b.seed, b.gardenletConfig)
				mappedEnabled = enabled
			} else if b.gardenComponent != nil {
				var enabled bool
				rs, enabled = b.gardenComponent(b.garden)
				mappedEnabled = enabled
			}
		}
	}
	if b.loggerFn != nil {
		lg = b.loggerFn()
	}
	dw := &simpleDeployWater{
		client:    c,
		namespace: ns,
		resources: rs,
		log:       lg,
	}

	var deployer DeployWaiter = dw
	if !mappedEnabled {
		deployer = OpDestroyAndWait(deployer)
	}

	return deployer
}
