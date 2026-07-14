package validations_test

import (
	"testing"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"github.com/NorskHelsenett/gatewayapi-operator/internal/webhook/v1/validations"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// helpers

func newHTTProute(ann map[string]string) *gatewayv1.HTTPRoute {
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-route",
			Namespace:   "default",
			Annotations: ann,
		},
	}
}

func newGatewayWithInfraAnn(infraAnn map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "eg",
			Infrastructure: &gatewayv1.GatewayInfrastructure{
				Annotations: infraAnn,
			},
		},
	}
}

func newGatewayWithObjAnn(objAnn map[string]string) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default", Annotations: objAnn},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
}

// ---- ValidateZone ----

func TestValidateZone_NoGateway(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMZone: "inet"})
	if err := validations.ValidateZone(route, nil); err != nil {
		t.Errorf("expected nil error with no gateway, got %v", err)
	}
}

func TestValidateZone_GatewayNoInfrastructure(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMZone: "inet"})
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
	if err := validations.ValidateZone(route, gw); err != nil {
		t.Errorf("expected nil error when gateway has no infrastructure, got %v", err)
	}
}

func TestValidateZone_GatewayNoZoneAnnotation(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMZone: "inet"})
	gw := newGatewayWithInfraAnn(nil)
	if err := validations.ValidateZone(route, gw); err != nil {
		t.Errorf("expected nil error when gateway has no zone annotation, got %v", err)
	}
}

func TestValidateZone_Matching(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMZone: "inet"})
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMZone: "inet",
	})
	if err := validations.ValidateZone(route, gw); err != nil {
		t.Errorf("expected nil error for matching zones, got %v", err)
	}
}

func TestValidateZone_Mismatch(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMZone: "inet"})
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMZone: "hnet-private",
	})
	if err := validations.ValidateZone(route, gw); err == nil {
		t.Error("expected error for zone mismatch, got nil")
	}
}

func TestValidateZone_DefaultZoneMatchesGateway(t *testing.T) {
	// Route has no zone annotation -> defaults to hnet-private
	route := newHTTProute(nil)
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMZone: "hnet-private",
	})
	if err := validations.ValidateZone(route, gw); err != nil {
		t.Errorf("expected nil error when route defaults match gateway zone, got %v", err)
	}
}

func TestValidateZone_DefaultZoneMismatch(t *testing.T) {
	// Route has no zone annotation -> defaults to hnet-private, gateway is inet
	route := newHTTProute(nil)
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMZone: "inet",
	})
	if err := validations.ValidateZone(route, gw); err == nil {
		t.Error("expected error when route default zone conflicts with gateway, got nil")
	}
}

// ---- ValidateIssuer ----

func TestValidateIssuer_NoGateway(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationClusterIssuer: "letsencrypt"})
	if err := validations.ValidateIssuer(route, nil); err != nil {
		t.Errorf("expected nil error with no gateway, got %v", err)
	}
}

func TestValidateIssuer_GatewayNoIssuerAnnotation(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationClusterIssuer: "letsencrypt"})
	gw := newGatewayWithObjAnn(nil)
	if err := validations.ValidateIssuer(route, gw); err != nil {
		t.Errorf("expected nil error when gateway has no issuer annotation, got %v", err)
	}
}

func TestValidateIssuer_Matching(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationClusterIssuer: "internpki"})
	gw := newGatewayWithObjAnn(map[string]string{annotations.AnnotationCertManagerClusterIssuer: "internpki"})
	if err := validations.ValidateIssuer(route, gw); err != nil {
		t.Errorf("expected nil error for matching issuers, got %v", err)
	}
}

func TestValidateIssuer_Mismatch(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationClusterIssuer: "letsencrypt"})
	gw := newGatewayWithObjAnn(map[string]string{annotations.AnnotationCertManagerClusterIssuer: "internpki"})
	if err := validations.ValidateIssuer(route, gw); err == nil {
		t.Error("expected error for issuer mismatch, got nil")
	}
}

func TestValidateIssuer_DefaultIssuerMatchesGateway(t *testing.T) {
	// Route has no issuer annotation -> defaults to internpki
	route := newHTTProute(nil)
	gw := newGatewayWithObjAnn(map[string]string{annotations.AnnotationCertManagerClusterIssuer: "internpki"})
	if err := validations.ValidateIssuer(route, gw); err != nil {
		t.Errorf("expected nil error when route defaults match gateway issuer, got %v", err)
	}
}

// ---- ValidateIPFamily ----

func TestValidateIpfamily_NoAnnotation(t *testing.T) {
	// Route has no ip-family annotation -> defaults to ipv4 (hnet zone); gateway has ipv6 -> conflict
	route := newHTTProute(nil)
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIpFamily: "ipv6",
	})
	if err := validations.ValidateIPFamily(route, gw); err == nil {
		t.Error("expected error when route defaults to ipv4 but gateway has ipv6, got nil")
	}
}

func TestValidateIpfamily_NoGateway(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIpFamily: "dual"})
	if err := validations.ValidateIPFamily(route, nil); err != nil {
		t.Errorf("expected nil error with no gateway, got %v", err)
	}
}

func TestValidateIpfamily_GatewayNoInfrastructure(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIpFamily: "dual"})
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
	if err := validations.ValidateIPFamily(route, gw); err != nil {
		t.Errorf("expected nil error when gateway has no infrastructure, got %v", err)
	}
}

func TestValidateIpfamily_GatewayNoIpFamilyAnnotation(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIpFamily: "dual"})
	gw := newGatewayWithInfraAnn(nil)
	if err := validations.ValidateIPFamily(route, gw); err != nil {
		t.Errorf("expected nil error when gateway has no ip-family annotation, got %v", err)
	}
}

func TestValidateIpfamily_Matching(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIpFamily: "dual"})
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIpFamily: "dual",
	})
	if err := validations.ValidateIPFamily(route, gw); err != nil {
		t.Errorf("expected nil error for matching ip-family, got %v", err)
	}
}

func TestValidateIpfamily_Mismatch(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIpFamily: "ipv4"})
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIpFamily: "dual",
	})
	if err := validations.ValidateIPFamily(route, gw); err == nil {
		t.Error("expected error for ip-family mismatch, got nil")
	}
}

// ---- ValidateAddresses ----

func TestValidateAddresses_NoAnnotationOnRoute(t *testing.T) {
	// Gateway has addresses set; route without the annotation must be rejected
	route := newHTTProute(nil)
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMAddresses: "192.168.1.100",
	})
	if err := validations.ValidateAddresses(route, gw); err == nil {
		t.Error("expected error when gateway has addresses but route does not, got nil")
	}
}

func TestValidateAddresses_NoAnnotationOnRouteNoGatewayAnnotation(t *testing.T) {
	// Neither route nor gateway has addresses set -> no conflict
	route := newHTTProute(nil)
	gw := newGatewayWithInfraAnn(nil)
	if err := validations.ValidateAddresses(route, gw); err != nil {
		t.Errorf("expected nil error when neither route nor gateway has addresses, got %v", err)
	}
}

func TestValidateAddresses_NoGateway(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMAddresses: "192.168.1.100"})
	if err := validations.ValidateAddresses(route, nil); err != nil {
		t.Errorf("expected nil error with no gateway, got %v", err)
	}
}

func TestValidateAddresses_GatewayNoInfrastructure(t *testing.T) {
	// HTTPRoute specifies addresses but the gateway has no infrastructure block -> error
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMAddresses: "192.168.1.100"})
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
	if err := validations.ValidateAddresses(route, gw); err == nil {
		t.Error("expected error when route specifies addresses but gateway has no infrastructure, got nil")
	}
}

func TestValidateAddresses_GatewayNoAddressesAnnotation(t *testing.T) {
	// HTTPRoute specifies addresses but the gateway has no addresses annotation -> error
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMAddresses: "192.168.1.100"})
	gw := newGatewayWithInfraAnn(nil)
	if err := validations.ValidateAddresses(route, gw); err == nil {
		t.Error("expected error when route specifies addresses but gateway has no addresses annotation, got nil")
	}
}

func TestValidateAddresses_Matching(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMAddresses: "192.168.1.100"})
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMAddresses: "192.168.1.100",
	})
	if err := validations.ValidateAddresses(route, gw); err != nil {
		t.Errorf("expected nil error for matching addresses, got %v", err)
	}
}

func TestValidateAddresses_Mismatch(t *testing.T) {
	route := newHTTProute(map[string]string{annotations.AnnotationIPAMAddresses: "192.168.1.100"})
	gw := newGatewayWithInfraAnn(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMAddresses: "10.0.0.5",
	})
	if err := validations.ValidateAddresses(route, gw); err == nil {
		t.Error("expected error for addresses mismatch, got nil")
	}
}
