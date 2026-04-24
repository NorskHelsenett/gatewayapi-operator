package v1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (v *HTTPRouteCustomValidator) GetReferredGateway(ctx context.Context, httproute *gatewayv1.HTTPRoute) (*gatewayv1.Gateway, error) {
	log := logf.FromContext(ctx)

	for _, parentRef := range httproute.Spec.ParentRefs {
		// Only handle Gateway resources
		if parentRef.Group != nil && *parentRef.Group != gatewayv1.GroupName {
			continue
		}
		if parentRef.Kind != nil && *parentRef.Kind != "Gateway" {
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
