package controller

// Annotation keys used by the HTTPRoute operator
const (
	// Decides if we should ignore the httproute or not
	// value type: bool
	AnnotationUseHttprouteOperator = "gatewayapi-operator.vitistack.io/enabled"
	// AnnotationIPAMZone specifies the zone
	// Value type: string
	AnnotationIPAMZone = "ipam.vitistack.io/zone"
	// AnnotationClusterIssuer specifies the cert-manager cluster issuer for TLS certificates
	// Value type: string
	AnnotationClusterIssuer = "gatewayapi-operator.vitistack.io/cluster-issuer"
)
