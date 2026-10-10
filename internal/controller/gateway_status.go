package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// gatewayStatus collects what one reconcile learns about a Gateway, and writes
// it onto the status in one patch.
type gatewayStatus struct {
	listeners      []gateway.Listener
	attachedRoutes []int32
	addresses      []gatewayv1.GatewayStatusAddress

	acceptedCondition   metav1.Condition
	programmedCondition metav1.Condition

	// tunnel is the tunnel condition, or nil when the class reaches no tunnel.
	tunnel *metav1.Condition
}

func newGatewayStatus(m *gateway.Model) *gatewayStatus {
	return &gatewayStatus{listeners: m.Listeners, attachedRoutes: m.AttachedRoutes}
}

func (s *gatewayStatus) accepted(status metav1.ConditionStatus, reason gatewayv1.GatewayConditionReason, message string) {
	s.acceptedCondition = metav1.Condition{
		Type:    string(gatewayv1.GatewayConditionAccepted),
		Status:  status,
		Reason:  string(reason),
		Message: message,
	}
}

func (s *gatewayStatus) programmed(status metav1.ConditionStatus, reason gatewayv1.GatewayConditionReason, message string) {
	s.programmedCondition = metav1.Condition{
		Type:    string(gatewayv1.GatewayConditionProgrammed),
		Status:  status,
		Reason:  string(reason),
		Message: message,
	}
}

func (s *gatewayStatus) validListeners() int {
	n := 0
	for _, l := range s.listeners {
		if l.Valid {
			n++
		}
	}

	return n
}

// apply writes the collected status onto gw. Conditions keep their transition
// time when their status does not change, and listener statuses are matched to
// the previous ones by name for the same reason.
func (s *gatewayStatus) apply(gw *gatewayv1.Gateway) {
	generation := gw.Generation

	set := func(conditions *[]metav1.Condition, c metav1.Condition) {
		c.ObservedGeneration = generation
		_ = meta.SetStatusCondition(conditions, c)
	}

	set(&gw.Status.Conditions, s.acceptedCondition)
	set(&gw.Status.Conditions, s.programmedCondition)
	if s.tunnel != nil {
		set(&gw.Status.Conditions, *s.tunnel)
	} else {
		_ = meta.RemoveStatusCondition(&gw.Status.Conditions, conditionTunnelProgrammed)
	}

	gw.Status.Addresses = s.addresses

	gatewayProgrammed := s.programmedCondition.Status == metav1.ConditionTrue

	previous := map[gatewayv1.SectionName][]metav1.Condition{}
	for _, l := range gw.Status.Listeners {
		previous[l.Name] = l.Conditions
	}

	statuses := make([]gatewayv1.ListenerStatus, 0, len(s.listeners))
	for i, l := range s.listeners {
		desired := append([]metav1.Condition{}, l.Conditions...)
		switch {
		case !l.Valid:
			desired = append(desired, metav1.Condition{
				Type:    string(gatewayv1.ListenerConditionProgrammed),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayv1.ListenerReasonInvalid),
				Message: "Listener is not valid",
			})
		case gatewayProgrammed:
			desired = append(desired, metav1.Condition{
				Type:    string(gatewayv1.ListenerConditionProgrammed),
				Status:  metav1.ConditionTrue,
				Reason:  string(gatewayv1.ListenerReasonProgrammed),
				Message: "Listener is programmed",
			})
		default:
			desired = append(desired, metav1.Condition{
				Type:    string(gatewayv1.ListenerConditionProgrammed),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayv1.ListenerReasonPending),
				Message: "Waiting for the Gateway to be programmed",
			})
		}

		conditions := previous[l.Name]
		types := map[string]bool{}
		for _, c := range desired {
			types[c.Type] = true
			set(&conditions, c)
		}
		conditions = removeConditionsExcept(conditions, types)

		statuses = append(statuses, gatewayv1.ListenerStatus{
			Name:           l.Name,
			SupportedKinds: l.SupportedKinds,
			AttachedRoutes: s.attachedRoutes[i],
			Conditions:     conditions,
		})
	}
	gw.Status.Listeners = statuses
}

func removeConditionsExcept(conditions []metav1.Condition, keep map[string]bool) []metav1.Condition {
	out := conditions[:0]
	for _, c := range conditions {
		if keep[c.Type] {
			out = append(out, c)
		}
	}

	return out
}
