package controller

import (
	"context"

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
	if httpRoute.Annotations[AnnotationUseHttprouteOperator] != "true" {
		log.Info("Skipping HTTPRoute - operator not enabled", "name", httpRoute.Name, "namespace", httpRoute.Namespace)
		return ctrl.Result{}, nil
	}

	// Validate that we have parent refs
	if len(httpRoute.Spec.ParentRefs) == 0 {
		log.Error(nil, "HTTPRoute has no parent references", "name", httpRoute.Name)
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling HTTPRoute", "name", httpRoute.Name, "namespace", httpRoute.Namespace)

	// Extract gateway information from first parent ref
	// TODO: Support multiple parent refs in the future
	gatewayName := string(httpRoute.Spec.ParentRefs[0].Name)
	gatewayNamespace := httpRoute.Namespace
	if httpRoute.Spec.ParentRefs[0].Namespace != nil {
		gatewayNamespace = string(*httpRoute.Spec.ParentRefs[0].Namespace)
	}

	// Handle deletion - update gateway listeners to remove this route's hostnames
	if !httpRoute.DeletionTimestamp.IsZero() {
		log.Info("HTTPRoute is being deleted, updating gateway listeners", "name", httpRoute.Name)

		// Check if finalizer is present
		if controllerutil.ContainsFinalizer(&httpRoute, httprouteFinalizerName) {
			// Update gateway to remove this route's listeners
			if err := r.handleHTTPRouteDeletion(ctx, gatewayName, gatewayNamespace); err != nil {
				log.Error(err, "Failed to handle HTTPRoute deletion")
				return ctrl.Result{}, err
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
					log.V(1).Info("Finalizer already removed", "name", httpRoute.Name)
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
				log.V(1).Info("HTTPRoute already deleted", "name", httpRoute.Name)
			} else {
				log.Info("Removed finalizer from HTTPRoute", "name", httpRoute.Name)
			}
		}

		return ctrl.Result{}, nil
	}

	// Check if gateway reference has changed
	currentGatewayRef := gatewayNamespace + "/" + gatewayName
	previousGatewayRef := httpRoute.Annotations[previousGatewayAnnotationKey]

	if previousGatewayRef != "" && previousGatewayRef != currentGatewayRef {
		log.Info("Gateway reference changed, updating old gateway", "oldGateway", previousGatewayRef, "newGateway", currentGatewayRef)

		// Parse old gateway namespace and name
		if err := r.updateOldGateway(ctx, previousGatewayRef); err != nil {
			log.Error(err, "Failed to update old gateway listeners", "gateway", previousGatewayRef)
			// Continue with reconciliation even if old gateway update fails
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
	if httpRoute.Annotations[previousGatewayAnnotationKey] != currentGatewayRef {
		httpRoute.Annotations[previousGatewayAnnotationKey] = currentGatewayRef
		needsUpdate = true
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
		if err := r.Patch(ctx, patch, client.Apply, client.ForceOwnership, client.FieldOwner("gatewayapi-operator")); err != nil {
			log.Error(err, "Failed to update HTTPRoute annotations")
			return ctrl.Result{}, err
		}
		log.Info("Updated HTTPRoute annotations", "name", httpRoute.Name)
	}

	// Get IPAM zone from annotation or use default
	ipamZone := httpRoute.Annotations[AnnotationIPAMZone]
	if ipamZone == "" {
		ipamZone = defaultIPAMZone
		log.Info("No IPAM zone annotation found, using default", "ipamZone", ipamZone)
	}

	// Get cluster issuer from annotation or use default
	clusterIssuer := httpRoute.Annotations[AnnotationClusterIssuer]
	if clusterIssuer == "" {
		clusterIssuer = defaultClusterIssuer
		log.Info("No cluster issuer annotation found, using default", "clusterIssuer", clusterIssuer)
	}

	// Ensure the Gateway exists and has correct listeners
	if err := r.ensureGateway(ctx, gatewayName, gatewayNamespace, ipamZone, clusterIssuer); err != nil {
		log.Error(err, "Failed to ensure Gateway")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// updateOldGateway updates the listeners on the old gateway when HTTPRoute changes gateways
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
			// Gateway doesn't exist anymore, nothing to update
			return nil
		}
		return err
	}

	// Collect listeners for the old gateway (excluding routes that no longer reference it)
	listeners, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
	if err != nil {
		return err
	}

	// If no listeners remain, delete the gateway instead of updating with empty listeners
	if len(listeners) == 0 {
		log.Info("No HTTPRoutes reference this gateway anymore, deleting it", "gateway", gatewayRef)
		if err := r.Delete(ctx, &gateway); err != nil {
			return err
		}
		log.Info("Deleted old gateway", "gateway", gatewayRef)
		return nil
	}

	// Use Server-Side Apply to update listeners
	// Include gatewayClassName since it's a required field
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
			Listeners:        listeners,
		},
	}

	err = r.Patch(ctx, patch, client.Apply, client.ForceOwnership, client.FieldOwner("gatewayapi-operator"))
	if err != nil {
		return err
	}

	log.Info("Updated old gateway listeners", "gateway", gatewayRef, "listeners", len(listeners))
	return nil
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
	if err := r.updateGatewayListeners(ctx, &gateway, gatewayNamespace); err != nil {
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
