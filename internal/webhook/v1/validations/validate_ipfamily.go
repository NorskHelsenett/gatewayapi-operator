package validations

import (
	"fmt"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ValidateIpfamily(httproute *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) error {
	httprouteIpFamily := httproute.GetAnnotations()[annotations.AnnotationIpFamily]

	// If HTTPRoute zone is not set it does not matter
	if httprouteIpFamily == "" {
		return nil
	}

	if !isHttprouteAndGatewayIpFamilyMatching(httprouteIpFamily, gateway) {
		gatewayIpFamily := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIpFamily])
		return fmt.Errorf("HTTPRoute %s annotation %q conflicts with existing Gateway %s/%s (has %q)",
			annotations.AnnotationIpFamily, httprouteIpFamily, gateway.Namespace, gateway.Name, gatewayIpFamily)
	}

	return nil

}

func isHttprouteAndGatewayIpFamilyMatching(httprouteIpFamily string, gateway *gatewayv1.Gateway) bool {
	if gateway == nil {
		return true
	}

	if gateway.Spec.Infrastructure == nil {
		return true
	}

	gatewayIpFamily := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIpFamily])

	// IPFamily on Gateway is not set. Validate HTTPRoute
	if gatewayIpFamily == "" {
		return true
	}

	// IPFamily on HTTPRoute matches IPFamily on Gateway - Validate HTTPRoute
	if gatewayIpFamily == httprouteIpFamily {
		return true
	}

	// Failed our checks - return false
	return false

}
