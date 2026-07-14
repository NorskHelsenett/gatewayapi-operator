package validations

import (
	"fmt"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ValidateAddresses(httproute *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) error {
	httprouteAddresses := httproute.GetAnnotations()[annotations.AnnotationIPAMAddresses]

	if !isHTTPRouteAndGatewayAddressesMatching(httprouteAddresses, gateway) {
		gatewayAddresses := ""
		if gateway.Spec.Infrastructure != nil && gateway.Spec.Infrastructure.Annotations != nil {
			gatewayAddresses = string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMAddresses])
		}
		if gatewayAddresses == "" {
			return fmt.Errorf("HTTPRoute specifies %s %q but Gateway %s/%s was not created with an addresses annotation; all routes on this gateway must omit it",
				annotations.AnnotationIPAMAddresses, httprouteAddresses, gateway.Namespace, gateway.Name)
		}
		if httprouteAddresses == "" {
			return fmt.Errorf("Gateway %s/%s requires %s %q; all routes on this gateway must set it",
				gateway.Namespace, gateway.Name, annotations.AnnotationIPAMAddresses, gatewayAddresses)
		}
		return fmt.Errorf("HTTPRoute %s annotation %q conflicts with existing Gateway %s/%s (has %q)",
			annotations.AnnotationIPAMAddresses, httprouteAddresses, gateway.Namespace, gateway.Name, gatewayAddresses)
	}

	return nil
}

func isHTTPRouteAndGatewayAddressesMatching(httprouteAddresses string, gateway *gatewayv1.Gateway) bool {
	// Gateway doesn't exist yet — any value on the HTTPRoute is fine, it will be set at creation.
	if gateway == nil {
		return true
	}

	// Gateway exists — extract its addresses (empty string if unset).
	gatewayAddresses := ""
	if gateway.Spec.Infrastructure != nil && gateway.Spec.Infrastructure.Annotations != nil {
		gatewayAddresses = string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMAddresses])
	}

	// HTTPRoute and Gateway must agree exactly: both empty, or both the same value.
	return gatewayAddresses == httprouteAddresses
}
