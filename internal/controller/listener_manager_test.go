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

func TestCollectListenersForGatewayUsesRouteNamespaceForOmittedParentRefNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	onGateway := newRoute("on-gateway", time.Now(), map[string]string{splunkIndex: "team_a"})
	// Same Gateway name without namespace, so it refers to other/gw and not team/gw.
	otherNamespace := newRoute("other-namespace", time.Now().Add(-time.Hour), map[string]string{splunkIndex: "other_a"})
	otherNamespace.Namespace = "other"

	r := &HTTPRouteReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).WithObjects(onGateway, otherNamespace).Build(),
		Scheme:              scheme,
		Recorder:            events.NewFakeRecorder(10),
		EnvoyPodAnnotations: testEnvoyPodAnnotations,
	}

	listeners, envoyPodAnnotations, _, _, _, err := r.collectListenersForGateway(context.Background(), "gw", "team")
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 1 || string(*listeners[0].Hostname) != "on-gateway.example.com" {
		t.Errorf("got listeners %v, want only on-gateway.example.com", listeners)
	}
	if got := envoyPodAnnotations[splunkIndex]; got != "team_a" {
		t.Errorf("splunkIndex = %q, want %q from the route in the Gateway's namespace", got, "team_a")
	}
}

func TestCollectListenersForGatewaySkipsTooLongValue(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	tooLong := newRoute("too-long", time.Now().Add(-time.Hour), map[string]string{
		splunkIndex: strings.Repeat("x", annotations.MaxAnnotationValueLength+1),
	})
	valid := newRoute("valid", time.Now(), map[string]string{splunkIndex: "team_a"})

	recorder := events.NewFakeRecorder(10)
	r := &HTTPRouteReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).WithObjects(tooLong, valid).Build(),
		Scheme:              scheme,
		Recorder:            recorder,
		EnvoyPodAnnotations: testEnvoyPodAnnotations,
	}

	_, envoyPodAnnotations, _, _, _, err := r.collectListenersForGateway(context.Background(), "gw", "team")
	if err != nil {
		t.Fatal(err)
	}
	if got := envoyPodAnnotations[splunkIndex]; got != "team_a" {
		t.Errorf("splunkIndex = %q, want the valid value %q", got, "team_a")
	}
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "Warning EnvoyPodAnnotationTooLong") {
			t.Errorf("unexpected event %q", event)
		}
	default:
		t.Fatal("expected a Warning event for the too long value")
	}
}

func TestSyncEnvoyPodAnnotationsRemovesDeconfiguredKeys(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			annotations.AnnotationManagedEnvoyPodAnnotations: "nhn.no/splunkIndex,nhn.no/splunkSourcetype,example.com/removed",
		}},
		Spec: gatewayv1.GatewaySpec{Infrastructure: &gatewayv1.GatewayInfrastructure{
			Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
				annotations.AnnotationIPAMZone: "inet",
				splunkIndex:                    "old-index",
				splunkSourcetype:               "old-sourcetype",
				"example.com/removed":          "old",
			},
		}},
	}

	syncEnvoyPodAnnotations(gw, []string{splunkIndex}, map[string]string{splunkIndex: "new-index"})

	got := gw.Spec.Infrastructure.Annotations
	if got[splunkIndex] != "new-index" {
		t.Errorf("splunkIndex = %q, want %q", got[splunkIndex], "new-index")
	}
	for _, key := range []gatewayv1.AnnotationKey{splunkSourcetype, "example.com/removed"} {
		if _, exists := got[key]; exists {
			t.Errorf("deconfigured key %s was not removed", key)
		}
	}
	if got[annotations.AnnotationIPAMZone] != "inet" {
		t.Error("IPAM annotation was not preserved")
	}
	if managed := gw.Annotations[annotations.AnnotationManagedEnvoyPodAnnotations]; managed != splunkIndex {
		t.Errorf("managed keys = %q, want %q", managed, splunkIndex)
	}
}

func TestSyncEnvoyPodAnnotationsNeverRemovesIPAMKeys(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			annotations.AnnotationManagedEnvoyPodAnnotations: "ipam.vitistack.io/zone",
		}},
		Spec: gatewayv1.GatewaySpec{Infrastructure: &gatewayv1.GatewayInfrastructure{
			Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{annotations.AnnotationIPAMZone: "inet"},
		}},
	}

	syncEnvoyPodAnnotations(gw, nil, nil)

	if gw.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone] != "inet" {
		t.Error("IPAM annotation listed as managed was removed")
	}
	if _, exists := gw.Annotations[annotations.AnnotationManagedEnvoyPodAnnotations]; exists {
		t.Error("managed keys annotation was not removed")
	}
}

func TestSyncEnvoyPodAnnotationsLeavesUnusedGatewayUntouched(t *testing.T) {
	gw := &gatewayv1.Gateway{}

	syncEnvoyPodAnnotations(gw, testEnvoyPodAnnotations, map[string]string{})

	if gw.Spec.Infrastructure != nil || gw.Annotations != nil {
		t.Errorf("gateway without Envoy pod annotations was modified: %+v", gw)
	}
}

func TestIsGatewayParentRef(t *testing.T) {
	group := func(s string) *gatewayv1.Group { g := gatewayv1.Group(s); return &g }
	kind := func(s string) *gatewayv1.Kind { k := gatewayv1.Kind(s); return &k }
	tests := []struct {
		name string
		ref  gatewayv1.ParentReference
		want bool
	}{
		{name: "omitted group and kind", ref: gatewayv1.ParentReference{Name: "gw"}, want: true},
		{name: "explicit Gateway", ref: gatewayv1.ParentReference{Group: group(gatewayv1.GroupName), Kind: kind("Gateway"), Name: "gw"}, want: true},
		{name: "Service parent", ref: gatewayv1.ParentReference{Group: group(""), Kind: kind("Service"), Name: "gw"}, want: false},
		{name: "other kind in Gateway API group", ref: gatewayv1.ParentReference{Kind: kind("ListenerSet"), Name: "gw"}, want: false},
		{name: "Gateway kind in custom group", ref: gatewayv1.ParentReference{Group: group("example.com"), Kind: kind("Gateway"), Name: "gw"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGatewayParentRef(tt.ref); got != tt.want {
				t.Errorf("IsGatewayParentRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCollectListenersForGatewayIgnoresNonGatewayParentRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	onGateway := newRoute("on-gateway", time.Now(), map[string]string{splunkIndex: "team_a"})
	// Older route whose Service parent has the same name and namespace as the Gateway.
	meshRoute := newRoute("mesh", time.Now().Add(-time.Hour), map[string]string{splunkIndex: "mesh_a"})
	core, service := gatewayv1.Group(""), gatewayv1.Kind("Service")
	meshRoute.Spec.ParentRefs = []gatewayv1.ParentReference{{Group: &core, Kind: &service, Name: "gw"}}

	r := &HTTPRouteReconciler{
		Client:              fake.NewClientBuilder().WithScheme(scheme).WithObjects(onGateway, meshRoute).Build(),
		Scheme:              scheme,
		Recorder:            events.NewFakeRecorder(10),
		EnvoyPodAnnotations: testEnvoyPodAnnotations,
	}

	listeners, envoyPodAnnotations, _, _, _, err := r.collectListenersForGateway(context.Background(), "gw", "team")
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 1 || string(*listeners[0].Hostname) != "on-gateway.example.com" {
		t.Errorf("got listeners %v, want only on-gateway.example.com", listeners)
	}
	if got := envoyPodAnnotations[splunkIndex]; got != "team_a" {
		t.Errorf("splunkIndex = %q, want %q from the route that references the Gateway", got, "team_a")
	}
}

func TestResolveEnvoyPodAnnotationsCountsCharactersNotBytes(t *testing.T) {
	value := strings.Repeat("ø", 2100) // 4200 bytes, but only 2100 characters
	route := newRoute("route", time.Now(), map[string]string{splunkIndex: value})

	values, _, tooLong := resolveEnvoyPodAnnotations(testEnvoyPodAnnotations, []*gatewayv1.HTTPRoute{route})
	if len(tooLong) != 0 || values[splunkIndex] != value {
		t.Errorf("value within the 4096-character limit was rejected (tooLong=%d)", len(tooLong))
	}
}
