// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package discover

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"

	"github.com/gardener/gardener/pkg/gardenadm/cmd"
)

// Options contains options for this command.
type Options struct {
	*cmd.Options
	cmd.ManifestOptions

	// Kubeconfig is the path to the kubeconfig file pointing to the garden cluster.
	Kubeconfig string
	// ShootManifest is the path to the shoot manifest file.
	ShootManifest string
	// ShootName is the name of an existing Shoot in the garden cluster to discover resources for.
	ShootName string
	// ShootNamespace is the namespace of an existing Shoot in the garden cluster to discover resources for.
	ShootNamespace string
	// ManagedInfrastructure indicates whether Gardener will manage the shoot's infrastructure (network, domains,
	// machines, etc.). Set this to true if using 'gardenadm bootstrap' for bootstrapping the shoot cluster. Set this to
	// false if managing the infrastructure outside of Gardener.
	ManagedInfrastructure bool
}

// ParseArgs parses the arguments to the options.
func (o *Options) ParseArgs(args []string) error {
	if err := cmd.DefaultKubeconfig(&o.Kubeconfig); err != nil {
		return fmt.Errorf("cloud not default kubeconfig: %w", err)
	}

	if len(args) > 0 {
		o.ShootManifest = strings.TrimSpace(args[0])

		if len(o.ConfigDir) == 0 {
			o.ConfigDir = filepath.Dir(o.ShootManifest)
		}
	}

	return o.ManifestOptions.ParseArgs(args)
}

// Validate validates the options.
func (o *Options) Validate() error {
	if len(o.Kubeconfig) == 0 {
		return fmt.Errorf("must provide a path to a garden cluster kubeconfig")
	}

	existingShoot := len(o.ShootName) > 0 || len(o.ShootNamespace) > 0
	newShoot := len(o.ShootManifest) > 0

	if existingShoot && newShoot {
		return fmt.Errorf("must not provide both a shoot manifest file and --shoot-name/--shoot-namespace")
	}

	if !existingShoot && !newShoot {
		return fmt.Errorf("must provide either a path to the shoot manifest file or both --shoot-name and --shoot-namespace")
	}

	if existingShoot {
		if len(o.ShootName) == 0 {
			return fmt.Errorf("must provide --shoot-name when --shoot-namespace is set")
		}
		if len(o.ShootNamespace) == 0 {
			return fmt.Errorf("must provide --shoot-namespace when --shoot-name is set")
		}
	}

	return o.ManifestOptions.Validate()
}

// Complete completes the options.
func (o *Options) Complete() error { return o.ManifestOptions.Complete() }

func (o *Options) addFlags(fs *pflag.FlagSet) {
	o.ManifestOptions.AddFlags(fs)
	fs.StringVarP(&o.Kubeconfig, "kubeconfig", "k", "", "Path to the kubeconfig file pointing to the garden cluster")
	fs.StringVar(&o.ShootName, "shoot-name", "", "Name of an existing Shoot in the garden cluster to discover resources for. "+
		"Mutually exclusive with the positional shoot manifest argument. Must be set together with --shoot-namespace.")
	fs.StringVar(&o.ShootNamespace, "shoot-namespace", "", "Namespace of an existing Shoot in the garden cluster to discover resources for. "+
		"Mutually exclusive with the positional shoot manifest argument. Must be set together with --shoot-name.")
	fs.BoolVar(&o.ManagedInfrastructure, "managed-infrastructure", true, "Indicates whether Gardener will manage the shoot's infrastructure (network, domains, machines, etc.). Set this to true if using 'gardenadm bootstrap' for bootstrapping the shoot cluster. Set this to false if managing the infrastructure outside of Gardener.")
}
