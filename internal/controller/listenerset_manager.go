package controller

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ensureListenerSet creates the ListenerSet if it does not exist, or updates its listeners
// if it does.  The ListenerSet's parentRef is set to gatewayName in the same namespace.
func (r *HTTPRouteReconciler) ensureListenerSet(
	ctx context.Context,
	lsName, lsNamespace string,
	gatewayName string,
	listeners []gatewayv1.Listener,
	ignoreDns annotations.IgnoreDnsUpdates,
	overrideInfra annotations.OverrideInfrastrucutre,
	overrideTtl annotations.OverrideTtl,
	clusterIssuer string,
) error {
	log := logf.FromContext(ctx)
	entries := listenersToListenerEntries(listeners)

	var existing gatewayv1.ListenerSet
	err := r.Get(ctx, types.NamespacedName{Name: lsName, Namespace: lsNamespace}, &existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// Create the ListenerSet.
		ls := &gatewayv1.ListenerSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lsName,
				Namespace: lsNamespace,
			},
			Spec: gatewayv1.ListenerSetSpec{
				ParentRef: gatewayv1.ParentGatewayReference{
					Name:      gatewayv1.ObjectName(gatewayName),
					Namespace: (*gatewayv1.Namespace)(ptr(gatewayNamespace)),
				},
				Listeners: entries,
			},
		}
		updateListenerSetAnnotations(ctx, ls, ignoreDns, overrideInfra, overrideTtl, clusterIssuer)
		if createErr := r.Create(ctx, ls); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				log.Info("ListenerSet already exists (concurrent create), updating", "listenerSet", lsName)
				// fall through to update below
			} else {
				log.Error(createErr, "Failed to create ListenerSet", "listenerSet", lsName)
				return createErr
			}
		} else {
			log.Info("Created ListenerSet", "listenerSet", lsName, "gateway", gatewayName, "listeners", len(entries))
			return nil
		}
	}

	// Update existing ListenerSet.
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest gatewayv1.ListenerSet
		if err := r.Get(ctx, types.NamespacedName{Name: lsName, Namespace: lsNamespace}, &latest); err != nil {
			return err
		}
		latest.Spec.Listeners = entries
		updateListenerSetAnnotations(ctx, &latest, ignoreDns, overrideInfra, overrideTtl, clusterIssuer)
		if err := r.Update(ctx, &latest); err != nil {
			return err
		}
		log.Info("Updated ListenerSet", "listenerSet", lsName, "listeners", len(entries))
		return nil
	})
}

// deleteListenerSet deletes a ListenerSet managed by the operator.
func (r *HTTPRouteReconciler) deleteListenerSet(ctx context.Context, lsName, lsNamespace string) error {
	log := logf.FromContext(ctx)
	var ls gatewayv1.ListenerSet
	if err := r.Get(ctx, types.NamespacedName{Name: lsName, Namespace: lsNamespace}, &ls); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := r.Delete(ctx, &ls); client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to delete ListenerSet", "listenerSet", lsName)
		return err
	}
	log.Info("Deleted ListenerSet", "listenerSet", lsName)
	return nil
}

// deleteDefaultGatewayIfUnused deletes the default stub Gateway for the given zone if no
// ListenerSets in the namespace still reference it.  Also removes the associated CTP.
func (r *HTTPRouteReconciler) deleteDefaultGatewayIfUnused(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
) error {
	log := logf.FromContext(ctx)

	// List all ListenerSets across all namespaces and check whether any still reference this gateway.
	var lsList gatewayv1.ListenerSetList
	if err := r.List(ctx, &lsList); err != nil {
		return err
	}
	for i := range lsList.Items {
		if string(lsList.Items[i].Spec.ParentRef.Name) == gatewayName {
			log.V(1).Info("Default Gateway still referenced by ListenerSet; skipping deletion",
				"gateway", gatewayName, "listenerSet", lsList.Items[i].Name)
			return nil
		}
	}

	var gw gatewayv1.Gateway
	if err := r.Get(ctx, types.NamespacedName{Name: gatewayName, Namespace: gatewayNamespace}, &gw); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := r.Delete(ctx, &gw); client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to delete default Gateway", "gateway", gatewayName)
		return err
	}
	log.Info("Deleted default Gateway (no ListenerSets remain)", "gateway", gatewayName)
	return r.deleteClientTrafficPolicy(ctx, gatewayName, gatewayNamespace)
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
