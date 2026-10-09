package validations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/webhook/v1/validations"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	splunkIndex      = "nhn.no/splunkIndex"
	splunkSourcetype = "nhn.no/splunkSourcetype"
)

var envoyPodAnnotations = []string{splunkIndex, splunkSourcetype}

func routeOnGateway(name string, created time.Time, ann map[string]string) gatewayv1.HTTPRoute {
	return gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "team",
			CreationTimestamp: metav1.NewTime(created),
			Annotations:       ann,
		},
	}
}

func TestValidateEnvoyPodAnnotations_NoOtherRoutes(t *testing.T) {
	route := newHTTProute(map[string]string{splunkIndex: "team_a"})
	if err := validations.ValidateEnvoyPodAnnotations(envoyPodAnnotations, route, nil); err != nil {
		t.Errorf("expected nil error without other routes, got %v", err)
	}
}

func TestValidateEnvoyPodAnnotations_SameValue(t *testing.T) {
	route := newHTTProute(map[string]string{splunkSourcetype: "kube:container:envoy"})
	others := []gatewayv1.HTTPRoute{
		routeOnGateway("existing", time.Now(), map[string]string{splunkSourcetype: "kube:container:envoy"}),
	}
	if err := validations.ValidateEnvoyPodAnnotations(envoyPodAnnotations, route, others); err != nil {
		t.Errorf("expected nil error for identical value, got %v", err)
	}
}

func TestValidateEnvoyPodAnnotations_UnsetOnEitherSide(t *testing.T) {
	others := []gatewayv1.HTTPRoute{routeOnGateway("existing", time.Now(), map[string]string{})}
	route := newHTTProute(map[string]string{splunkIndex: "team_a"})
	if err := validations.ValidateEnvoyPodAnnotations(envoyPodAnnotations, route, others); err != nil {
		t.Errorf("expected nil error when other route has no value, got %v", err)
	}

	others = []gatewayv1.HTTPRoute{routeOnGateway("existing", time.Now(), map[string]string{splunkIndex: "team_a"})}
	if err := validations.ValidateEnvoyPodAnnotations(envoyPodAnnotations, newHTTProute(nil), others); err != nil {
		t.Errorf("expected nil error when new route has no value, got %v", err)
	}
}

func TestValidateEnvoyPodAnnotations_ConflictNamesExistingRoute(t *testing.T) {
	now := time.Now()
	route := newHTTProute(map[string]string{splunkIndex: "team_b"})
	others := []gatewayv1.HTTPRoute{
		routeOnGateway("newer", now, map[string]string{splunkIndex: "team_b"}),
		routeOnGateway("oldest", now.Add(-time.Hour), map[string]string{splunkIndex: "team_a"}),
	}
	err := validations.ValidateEnvoyPodAnnotations(envoyPodAnnotations, route, others)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	for _, want := range []string{splunkIndex, `"team_b"`, `"team_a"`, "team/oldest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestValidateEnvoyPodAnnotations_IgnoresKeysNotConfigured(t *testing.T) {
	route := newHTTProute(map[string]string{splunkIndex: "team_b"})
	others := []gatewayv1.HTTPRoute{routeOnGateway("existing", time.Now(), map[string]string{splunkIndex: "team_a"})}
	if err := validations.ValidateEnvoyPodAnnotations([]string{splunkSourcetype}, route, others); err != nil {
		t.Errorf("expected nil error for a key that is not configured, got %v", err)
	}
}
