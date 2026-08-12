#!/usr/bin/env bash
#
# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

set -o nounset
set -o pipefail
set -o errexit

# source $(dirname "${0}")/ci-common.sh

# clamp_mss_to_pmtu

# # export all container logs and events after test execution
# trap "
#   ( export_artifacts_host_services; export_artifacts_infra; export_artifacts_load_balancers )
#   ( export_artifacts_gind )
#   ( export KUBECONFIG=$KUBECONFIG_SELFHOSTEDSHOOT_CLUSTER; export_artifacts_for_cluster 'self-hosted-shoot' )
#   ( make gind-down )
# " EXIT

# The unmanaged-infra disaster-recovery scenario runs entirely against the self-hosted shoot on the gind machine
# containers. It does not require a runtime or virtual garden cluster: the etcd backup lives on the node's local disk
# and the Gardener configuration resources needed by 'gardenadm restore' are provided to the test directly.
make gind-up GARDENADM_INIT_FLAGS="--log-level=debug"
make test-e2e-local-gardenadm-unmanaged-infra-dr
