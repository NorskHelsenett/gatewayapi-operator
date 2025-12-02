package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// collectListenersForGateway gathers all hostnames from HTTPRoutes referencing the gateway
// and creates HTTPS listeners for each hostname
func (r *HTTPRouteReconciler) collectListenersForGateway(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
) ([]gatewayv1.Listener, error) {
	log := logf.FromContext(ctx)

	// List all HTTPRoutes that reference this gateway
	// Use a bypass cache to ensure we get the latest state and avoid race conditions
	httpRouteList := &gatewayv1.HTTPRouteList{}
	listOpts := []client.ListOption{}
	// Bypass cache to get the most up-to-date list
	if err := r.List(ctx, httpRouteList, listOpts...); err != nil {
		return nil, err
	}

	// Collect unique hostnames from HTTPRoutes that reference this Gateway
	hostnameSet := make(map[string]bool)
	routeCount := 0
	skippedCount := 0

	for _, route := range httpRouteList.Items {
		// Skip routes being deleted or not enabled for the operator
		if !route.DeletionTimestamp.IsZero() {
			log.V(1).Info("Skipping route being deleted", "route", route.Name, "namespace", route.Namespace)
			skippedCount++
			continue
		}
		if route.Annotations[AnnotationUseHttprouteOperator] != "true" {
			skippedCount++
			continue
		}

		// Check if this route references our gateway
		for _, parentRef := range route.Spec.ParentRefs {
			refName := string(parentRef.Name)
			refNamespace := gatewayNamespace
			if parentRef.Namespace != nil {
				refNamespace = string(*parentRef.Namespace)
			}

			if refName == gatewayName && refNamespace == gatewayNamespace {
				routeCount++
				// Collect all hostnames from this route
				for _, hostname := range route.Spec.Hostnames {
					hostnameSet[string(hostname)] = true
					log.V(1).Info("Collected hostname", "hostname", hostname, "route", route.Name, "gateway", gatewayName)
				}
				break
			}
		}
	}

	// Create HTTPS listeners for all collected hostnames
	listeners := make([]gatewayv1.Listener, 0, len(hostnameSet))
	for hostname := range hostnameSet {
		listener := r.createHTTPSListener(hostname, gatewayNamespace)
		listeners = append(listeners, listener)
	}

	log.Info("Collected listeners for Gateway",
		"gateway", gatewayName,
		"listeners", len(listeners),
		"activeRoutes", routeCount,
		"skippedRoutes", skippedCount,
		"totalRoutes", len(httpRouteList.Items))
	return listeners, nil
}

// createHTTPSListener creates an HTTPS listener for a hostname with TLS configuration
func (r *HTTPRouteReconciler) createHTTPSListener(
	hostname string,
	gatewayNamespace string,
) gatewayv1.Listener {
	// Use hostname as the listener section name
	listenerName := gatewayv1.SectionName(hostname)
	hn := gatewayv1.Hostname(hostname)

	// Construct TLS certificate secret name
	certSecretName := hostname + tlsCertSuffix

	// Certificate is in the gateway's namespace
	certNamespace := gatewayv1.Namespace(gatewayNamespace)

	terminate := gatewayv1.TLSModeTerminate
	fromAll := gatewayv1.NamespacesFromAll

	return gatewayv1.Listener{
		Name:     listenerName,
		Protocol: gatewayv1.HTTPSProtocolType,
		Port:     httpsPort,
		Hostname: &hn,
		AllowedRoutes: &gatewayv1.AllowedRoutes{
			Namespaces: &gatewayv1.RouteNamespaces{
				From: &fromAll,
			},
		},
		TLS: &gatewayv1.GatewayTLSConfig{
			Mode: &terminate,
			CertificateRefs: []gatewayv1.SecretObjectReference{
				{
					Group:     (*gatewayv1.Group)(ptr("")),
					Kind:      (*gatewayv1.Kind)(ptr("Secret")),
					Name:      gatewayv1.ObjectName(certSecretName),
					Namespace: &certNamespace,
				},
			},
		},
	}
}

// updateGatewayListeners updates the gateway's listeners based on all HTTPRoutes referencing it
func (r *HTTPRouteReconciler) updateGatewayListeners(
	ctx context.Context,
	gateway *gatewayv1.Gateway,
	gatewayNamespace string,
) error {
	log := logf.FromContext(ctx)

	gatewayName := gateway.Name

	// Collect listeners from all HTTPRoutes referencing this gateway
	newListeners, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
	if err != nil {
		return err
	}

	// If no listeners remain, delete the gateway
	if len(newListeners) == 0 {
		log.Info("No HTTPRoutes reference this gateway anymore, deleting it", "gateway", gatewayName, "namespace", gateway.Namespace)
		if err := r.Delete(ctx, gateway); err != nil {
			return err
		}
		log.Info("Deleted gateway", "gateway", gatewayName)
		return nil
	}

	// Use Server-Side Apply to update listeners
	// Include gatewayClassName since it's a required field, but we take it from the existing gateway
	patch := &gatewayv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gateway.Spec.GatewayClassName,
			Listeners:        newListeners,
		},
	}

	err = r.Patch(ctx, patch, client.Apply, client.ForceOwnership, client.FieldOwner("gatewayapi-operator"))
	if err != nil {
		return err
	}

	log.Info("Updated Gateway listeners", "gateway", gatewayName, "listeners", len(newListeners))
	return nil
}
