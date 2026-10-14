package controller

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/secrets"
)

// SecretAccessLabel marks the RoleBindings that let the operator read TLS
// Secrets in a namespace, with the name of the ClusterRole they bind.
const SecretAccessLabel = "cloudflare.unmango.dev/gateway-tls-secrets"

// secretAccessRequest is the one request the Secret access controllers
// reconcile: the namespaces they act on depend on every Gateway at once.
var secretAccessRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "gateway-tls-secrets"}}

// GatewaySecretWatchReconciler points Access at the namespaces Gateways may
// take listener certificates from: the namespace of each Gateway of a class
// this controller owns that names a Secret in its own namespace, and each
// namespace whose ReferenceGrants let a Gateway name a Secret there. It runs on
// every replica, since every replica serves xDS and reads certificates.
type GatewaySecretWatchReconciler struct {
	client.Client
	Access *secrets.Access
}

func (r *GatewaySecretWatchReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	namespaces, err := certificateNamespaces(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.Access.SetNamespaces(ctx, namespaces)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewaySecretWatchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return watchCertificateNamespaces(ctrl.NewControllerManagedBy(mgr)).
		WithOptions(controller.Options{NeedLeaderElection: new(false)}).
		Named("gateway-secret-watch").
		Complete(r)
}

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

// GatewaySecretAccessReconciler grants the operator read access to Secrets in
// the namespaces GatewaySecretWatchReconciler watches, and only those, by
// binding Role in each of them to Subject. It removes the bindings of
// namespaces no Gateway takes certificates from any more.
//
// The operator holds no access to Secrets of its own. It may create these
// RoleBindings because it is allowed to bind Role, and nothing else.
type GatewaySecretAccessReconciler struct {
	client.Client

	// Role is the ClusterRole granting read access to Secrets. Each RoleBinding
	// is owned by it, so they go when it does.
	Role *rbacv1.ClusterRole

	// Subject is the identity the operator runs as.
	Subject rbacv1.Subject

	// APIReader reads RoleBindings uncached. The manager caches only the
	// labelled ones, and a binding of the same name without the label would
	// otherwise look absent and never be taken over.
	APIReader client.Reader
}

func (r *GatewaySecretAccessReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	namespaces, err := certificateNamespaces(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	bindings := &rbacv1.RoleBindingList{}
	if err := r.List(ctx, bindings, client.MatchingLabels{SecretAccessLabel: r.Role.Name}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range bindings.Items {
		binding := &bindings.Items[i]
		if namespaces.Has(binding.Namespace) && binding.Name == r.Role.Name {
			continue
		}
		log.Info("Revoking access to Secrets", "namespace", binding.Namespace)
		if err := r.Delete(ctx, binding); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}

	for ns := range namespaces {
		if err := r.bind(ctx, ns); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// bind creates or updates the RoleBinding granting access to Secrets in ns.
func (r *GatewaySecretAccessReconciler) bind(ctx context.Context, ns string) error {
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: r.Role.Name, Namespace: ns},
	}
	roleRef := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: r.Role.Name}

	err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(binding), binding)
	if err == nil && binding.RoleRef != roleRef {
		// roleRef is immutable, so a binding to another role is replaced.
		if err := r.Delete(ctx, binding); client.IgnoreNotFound(err) != nil {
			return err
		}
		binding = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: r.Role.Name, Namespace: ns}}
		err = apierrors.NewNotFound(rbacv1.Resource("rolebindings"), r.Role.Name)
	}
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	create := apierrors.IsNotFound(err)

	if binding.Labels == nil {
		binding.Labels = map[string]string{}
	}
	binding.Labels[SecretAccessLabel] = r.Role.Name
	binding.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: rbacv1.SchemeGroupVersion.String(),
		Kind:       "ClusterRole",
		Name:       r.Role.Name,
		UID:        r.Role.UID,
	}}
	binding.RoleRef = roleRef
	binding.Subjects = []rbacv1.Subject{r.Subject}

	if create {
		logf.FromContext(ctx).Info("Granting access to Secrets", "namespace", ns)
		return r.Create(ctx, binding)
	}

	return r.Update(ctx, binding)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewaySecretAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return watchCertificateNamespaces(ctrl.NewControllerManagedBy(mgr)).
		Watches(&rbacv1.RoleBinding{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []reconcile.Request {
				if obj.GetLabels()[SecretAccessLabel] != r.Role.Name {
					return nil
				}
				return []reconcile.Request{secretAccessRequest}
			},
		)).
		Named("gateway-secret-access").
		Complete(r)
}

// watchCertificateNamespaces watches what certificateNamespaces reads.
func watchCertificateNamespaces(b *ctrl.Builder) *ctrl.Builder {
	enqueue := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{secretAccessRequest}
	})

	return b.
		Watches(&gatewayv1.Gateway{}, enqueue).
		Watches(&gatewayv1.GatewayClass{}, enqueue).
		Watches(&gatewayv1beta1.ReferenceGrant{}, enqueue)
}

// certificateNamespaces lists the namespaces the Gateways of every class this
// controller owns may take listener certificates from.
func certificateNamespaces(ctx context.Context, reader client.Reader) (sets.Set[string], error) {
	namespaces := sets.New[string]()

	grants := &gatewayv1beta1.ReferenceGrantList{}
	if err := reader.List(ctx, grants); err != nil && !meta.IsNoMatchError(err) {
		return nil, err
	}
	refs := &gateway.References{Grants: grants.Items}

	// Listed here rather than through ourGateways, which swallows errors: a
	// failed list must not read as no Gateways and revoke every binding.
	classes := &gatewayv1.GatewayClassList{}
	if err := reader.List(ctx, classes); err != nil {
		return nil, err
	}
	ours := sets.New[gatewayv1.ObjectName]()
	for _, class := range classes.Items {
		if string(class.Spec.ControllerName) == gateway.ControllerName {
			ours.Insert(gatewayv1.ObjectName(class.Name))
		}
	}

	gateways := &gatewayv1.GatewayList{}
	if err := reader.List(ctx, gateways); err != nil {
		return nil, err
	}
	for i := range gateways.Items {
		gw := &gateways.Items[i]
		if !ours.Has(gw.Spec.GatewayClassName) || !gw.DeletionTimestamp.IsZero() {
			continue
		}
		namespaces = namespaces.Union(gateway.CertificateNamespaces(gw, refs))
	}

	return namespaces, nil
}
