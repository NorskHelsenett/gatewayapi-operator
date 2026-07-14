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

	// Legacy gateway class
	legacyGatewayClassName = "eg"

	// Healthnet gateway class
	hnetGatewayClassName = "eg-hnet"

	// Internet gateway class
	inetGatewayClassName = "eg-inet"

	// httpsPort is the default HTTPS port
	httpsPort = 443

	// httpport is the default httpport
	httpPort = 80

	// tlsCertSuffix is the suffix for TLS certificate secret names
	tlsCertSuffix = "-tls"

	// DefaultIPAMZone is the default IPAM zone if not specified
	DefaultIPAMZone = "hnet-private"

	InetIPAMZone = "inet"

	HnetIPAMZone = "hnet"

	DefaultInetIpFamily = "dual"

	DefaultHnetIpFamily = "ipv4"

	// Default Gateway names for each IPAM zone used by auto-created ListenerSet parent Gateways.
	defaultGatewayHnet        = "default-gateway-hnet"
	defaultGatewayHnetPrivate = "default-gateway-hnet-private"
	defaultGatewayInet        = "default-gateway-inet"

	// placeholderListenerName is the name of the stub listener placed on a Gateway that exists
	// solely to satisfy the Gateway API MinItems=1 requirement while real listeners live in
	// attached ListenerSets.
	placeholderListenerName = "placeholder"

	// enableClientTrafficPolicyPQCEnvVar toggles ClientTrafficPolicy creation.
	enableClientTrafficPolicyPQCEnvVar = "ENABLE_CLIENTTRAFFICPOLICY_PQC"

	// clientTrafficPolicyNameSuffix is appended to gateway names.
	clientTrafficPolicyNameSuffix = "-gwo-pqc"

	// gatewayNamespace is the namespace where all operator-managed Gateways live.
	gatewayNamespace = "gateways"
)

// ptr returns a pointer to the provided string
func ptr(s string) *string {
	return &s
}

// defaultGatewayNameForZone returns the default Gateway name that auto-created
// ListenerSets should reference as their parent, keyed by IPAM zone.
func defaultGatewayNameForZone(zone string) string {
	switch zone {
	case InetIPAMZone:
		return defaultGatewayInet
	case HnetIPAMZone:
		return defaultGatewayHnet
	default: // hnet-private and anything unrecognised
		return defaultGatewayHnetPrivate
	}
}
