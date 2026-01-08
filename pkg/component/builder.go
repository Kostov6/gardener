// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package component

import (
	"context"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
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
		if err := managedresources.CreateForSeed(ctx, s.client, s.namespace, b.Name, false, data); err != nil {
			return err
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
		if err := managedresources.DeleteForSeed(ctx, s.client, s.namespace, b.Name); err != nil {
			return err
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
	shootComponent func(*gardencorev1beta1.Shoot) Resources
	// Optional seed-aware mapping: if set, Build will derive Resources from the Seed.
	seed          *gardencorev1beta1.Seed
	seedComponent func(*gardencorev1beta1.Seed) Resources
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

// WithShoot supplies a Shoot object for mapper-based resource derivation.
// If Map was configured, Build will call the mapper with this Shoot to obtain Resources.
func (b *Builder) WithShoot(shoot *gardencorev1beta1.Shoot) *Builder { b.shoot = shoot; return b }

// Map configures a function to derive Resources from a Shoot object.
// When set, and if WithResources was not provided, Build will use this mapper.
func (b *Builder) ShootComponent(fn func(*gardencorev1beta1.Shoot) Resources) *Builder {
	b.shootComponent = fn
	return b
}

// WithSeed supplies a Seed object for mapper-based resource derivation.
func (b *Builder) WithSeed(seed *gardencorev1beta1.Seed) *Builder { b.seed = seed; return b }

// SeedComponent configures a function to derive Resources from a Seed object.
func (b *Builder) SeedComponent(fn func(*gardencorev1beta1.Seed) Resources) *Builder {
	b.seedComponent = fn
	return b
}

// Build creates the DeployWaiter.
func (b *Builder) Build() DeployWaiter {
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
	// Prefer explicit WithResources; otherwise fall back to mapper if configured.
	if b.resourcesFn != nil {
		rs = b.resourcesFn()
	} else if b.shootComponent != nil {
		rs = b.shootComponent(b.shoot)
	} else if b.seedComponent != nil {
		rs = b.seedComponent(b.seed)
	}
	if b.loggerFn != nil {
		lg = b.loggerFn()
	}
	return &simpleDeployWater{
		client:    c,
		namespace: ns,
		resources: rs,
		log:       lg,
	}
}
