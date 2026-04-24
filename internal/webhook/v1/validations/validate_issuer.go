package validations

import (
	"fmt"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"github.com/NorskHelsenett/gatewayapi-operator/internal/controller"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ValidateIssuer(httproute *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) error {
	httprouteIssuer := httproute.GetAnnotations()[annotations.AnnotationClusterIssuer]

	// If HTTPRoute issuer is not set, default to "internpki" and continue validation
	if httprouteIssuer == "" {
		httprouteIssuer = controller.DefaultClusterIssuer
	}

	if !isHTTPRouteAndGatewayIssuerMatching(httprouteIssuer, gateway) {
		gatewayIssuer := gateway.ObjectMeta.Annotations[annotations.AnnotationCertManagerClusterIssuer]
		return fmt.Errorf("HTTPRoute %s annotation %q conflicts with existing Gateway %s/%s (has %q)",
			annotations.AnnotationClusterIssuer, httprouteIssuer, gateway.Namespace, gateway.Name, gatewayIssuer)
	}

	return nil

}

func isHTTPRouteAndGatewayIssuerMatching(httprouteIssuer string, gateway *gatewayv1.Gateway) bool {
	if gateway == nil {
		return true
	}

	gatewayIssuer := string(gateway.ObjectMeta.Annotations[annotations.AnnotationCertManagerClusterIssuer])

	// Issuer on Gateway is not set. Validate HTTPRoute
	if gatewayIssuer == "" {
		return true
	}

	// Issuer on HTTPRoute matches Issuer on Gateway - Validate HTTPRoute
	if gatewayIssuer == httprouteIssuer {
		return true
	}

	// Failed our checks - return false
	return false

}
