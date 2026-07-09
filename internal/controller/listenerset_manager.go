package controller

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// updateUserListenerSet updates the listeners in a user-defined ListenerSet.
// The ListenerSet must already exist -- this function returns an error if it is not found.
// The operator never creates or deletes user-defined ListenerSets.
func (r *HTTPRouteReconciler) updateUserListenerSet(
	ctx context.Context,
	lsName, lsNamespace string,
	listeners []gatewayv1.Listener,
	ignoreDns annotations.IgnoreDnsUpdates,
	overrideInfra annotations.OverrideInfrastrucutre,
	overrideTtl annotations.OverrideTtl,
	clusterIssuer string,
) error {
	log := logf.FromContext(ctx)
	entries := listenersToListenerEntries(listeners)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest gatewayv1.ListenerSet
		if err := r.Get(ctx, types.NamespacedName{Name: lsName, Namespace: lsNamespace}, &latest); err != nil {
			return err // including not-found: ListenerSet must be pre-created by user
		}
		latest.Spec.Listeners = entries
		updateListenerSetAnnotations(ctx, &latest, ignoreDns, overrideInfra, overrideTtl, clusterIssuer)
		if err := r.Update(ctx, &latest); err != nil {
			return err
		}
		log.Info("Updated user ListenerSet", "listenerSet", lsName, "listeners", len(entries))
		return nil
	})
}

// listenersToListenerEntries converts Gateway Listeners to ListenerSet ListenerEntries.
func listenersToListenerEntries(listeners []gatewayv1.Listener) []gatewayv1.ListenerEntry {
	entries := make([]gatewayv1.ListenerEntry, 0, len(listeners))
	for _, l := range listeners {
		entries = append(entries, gatewayv1.ListenerEntry{
			Name:          l.Name,
			Hostname:      l.Hostname,
			Port:          l.Port,
			Protocol:      l.Protocol,
			TLS:           l.TLS,
			AllowedRoutes: l.AllowedRoutes,
		})
	}
	return entries
}

// updateListenerSetAnnotations syncs DNS-related and cert-manager annotations from HTTPRoutes onto a ListenerSet.
// clusterIssuer is only written when non-empty; pass "" to preserve the existing value (e.g. during deletion sync).
func updateListenerSetAnnotations(ctx context.Context, ls *gatewayv1.ListenerSet, ignoreDns annotations.IgnoreDnsUpdates, overrideInfra annotations.OverrideInfrastrucutre, overrideTTL annotations.OverrideTtl, clusterIssuer string) {
	log := logf.FromContext(ctx)

	if ls.Annotations == nil {
		ls.Annotations = make(map[string]string)
	}

	if clusterIssuer != "" {
		ls.Annotations[clusterIssuerAnnotation] = clusterIssuer
		log.Info("Updated ListenerSet annotation cert-manager.io/cluster-issuer", "issuer", clusterIssuer)
	}

	dnsIgnoreVal := ignoreDns.ConvertToGatewayAnnotation()
	dnsOverrideInfraVal := overrideInfra.ConvertToGatewayAnnotation()
	dnsOverrideTtlVal := overrideTTL.ConvertToGatewayAnnotation()

	if dnsIgnoreVal != "" {
		ls.Annotations[annotations.AnnotationDnsIgnore] = dnsIgnoreVal
		log.Info("Updated ListenerSet annotation dns.nhn.no/ignore")
	} else {
		delete(ls.Annotations, annotations.AnnotationDnsIgnore)
	}

	if dnsOverrideInfraVal != "" {
		ls.Annotations[annotations.AnnotationOverrideInfrastructure] = dnsOverrideInfraVal
		log.Info("Updated ListenerSet annotation dns.nhn.no/override-infrastructure")
	} else {
		delete(ls.Annotations, annotations.AnnotationOverrideInfrastructure)
	}

	if dnsOverrideTtlVal != "" {
		ls.Annotations[annotations.AnnotationOverrideTTL] = dnsOverrideTtlVal
		log.Info("Updated ListenerSet annotation dns.nhn.no/override-ttl")
	} else {
		delete(ls.Annotations, annotations.AnnotationOverrideTTL)
	}
}
