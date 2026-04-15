package controller

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// collectListenersForGateway gathers all hostnames from HTTPRoutes referencing the gateway
// and creates HTTPS listeners for each hostname
func (r *HTTPRouteReconciler) collectListenersForGateway(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
) ([]gatewayv1.Listener, annotations.IgnoreDnsUpdates, annotations.OverrideInfrastrucutre, annotations.OverrideTtl, error) {
	log := logf.FromContext(ctx)

	// List all HTTPRoutes that reference this gateway
	// Use a bypass cache to ensure we get the latest state and avoid race conditions
	httpRouteList := &gatewayv1.HTTPRouteList{}
	listOpts := []client.ListOption{}
	// Bypass cache to get the most up-to-date list
	if err := r.List(ctx, httpRouteList, listOpts...); err != nil {
		return nil, nil, nil, nil, err
	}

	// Collect unique hostnames from HTTPRoutes that reference this Gateway
	hostnameSet := make(map[string]bool)
	httpHostnameSet := make(map[string]bool)

	ignoreDnsUpdates := annotations.NewIgnoreDnsUpdates()
	overrideTtl := annotations.NewOverrideTtl()
	overrideInfrastructure := annotations.NewOverrideInfrastructure()

	routeCount := 0
	skippedCount := 0

	for _, route := range httpRouteList.Items {
		// Skip routes being deleted or not enabled for the operator
		if !route.DeletionTimestamp.IsZero() {
			log.Info("Skipping route being deleted", "route", route.Name, "namespace", route.Namespace)
			skippedCount++
			continue
		}
		// Filter out all httproutes that are not enabled for this operator

		if route.Annotations[annotations.AnnotationUseHttprouteOperator] != "true" {
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
					// Add all httproutes that should be created on port 80 without TLS (redirect)
					if route.Annotations[annotations.AnnotationHttpOnlyListener] == "true" {
						httpHostnameSet[string(hostname)] = true
					} else {
						// Add all TLS 443 http(s)routes
						hostnameSet[string(hostname)] = true
					}

					// Parse the "dns.nhn.no/ignore: <boolean>" if present on the HTTProute
					ignoreDnsUpdates.ParseAnnotation(string(hostname), route.Annotations[annotations.AnnotationDnsIgnore])

					// Parse the "dns.nhn.no/override-infrastructure: '{"infrastructure":["<infrastructure>","<infrastructure>"]}'" if present on the HTTProute
					overrideInfrastructure.ParseAnnotation(string(hostname), route.Annotations[annotations.AnnotationOverrideInfrastructure])

					// Parse the "dns.nhn.no/override-ttl: '<int>'"" if present on the HTTProute
					overrideTtl.ParseAnnotation(string(hostname), route.Annotations[annotations.AnnotationOverrideTTL])

				}
				break
			}
		}
	}

	// Create HTTPS listeners for all collected hostnames
	log.Info("Creating listeners from hostnames", "uniqueHostnames", len(hostnameSet)+len(httpHostnameSet))
	listeners := make([]gatewayv1.Listener, 0, len(hostnameSet)+len(httpHostnameSet))

	for hostname := range hostnameSet {
		httpsListener := r.createHTTPSListener(hostname, gatewayNamespace)
		log.Info("Created HTTPS listener", "name", httpsListener.Name, "hostname", hostname)
		listeners = append(listeners, httpsListener)

	}

	for httpHostname := range httpHostnameSet {
		httpListener := r.createHTTPListener(httpHostname)
		log.Info("Created HTTP listener", "name", httpListener.Name, "hostname", httpHostname)
		listeners = append(listeners, httpListener)
	}

	log.Info("Collected listeners for Gateway",
		"gateway", gatewayName,
		"listeners", len(listeners),
		"activeRoutes", routeCount,
		"skippedRoutes", skippedCount,
		"totalRoutes", len(httpRouteList.Items))

	return listeners, ignoreDnsUpdates, overrideInfrastructure, overrideTtl, nil
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
		TLS: &gatewayv1.ListenerTLSConfig{
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

func (r *HTTPRouteReconciler) createHTTPListener(
	hostname string,
) gatewayv1.Listener {
	// Use hostname as the listener section name
	listenerName := gatewayv1.SectionName(hostname) + "-http"
	hn := gatewayv1.Hostname(hostname)
	fromAll := gatewayv1.NamespacesFromAll

	return gatewayv1.Listener{
		Name:     listenerName,
		Protocol: gatewayv1.HTTPProtocolType,
		Port:     httpPort,
		Hostname: &hn,
		AllowedRoutes: &gatewayv1.AllowedRoutes{
			Namespaces: &gatewayv1.RouteNamespaces{
				From: &fromAll,
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

	// Collect listeners and annotations from all HTTPRoutes referencing this gateway
	newListeners, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
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

	log.Info("Applying Gateway patch", "listeners", len(newListeners))
	namespacedName := &types.NamespacedName{
		Namespace: gatewayNamespace,
		Name:      gatewayName,
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch latest version
		var latest gatewayv1.Gateway
		if err := r.Get(ctx, *namespacedName, &latest); err != nil {
			// If the object is already gone, nothing to do
			if client.IgnoreNotFound(err) == nil {
				return nil
			}
			return err
		}
		// Update the listeners array before updating the object
		latest.Spec.Listeners = newListeners

		// Update the gateway
		UpdateGatewayAnnotations(ctx, &latest, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation)

		return r.Update(ctx, &latest)
	})

	if err != nil {
		// Ignore not found errors - the object might have been deleted by another reconciliation
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "Failed to remove finalizer")
			return err
		} else {
			log.Info("Gateway already deleted", "name", gateway.Name)
			return nil
		}
	} else {
		log.Info("Updated Gateway listeners", "gateway", gatewayName, "listeners", len(newListeners))
		return nil
	}

}

func UpdateGatewayAnnotations(ctx context.Context, gw *gatewayv1.Gateway, ignoreDns annotations.IgnoreDnsUpdates, overrideInfra annotations.OverrideInfrastrucutre, overrideTTL annotations.OverrideTtl) {
	log := logf.FromContext(ctx)

	dnsIgnoreAnnotationVal := ignoreDns.ConvertToGatewayAnnotation()
	dnsOverrideInfraVal := overrideInfra.ConvertToGatewayAnnotation()
	dnsOverrideTtl := overrideTTL.ConvertToGatewayAnnotation()

	if dnsIgnoreAnnotationVal != "" {
		gw.ObjectMeta.Annotations[annotations.AnnotationDnsIgnore] = dnsIgnoreAnnotationVal
		log.Info("Updated gateway annotation dns.nhn.no/ignore")
	} else {
		log.Info("Deleting gateway annotation dns.nhn.no/ignore")
		delete(gw.ObjectMeta.Annotations, annotations.AnnotationDnsIgnore)
	}

	if dnsOverrideInfraVal != "" {
		gw.ObjectMeta.Annotations[annotations.AnnotationOverrideInfrastructure] = dnsOverrideInfraVal
		log.Info("Updated gateway annotation dns.nhn.no/override-infrastructure")

	} else {
		log.Info("Deleting gateway annotation dns.nhn.no/override-infrastructure")
		delete(gw.ObjectMeta.Annotations, annotations.AnnotationOverrideInfrastructure)
	}

	if dnsOverrideTtl != "" {
		gw.ObjectMeta.Annotations[annotations.AnnotationOverrideTTL] = dnsOverrideTtl
		log.Info("Updated gateway annotation dns.nhn.no/override-ttl")

	} else {
		log.Info("Deleting gateway annotation dns.nhn.no/override-ttl")
		delete(gw.ObjectMeta.Annotations, annotations.AnnotationOverrideTTL)
	}

}
