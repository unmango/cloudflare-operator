package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/go-logr/logr"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/xds"
)

type staticResolver struct {
	addr xds.Address
	err  error
}

func (r staticResolver) Resolve(context.Context) (xds.Address, error) {
	return r.addr, r.err
}

var _ = Describe("Gateway Controller", func() {
	const (
		className = "gateway-test"
		gwName    = "web"
	)

	var (
		reconciler GatewayReconciler
		key        types.NamespacedName
		class      *gatewayv1.GatewayClass
		config     *cfv1alpha1.CloudflareGatewayConfig
		gw         *gatewayv1.Gateway
	)

	envoyKey := types.NamespacedName{Name: gwName + "-envoy", Namespace: testNamespace}

	reconcileOnce := func() reconcile.Result {
		GinkgoHelper()
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		return result
	}

	observed := func() *gatewayv1.Gateway {
		GinkgoHelper()
		obj := &gatewayv1.Gateway{}
		Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())

		return obj
	}

	condition := func(t string) *metav1.Condition {
		GinkgoHelper()
		c := meta.FindStatusCondition(observed().Status.Conditions, t)
		Expect(c).NotTo(BeNil(), "condition %s", t)

		return c
	}

	listenerCondition := func(t gatewayv1.ListenerConditionType) *metav1.Condition {
		GinkgoHelper()
		listeners := observed().Status.Listeners
		Expect(listeners).To(HaveLen(1))
		c := meta.FindStatusCondition(listeners[0].Conditions, string(t))
		Expect(c).NotTo(BeNil(), "listener condition %s", t)

		return c
	}

	deployment := func() *appsv1.Deployment {
		GinkgoHelper()
		obj := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, envoyKey, obj)).To(Succeed())

		return obj
	}

	service := func() *corev1.Service {
		GinkgoHelper()
		obj := &corev1.Service{}
		Expect(k8sClient.Get(ctx, envoyKey, obj)).To(Succeed())

		return obj
	}

	// envtest runs no Deployment controller, so availability is written by hand.
	markAvailable := func() {
		GinkgoHelper()
		deploy := deployment()
		deploy.Status.Replicas = 1
		deploy.Status.ReadyReplicas = 1
		deploy.Status.AvailableReplicas = 1
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())
	}

	createAll := func() {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, config)).To(Succeed())
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		class.Status.Conditions = []metav1.Condition{{
			Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.GatewayClassReasonAccepted),
			LastTransitionTime: metav1.Now(),
		}}
		Expect(k8sClient.Status().Update(ctx, class)).To(Succeed())
		Expect(k8sClient.Create(ctx, gw)).To(Succeed())
	}

	BeforeEach(func() {
		reconciler = GatewayReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			XDS:    staticResolver{addr: xds.Address{Host: "xds.operator.svc", Port: 18000}},
		}
		key = types.NamespacedName{Name: gwName, Namespace: testNamespace}

		config = &cfv1alpha1.CloudflareGatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-test-params", Namespace: testNamespace},
		}
		ns := gatewayv1.Namespace(testNamespace)
		class = &gatewayv1.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: gatewayv1.GatewayClassSpec{
				ControllerName: gateway.ControllerName,
				ParametersRef: &gatewayv1.ParametersReference{
					Group:     gateway.ParametersGroup,
					Kind:      gateway.ParametersKind,
					Name:      config.Name,
					Namespace: &ns,
				},
			},
		}
		gw = &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: testNamespace},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: className,
				Listeners: []gatewayv1.Listener{{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				}},
			},
		}
	})

	AfterEach(func() {
		deleteIfExists(ctx, envoyKey, &appsv1.Deployment{})
		deleteIfExists(ctx, envoyKey, &corev1.Service{})
		deleteIfExists(ctx, key, &cfv1alpha1.CloudflareTunnel{})
		deleteIfExists(ctx, key, &gatewayv1.Gateway{})
		deleteIfExists(ctx, types.NamespacedName{Name: className}, &gatewayv1.GatewayClass{})
		deleteIfExists(ctx, client.ObjectKeyFromObject(config), &cfv1alpha1.CloudflareGatewayConfig{})
	})

	It("should ignore a Gateway of a class it does not own", func() {
		class.Spec.ControllerName = "example.com/other"
		createAll()

		reconcileOnce()

		Expect(k8sClient.Get(ctx, envoyKey, &appsv1.Deployment{})).NotTo(Succeed())
		Expect(condition(string(gatewayv1.GatewayConditionAccepted)).Reason).
			To(Equal(string(gatewayv1.GatewayReasonPending)))
	})

	Context("When the class is not accepted", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, config)).To(Succeed())
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		})

		It("should not accept the Gateway or provision anything", func() {
			reconcileOnce()

			accepted := condition(string(gatewayv1.GatewayConditionAccepted))
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(accepted.Message).To(ContainSubstring(className))
			Expect(k8sClient.Get(ctx, envoyKey, &appsv1.Deployment{})).NotTo(Succeed())
		})
	})

	Context("When the class serves Gateways inside the cluster only", func() {
		BeforeEach(createAll)

		It("should provision Envoy for the Gateway", func() {
			reconcileOnce()

			deploy := deployment()
			Expect(metav1.IsControlledBy(deploy, observed())).To(BeTrue())
			Expect(deploy.Spec.Selector.MatchLabels).To(HaveKeyWithValue(gateway.LabelGateway, string(observed().UID)))
			Expect(deploy.Spec.Template.Labels).To(HaveKeyWithValue(gateway.LabelGatewayName, gwName))

			Expect(deploy.Spec.Template.Spec.Containers).To(HaveLen(1))
			envoy := deploy.Spec.Template.Spec.Containers[0]
			Expect(envoy.Image).To(Equal(gateway.DefaultEnvoyImage))
			Expect(envoy.Args).To(ContainElement(ContainSubstring(`"id":"default/web"`)))
			Expect(envoy.Args).To(ContainElement(ContainSubstring("xds.operator.svc")))
			Expect(envoy.Resources.Limits).To(HaveKey(corev1.ResourceMemory))
			Expect(*envoy.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())

			svc := service()
			Expect(metav1.IsControlledBy(svc, observed())).To(BeTrue())
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(svc.Spec.Selector).To(Equal(deploy.Spec.Selector.MatchLabels))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(svc.Spec.Ports[0].TargetPort.IntValue()).To(Equal(10080))
		})

		It("should accept the Gateway and wait for Envoy to become available", func() {
			reconcileOnce()

			Expect(condition(string(gatewayv1.GatewayConditionAccepted)).Status).To(Equal(metav1.ConditionTrue))
			programmed := condition(string(gatewayv1.GatewayConditionProgrammed))
			Expect(programmed.Status).To(Equal(metav1.ConditionFalse))
			Expect(programmed.Reason).To(Equal(string(gatewayv1.GatewayReasonPending)))

			Expect(listenerCondition(gatewayv1.ListenerConditionAccepted).Status).To(Equal(metav1.ConditionTrue))
			Expect(listenerCondition(gatewayv1.ListenerConditionProgrammed).Status).To(Equal(metav1.ConditionFalse))
			Expect(meta.FindStatusCondition(observed().Status.Conditions, conditionTunnelProgrammed)).To(BeNil())
		})

		It("should program the Gateway once Envoy is available", func() {
			reconcileOnce()
			markAvailable()
			reconcileOnce()

			programmed := condition(string(gatewayv1.GatewayConditionProgrammed))
			Expect(programmed.Status).To(Equal(metav1.ConditionTrue))
			Expect(programmed.ObservedGeneration).To(Equal(observed().Generation))
			Expect(listenerCondition(gatewayv1.ListenerConditionProgrammed).Status).To(Equal(metav1.ConditionTrue))

			addresses := observed().Status.Addresses
			Expect(addresses).To(HaveLen(1))
			Expect(*addresses[0].Type).To(Equal(gatewayv1.IPAddressType))
			Expect(addresses[0].Value).To(Equal(service().Spec.ClusterIP))

			listeners := observed().Status.Listeners
			Expect(listeners[0].AttachedRoutes).To(BeZero())
			Expect(listeners[0].SupportedKinds).To(HaveLen(2))
			Expect(string(listeners[0].SupportedKinds[0].Kind)).To(Equal("HTTPRoute"))
		})

		It("should keep the transition time when nothing changes", func() {
			reconcileOnce()
			markAvailable()
			reconcileOnce()
			before := condition(string(gatewayv1.GatewayConditionProgrammed)).LastTransitionTime

			reconcileOnce()

			Expect(condition(string(gatewayv1.GatewayConditionProgrammed)).LastTransitionTime).To(Equal(before))
		})

		It("should follow listener changes on the Service", func() {
			reconcileOnce()

			current := observed()
			current.Spec.Listeners = append(current.Spec.Listeners, gatewayv1.Listener{
				Name:     "alt",
				Port:     8080,
				Protocol: gatewayv1.HTTPProtocolType,
			})
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			reconcileOnce()

			ports := service().Spec.Ports
			Expect(ports).To(HaveLen(2))
			Expect(ports[1].Port).To(Equal(int32(8080)))
			Expect(ports[1].TargetPort.IntValue()).To(Equal(8080))
		})

		It("should propagate spec.infrastructure labels and annotations", func() {
			current := observed()
			current.Spec.Infrastructure = &gatewayv1.GatewayInfrastructure{
				Labels:      map[gatewayv1.LabelKey]gatewayv1.LabelValue{"team": "web"},
				Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{"example.com/note": "hi"},
			}
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			reconcileOnce()

			Expect(deployment().Spec.Template.Labels).To(HaveKeyWithValue("team", "web"))
			Expect(deployment().Spec.Template.Annotations).To(HaveKeyWithValue("example.com/note", "hi"))
			Expect(service().Labels).To(HaveKeyWithValue("team", "web"))
		})
	})

	Context("When the config asks for a LoadBalancer", func() {
		BeforeEach(func() {
			config.Spec.Envoy = &cfv1alpha1.CloudflareGatewayEnvoy{ServiceType: corev1.ServiceTypeLoadBalancer}
			createAll()
		})

		It("should wait for the load balancer to assign an address", func() {
			reconcileOnce()
			markAvailable()
			reconcileOnce()

			Expect(service().Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
			programmed := condition(string(gatewayv1.GatewayConditionProgrammed))
			Expect(programmed.Status).To(Equal(metav1.ConditionFalse))
			Expect(programmed.Reason).To(Equal(string(gatewayv1.GatewayReasonAddressNotAssigned)))
		})

		It("should report the load balancer's address", func() {
			reconcileOnce()
			markAvailable()
			svc := service()
			svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "203.0.113.10"}}
			Expect(k8sClient.Status().Update(ctx, svc)).To(Succeed())

			reconcileOnce()

			Expect(condition(string(gatewayv1.GatewayConditionProgrammed)).Status).To(Equal(metav1.ConditionTrue))
			Expect(observed().Status.Addresses).To(ConsistOf(gatewayv1.GatewayStatusAddress{
				Type:  new(gatewayv1.IPAddressType),
				Value: "203.0.113.10",
			}))
		})

		It("should keep allocated node ports across updates", func() {
			reconcileOnce()
			before := service().Spec.Ports[0].NodePort
			Expect(before).NotTo(BeZero())

			reconcileOnce()

			Expect(service().Spec.Ports[0].NodePort).To(Equal(before))
		})
	})

	Context("When the config scales Envoy to zero", func() {
		BeforeEach(func() {
			config.Spec.Envoy = &cfv1alpha1.CloudflareGatewayEnvoy{Replicas: new(int32(0))}
			createAll()
		})

		It("should report that there are no resources", func() {
			reconcileOnce()

			Expect(*deployment().Spec.Replicas).To(BeZero())
			Expect(condition(string(gatewayv1.GatewayConditionProgrammed)).Reason).
				To(Equal(string(gatewayv1.GatewayReasonNoResources)))
		})
	})

	Context("When the xDS server address cannot be resolved", func() {
		BeforeEach(func() {
			reconciler.XDS = staticResolver{err: errors.New("no Service")}
			createAll()
		})

		It("should report the Gateway pending and retry", func() {
			result := reconcileOnce()

			Expect(result.RequeueAfter).To(Equal(retryXDSAddress))
			programmed := condition(string(gatewayv1.GatewayConditionProgrammed))
			Expect(programmed.Status).To(Equal(metav1.ConditionFalse))
			Expect(programmed.Message).To(ContainSubstring("no Service"))
			Expect(k8sClient.Get(ctx, envoyKey, &appsv1.Deployment{})).NotTo(Succeed())
		})
	})

	Context("When the Deployment name is taken", func() {
		BeforeEach(func() {
			createAll()
			labels := map[string]string{"app": "someone-else"}
			Expect(k8sClient.Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: envoyKey.Name, Namespace: envoyKey.Namespace},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example"}}},
					},
				},
			})).To(Succeed())
		})

		It("should leave it alone and report the Gateway not programmed", func() {
			reconcileOnce()

			Expect(deployment().Spec.Template.Spec.Containers[0].Name).To(Equal("app"))
			programmed := condition(string(gatewayv1.GatewayConditionProgrammed))
			Expect(programmed.Status).To(Equal(metav1.ConditionFalse))
			Expect(programmed.Message).To(ContainSubstring("not controlled by this Gateway"))
		})
	})

	Context("When the Gateway sets spec.addresses", func() {
		BeforeEach(func() {
			gw.Spec.Addresses = []gatewayv1.GatewaySpecAddress{{Value: "203.0.113.20"}}
			createAll()
		})

		It("should not accept it", func() {
			reconcileOnce()

			accepted := condition(string(gatewayv1.GatewayConditionAccepted))
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(accepted.Reason).To(Equal(string(gatewayv1.GatewayReasonUnsupportedAddress)))
		})
	})

	Context("When no listener is valid", func() {
		BeforeEach(func() {
			gw.Spec.Listeners[0].Protocol = gatewayv1.UDPProtocolType
			createAll()
		})

		It("should not accept the Gateway and say why on the listener", func() {
			reconcileOnce()

			accepted := condition(string(gatewayv1.GatewayConditionAccepted))
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(accepted.Reason).To(Equal(string(gatewayv1.GatewayReasonListenersNotValid)))
			Expect(listenerCondition(gatewayv1.ListenerConditionAccepted).Reason).
				To(Equal(string(gatewayv1.ListenerReasonUnsupportedProtocol)))
			Expect(listenerCondition(gatewayv1.ListenerConditionProgrammed).Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("When the class provisions a tunnel per Gateway", func() {
		BeforeEach(func() {
			config.Spec.Template = &cfv1alpha1.CloudflareGatewayTunnelTemplate{
				ObjectMeta: cfv1alpha1.CloudflareGatewayTunnelTemplateMeta{Labels: map[string]string{"team": "web"}},
				Spec: cfv1alpha1.CloudflareTunnelSpec{
					AccountId: testAccountId,
					Config: &cfv1alpha1.CloudflareTunnelConfig{
						Ingress: []cfv1alpha1.CloudflareTunnelConfigIngress{
							{Hostname: "ssh." + testHostname, Service: "ssh://bastion:22"},
							{Service: testCatchAllService},
						},
					},
				},
			}
			createAll()
		})

		tunnel := func() *cfv1alpha1.CloudflareTunnel {
			GinkgoHelper()
			obj := &cfv1alpha1.CloudflareTunnel{}
			Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())

			return obj
		}

		It("should create a tunnel that sends the Gateway's traffic to Envoy", func() {
			reconcileOnce()

			t := tunnel()
			Expect(metav1.IsControlledBy(t, observed())).To(BeTrue())
			Expect(t.Labels).To(HaveKeyWithValue("team", "web"))
			Expect(t.Spec.AccountId).To(Equal(testAccountId))
			Expect(t.Spec.ConfigSource).To(Equal(cfv1alpha1.CloudflareCloudflareTunnelConfigSource))
			Expect(t.Spec.Config.Ingress).To(HaveLen(2))
			Expect(t.Spec.Config.Ingress[0].Hostname).To(Equal("ssh." + testHostname))
			Expect(t.Spec.Config.Ingress[1].Hostname).To(BeEmpty())
			Expect(t.Spec.Config.Ingress[1].Service).To(Equal("http://web-envoy.default.svc:80"))
		})

		It("should report the tunnel pending until it is created", func() {
			reconcileOnce()

			tunnelProgrammed := condition(conditionTunnelProgrammed)
			Expect(tunnelProgrammed.Status).To(Equal(metav1.ConditionFalse))
			Expect(tunnelProgrammed.Reason).To(Equal(reasonTunnelPending))
		})

		It("should report the tunnel programmed once it has an id", func() {
			reconcileOnce()
			t := tunnel()
			t.Status.Id = new("tunnel-id")
			Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())

			reconcileOnce()

			Expect(condition(conditionTunnelProgrammed).Status).To(Equal(metav1.ConditionTrue))
		})

		It("should report a degraded tunnel", func() {
			reconcileOnce()
			t := tunnel()
			t.Status.Id = new("tunnel-id")
			t.Status.Conditions = []metav1.Condition{{
				Type:               typeDegradedCloudflareTunnel,
				Status:             metav1.ConditionTrue,
				Reason:             reasonInvalidSpec,
				Message:            "bad rules",
				LastTransitionTime: metav1.Now(),
			}}
			Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())

			reconcileOnce()

			tunnelProgrammed := condition(conditionTunnelProgrammed)
			Expect(tunnelProgrammed.Status).To(Equal(metav1.ConditionFalse))
			Expect(tunnelProgrammed.Message).To(ContainSubstring("bad rules"))
		})
	})

	Context("When a tunnel by the Gateway's name already exists", func() {
		BeforeEach(func() {
			config.Spec.Template = &cfv1alpha1.CloudflareGatewayTunnelTemplate{
				Spec: cfv1alpha1.CloudflareTunnelSpec{AccountId: testAccountId},
			}
			createAll()
			Expect(k8sClient.Create(ctx, &cfv1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: testNamespace},
				Spec:       cfv1alpha1.CloudflareTunnelSpec{AccountId: "someone-else"},
			})).To(Succeed())
		})

		It("should leave it alone and report the conflict", func() {
			reconcileOnce()

			t := &cfv1alpha1.CloudflareTunnel{}
			Expect(k8sClient.Get(ctx, key, t)).To(Succeed())
			Expect(t.Spec.AccountId).To(Equal("someone-else"))
			Expect(condition(conditionTunnelProgrammed).Reason).To(Equal(reasonTunnelConflict))
		})
	})

	Context("When the class attaches Gateways to a shared tunnel", func() {
		BeforeEach(func() {
			config.Spec.TunnelRef = &cfv1alpha1.CloudflareGatewayTunnelReference{Name: "shared", Namespace: testNamespace}
			createAll()
		})

		It("should say the shared tunnel is not written yet and where to route it", func() {
			reconcileOnce()

			tunnelProgrammed := condition(conditionTunnelProgrammed)
			Expect(tunnelProgrammed.Status).To(Equal(metav1.ConditionFalse))
			Expect(tunnelProgrammed.Reason).To(Equal(reasonTunnelNotImplemented))
			Expect(tunnelProgrammed.Message).To(ContainSubstring("http://web-envoy.default.svc"))
		})
	})

	Context("When routes name the Gateway", func() {
		var (
			routes RouteReconciler[*gatewayv1.HTTPRoute]
			app    *gatewayv1.HTTPRoute
			stray  *gatewayv1.HTTPRoute
		)

		backendKey := types.NamespacedName{Name: "backend", Namespace: testNamespace}

		httpRoute := func(name string, section *gatewayv1.SectionName, backend string) *gatewayv1.HTTPRoute {
			port := gatewayv1.PortNumber(8080)
			return &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{Name: gwName, SectionName: section}},
					},
					Rules: []gatewayv1.HTTPRouteRule{{
						BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName(backend),
								Port: &port,
							},
						}}},
					}},
				},
			}
		}

		routeStatus := func(r *gatewayv1.HTTPRoute) []gatewayv1.RouteParentStatus {
			GinkgoHelper()
			_, err := routes.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(r)})
			Expect(err).NotTo(HaveOccurred())

			obj := &gatewayv1.HTTPRoute{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(r), obj)).To(Succeed())
			return obj.Status.Parents
		}

		BeforeEach(func() {
			routes = RouteReconciler[*gatewayv1.HTTPRoute]{
				Client: k8sClient,
				New:    func() *gatewayv1.HTTPRoute { return &gatewayv1.HTTPRoute{} },
			}
			createAll()

			Expect(k8sClient.Create(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: backendKey.Name, Namespace: backendKey.Namespace},
				Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}}},
			})).To(Succeed())

			app = httpRoute("app", nil, "backend")
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
			stray = httpRoute("stray", new(gatewayv1.SectionName("nope")), "missing")
			Expect(k8sClient.Create(ctx, stray)).To(Succeed())
		})

		AfterEach(func() {
			deleteIfExists(ctx, client.ObjectKeyFromObject(app), &gatewayv1.HTTPRoute{})
			deleteIfExists(ctx, client.ObjectKeyFromObject(stray), &gatewayv1.HTTPRoute{})
			deleteIfExists(ctx, backendKey, &corev1.Service{})
		})

		It("should count the routes attached to each listener", func() {
			reconcileOnce()

			Expect(observed().Status.Listeners[0].AttachedRoutes).To(Equal(int32(1)))
		})

		It("should accept a route that attaches and resolve its backend", func() {
			parents := routeStatus(app)
			Expect(parents).To(HaveLen(1))
			Expect(string(parents[0].ControllerName)).To(Equal(gateway.ControllerName))

			accepted := meta.FindStatusCondition(parents[0].Conditions, string(gatewayv1.RouteConditionAccepted))
			Expect(accepted).NotTo(BeNil())
			Expect(accepted.Status).To(Equal(metav1.ConditionTrue))
			Expect(accepted.ObservedGeneration).To(Equal(app.Generation))

			resolved := meta.FindStatusCondition(parents[0].Conditions, string(gatewayv1.RouteConditionResolvedRefs))
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should reject a route naming a listener the Gateway lacks", func() {
			parents := routeStatus(stray)
			Expect(parents).To(HaveLen(1))

			accepted := meta.FindStatusCondition(parents[0].Conditions, string(gatewayv1.RouteConditionAccepted))
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(accepted.Reason).To(Equal(string(gatewayv1.RouteReasonNoMatchingParent)))

			resolved := meta.FindStatusCondition(parents[0].Conditions, string(gatewayv1.RouteConditionResolvedRefs))
			Expect(resolved.Reason).To(Equal(string(gatewayv1.RouteReasonBackendNotFound)))
		})

		It("should drop its status once the Gateway is gone", func() {
			Expect(routeStatus(app)).To(HaveLen(1))
			deleteIfExists(ctx, key, &gatewayv1.Gateway{})

			Expect(routeStatus(app)).To(BeEmpty())
		})

		It("should translate the route's backend into a cluster", func() {
			snapshots := xds.NewCache(logr.Discard())
			translator := GatewayXDSReconciler{Client: k8sClient, Cache: snapshots}
			_, err := translator.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			snapshot, err := snapshots.GetSnapshot(gateway.NodeID(testNamespace, gwName))
			Expect(err).NotTo(HaveOccurred())
			Expect(snapshot.GetResources("type.googleapis.com/envoy.config.cluster.v3.Cluster")).
				To(HaveKey("default/backend/8080"))
		})
	})

	Context("When a listener terminates TLS", func() {
		secretKey := types.NamespacedName{Name: "gateway-test-cert", Namespace: testNamespace}

		BeforeEach(func() {
			gw.Spec.Listeners = []gatewayv1.Listener{{
				Name:     "https",
				Port:     443,
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.ListenerTLSConfig{
					CertificateRefs: []gatewayv1.SecretObjectReference{{Name: gatewayv1.ObjectName(secretKey.Name)}},
				},
			}}
			createAll()
		})

		AfterEach(func() {
			deleteIfExists(ctx, secretKey, &corev1.Secret{})
		})

		It("should not resolve a certificate it may not read", func() {
			reconcileOnce()

			resolved := listenerCondition(gatewayv1.ListenerConditionResolvedRefs)
			Expect(resolved.Status).To(Equal(metav1.ConditionFalse))
			Expect(resolved.Reason).To(Equal(string(gatewayv1.ListenerReasonInvalidCertificateRef)))
			Expect(resolved.Message).To(ContainSubstring("rbac.gatewayTLSSecrets"))
		})

		It("should report a Secret that does not exist", func() {
			reconciler.Features.SecretsReadable = true
			reconcileOnce()

			resolved := listenerCondition(gatewayv1.ListenerConditionResolvedRefs)
			Expect(resolved.Reason).To(Equal(string(gatewayv1.ListenerReasonInvalidCertificateRef)))
			Expect(resolved.Message).To(ContainSubstring("does not exist"))
		})

		It("should resolve a valid certificate", func() {
			certPEM, keyPEM := selfSignedCertificate("secure.example.com")
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretKey.Name, Namespace: secretKey.Namespace},
				Type:       corev1.SecretTypeTLS,
				Data:       map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
			})).To(Succeed())

			reconciler.Features.SecretsReadable = true
			reconcileOnce()

			Expect(listenerCondition(gatewayv1.ListenerConditionResolvedRefs).Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("When translating Gateways into xDS snapshots", func() {
		var (
			snapshots  cache.SnapshotCache
			translator GatewayXDSReconciler
		)

		BeforeEach(func() {
			snapshots = xds.NewCache(logr.Discard())
			translator = GatewayXDSReconciler{Client: k8sClient, Cache: snapshots}
			createAll()
		})

		translate := func() {
			GinkgoHelper()
			_, err := translator.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}

		It("should set a snapshot under the Gateway's node id", func() {
			translate()

			snapshot, err := snapshots.GetSnapshot(gateway.NodeID(testNamespace, gwName))
			Expect(err).NotTo(HaveOccurred())
			Expect(snapshot.GetResources("type.googleapis.com/envoy.config.listener.v3.Listener")).To(HaveKey("http-80"))
		})

		It("should clear the snapshot once the Gateway is gone", func() {
			translate()
			deleteIfExists(ctx, key, &gatewayv1.Gateway{})

			translate()

			_, err := snapshots.GetSnapshot(gateway.NodeID(testNamespace, gwName))
			Expect(err).To(HaveOccurred())
		})
	})
})

// selfSignedCertificate returns a PEM certificate and key for host.
func selfSignedCertificate(host string) ([]byte, []byte) {
	GinkgoHelper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).NotTo(HaveOccurred())

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
