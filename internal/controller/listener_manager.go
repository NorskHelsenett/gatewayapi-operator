package controller

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	corev1 "k8s.io/api/core/v1"
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
) ([]gatewayv1.Listener, map[string]string, annotations.IgnoreDnsUpdates, annotations.OverrideInfrastrucutre, annotations.OverrideTtl, error) {
	log := logf.FromContext(ctx)

	// List all HTTPRoutes that reference this gateway
	// Use a bypass cache to ensure we get the latest state and avoid race conditions
	httpRouteList := &gatewayv1.HTTPRouteList{}
	listOpts := []client.ListOption{}
	// Bypass cache to get the most up-to-date list
	if err := r.List(ctx, httpRouteList, listOpts...); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Collect unique hostnames from HTTPRoutes that reference this Gateway
	hostnameSet := make(map[string]bool)
	httpHostnameSet := make(map[string]bool)

	ignoreDnsUpdates := annotations.NewIgnoreDnsUpdates()
	overrideTtl := annotations.NewOverrideTtl()
	overrideInfrastructure := annotations.NewOverrideInfrastructure()
	var routesOnGateway []*gatewayv1.HTTPRoute

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
			if !IsGatewayParentRef(parentRef) {
				continue
			}
			refName := string(parentRef.Name)
			// Gateway API defaults an omitted parentRef namespace to the route's own namespace.
			refNamespace := route.Namespace
			if parentRef.Namespace != nil {
				refNamespace = string(*parentRef.Namespace)
			}

			if refName == gatewayName && refNamespace == gatewayNamespace {
				routeCount++
				routesOnGateway = append(routesOnGateway, &route)
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

	envoyPodAnnotations, conflicts, tooLong := resolveEnvoyPodAnnotations(r.EnvoyPodAnnotations, routesOnGateway)
	for _, invalid := range tooLong {
		length := utf8.RuneCountInString(invalid.route.Annotations[invalid.key])
		log.Info("Ignoring Envoy pod annotation with too long value", "annotation", invalid.key,
			"route", invalid.route.Namespace+"/"+invalid.route.Name, "length", length)
		r.Recorder.Eventf(invalid.route, nil, corev1.EventTypeWarning, "EnvoyPodAnnotationTooLong", "ResolveGatewayAnnotations",
			"Annotation %s is ignored: value is %d characters, Gateway API allows at most %d",
			invalid.key, length, annotations.MaxAnnotationValueLength)
	}
	for _, conflict := range conflicts {
		ignored := conflict.route.Annotations[conflict.key]
		used := conflict.owner.Annotations[conflict.key]
		log.Info("Ignoring conflicting Envoy pod annotation", "annotation", conflict.key,
			"route", conflict.route.Namespace+"/"+conflict.route.Name, "ignoredValue", ignored,
			"ownerRoute", conflict.owner.Namespace+"/"+conflict.owner.Name, "usedValue", used)
		r.Recorder.Eventf(conflict.route, conflict.owner, corev1.EventTypeWarning, "EnvoyPodAnnotationConflict", "ResolveGatewayAnnotations",
			"Annotation %s=%q is ignored: Gateway %s/%s uses %q from older HTTPRoute %s/%s",
			conflict.key, ignored, gatewayNamespace, gatewayName, used, conflict.owner.Namespace, conflict.owner.Name)
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

	return listeners, envoyPodAnnotations, ignoreDnsUpdates, overrideInfrastructure, overrideTtl, nil
}

type envoyPodAnnotationConflict struct {
	key   string
	route *gatewayv1.HTTPRoute // newer route whose value is ignored
	owner *gatewayv1.HTTPRoute // older route whose value is used
}

// IsGatewayParentRef reports whether a parentRef points at a Gateway; omitted Group and Kind default to a Gateway API Gateway.
func IsGatewayParentRef(ref gatewayv1.ParentReference) bool {
	return (ref.Group == nil || *ref.Group == gatewayv1.GroupName) && (ref.Kind == nil || *ref.Kind == "Gateway")
}

type envoyPodAnnotationTooLong struct {
	key   string
	route *gatewayv1.HTTPRoute
}

// resolveEnvoyPodAnnotations picks one value per key; the oldest route wins so the shared Envoy pods are not restarted by a newcomer.
func resolveEnvoyPodAnnotations(keys []string, routes []*gatewayv1.HTTPRoute) (map[string]string, []envoyPodAnnotationConflict, []envoyPodAnnotationTooLong) {
	sorted := slices.SortedStableFunc(slices.Values(routes), func(a, b *gatewayv1.HTTPRoute) int {
		return cmp.Or(
			a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})

	values := make(map[string]string)
	owners := make(map[string]*gatewayv1.HTTPRoute)
	var conflicts []envoyPodAnnotationConflict
	var tooLong []envoyPodAnnotationTooLong
	for _, route := range sorted {
		for _, key := range keys {
			value, ok := route.Annotations[key]
			if !ok {
				continue
			}
			// Gateway API would reject every update of the Gateway with this value; like the API server, count characters, not bytes.
			if utf8.RuneCountInString(value) > annotations.MaxAnnotationValueLength {
				tooLong = append(tooLong, envoyPodAnnotationTooLong{key: key, route: route})
				continue
			}
			owner, found := owners[key]
			if !found {
				values[key] = value
				owners[key] = route
			} else if values[key] != value {
				conflicts = append(conflicts, envoyPodAnnotationConflict{key: key, route: route, owner: owner})
			}
		}
	}
	return values, conflicts, tooLong
}

// syncEnvoyPodAnnotations writes the desired values and removes configured or previously managed keys that are no longer desired.
func syncEnvoyPodAnnotations(gw *gatewayv1.Gateway, keys []string, desired map[string]string) {
	var existing map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue
	if gw.Spec.Infrastructure != nil {
		existing = gw.Spec.Infrastructure.Annotations
	}
	result := make(map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue, len(existing)+len(desired))
	maps.Copy(result, existing)

	managed := annotations.ParseManagedEnvoyPodAnnotations(gw.Annotations[annotations.AnnotationManagedEnvoyPodAnnotations])
	for _, key := range slices.Concat(managed, keys) {
		delete(result, gatewayv1.AnnotationKey(key))
	}
	var written []string
	for _, key := range keys {
		if value, ok := desired[key]; ok {
			result[gatewayv1.AnnotationKey(key)] = gatewayv1.AnnotationValue(value)
			written = append(written, key)
		}
	}

	if gw.Spec.Infrastructure == nil && len(result) > 0 {
		gw.Spec.Infrastructure = &gatewayv1.GatewayInfrastructure{}
	}
	if gw.Spec.Infrastructure != nil {
		gw.Spec.Infrastructure.Annotations = result
	}

	if len(written) == 0 {
		delete(gw.Annotations, annotations.AnnotationManagedEnvoyPodAnnotations)
		return
	}
	if gw.Annotations == nil {
		gw.Annotations = make(map[string]string)
	}
	gw.Annotations[annotations.AnnotationManagedEnvoyPodAnnotations] = strings.Join(written, ",")
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
// It returns (true, nil) when the Gateway was deleted or is already gone (e.g. due to concurrent deletion),
// so callers can skip any post-update work that assumes the Gateway still exists.
// When no listeners remain, it also deletes any associated ClientTrafficPolicy.
func (r *HTTPRouteReconciler) updateGatewayListeners(
	ctx context.Context,
	gateway *gatewayv1.Gateway,
	gatewayNamespace string,
) (deleted bool, err error) {
	log := logf.FromContext(ctx)

	gatewayName := gateway.Name

	// Collect listeners and annotations from all HTTPRoutes referencing this gateway
	newListeners, envoyPodAnnotations, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
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
		syncEnvoyPodAnnotations(&latest, r.EnvoyPodAnnotations, envoyPodAnnotations)

		// Update the gateway's annotations
		UpdateGatewayAnnotations(ctx, &latest, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation)

		// Update the gateway's GatewayClass
		UpdateGatewayClass(&latest)

		return r.Update(ctx, &latest)
	})

	if err != nil {
		// Ignore not found errors - the object might have been deleted by another reconciliation
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
		// set ipv4 only gatewayclass if ipv4 is specified on inet gateway
		if gw.Spec.Infrastructure != nil && gw.Spec.Infrastructure.Annotations != nil {
			if gw.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone] == InetIPAMZone &&
				gw.Spec.Infrastructure.Annotations[annotations.AnnotationIpFamily] == IPv4IpFamily {
				gw.Spec.GatewayClassName = gatewayv1.ObjectName(inetIpv4GatewayClassName)

			} else if gw.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone] == InetIPAMZone {
				gw.Spec.GatewayClassName = gatewayv1.ObjectName(inetGatewayClassName)

			} else {
				// Default to hnet gwclass if infrastructure annotations is set, but not to inet
				gw.Spec.GatewayClassName = gatewayv1.ObjectName(hnetGatewayClassName)
			}
		} else {
			// Use Hnet gatewayclass if AnnotationIPAMZone is not InetIPAMZone
			gw.Spec.GatewayClassName = gatewayv1.ObjectName(hnetGatewayClassName)

		}
	}
}
