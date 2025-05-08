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
	"os"
	"strings"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	cfclient "github.com/unmango/cloudflare-operator/internal/client"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
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

type tunnel struct {
	AccountId *string
	Id        *string
	Token     *string
}

// CloudflaredReconciler reconciles a Cloudflared object
type CloudflaredReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	Cloudflare cfclient.Client
}

// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds/finalizers,verbs=update
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaretunnels/status,verbs=get
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

	// Kind hasn't been set, we need to create the app
	if cloudflared.Status.Kind == "" {
		if err := r.createApp(ctx, cloudflared); err != nil {
			log.Error(err, "Failed to create app for Cloudflared")
			return ctrl.Result{}, err
		} else {
			log.Info("Successfully created app for Cloudflared")
			return ctrl.Result{}, nil
		}
	}

	appKey := client.ObjectKey{
		Namespace: cloudflared.Namespace,
		Name:      cloudflared.Name,
	}

	app, err := r.getApp(ctx, appKey, cloudflared.Status.Kind)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to get app for Cloudflared")
			return ctrl.Result{}, err
		}

		// TODO: Probably should just clear Status.Kind and re-queue
		if err = r.createApp(ctx, cloudflared); err != nil {
			log.Error(err, "Failed to create app for Cloudflared")
			return ctrl.Result{}, err
		} else {
			log.Info("Successfully created app for Cloudflared")
			return ctrl.Result{}, nil
		}
	}

	// Kind has changed, we need to re-create the app
	if cloudflared.Status.Kind != cloudflared.Spec.Kind {
		log.Info("Spec app Kind does not match observed app Kind, deleting app",
			"spec", cloudflared.Spec.Kind,
			"status", cloudflared.Status.Kind,
		)
		if err := r.deleteApp(ctx, app); err != nil {
			log.Error(err, "Failed to delete app for Cloudflared", "kind", app.GetObjectKind())
			return ctrl.Result{}, err
		} else {
			log.Info("Successfully deleted app for Cloudflared, requeuing to create replacement",
				"kind", app.GetObjectKind(),
			)
			return ctrl.Result{Requeue: true}, err
		}
	}

	log.Info("Checking for tunnel ref", "config", cloudflared.Spec.Config)
	if config := cloudflared.Spec.Config; config != nil && config.TunnelRef != nil {
		tunnel := &cfv1alpha1.CloudflareTunnel{}
		if err := r.Get(ctx, req.NamespacedName, tunnel); err != nil {
			log.Error(err, "Failed to lookup referenced CloudflareTunnel")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		hasOwnerRef, err := controllerutil.HasOwnerReference(cloudflared.OwnerReferences, tunnel, r.Scheme)
		if err != nil {
			log.Error(err, "Failed to lookup owner reference")
			return ctrl.Result{}, nil
		}

		if !hasOwnerRef {
			log.Info("Adding owner reference to CloudflareTunnel on Cloudflared", "tunnel", tunnel.Name, "cloudflared", cloudflared.Name)
			// All cloudflared connections need to be closed before deleting the tunnel,
			// set an owner reference to ensure proper deletion order
			if err := controllerutil.SetOwnerReference(tunnel, cloudflared, r.Scheme,
				controllerutil.WithBlockOwnerDeletion(true),
			); err != nil {
				log.Error(err, "Failed to set owner reference to CloudflareTunnel on Cloudflared")
				return ctrl.Result{}, nil
			}
			if err := r.Update(ctx, cloudflared); err != nil {
				log.Error(err, "Failed to update Cloudflared with owner reference")
				return ctrl.Result{}, nil
			}
		}
	}

	if err = r.updateApp(ctx, app, cloudflared); err != nil {
		log.Error(err, "Failed to update app for Cloudflared")
		return ctrl.Result{}, err
	} else {
		log.Info("Successfully updated app for Cloudflared")
		return ctrl.Result{}, nil
	}
}

func (r *CloudflaredReconciler) getApp(ctx context.Context, key client.ObjectKey, kind cfv1alpha1.CloudflaredKind) (app client.Object, err error) {
	switch kind {
	case cfv1alpha1.DaemonSetCloudflaredKind:
		app = &appsv1.DaemonSet{}
		err = r.Get(ctx, key, app)
	case cfv1alpha1.DeploymentCloudflaredKind:
		app = &appsv1.Deployment{}
		err = r.Get(ctx, key, app)
	default:
		err = fmt.Errorf("unsupported kind: %s", kind)
	}

	return
}

// // I wrote this and I want to use it but not in this PR so commented it becomes
// func (r *CloudflaredReconciler) appReady(app client.Object) bool {
// 	switch app := app.(type) {
// 	case *appsv1.DaemonSet:
// 		return app.Status.DesiredNumberScheduled == app.Status.NumberReady
// 	case *appsv1.Deployment:
// 		for _, c := range app.Status.Conditions {
// 			if c.Type == appsv1.DeploymentAvailable {
// 				return c.Status == corev1.ConditionTrue
// 			}
// 		}
// 	}
//
// 	return false
// }

func (r *CloudflaredReconciler) createApp(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	log := logf.FromContext(ctx)

	tunnel, err := r.lookupTunnel(ctx, cloudflared)
	if err != nil {
		return err
	}

	template := r.podTemplateSpec(cloudflared, tunnel)

	var app client.Object
	switch cloudflared.Spec.Kind {
	case cfv1alpha1.DaemonSetCloudflaredKind:
		log.Info("Creating DaemonSet for Cloudflared")
		app = r.createDaemonSet(cloudflared, template)
	case cfv1alpha1.DeploymentCloudflaredKind:
		log.Info("Creating Deployment for Cloudflared")
		app = r.createDeployment(cloudflared, template)
	default:
		return fmt.Errorf("unsupported kind: %s", cloudflared.Spec.Kind)
	}

	if err := ctrl.SetControllerReference(cloudflared, app, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference")
		return err
	}

	if err := r.Create(ctx, app); err != nil {
		log.Error(err, "Failed to create a new app for Cloudflared")
		return err
	}

	if err := patchSubResource(ctx, r.Status(), cloudflared, func(obj *cfv1alpha1.Cloudflared) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeAvailableCloudflared,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: fmt.Sprintf("%s created successfully", cloudflared.Spec.Kind),
		})
		obj.Status.Kind = cloudflared.Spec.Kind
		obj.Status.TunnelId = tunnel.Id
	}); err != nil {
		log.Error(err, "Failed to update cloudflared status conditions")
		return err
	} else {
		return nil
	}
}

func (r *CloudflaredReconciler) createDaemonSet(cloudflared *cfv1alpha1.Cloudflared, podTemplate corev1.PodTemplateSpec) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cloudflared.Name,
			Namespace: cloudflared.Namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: podTemplate.Labels,
			},
			Template: podTemplate,
		},
	}
}

func (r *CloudflaredReconciler) createDeployment(cloudflared *cfv1alpha1.Cloudflared, podTemplate corev1.PodTemplateSpec) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cloudflared.Name,
			Namespace: cloudflared.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cloudflared.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: podTemplate.Labels,
			},
			Template: podTemplate,
		},
	}
}

func (r *CloudflaredReconciler) updateApp(ctx context.Context, app client.Object, cloudflared *cfv1alpha1.Cloudflared) error {
	switch app := app.(type) {
	case *appsv1.DaemonSet:
		return r.updateDaemonSet(ctx, app, cloudflared)
	case *appsv1.Deployment:
		return r.updateDeployment(ctx, app, cloudflared)
	default:
		return fmt.Errorf("unsupported app type: %T", app)
	}
}

func (r *CloudflaredReconciler) updateDaemonSet(ctx context.Context, app *appsv1.DaemonSet, cloudflared *cfv1alpha1.Cloudflared) (err error) {
	log := logf.FromContext(ctx)

	tunnel, err := r.lookupTunnel(ctx, cloudflared)
	if err != nil {
		return err
	}

	if err := patch(ctx, r, app, func(obj *appsv1.DaemonSet) {
		// Blindly apply the spec and let the DaemonSet controller reconcile differences
		obj.Spec.Template = r.podTemplateSpec(cloudflared, tunnel)
	}); err != nil {
		log.Error(err, "Failed to patch Cloudflared DaemonSet")
		return err
	} else {
		return nil
	}
}

func (r *CloudflaredReconciler) updateDeployment(ctx context.Context, app *appsv1.Deployment, cloudflared *cfv1alpha1.Cloudflared) (err error) {
	log := logf.FromContext(ctx)

	tunnel, err := r.lookupTunnel(ctx, cloudflared)
	if err != nil {
		return err
	}

	if err := patch(ctx, r, app, func(obj *appsv1.Deployment) {
		// Blindly apply the spec and let the Deployment controller reconcile differences
		obj.Spec.Template = r.podTemplateSpec(cloudflared, tunnel)
		if replicas := cloudflared.Spec.Replicas; replicas != app.Spec.Replicas {
			obj.Spec.Replicas = replicas
		}
	}); err != nil {
		log.Error(err, "Failed to patch Cloudflared Deployment")
		return err
	} else {
		return nil
	}
}

func (r *CloudflaredReconciler) deleteApp(ctx context.Context, app client.Object) error {
	if err := r.Delete(ctx, app); err != nil {
		return client.IgnoreNotFound(err)
	} else {
		return nil
	}
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

func (r *CloudflaredReconciler) lookupTunnel(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) (tunnel tunnel, err error) {
	log := logf.FromContext(ctx)
	config := cloudflared.Spec.Config
	if config == nil {
		return tunnel, nil
	}

	if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
		logf.FromContext(ctx).V(1).Info("No Cloudflare API token set, not looking up tunnel")
		return tunnel, nil
	}

	if tunnel.Id = config.TunnelId; tunnel.Id != nil {
		log.Info("Using inline config for Cloudflare tunnel lookup")
		if tunnel.AccountId = config.AccountId; tunnel.AccountId == nil {
			return tunnel, fmt.Errorf("accountId is required when tunnelId != nil")
		}
	}

	if ref := config.TunnelRef; ref != nil {
		cftunnel := &cfv1alpha1.CloudflareTunnel{}
		key := client.ObjectKey{
			Namespace: cloudflared.Namespace,
			Name:      ref.Name,
		}

		log.Info("Using tunnel ref for Cloudflare tunnel lookup")
		if err := r.Get(ctx, key, cftunnel); err != nil {
			return tunnel, err
		}

		tunnel.AccountId = &cftunnel.Status.AccountTag
		tunnel.Id = &cftunnel.Status.Id

		log.Info("Found tunnel credentials",
			"tunnelId", tunnel.Id,
			"accountId", tunnel.AccountId,
		)
	}

	if tunnel.Id != nil && tunnel.AccountId != nil {
		tunnel.Token, err = r.Cloudflare.GetTunnelToken(ctx, *tunnel.Id, zero_trust.TunnelCloudflaredTokenGetParams{
			AccountID: cloudflare.F(*tunnel.AccountId),
		})
		if err != nil {
			return tunnel, err
		}
	}

	return tunnel, nil
}

func (r *CloudflaredReconciler) podTemplateSpec(cloudflared *cfv1alpha1.Cloudflared, tunnel tunnel) corev1.PodTemplateSpec {
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

	env := []corev1.EnvVar{}
	if tunnel.Token != nil {
		env = append(env, corev1.EnvVar{
			Name:  "TUNNEL_TOKEN",
			Value: *tunnel.Token,
		})
	}

	args := []string{"--hello-world"}
	if tunnel.Id != nil {
		args = []string{"run", *tunnel.Id}
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
		Args: args,
		Env:  env,
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

// SetupWithManager sets up the controller with the Manager.
func (r *CloudflaredReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfv1alpha1.Cloudflared{}).
		Named("cloudflared").
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
