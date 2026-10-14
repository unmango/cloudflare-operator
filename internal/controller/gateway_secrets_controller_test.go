package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

var _ = Describe("Gateway Secret access", func() {
	const (
		className = "secret-access-test"
		gwName    = "secure"
		certsNS   = "secret-access-certs"
		roleName  = "operator-gateway-tls-secret-reader"
	)

	var (
		reconciler GatewaySecretAccessReconciler
		role       *rbacv1.ClusterRole
		class      *gatewayv1.GatewayClass
		gw         *gatewayv1.Gateway
		grant      *gatewayv1beta1.ReferenceGrant
	)

	subject := rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Namespace: "operator-system", Name: "operator"}

	reconcileOnce := func() {
		GinkgoHelper()
		_, err := reconciler.Reconcile(ctx, secretAccessRequest)
		Expect(err).NotTo(HaveOccurred())
	}

	bindingNamespaces := func() []string {
		GinkgoHelper()
		bindings := &rbacv1.RoleBindingList{}
		Expect(k8sClient.List(ctx, bindings, client.MatchingLabels{SecretAccessLabel: roleName})).To(Succeed())

		namespaces := make([]string, 0, len(bindings.Items))
		for _, b := range bindings.Items {
			namespaces = append(namespaces, b.Namespace)
		}
		return namespaces
	}

	certRef := func(namespace string) gatewayv1.SecretObjectReference {
		ref := gatewayv1.SecretObjectReference{Name: "cert"}
		if namespace != "" {
			ref.Namespace = new(gatewayv1.Namespace(namespace))
		}
		return ref
	}

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: certsNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())

		role = &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			}},
		}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())

		class = &gatewayv1.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec:       gatewayv1.GatewayClassSpec{ControllerName: gateway.ControllerName},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())

		gw = &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: testNamespace},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: className,
				Listeners: []gatewayv1.Listener{{
					Name:     "https",
					Port:     443,
					Protocol: gatewayv1.HTTPSProtocolType,
					TLS: &gatewayv1.ListenerTLSConfig{
						CertificateRefs: []gatewayv1.SecretObjectReference{certRef(""), certRef(certsNS)},
					},
				}},
			},
		}

		grant = &gatewayv1beta1.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-certs", Namespace: certsNS},
			Spec: gatewayv1beta1.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gatewayv1.GroupName,
					Kind:      "Gateway",
					Namespace: testNamespace,
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{Kind: "Secret"}},
			},
		}

		reconciler = GatewaySecretAccessReconciler{Client: k8sClient, Role: role, Subject: subject, APIReader: k8sClient}
	})

	AfterEach(func() {
		for _, ns := range []string{testNamespace, certsNS} {
			deleteIfExists(ctx, types.NamespacedName{Name: roleName, Namespace: ns}, &rbacv1.RoleBinding{})
		}
		deleteIfExists(ctx, client.ObjectKeyFromObject(grant), &gatewayv1beta1.ReferenceGrant{})
		deleteIfExists(ctx, client.ObjectKeyFromObject(gw), &gatewayv1.Gateway{})
		deleteIfExists(ctx, client.ObjectKeyFromObject(class), &gatewayv1.GatewayClass{})
		deleteIfExists(ctx, client.ObjectKeyFromObject(role), &rbacv1.ClusterRole{})
	})

	It("should bind the role only in the Gateway's own namespace without a ReferenceGrant", func() {
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		reconcileOnce()

		Expect(bindingNamespaces()).To(ConsistOf(testNamespace))

		binding := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, binding)).To(Succeed())
		Expect(binding.RoleRef).To(Equal(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleName}))
		Expect(binding.Subjects).To(ConsistOf(subject))
		Expect(binding.OwnerReferences).To(ConsistOf(HaveField("UID", role.UID)))
	})

	It("should bind the role where a ReferenceGrant allows certificates", func() {
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		Expect(k8sClient.Create(ctx, grant)).To(Succeed())
		reconcileOnce()

		Expect(bindingNamespaces()).To(ConsistOf(testNamespace, certsNS))
	})

	It("should not bind the role for a grant to another kind", func() {
		grant.Spec.To = []gatewayv1beta1.ReferenceGrantTo{{Kind: "Service"}}
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		Expect(k8sClient.Create(ctx, grant)).To(Succeed())
		reconcileOnce()

		Expect(bindingNamespaces()).To(ConsistOf(testNamespace))
	})

	It("should revoke access once the ReferenceGrant is gone", func() {
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		Expect(k8sClient.Create(ctx, grant)).To(Succeed())
		reconcileOnce()
		deleteIfExists(ctx, client.ObjectKeyFromObject(grant), &gatewayv1beta1.ReferenceGrant{})

		reconcileOnce()

		Expect(bindingNamespaces()).To(ConsistOf(testNamespace))
	})

	It("should take over a binding of the same name it did not label", func() {
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: testNamespace},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "view"},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		reconcileOnce()

		binding := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, binding)).To(Succeed())
		Expect(binding.RoleRef.Name).To(Equal(roleName))
		Expect(binding.Labels).To(HaveKeyWithValue(SecretAccessLabel, roleName))
		Expect(binding.Subjects).To(ConsistOf(subject))
	})

	It("should ignore Gateways of classes it does not own", func() {
		gw.Spec.GatewayClassName = "not-ours"
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		reconcileOnce()

		Expect(bindingNamespaces()).To(BeEmpty())
	})
})
