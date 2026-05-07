// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package stagedspec_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/utils/ptr"

	"github.com/gardener/gardener/pkg/apis/core"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	. "github.com/gardener/gardener/plugin/pkg/shoot/stagedspec"
)

var _ = Describe("StagedSpec", func() {
	var (
		plugin   *StagedSpec
		shoot    *core.Shoot
		oldShoot *core.Shoot
		ctx      context.Context
	)

	BeforeEach(func() {
		plugin = New()

		ctx = context.TODO()

		shoot = &core.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-shoot",
				Namespace: "garden-my-project",
			},
			Spec: core.ShootSpec{
				Kubernetes: core.Kubernetes{
					Version: "1.28.0",
				},
				Maintenance: &core.Maintenance{
					ConfineSpecUpdateRollout: ptr.To(true),
				},
			},
		}
		oldShoot = shoot.DeepCopy()
	})

	Describe("#Validate", func() {
		It("should allow if the resource is not a Shoot", func() {
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Secret").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("secrets").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{Name: "user"})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow if it is a subresource update", func() {
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "status", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{Name: "user"})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow if confineSpecUpdateRollout is nil", func() {
			shoot.Spec.Maintenance.ConfineSpecUpdateRollout = nil
			shoot.Spec.Kubernetes.Version = "1.29.0"
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{Name: "user"})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow if confineSpecUpdateRollout is false", func() {
			shoot.Spec.Maintenance.ConfineSpecUpdateRollout = ptr.To(false)
			shoot.Spec.Kubernetes.Version = "1.29.0"
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{Name: "user"})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow if the spec has not changed", func() {
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{Name: "user"})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow if the request comes from a gardenlet (group check)", func() {
			shoot.Spec.Kubernetes.Version = "1.29.0"
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{
				Name:   "gardener.cloud:system:seed:my-seed",
				Groups: []string{v1beta1constants.SeedsGroup},
			})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow if the request comes from a gardenlet (username prefix check)", func() {
			shoot.Spec.Kubernetes.Version = "1.29.0"
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{
				Name: v1beta1constants.SeedUserNamePrefix + "my-seed",
			})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject if spec changed and user is not a gardenlet", func() {
			shoot.Spec.Kubernetes.Version = "1.29.0"
			attrs := admission.NewAttributesRecord(shoot, oldShoot, core.Kind("Shoot").WithVersion("version"), shoot.Namespace, shoot.Name, core.Resource("shoots").WithVersion("version"), "", admission.Update, &metav1.UpdateOptions{}, false, &user.DefaultInfo{Name: "user@example.com"})

			err := plugin.Validate(ctx, attrs, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("confineSpecUpdateRollout"))
			Expect(err.Error()).To(ContainSubstring("shoot-my-shoot-staged-spec"))
			Expect(err.Error()).To(ContainSubstring("garden-my-project"))
		})
	})
})
