package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	splunkIndex      = "nhn.no/splunkIndex"
	splunkSourcetype = "nhn.no/splunkSourcetype"
)

var testEnvoyPodAnnotations = []string{splunkIndex, splunkSourcetype}

func newRoute(name string, created time.Time, ann map[string]string) *gatewayv1.HTTPRoute {
	ann[annotations.AnnotationUseHttprouteOperator] = "true"
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "team",
			CreationTimestamp: metav1.NewTime(created),
			Annotations:       ann,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{Name: "gw"}},
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(name + ".example.com")},
		},
	}
}

func TestCollectListenersForGatewayResolvesEnvoyPodAnnotationConflict(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	older := newRoute("older", now.Add(-time.Hour), map[string]string{
		splunkIndex:                  "team_a",
		splunkSourcetype:             "kube:container:envoy",
		"example.com/not-configured": "ignored",
	})
	newer := newRoute("newer", now, map[string]string{splunkIndex: "team_b"})
	unset := newRoute("unset", now, map[string]string{})

	recorder := events.NewFakeRecorder(10)
	r := &HTTPRouteReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).WithObjects(newer, older, unset).Build(),
		Scheme:              scheme,
		Recorder:            recorder,
		EnvoyPodAnnotations: testEnvoyPodAnnotations,
	}

	listeners, envoyPodAnnotations, _, _, _, err := r.collectListenersForGateway(context.Background(), "gw", "team")
	if err != nil {
		t.Fatalf("conflict must not block reconcile, got %v", err)
	}
	if len(listeners) != 3 {
		t.Errorf("got %d listeners, want 3", len(listeners))
	}
	if got := envoyPodAnnotations[splunkIndex]; got != "team_a" {
		t.Errorf("splunkIndex = %q, want value from oldest route %q", got, "team_a")
	}
	if got := envoyPodAnnotations[splunkSourcetype]; got != "kube:container:envoy" {
		t.Errorf("splunkSourcetype = %q, want %q", got, "kube:container:envoy")
	}
	if len(envoyPodAnnotations) != 2 {
		t.Errorf("got %v, want only the configured keys", envoyPodAnnotations)
	}

	select {
	case event := <-recorder.Events:
		for _, want := range []string{"Warning", "EnvoyPodAnnotationConflict", `"team_b"`, `"team_a"`, "team/older"} {
			if !strings.Contains(event, want) {
				t.Errorf("event %q does not contain %q", event, want)
			}
		}
	default:
		t.Fatal("expected a Warning event on the newer route")
	}
	if len(recorder.Events) != 0 {
		t.Errorf("expected exactly one event, got %d more", len(recorder.Events))
	}
}

func TestSyncEnvoyPodAnnotations(t *testing.T) {
	existing := map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMZone: "inet",
		splunkIndex:                    "old-index",
		splunkSourcetype:               "old-sourcetype",
	}

	got := syncEnvoyPodAnnotations(testEnvoyPodAnnotations, existing, map[string]string{splunkIndex: "new-index"})
	if got[splunkIndex] != "new-index" {
		t.Errorf("splunkIndex = %q, want %q", got[splunkIndex], "new-index")
	}
	if _, exists := got[splunkSourcetype]; exists {
		t.Error("stale splunkSourcetype annotation was not removed")
	}
	if got[annotations.AnnotationIPAMZone] != "inet" {
		t.Error("IPAM annotation was not preserved")
	}
	if existing[splunkIndex] != "old-index" {
		t.Error("input map was mutated")
	}
}
