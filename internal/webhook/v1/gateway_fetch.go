package v1

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"github.com/NorskHelsenett/gatewayapi-operator/internal/controller"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (v *HTTPRouteCustomValidator) GetReferredGateway(ctx context.Context, httproute *gatewayv1.HTTPRoute) (*gatewayv1.Gateway, error) {
	log := logf.FromContext(ctx)

	for _, parentRef := range httproute.Spec.ParentRefs {
		if !controller.IsGatewayParentRef(parentRef) {
			continue
		}

		// Namespace defaults to the HTTPRoute's namespace if not specified
		namespace := httproute.Namespace
		if parentRef.Namespace != nil {
			namespace = string(*parentRef.Namespace)
		}

		gateway := &gatewayv1.Gateway{}
		err := v.Get(ctx, types.NamespacedName{
			Name:      string(parentRef.Name),
			Namespace: namespace,
		}, gateway)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Gateway not found for HTTPRoute", "gateway", parentRef.Name, "namespace", namespace)
				continue
			}
			log.Error(err, "Failed to fetch Gateway for HTTPRoute", "gateway", parentRef.Name, "namespace", namespace)
			return nil, err
		}

		return gateway, nil
	}

	return nil, nil
}

// ListOtherRoutesOnGateway returns the operator-managed HTTPRoutes, except httproute itself, that share its Gateway.
func (v *HTTPRouteCustomValidator) ListOtherRoutesOnGateway(ctx context.Context, httproute *gatewayv1.HTTPRoute) ([]gatewayv1.HTTPRoute, error) {
	// The controller uses the first parentRef as the Gateway, but only if it actually refers to one.
	if len(httproute.Spec.ParentRefs) == 0 || !controller.IsGatewayParentRef(httproute.Spec.ParentRefs[0]) {
		return nil, nil
	}
	gatewayName := string(httproute.Spec.ParentRefs[0].Name)
	gatewayNamespace := httproute.Namespace
	if httproute.Spec.ParentRefs[0].Namespace != nil {
		gatewayNamespace = string(*httproute.Spec.ParentRefs[0].Namespace)
	}

	var routeList gatewayv1.HTTPRouteList
	if err := v.List(ctx, &routeList); err != nil {
		return nil, err
	}

	var routes []gatewayv1.HTTPRoute
	for _, route := range routeList.Items {
		if route.Namespace == httproute.Namespace && route.Name == httproute.Name {
			continue
		}
		if !route.DeletionTimestamp.IsZero() || route.Annotations[annotations.AnnotationUseHttprouteOperator] != "true" {
			continue
		}
		for _, parentRef := range route.Spec.ParentRefs {
			if !controller.IsGatewayParentRef(parentRef) {
				continue
			}
			refNamespace := route.Namespace
			if parentRef.Namespace != nil {
				refNamespace = string(*parentRef.Namespace)
			}
			if string(parentRef.Name) == gatewayName && refNamespace == gatewayNamespace {
				routes = append(routes, route)
				break
			}
		}
	}
	return routes, nil
}
