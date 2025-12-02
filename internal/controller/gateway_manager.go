package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ensureGateway ensures a Gateway exists with proper listeners.
// Creates the gateway if it doesn't exist, otherwise updates its listeners.
func (r *HTTPRouteReconciler) ensureGateway(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
	ipamZone string,
	clusterIssuer string,
) error {
	log := logf.FromContext(ctx)

	// Check if Gateway exists
	gateway := &gatewayv1.Gateway{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: gatewayNamespace,
	}, gateway)

	if err != nil {
		if errors.IsNotFound(err) {
			// Gateway doesn't exist, create it
			log.Info("Creating new Gateway", "gateway", gatewayName, "namespace", gatewayNamespace)
			return r.createGateway(ctx, gatewayName, gatewayNamespace, ipamZone, clusterIssuer)
		}
		log.Error(err, "Failed to get Gateway", "gateway", gatewayName)
		return err
	}

	// Gateway exists, validate cluster issuer matches
	existingIssuer := gateway.Annotations[clusterIssuerAnnotation]
	if existingIssuer != clusterIssuer {
		err := errors.NewBadRequest("HTTPRoute cluster issuer mismatch: Gateway has issuer '" + existingIssuer + "' but HTTPRoute requires '" + clusterIssuer + "'")
		log.Error(err, "Cluster issuer mismatch", "gateway", gatewayName, "gatewayIssuer", existingIssuer, "routeIssuer", clusterIssuer)
		return err
	}

	// Gateway exists, validate IPAM zone matches if set
	if gateway.Spec.Infrastructure != nil && gateway.Spec.Infrastructure.Annotations != nil {
		if existingZone, exists := gateway.Spec.Infrastructure.Annotations["ipam.vitistack.io/zone"]; exists {
			if string(existingZone) != ipamZone {
				err := errors.NewBadRequest("HTTPRoute IPAM zone mismatch: Gateway has zone '" + string(existingZone) + "' but HTTPRoute requires '" + ipamZone + "'")
				log.Error(err, "IPAM zone mismatch", "gateway", gatewayName, "gatewayZone", string(existingZone), "routeZone", ipamZone)
				return err
			}
		}
	}

	// Gateway exists and configuration matches, update listeners
	log.Info("Gateway exists, updating listeners", "gateway", gatewayName, "namespace", gatewayNamespace)
	return r.updateGatewayListeners(ctx, gateway, gatewayNamespace)
}

// createGateway creates a new Gateway resource with initial configuration
func (r *HTTPRouteReconciler) createGateway(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
	ipamZone string,
	clusterIssuer string,
) error {
	log := logf.FromContext(ctx)

	// Collect all listeners from HTTPRoutes that reference this gateway
	listeners, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
	if err != nil {
		log.Error(err, "Failed to collect listeners for new Gateway")
		return err
	}

	newGateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
			Annotations: map[string]string{
				clusterIssuerAnnotation: clusterIssuer,
			},
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayClassName,
			Listeners:        listeners,
			Infrastructure: &gatewayv1.GatewayInfrastructure{
				Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
					"ipam.vitistack.io/zone": gatewayv1.AnnotationValue(ipamZone),
				},
			},
		},
	}

	if err := r.Create(ctx, newGateway); err != nil {
		log.Error(err, "Failed to create Gateway", "gateway", gatewayName)
		return err
	}

	log.Info("Successfully created Gateway", "gateway", gatewayName, "namespace", gatewayNamespace, "listeners", len(listeners))
	return nil
}
