// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package botanist

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener/pkg/utils/flow"
)

// MigrateSecrets exports the secrets generated with the fake client and imports them with the real client.
func (b *GardenadmBotanist) MigrateSecrets(ctx context.Context, fakeClient, realClient client.Client) error {
	secretList := &corev1.SecretList{}
	if err := fakeClient.List(ctx, secretList, client.InNamespace(b.Shoot.ControlPlaneNamespace)); err != nil {
		return fmt.Errorf("failed listing secrets with fake client: %w", err)
	}

	var taskFns []flow.TaskFn

	for _, sec := range secretList.Items {
		s := sec // capture for closure
		taskFns = append(taskFns, func(ctx context.Context) error {
			ns := s.Namespace
			if ns == "" {
				ns = b.Shoot.ControlPlaneNamespace // or your default namespace
			}

			// Check if the Secret already exists
			existing := &corev1.Secret{}
			key := client.ObjectKey{Name: s.Name, Namespace: ns}
			if err := realClient.Get(ctx, key, existing); err == nil {
				// Already present: skip create
				return nil
			} else if !apierrors.IsNotFound(err) {
				// Unexpected error while checking existence
				return fmt.Errorf("checking secret %s/%s: %w", ns, s.Name, err)
			}
			b.Logger.Info("Creating secret", "name", s.Name)

			// Create only when not found
			return realClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        s.Name,
					Namespace:   ns,
					Labels:      s.Labels,
					Annotations: s.Annotations,
				},
				Type:      s.Type,
				Immutable: s.Immutable,
				Data:      s.Data,
				// Optionally: StringData: s.StringData,
			})
		})
	}

	return flow.Parallel(taskFns...)(ctx)
}
