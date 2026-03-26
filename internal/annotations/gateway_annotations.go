package annotations

const (
	AnnotationDnsIgnore = "dns.nhn.no/ignore"

	AnnotationOverrideInfrastructure = "dns.nhn.no/override-infrastructure"

	AnnotationOverrideTTL = "dns.nhn.no/override-ttl"

	// Decides if we should ignore the httproute or not
	// value type: bool
	AnnotationUseHttprouteOperator = "gatewayapi-operator.vitistack.io/enabled"

	// Decides if we should create the listener for HTTPRoute on port 80 without TLS
	AnnotationHttpOnlyListener = "gatewayapi-operator.vitistack.io/http"
	// AnnotationIPAMZone specifies the zone for IP - default when not specified is hnet-private. Other options are hnet and inet
	// Value type: string
	AnnotationIPAMZone = "ipam.vitistack.io/zone"
	// AnnotationClusterIssuer specifies the cert-manager cluster issuer for TLS certificates
	// Value type: string
	AnnotationClusterIssuer = "gatewayapi-operator.vitistack.io/cluster-issuer"

	AnnotationCertManagerClusterIssuer = "cert-manager.io/cluster-issuer"

	AnnotationIpFamily = "ipam.vitistack.io/ip-family"
)
