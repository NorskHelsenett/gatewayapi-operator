package controller

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// HTTPRouteReconciler reconciles a HTTPRoute object
type HTTPRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=listenersets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=clienttrafficpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the HTTPRoute
	var httpRoute gatewayv1.HTTPRoute
	if err := r.Get(ctx, req.NamespacedName, &httpRoute); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if operator is not enabled for this HTTPRoute
	if httpRoute.Annotations[annotations.AnnotationUseHttprouteOperator] != "true" {
		log.V(1).Info("Skipping HTTPRoute - operator not enabled", "name", httpRoute.Name, "namespace", httpRoute.Namespace)
		return ctrl.Result{}, nil
	}

	// Validate that we have parent refs
	if len(httpRoute.Spec.ParentRefs) == 0 {
		log.Error(nil, "HTTPRoute has no parent references", "name", httpRoute.Name)
		return ctrl.Result{}, nil
	}

	log.V(1).Info("Reconciling HTTPRoute", "name", httpRoute.Name, "namespace", httpRoute.Namespace)

	// Extract parent reference. When kind=ListenerSet the route binds to a user-defined
	// ListenerSet; the operator only updates that ListenerSet's listeners.
	// When kind=Gateway (or unset) the operator manages the Gateway's listeners directly.
	parentRef := httpRoute.Spec.ParentRefs[0]
	isListenerSetMode := parentRef.Kind != nil && string(*parentRef.Kind) == "ListenerSet"

	var gatewayName, listenerSetName string
	if isListenerSetMode {
		listenerSetName = string(parentRef.Name)
	} else {
		gatewayName = string(parentRef.Name)
	}
	gatewayNamespace := httpRoute.Namespace
	if parentRef.Namespace != nil {
		gatewayNamespace = string(*parentRef.Namespace)
	}

	// Handle deletion
	if !httpRoute.DeletionTimestamp.IsZero() {
		log.Info("HTTPRoute is being deleted, updating listeners", "name", httpRoute.Name)

		// Check if finalizer is present
		if controllerutil.ContainsFinalizer(&httpRoute, httprouteFinalizerName) {
			var delErr error
			if isListenerSetMode {
				delErr = r.handleHTTPRouteDeletionListenerSet(ctx, listenerSetName, gatewayNamespace)
			} else {
				delErr = r.handleHTTPRouteDeletion(ctx, gatewayName, gatewayNamespace)
			}
			if delErr != nil {
				log.Error(delErr, "Failed to handle HTTPRoute deletion")
				return ctrl.Result{}, delErr
			}

			// Remove finalizer using retry logic to handle conflicts
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				// Fetch latest version
				var latest gatewayv1.HTTPRoute
				if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
					// If the object is already gone, nothing to do
					if client.IgnoreNotFound(err) == nil {
						return nil
					}
					return err
				}

				// Check if finalizer is still present (might have been removed by another reconciliation)
				if !controllerutil.ContainsFinalizer(&latest, httprouteFinalizerName) {
					log.Info("Finalizer already removed", "name", httpRoute.Name)
					return nil
				}

				// Remove finalizer - TODO: This should be a patch to avoid race-conditions
				controllerutil.RemoveFinalizer(&latest, httprouteFinalizerName)
				return r.Update(ctx, &latest)
			})

			if err != nil {
				// Ignore not found errors - the object might have been deleted by another reconciliation
				if client.IgnoreNotFound(err) != nil {
					log.Error(err, "Failed to remove finalizer")
					return ctrl.Result{}, err
				}
				log.Info("HTTPRoute already deleted", "name", httpRoute.Name)
			} else {
				log.Info("Removed finalizer from HTTPRoute", "name", httpRoute.Name)
			}
		}

		return ctrl.Result{}, nil
	}

	// Check if gateway reference has changed — only tracked in Gateway mode.
	// In ListenerSet mode the user controls the gateway via the ListenerSet spec.
	if !isListenerSetMode {
		currentGatewayRef := gatewayNamespace + "/" + gatewayName
		previousGatewayRef := httpRoute.Annotations[previousGatewayAnnotationKey]

		if previousGatewayRef != "" && previousGatewayRef != currentGatewayRef {
			log.Info("Gateway reference changed, updating old gateway", "oldGateway", previousGatewayRef, "newGateway", currentGatewayRef)
			if err := r.updateOldGateway(ctx, previousGatewayRef); err != nil {
				log.Error(err, "Failed to update old gateway listeners", "gateway", previousGatewayRef)
				return ctrl.Result{}, err
			}
		}
	}

	// Add finalizer if not present using controllerutil
	if !controllerutil.ContainsFinalizer(&httpRoute, httprouteFinalizerName) {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Fetch latest version
			var latest gatewayv1.HTTPRoute
			if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
				return err
			}

			// Check again if finalizer is already present (might have been added by another reconciliation)
			if controllerutil.ContainsFinalizer(&latest, httprouteFinalizerName) {
				return nil
			}

			// Add finalizer - TODO: This should be a patch to avoid race-conditions
			controllerutil.AddFinalizer(&latest, httprouteFinalizerName)
			return r.Update(ctx, &latest)
		})

		if err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		log.Info("Added finalizer to HTTPRoute", "name", httpRoute.Name)
		// Return and let Kubernetes re-trigger reconciliation with the updated object
		return ctrl.Result{}, nil
	}

	// Update annotations
	needsUpdate := false
	if httpRoute.Annotations == nil {
		httpRoute.Annotations = make(map[string]string)
	}
	if _, exists := httpRoute.Annotations[reconcileAnnotationKey]; !exists {
		httpRoute.Annotations[reconcileAnnotationKey] = "true"
		needsUpdate = true
	}
	// Track the previous gateway ref only in Gateway mode
	if !isListenerSetMode {
		currentGatewayRef := gatewayNamespace + "/" + gatewayName
		if httpRoute.Annotations[previousGatewayAnnotationKey] != currentGatewayRef {
			httpRoute.Annotations[previousGatewayAnnotationKey] = currentGatewayRef
			needsUpdate = true
		}
	}

	if needsUpdate {
		patch := &gatewayv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1",
				Kind:       "HTTPRoute",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        httpRoute.Name,
				Namespace:   httpRoute.Namespace,
				Annotations: httpRoute.Annotations,
			},
		}
		err := r.Patch(ctx, patch, client.Apply, client.ForceOwnership, client.FieldOwner("gatewayapi-operator"))
		if err != nil {
			log.Error(err, "Failed to update HTTPRoute annotations")
			return ctrl.Result{}, err
		}
		log.Info("Updated HTTPRoute annotations", "name", httpRoute.Name)
	}

	// Get IPAM zone from annotation or use default
	ipamZone := httpRoute.Annotations[annotations.AnnotationIPAMZone]
	if ipamZone == "" {
		ipamZone = DefaultIPAMZone
		log.V(1).Info("No IPAM zone annotation found, using default", "ipamZone", ipamZone)
	}

	// Get ip-family from HTTProute or use the appropriate default for zone
	ipFamily := httpRoute.Annotations[annotations.AnnotationIpFamily]
	if ipFamily == "" {
		if ipamZone == InetIPAMZone {
			ipFamily = DefaultInetIpFamily
		} else {
			ipFamily = DefaultHnetIpFamily
		}
		log.V(1).Info("No ip-family annotation found, using default for zone", "ipFamily", ipFamily, "ipamZone", ipamZone)
	}
	// Get cluster issuer from annotation or use default
	clusterIssuer := httpRoute.Annotations[annotations.AnnotationClusterIssuer]
	if clusterIssuer == "" {
		clusterIssuer = DefaultClusterIssuer
		log.V(1).Info("No cluster issuer annotation found, using default", "clusterIssuer", clusterIssuer)
	}

	// Ensure correct listeners for this HTTPRoute
	if isListenerSetMode {
		if err := r.reconcileListenerSetMode(ctx, listenerSetName, gatewayNamespace, ipamZone, ipFamily, clusterIssuer); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.ensureGateway(ctx, gatewayName, gatewayNamespace, ipamZone, ipFamily, clusterIssuer); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// updateOldGateway reconciles the old gateway when an HTTPRoute changes its parentRef.
// It re-evaluates remaining listeners and either updates the ListenerSet or deletes
// the gateway, ListenerSet, and ClientTrafficPolicy when nothing remains.
func (r *HTTPRouteReconciler) updateOldGateway(ctx context.Context, gatewayRef string) error {
	log := logf.FromContext(ctx)

	// Parse gateway reference (format: namespace/name)
	var gatewayNamespace, gatewayName string
	for i, ch := range gatewayRef {
		if ch == '/' {
			gatewayNamespace = gatewayRef[:i]
			gatewayName = gatewayRef[i+1:]
			break
		}
	}

	if gatewayNamespace == "" || gatewayName == "" {
		log.Error(nil, "Invalid gateway reference format", "gatewayRef", gatewayRef)
		return nil // Don't fail reconciliation for invalid format
	}

	// Get the old gateway
	var gateway gatewayv1.Gateway
	gatewayKey := client.ObjectKey{
		Name:      gatewayName,
		Namespace: gatewayNamespace,
	}

	if err := r.Get(ctx, gatewayKey, &gateway); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Gateway is already gone; clean up any orphaned CTP.
			log.Info("Old gateway not found, cleaning up ClientTrafficPolicy", "gateway", gatewayRef)
			if ctpErr := r.deleteClientTrafficPolicy(ctx, gatewayName, gatewayNamespace); ctpErr != nil {
				log.Error(ctpErr, "Failed to delete ClientTrafficPolicy for missing old gateway", "gateway", gatewayRef)
				return ctpErr
			}
			return nil
		}
		return err
	}

	// Delegate to updateGatewayListeners which handles the full lifecycle:
	// re-collect listeners → update ListenerSet → update Gateway stub → delete all if empty.
	if _, err := r.updateGatewayListeners(ctx, &gateway, gatewayNamespace); err != nil {
		log.Error(err, "Failed to update old gateway listeners", "gateway", gatewayRef)
		return err
	}
	log.Info("Updated old gateway", "gateway", gatewayRef)
	return nil
}

// reconcileListenerSetMode syncs the listeners of a user-defined ListenerSet based on all
// HTTPRoutes that reference it (parentRef.kind=ListenerSet). The ListenerSet must already
// exist — the operator never creates or deletes user-defined ListenerSets.
func (r *HTTPRouteReconciler) reconcileListenerSetMode(
	ctx context.Context,
	listenerSetName, listenerSetNamespace string,
	ipamZone, ipFamily, clusterIssuer string,
) error {
	log := logf.FromContext(ctx)

	listeners, ignoreDns, overrideInfra, overrideTtl, err := r.collectListenersForListenerSet(ctx, listenerSetName, listenerSetNamespace)
	if err != nil {
		return err
	}

	if len(listeners) == 0 {
		log.V(1).Info("No active routes for ListenerSet; nothing to update", "listenerSet", listenerSetName)
		return nil
	}

	if err := r.updateUserListenerSet(ctx, listenerSetName, listenerSetNamespace, listeners, ignoreDns, overrideInfra, overrideTtl, clusterIssuer); err != nil {
		log.Error(err, "Failed to update ListenerSet", "listenerSet", listenerSetName)
		return err
	}
	return nil
}

// handleHTTPRouteDeletionListenerSet re-syncs a user-defined ListenerSet after an HTTPRoute
// that referenced it is deleted. The ListenerSet itself is never deleted by the operator.
func (r *HTTPRouteReconciler) handleHTTPRouteDeletionListenerSet(
	ctx context.Context,
	listenerSetName, listenerSetNamespace string,
) error {
	log := logf.FromContext(ctx)

	listeners, ignoreDns, overrideInfra, overrideTtl, err := r.collectListenersForListenerSet(ctx, listenerSetName, listenerSetNamespace)
	if err != nil {
		return err
	}

	if len(listeners) == 0 {
		log.Info("No routes remain for ListenerSet after deletion; leaving it unchanged (user-owned)", "listenerSet", listenerSetName)
		return nil
	}

	// Pass "" for clusterIssuer: on deletion we don't have a resolved issuer for the remaining
	// routes, so preserve whatever is already on the ListenerSet. The next normal reconcile
	// of any remaining route will write the correct value.
	return r.updateUserListenerSet(ctx, listenerSetName, listenerSetNamespace, listeners, ignoreDns, overrideInfra, overrideTtl, "")
}

// handleHTTPRouteDeletion updates gateway listeners when an HTTPRoute is deleted
func (r *HTTPRouteReconciler) handleHTTPRouteDeletion(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
) error {
	log := logf.FromContext(ctx)

	// Get the gateway to update its listeners
	var gateway gatewayv1.Gateway
	gatewayKey := client.ObjectKey{
		Name:      gatewayName,
		Namespace: gatewayNamespace,
	}

	if err := r.Get(ctx, gatewayKey, &gateway); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Gateway doesn't exist, nothing to update
			log.Info("Gateway doesn't exist, nothing to update", "gateway", gatewayName)
			return nil
		}
		log.Error(err, "Failed to get Gateway")
		return err
	}

	// Update gateway listeners to exclude the deleted route's hostnames
	// Server-Side Apply will handle any conflicts automatically
	if _, err := r.updateGatewayListeners(ctx, &gateway, gatewayNamespace); err != nil {
		log.Error(err, "Failed to update Gateway listeners after HTTPRoute deletion")
		return err
	}

	log.Info("Successfully updated Gateway after HTTPRoute deletion", "gateway", gatewayName)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}).
		Named("httproute").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}
