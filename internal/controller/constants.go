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

	// DefaultClusterIssuer is the default cert-manager cluster issuer
	DefaultClusterIssuer = "internpki"

	// gatewayClassName is the Gateway API gateway class name
	gatewayClassName = "eg"

	// httpsPort is the default HTTPS port
	httpsPort = 443

	// httpport is the default httpport
	httpPort = 80

	// tlsCertSuffix is the suffix for TLS certificate secret names
	tlsCertSuffix = "-tls"

	// DefaultIPAMZone is the default IPAM zone if not specified
	DefaultIPAMZone = "hnet-private"

	inetIPAMZone = "inet"

	defaultInetIpFamily = "dual"

	defaultHnetIpFamily = "ipv4"
)

// ptr returns a pointer to the provided string
func ptr(s string) *string {
	return &s
}
