// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package init

import (
	"github.com/spf13/pflag"

	"github.com/gardener/gardener/pkg/gardenadm/cmd"
)

// Options contains options for this command.
type Options struct {
	*cmd.Options
	cmd.ManifestOptions

	// SecretFile optionally points to a YAML/JSON file containing one or more Kubernetes Secret objects
	// to be applied during bootstrap.
	SecretFile string

	// UseBootstrapEtcd indicates whether to use the bootstrap etcd instead of transitioning to etcd-druid.
	UseBootstrapEtcd bool

	Bootstrap bool

	// StoreContainer is the store container identifier for etcd backup/restore.
	StoreContainer string
	// NoMCM skips deployment of machine-controller-manager and any worker-related steps.
	NoMCM bool
}

// ParseArgs parses the arguments to the options.
func (o *Options) ParseArgs(args []string) error {
	return o.ManifestOptions.ParseArgs(args)
}

// Validate validates the options.
func (o *Options) Validate() error {
	return o.ManifestOptions.Validate()
}

// Complete completes the options.
func (o *Options) Complete() error {
	return o.ManifestOptions.Complete()
}

func (o *Options) addFlags(fs *pflag.FlagSet) {
	o.ManifestOptions.AddFlags(fs)
	fs.StringVar(&o.SecretFile, "secret-file", "", "Path to a YAML/JSON file containing one or more Kubernetes Secret objects to apply during bootstrap.")
	fs.BoolVar(&o.UseBootstrapEtcd, "use-bootstrap-etcd", false, "If set, the control plane continues using the bootstrap etcd instead of transitioning to etcd-druid. This is useful for testing purposes to save time.")
	fs.BoolVar(&o.Bootstrap, "bootstrap", false, "If set, only bootstap")
	fs.StringVar(&o.StoreContainer, "store-container", "", "The store container identifier for etcd backup/restore.")
	fs.BoolVar(&o.NoMCM, "no-mcm", false, "If set, skip deploying machine-controller-manager and worker-related components.")
}
