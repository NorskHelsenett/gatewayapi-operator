package controller

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
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
	ipFamily string,
	clusterIssuer string,
	ipamAddresses string,
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
			created, err := r.createGateway(ctx, gatewayName, gatewayNamespace, ipamZone, ipFamily, clusterIssuer, ipamAddresses)
			if err != nil {
				return err
			}
			if created == nil {
				// Gateway no longer exists (e.g. it was deleted concurrently); nothing more to do.
				return nil
			}

			// Pass the just-created Gateway object directly so ensureClientTrafficPolicy
			// can set an OwnerReference using the live UID without re-fetching via the
			// informer cache (which may lag immediately after Create()).
			return r.ensureClientTrafficPolicy(ctx, created)
		}
		log.Error(err, "Failed to get Gateway", "gateway", gatewayName)
		return err
	}

	// Gateway exists, validate cluster issuer matches
	existingIssuer := gateway.Annotations[clusterIssuerAnnotation]
	if existingIssuer != clusterIssuer {
		return errors.NewBadRequest("HTTPRoute cluster issuer mismatch: Gateway has issuer '" + existingIssuer + "' but HTTPRoute requires '" + clusterIssuer + "'")
	}

	// Gateway exists, validate IPAM settings match
	gatewayInfraAddresses := ""
	if gateway.Spec.Infrastructure != nil && gateway.Spec.Infrastructure.Annotations != nil {
		if existingZone, exists := gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone]; exists {
			if string(existingZone) != ipamZone {
				return errors.NewBadRequest("HTTPRoute IPAM zone mismatch: Gateway has zone '" + string(existingZone) + "' but HTTPRoute requires '" + ipamZone + "'")
			}
		}
		if existingFamily, exists := gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIpFamily]; exists {
			if string(existingFamily) != ipFamily {
				return errors.NewBadRequest("HTTPRoute IPAM ip-family mismatch: Gateway has ip-family '" + string(existingFamily) + "' but HTTPRoute requires '" + ipFamily + "'")
			}
		}
		gatewayInfraAddresses = string(gateway.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMAddresses])
	}
	if ipamAddresses != gatewayInfraAddresses {
		if gatewayInfraAddresses == "" {
			return errors.NewBadRequest("HTTPRoute specifies IPAM addresses '" + ipamAddresses + "' but Gateway was not created with an addresses annotation; all routes on this gateway must omit it")
		}
		if ipamAddresses == "" {
			return errors.NewBadRequest("Gateway has IPAM addresses '" + gatewayInfraAddresses + "'; all routes on this gateway must set annotation " + string(annotations.AnnotationIPAMAddresses))
		}
		return errors.NewBadRequest("HTTPRoute IPAM addresses mismatch: Gateway has addresses '" + gatewayInfraAddresses + "' but HTTPRoute requires '" + ipamAddresses + "'")
	}

	// Gateway exists and configuration matches, update listeners
	log.V(1).Info("Gateway exists, updating listeners", "gateway", gatewayName, "namespace", gatewayNamespace)
	deleted, err := r.updateGatewayListeners(ctx, gateway, gatewayNamespace)
	if err != nil {
		return err
	}
	if deleted {
		// Gateway (and its CTP) were deleted because no listeners remain; nothing more to do.
		return nil
	}
	return r.ensureClientTrafficPolicy(ctx, gateway)
}

// createGateway creates a new Gateway resource with initial configuration.
// It returns the created Gateway object (populated in-place by the API server)
// so callers can use it without re-fetching from the potentially stale cache.
func (r *HTTPRouteReconciler) createGateway(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
	ipamZone string,
	ipFamily string,
	clusterIssuer string,
	ipamAddresses string,
) (*gatewayv1.Gateway, error) {
	log := logf.FromContext(ctx)

	// Collect all listeners from HTTPRoutes that reference this gateway
	listeners, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation, err := r.collectListenersForGateway(ctx, gatewayName, gatewayNamespace)
	if err != nil {
		log.Error(err, "Failed to collect listeners for new Gateway")
		return nil, err
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
			Listeners: listeners,
			Infrastructure: &gatewayv1.GatewayInfrastructure{
				Annotations: buildInfrastructureAnnotations(ipamZone, ipFamily, ipamAddresses),
			},
		},
	}

	UpdateGatewayAnnotations(ctx, newGateway, ignoreDnsUpdatesAnnoation, overrideinfrastructureAnnoation, overrideTtlAnnotation)

	UpdateGatewayClass(newGateway)

	if err := r.Create(ctx, newGateway); err != nil {
		if errors.IsAlreadyExists(err) {
			// Another reconcile created the gateway concurrently; fetch, validate, and update it
			log.Info("Gateway already exists (concurrent create), validating and updating listeners", "gateway", gatewayName)
			existing := &gatewayv1.Gateway{}
			if getErr := r.Get(ctx, types.NamespacedName{Name: gatewayName, Namespace: gatewayNamespace}, existing); getErr != nil {
				log.Error(getErr, "Failed to get Gateway after concurrent create", "gateway", gatewayName)
				return nil, getErr
			}
			// Validate annotations on HTTPRoute and Gateway
			existingIssuer := existing.Annotations[clusterIssuerAnnotation]
			if existingIssuer != clusterIssuer {
				return nil, errors.NewBadRequest("HTTPRoute cluster issuer mismatch: Gateway has issuer '" + existingIssuer + "' but HTTPRoute requires '" + clusterIssuer + "'")
			}

			// Validate IPAM zone, ip-family, and addresses match
			concurrentGatewayAddresses := ""
			if existing.Spec.Infrastructure != nil && existing.Spec.Infrastructure.Annotations != nil {
				if existingZone, exists := existing.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMZone]; exists {
					if string(existingZone) != ipamZone {
						return nil, errors.NewBadRequest("HTTPRoute IPAM zone mismatch: Gateway has zone '" + string(existingZone) + "' but HTTPRoute requires '" + ipamZone + "'")
					}
				}
				if existingFamily, exists := existing.Spec.Infrastructure.Annotations[annotations.AnnotationIpFamily]; exists {
					if string(existingFamily) != ipFamily {
						return nil, errors.NewBadRequest("HTTPRoute IPAM ip-family mismatch: Gateway has ip-family '" + string(existingFamily) + "' but HTTPRoute requires '" + ipFamily + "'")
					}
				}
				concurrentGatewayAddresses = string(existing.Spec.Infrastructure.Annotations[annotations.AnnotationIPAMAddresses])
			}
			if ipamAddresses != concurrentGatewayAddresses {
				if concurrentGatewayAddresses == "" {
					return nil, errors.NewBadRequest("HTTPRoute specifies IPAM addresses '" + ipamAddresses + "' but Gateway was not created with an addresses annotation; all routes on this gateway must omit it")
				}
				if ipamAddresses == "" {
					return nil, errors.NewBadRequest("Gateway has IPAM addresses '" + concurrentGatewayAddresses + "'; all routes on this gateway must set annotation " + string(annotations.AnnotationIPAMAddresses))
				}
				return nil, errors.NewBadRequest("HTTPRoute IPAM addresses mismatch: Gateway has addresses '" + concurrentGatewayAddresses + "' but HTTPRoute requires '" + ipamAddresses + "'")
			}

			deleted, err := r.updateGatewayListeners(ctx, existing, gatewayNamespace)
			if err != nil {
				return nil, err
			}
			if deleted {
				// No listeners remain; gateway and CTP were deleted. Signal to ensureGateway
				// that no further CTP work is needed.
				return nil, nil
			}
			return existing, nil
		}
		log.Error(err, "Failed to create Gateway", "gateway", gatewayName)
		return nil, err
	}

	log.Info("Successfully created Gateway", "gateway", gatewayName, "namespace", gatewayNamespace, "listeners", len(listeners))
	return newGateway, nil
}

// buildInfrastructureAnnotations constructs the Gateway.Spec.Infrastructure.Annotations map.
// ipamAddresses is optional; it is omitted when empty.
func buildInfrastructureAnnotations(ipamZone, ipFamily, ipamAddresses string) map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue {
	m := map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
		annotations.AnnotationIPAMZone: gatewayv1.AnnotationValue(ipamZone),
		annotations.AnnotationIpFamily: gatewayv1.AnnotationValue(ipFamily),
	}
	if ipamAddresses != "" {
		m[annotations.AnnotationIPAMAddresses] = gatewayv1.AnnotationValue(ipamAddresses)
	}
	return m
}
