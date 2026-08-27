package validations

import (
	"fmt"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ValidateRetentionPeriod(httproute *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) error {
	httprouteRetention := httproute.GetAnnotations()[annotations.AnnotationIPAMRetentionPeriodDays]

	if !isHTTPRouteAndGatewayRetentionMatching(httprouteRetention, gateway) {
		gatewayRetention := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMRetentionPeriodDays])
		return fmt.Errorf("HTTPRoute %s annotation %q conflicts with existing Gateway %s/%s (has %q)",
			annotations.AnnotationIPAMRetentionPeriodDays, httprouteRetention, gateway.Namespace, gateway.Name, gatewayRetention)
	}

	return nil
}

func isHTTPRouteAndGatewayRetentionMatching(httprouteRetention string, gateway *gatewayv1.Gateway) bool {
	if gateway == nil {
		return true
	}

	gatewayRetention := ""
	if gateway.Spec.Infrastructure != nil && gateway.Spec.Infrastructure.Annotations != nil {
		gatewayRetention = string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMRetentionPeriodDays])
	}

	// Only reject if both sides specify a value and they differ.
	if httprouteRetention != "" && gatewayRetention != "" {
		return httprouteRetention == gatewayRetention
	}
	return true
}
