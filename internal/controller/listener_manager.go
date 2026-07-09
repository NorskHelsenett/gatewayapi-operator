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

		// Check if this route references our gateway via a direct Gateway parentRef
		for _, parentRef := range route.Spec.ParentRefs {
			// Skip ListenerSet parentRefs — those are handled by collectListenersForListenerSet
			if parentRef.Kind != nil && string(*parentRef.Kind) == "ListenerSet" {
				continue
			}
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
	listeners := make([]gatewayv1.Listener, 0, len(hostnameSet)+len(httpHostnameSet))

	for hostname := range hostnameSet {
		listeners = append(listeners, r.createHTTPSListener(hostname, gatewayNamespace))
	}

	for httpHostname := range httpHostnameSet {
		listeners = append(listeners, r.createHTTPListener(httpHostname))
	}

	log.Info("Collected listeners for Gateway",
		"gateway", gatewayName,
		"listeners", len(listeners),
		"activeRoutes", routeCount,
		"skippedRoutes", skippedCount,
		"totalRoutes", len(httpRouteList.Items))

	return listeners, ignoreDnsUpdates, overrideInfrastructure, overrideTtl, nil
}

// collectListenersForListenerSet gathers all hostnames from HTTPRoutes whose parentRef
// points directly to the named ListenerSet (kind=ListenerSet) and builds listener entries.
func (r *HTTPRouteReconciler) collectListenersForListenerSet(
	ctx context.Context,
	listenerSetName, listenerSetNamespace string,
) ([]gatewayv1.Listener, annotations.IgnoreDnsUpdates, annotations.OverrideInfrastrucutre, annotations.OverrideTtl, error) {
	log := logf.FromContext(ctx)

	httpRouteList := &gatewayv1.HTTPRouteList{}
	if err := r.List(ctx, httpRouteList); err != nil {
		return nil, nil, nil, nil, err
	}

	hostnameSet := make(map[string]bool)
	httpHostnameSet := make(map[string]bool)
	ignoreDnsUpdates := annotations.NewIgnoreDnsUpdates()
	overrideTtl := annotations.NewOverrideTtl()
	overrideInfrastructure := annotations.NewOverrideInfrastructure()
	routeCount := 0

	for _, route := range httpRouteList.Items {
		if !route.DeletionTimestamp.IsZero() {
			continue
		}
		if route.Annotations[annotations.AnnotationUseHttprouteOperator] != "true" {
			continue
		}

		for _, parentRef := range route.Spec.ParentRefs {
			if parentRef.Kind == nil || string(*parentRef.Kind) != "ListenerSet" {
				continue
			}
			refName := string(parentRef.Name)
			refNamespace := listenerSetNamespace
			if parentRef.Namespace != nil {
				refNamespace = string(*parentRef.Namespace)
			}
			if refName != listenerSetName || refNamespace != listenerSetNamespace {
				continue
			}

			routeCount++
			for _, hostname := range route.Spec.Hostnames {
				if route.Annotations[annotations.AnnotationHttpOnlyListener] == "true" {
					httpHostnameSet[string(hostname)] = true
				} else {
					hostnameSet[string(hostname)] = true
				}
				ignoreDnsUpdates.ParseAnnotation(string(hostname), route.Annotations[annotations.AnnotationDnsIgnore])
				overrideInfrastructure.ParseAnnotation(string(hostname), route.Annotations[annotations.AnnotationOverrideInfrastructure])
				overrideTtl.ParseAnnotation(string(hostname), route.Annotations[annotations.AnnotationOverrideTTL])
			}
			break
		}
	}

	listeners := make([]gatewayv1.Listener, 0, len(hostnameSet)+len(httpHostnameSet))
	for hostname := range hostnameSet {
		listeners = append(listeners, r.createHTTPSListener(hostname, listenerSetNamespace))
	}
	for httpHostname := range httpHostnameSet {
		listeners = append(listeners, r.createHTTPListener(httpHostname))
	}

	log.Info("Collected listeners for ListenerSet",
		"listenerSet", listenerSetName,
		"listeners", len(listeners),
		"activeRoutes", routeCount)

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

// updateGatewayListeners updates the gateway's listeners based on all HTTPRoutes referencing it.
// It returns (true, nil) when the Gateway was deleted or is already gone.
// When no listeners remain, it deletes the gateway and associated ClientTrafficPolicy.
func (r *HTTPRouteReconciler) updateGatewayListeners(
	ctx context.Context,
	gateway *gatewayv1.Gateway,
	gatewayNamespace string,
) (deleted bool, err error) {
	log := logf.FromContext(ctx)

	gatewayName := gateway.Name

	// Collect listeners from all Gateway-mode HTTPRoutes referencing this gateway
	newListeners, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
	if err != nil {
		return false, err
	}

	// If no listeners remain, delete the gateway and any associated CTP
	if len(newListeners) == 0 {
		log.Info("No HTTPRoutes reference this gateway anymore, deleting it", "gateway", gatewayName, "namespace", gateway.Namespace)
		if delErr := r.Delete(ctx, gateway); delErr != nil {
			if client.IgnoreNotFound(delErr) != nil {
				return false, delErr
			}
			log.Info("Gateway already deleted (concurrent deletion)", "gateway", gatewayName)
		} else {
			log.Info("Deleted gateway", "gateway", gatewayName)
		}
		if ctpErr := r.deleteClientTrafficPolicy(ctx, gatewayName, gatewayNamespace); ctpErr != nil {
			log.Error(ctpErr, "Failed to delete ClientTrafficPolicy for gateway", "gateway", gatewayName)
			return false, ctpErr
		}
		return true, nil
	}

	namespacedName := &types.NamespacedName{
		Namespace: gatewayNamespace,
		Name:      gatewayName,
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest gatewayv1.Gateway
		if err := r.Get(ctx, *namespacedName, &latest); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return nil
			}
			return err
		}
		latest.Spec.Listeners = newListeners
		UpdateGatewayAnnotations(ctx, &latest, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation)
		UpdateGatewayClass(&latest)
		UpdateGatewayAllowedListeners(&latest)
		return r.Update(ctx, &latest)
	})

	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "Failed to update Gateway listeners")
			return false, err
		}
		log.Info("Gateway already deleted", "name", gateway.Name)
		return true, nil
	}
	log.Info("Updated Gateway listeners", "gateway", gatewayName, "listeners", len(newListeners))
	return false, nil
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

func UpdateGatewayClass(gw *gatewayv1.Gateway) {
	// only update GatewayClass when no Class is set or when old default "eg" is used.
	if gw.Spec.GatewayClassName == "" || gw.Spec.GatewayClassName == legacyGatewayClassName {
		// If gateway is in InetIPAMZone we use the Inet GatewayClass
		if gw.Spec.Infrastructure != nil && gw.Spec.Infrastructure.Annotations != nil &&
			gw.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone] == InetIPAMZone {
			gw.Spec.GatewayClassName = gatewayv1.ObjectName(inetGatewayClassName)
		} else {
			// Use Hnet gatewayclass if AnnotationIPAMZone is not InetIPAMZone
			gw.Spec.GatewayClassName = gatewayv1.ObjectName(hnetGatewayClassName)
		}
	}
}

// UpdateGatewayAllowedListeners ensures the Gateway allows user-defined ListenerSets
// in the same namespace to attach. It is idempotent and safe to call on both
// newly created and pre-existing Gateways.
func UpdateGatewayAllowedListeners(gw *gatewayv1.Gateway) {
	from := gatewayv1.NamespacesFromAll
	if gw.Spec.AllowedListeners == nil {
		gw.Spec.AllowedListeners = &gatewayv1.AllowedListeners{
			Namespaces: &gatewayv1.ListenerNamespaces{
				From: &from,
			},
		}
		return
	}
	if gw.Spec.AllowedListeners.Namespaces == nil {
		gw.Spec.AllowedListeners.Namespaces = &gatewayv1.ListenerNamespaces{
			From: &from,
		}
		return
	}
	if gw.Spec.AllowedListeners.Namespaces.From == nil {
		gw.Spec.AllowedListeners.Namespaces.From = &from
	}
}
