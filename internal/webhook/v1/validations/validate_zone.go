package validations

import (
	"fmt"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ValidateZone(httproute *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) error {
	httprouteZone := httproute.GetAnnotations()[annotations.AnnotationIPAMZone]

	// If HTTPRoute zone is not set, lets default it to hnet-private like the IPAM-operator does
	if httprouteZone == "" {
		httprouteZone = "hnet-private"
	}

	if !isHttprouteAndGatewayZoneMatching(httprouteZone, gateway) {
		gatewayZone := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone])
		return fmt.Errorf("HTTPRoute %s annotation %q conflicts with existing Gateway %s/%s (has %q)",
			annotations.AnnotationIPAMZone, httprouteZone, gateway.Namespace, gateway.Name, gatewayZone)
	}

	return nil

}

func isHttprouteAndGatewayZoneMatching(httprouteZone string, gateway *gatewayv1.Gateway) bool {
	if gateway == nil {
		return true
	}

	if gateway.Spec.Infrastructure == nil {
		return true
	}

	gatewayZone := string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone])

	// Zone on Gateway is not set. Validate HTTPRoute
	if gatewayZone == "" {
		return true
	}

	// Zone on HTTPRoute matches Zone on Gateway - Validate HTTPRoute
	if gatewayZone == httprouteZone {
		return true
	}

	// Failed our checks - return false
	return false

}
