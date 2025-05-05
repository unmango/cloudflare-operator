/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultCloudflaredImage = "docker.io/cloudflare/cloudflared:latest"
	defaultMetricsPort      = 2000
	cloudflaredFinalizer    = "cloudflared.unmango.dev/finalizer"
)

const (
	typeAvailableCloudflared = "Available"
	typeDegradedCloudflared  = "Degraded"
)

// CloudflaredReconciler reconciles a Cloudflared object
type CloudflaredReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=daemonsets;deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *CloudflaredReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cloudflared := &cfv1alpha1.Cloudflared{}
	if err := r.Get(ctx, req.NamespacedName, cloudflared); err != nil {
		log.V(1).Info("Cloudflared resource not found, ignoring")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if len(cloudflared.Status.Conditions) == 0 {
		if err := patchSubResource(ctx, r.Status(), cloudflared, func(obj *cfv1alpha1.Cloudflared) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeAvailableCloudflared,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation",
			})
		}); err != nil {
			log.Error(err, "Failed to update cloudflared status conditions")
			return ctrl.Result{}, err
		}
	}

	if !cloudflared.DeletionTimestamp.IsZero() {
		if err := r.delete(ctx, cloudflared); err != nil {
			log.Error(err, "Failed to delete cloudflared")
			return ctrl.Result{}, err
		} else {
			log.Info("Successfully deleted cloudflared")
			return ctrl.Result{}, nil
		}
	}

	var app client.Object
	switch cloudflared.Spec.Kind {
	case cfv1alpha1.DaemonSetCloudflaredKind:
		app = &appsv1.DaemonSet{}
		if err := r.Get(ctx, req.NamespacedName, app); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to get DaemonSet")
				return ctrl.Result{}, err
			}

			log.Info("Creating DaemonSet")
			if err = r.createDaemonSet(ctx, cloudflared); err != nil {
				return ctrl.Result{}, err
			} else {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
	case cfv1alpha1.DeploymentCloudflaredKind:
		app := &appsv1.Deployment{}
		if err := r.Get(ctx, req.NamespacedName, app); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to get Deployment")
				return ctrl.Result{}, err
			}

			log.Info("Creating Deployment")
			if err = r.createDeployment(ctx, cloudflared); err != nil {
				return ctrl.Result{}, err
			} else {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
	}

	if err := r.setStatus(ctx, cloudflared, metav1.Condition{
		Type:    typeAvailableCloudflared,
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciling",
		Message: "DaemonSet created successfully",
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CloudflaredReconciler) delete(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cloudflared, cloudflaredFinalizer) {
		log.V(1).Info("No finalizer on Cloudflared, nothing to do")
		return nil
	}

	log.Info("Performing finalizer operations before deleting Cloudflared")
	if err := patchSubResource(ctx, r.Status(), cloudflared, func(obj *cfv1alpha1.Cloudflared) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeDegradedCloudflared,
			Status:  metav1.ConditionUnknown,
			Reason:  "Finalizing",
			Message: "Performing finalizer operations",
		})
	}); err != nil {
		return err
	}

	r.Recorder.Event(cloudflared, "Warning", "Deleting",
		fmt.Sprintf("Custom resource %s is being deleted from the namespace %s",
			cloudflared.Name, cloudflared.Namespace),
	)

	if err := patchSubResource(ctx, r.Status(), cloudflared, func(obj *cfv1alpha1.Cloudflared) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeDegradedCloudflared,
			Status:  metav1.ConditionTrue,
			Reason:  "Finalizing",
			Message: "Finalizer operations were successfully accomplished",
		})
	}); err != nil {
		return err
	}

	log.Info("Removing Cloudflared finalizer after successfully finalizing")
	if err := patch(ctx, r, cloudflared, func(obj *cfv1alpha1.Cloudflared) {
		_ = controllerutil.RemoveFinalizer(cloudflared, cloudflaredFinalizer)
	}); err != nil {
		log.Error(err, "Failed to remove finalizer from Cloudflared")
		return err
	} else {
		return nil
	}
}

func (r *CloudflaredReconciler) createDaemonSet(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	log := logf.FromContext(ctx)
	template := r.podTemplateSpec(cloudflared)

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cloudflared.Name,
			Namespace: cloudflared.Namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: template.Labels,
			},
			Template: template,
		},
	}

	if err := ctrl.SetControllerReference(cloudflared, daemonset, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference")
		return err
	}

	if err := r.Create(ctx, daemonset); err != nil {
		log.Error(err, "Failed to create a new DaemonSet",
			"namespace", daemonset.Namespace,
			"name", daemonset.Name,
		)
		return err
	}

	return nil
}

func (r *CloudflaredReconciler) createDeployment(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	log := logf.FromContext(ctx)
	template := r.podTemplateSpec(cloudflared)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cloudflared.Name,
			Namespace: cloudflared.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cloudflared.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: template.Labels,
			},
			Template: template,
		},
	}

	if err := ctrl.SetControllerReference(cloudflared, deployment, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference")
		return err
	}

	if err := r.Create(ctx, deployment); err != nil {
		log.Error(err, "Failed to created new Deployment",
			"namespace", deployment.Namespace,
			"name", deployment.Name,
		)
		return err
	}

	return nil
}

func (r *CloudflaredReconciler) podTemplateSpec(cloudflared *cfv1alpha1.Cloudflared) corev1.PodTemplateSpec {
	template := corev1.PodTemplateSpec{}

	if cloudflared.Spec.Template != nil {
		cloudflared.Spec.Template.DeepCopyInto(&template)
	}

	template.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Sysctls: []corev1.Sysctl{{
			Name:  "net.ipv4.ping_group_range",
			Value: "65532 65532",
		}},
	}

	if config := cloudflared.Spec.Config; config != nil && config.ValueFrom != nil {
		if ref := config.ValueFrom.SecretKeyRef; ref != nil {
			template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ref.Name,
						Items: []corev1.KeyToPath{{
							Key:  ref.Key,
							Path: "config.yml",
						}},
					},
				},
			})
		}
		if ref := config.ValueFrom.ConfigMapKeyRef; ref != nil {
			template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: ref.Name,
						},
						Items: []corev1.KeyToPath{{
							Key:  ref.Key,
							Path: "config.yml",
						}},
					},
				},
			})
		}
	}

	var volumeMounts []corev1.VolumeMount
	if len(template.Spec.Volumes) > 0 {
		volumeMounts = []corev1.VolumeMount{{
			Name:      "config",
			MountPath: "/etc/cloudflared",
		}}
	}

	version := strings.TrimPrefix(cloudflared.Spec.Version, "v")

	// create the base container
	container := corev1.Container{
		Name:  "cloudflared",
		Image: "docker.io/cloudflare/cloudflared:" + version,
		Command: []string{
			"cloudflared", "tunnel", "--no-autoupdate",
			"--metrics", fmt.Sprintf("0.0.0.0:%d", defaultMetricsPort),
		},
		Args: []string{"--hello-world"},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt32(defaultMetricsPort),
					Path: "/ready",
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			FailureThreshold:    1,
		},
		VolumeMounts:    volumeMounts,
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			RunAsUser:                ptr.To[int64](1001),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	var containers []corev1.Container
	for _, ctr := range template.Spec.Containers {
		if ctr.Name == "cloudflared" {
			r.applyCustomizations(&container, &ctr)
		} else {
			containers = append(containers, ctr)
		}
	}

	template.Spec.Containers = append(containers, container)
	template.Labels = r.labels(container)

	return template
}

func (r *CloudflaredReconciler) applyCustomizations(base, custom *corev1.Container) {
	if len(custom.Image) > 0 {
		base.Image = custom.Image
	}
}

func (r *CloudflaredReconciler) labels(ctr corev1.Container) map[string]string {
	version := "latest"
	if s := strings.Split(ctr.Image, ":"); len(s) > 1 {
		version = strings.TrimPrefix(s[1], "v")
	}

	return map[string]string{
		"app.kubernetes.io/name":       "cloudflare-operator",
		"app.kubernetes.io/version":    version,
		"app.kubernetes.io/managed-by": "CloudflaredController",
	}
}

func (r *CloudflaredReconciler) setStatus(
	ctx context.Context,
	cloudflared *cfv1alpha1.Cloudflared,
	condition metav1.Condition,
) error {
	log := logf.FromContext(ctx)

	_ = meta.SetStatusCondition(&cloudflared.Status.Conditions, condition)
	if err := r.Status().Update(ctx, cloudflared); err != nil {
		log.Error(err, "Failed to update Cloudflared status")
		return err
	}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cloudflared.Namespace,
		Name:      cloudflared.Name,
	}, cloudflared); err != nil {
		log.Error(err, "Failed to re-fetch Cloudflared")
		return err
	}

	return nil
}

func (r *CloudflaredReconciler) containsFinalizer(cloudflared *cfv1alpha1.Cloudflared) bool {
	return controllerutil.ContainsFinalizer(cloudflared, cloudflaredFinalizer)
}

func (r *CloudflaredReconciler) addFinalizer(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	if ok := controllerutil.AddFinalizer(cloudflared, cloudflaredFinalizer); !ok {
		return fmt.Errorf("finalizer for cloudflared was not added")
	}

	return r.Update(ctx, cloudflared)
}

func (r *CloudflaredReconciler) removeFinalizer(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	log := logf.FromContext(ctx)

	if ok := controllerutil.RemoveFinalizer(cloudflared, cloudflaredFinalizer); !ok {
		err := fmt.Errorf("finalizer for cloudflared was not removed")
		log.Error(err, "Failed to remove finalizer for Cloudflared")
		return err
	}

	if err := r.Update(ctx, cloudflared); err != nil {
		log.Error(err, "Failed to remove finalizer for Cloudflared")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudflaredReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfv1alpha1.Cloudflared{}).
		Named("cloudflared").
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
