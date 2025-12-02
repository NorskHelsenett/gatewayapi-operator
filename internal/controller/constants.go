package controller

const (
	// httprouteFinalizerName is the finalizer added to HTTPRoutes
	httprouteFinalizerName = "gatewayapi-operator.vitistack.io/finalizer"

	// reconcileAnnotationKey marks HTTPRoute resources that have been reconciled
	reconcileAnnotationKey = "gatewayapi-operator.vitistack.io/reconciled"

	// previousGatewayAnnotationKey tracks the previous gateway reference
	// TODO: find a better way to implement this:
	previousGatewayAnnotationKey = "gatewayapi-operator.vitistack.io/previous-gateway"

	// clusterIssuerAnnotation specifies the cert-manager cluster issuer
	clusterIssuerAnnotation = "cert-manager.io/cluster-issuer"

	// defaultClusterIssuer is the default cert-manager cluster issuer
	defaultClusterIssuer = "internpki"

	// gatewayClassName is the Gateway API gateway class name
	gatewayClassName = "eg"

	// httpPort is the default HTTP port
	httpPort = 80

	// httpsPort is the default HTTPS port
	httpsPort = 443

	// tlsCertSuffix is the suffix for TLS certificate secret names
	tlsCertSuffix = "-tls"

	// defaultIPAMZone is the default IPAM zone if not specified
	defaultIPAMZone = "hnet-private"
)

// ptr returns a pointer to the provided string
func ptr(s string) *string {
	return &s
}
