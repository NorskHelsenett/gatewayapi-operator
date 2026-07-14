package validations

import (
	"fmt"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ValidateAddresses(httproute *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) error {
	httprouteAddresses := httproute.GetAnnotations()[annotations.AnnotationIPAMAddresses]

	if !isHTTPRouteAndGatewayAddressesMatching(httprouteAddresses, gateway) {
		gatewayAddresses := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMAddresses])
		return fmt.Errorf("HTTPRoute %s annotation %q conflicts with existing Gateway %s/%s (has %q)",
			annotations.AnnotationIPAMAddresses, httprouteAddresses, gateway.Namespace, gateway.Name, gatewayAddresses)
	}

	return nil
}

func isHTTPRouteAndGatewayAddressesMatching(httprouteAddresses string, gateway *gatewayv1.Gateway) bool {
	if gateway == nil {
		return true
	}

	if gateway.Spec.Infrastructure == nil {
		return true
	}

	gatewayAddresses := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMAddresses])

	// Gateway has no addresses annotation. No conflict possible.
	if gatewayAddresses == "" {
		return true
	}

	// Gateway has addresses set; HTTPRoute must specify the same value.
	return gatewayAddresses == httprouteAddresses
}
