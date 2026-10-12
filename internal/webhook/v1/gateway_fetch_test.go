package v1

import (
	"context"
	"testing"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestListOtherRoutesOnGatewayIgnoresNonGatewayParentRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	route := func(name string, ref gatewayv1.ParentReference) *gatewayv1.HTTPRoute {
		return &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   "team",
				Annotations: map[string]string{annotations.AnnotationUseHttprouteOperator: "true"},
			},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{ref}},
			},
		}
	}
	core, service := gatewayv1.Group(""), gatewayv1.Kind("Service")
	sameGateway := route("same-gateway", gatewayv1.ParentReference{Name: "gw"})
	meshRoute := route("mesh", gatewayv1.ParentReference{Group: &core, Kind: &service, Name: "gw"})

	v := &HTTPRouteCustomValidator{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sameGateway, meshRoute).Build()}
	routes, err := v.ListOtherRoutesOnGateway(context.Background(), route("new", gatewayv1.ParentReference{Name: "gw"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Name != "same-gateway" {
		t.Errorf("got %d routes, want only same-gateway (the Service parent must not count)", len(routes))
	}
}
