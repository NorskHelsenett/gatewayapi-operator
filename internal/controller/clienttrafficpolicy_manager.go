package controller

import (
	"context"
	goerrors "errors"
	"os"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// clientTrafficPolicyPQCECDHCurves defines the PQC-prioritised ECDH curves applied
// to the Gateway via a ClientTrafficPolicy. The order matters: X25519MLKEM768 is
// preferred (post-quantum hybrid), followed by the classical fallbacks.
var clientTrafficPolicyPQCECDHCurves = []string{
	"X25519MLKEM768",
	"X25519",
	"P-256",
}

// ctpEnabled reports whether ClientTrafficPolicy creation is enabled via the
// ENABLE_CLIENTTRAFFICPOLICY_PQC environment variable. CTP management is
// disabled only when the variable is explicitly set to "false"; unset (the
// default) or any other value is treated as enabled.
func ctpEnabled() bool {
	return os.Getenv(enableClientTrafficPolicyPQCEnvVar) != "false"
}

// ensureClientTrafficPolicy reconciles the ClientTrafficPolicy for the given Gateway.
//
// When ENABLE_CLIENTTRAFFICPOLICY_PQC is unset or set to any value other than "false",
// the CTP is created/updated with the configured PQC ECDH curves.
//
// When the flag is explicitly set to "false" any previously-created CTP is
// deleted so that disabling the flag is sufficient to clean up all
// operator-managed CTPs. This makes the removal path easy: flip the env var,
// and every gateway reconcile will delete its CTP. The function is safe to
// call on clusters that do not have the ClientTrafficPolicy CRD installed.
//
// gateway must be the live object (UID populated) so that an OwnerReference can
// be set without re-fetching from the potentially stale informer cache.
func (r *HTTPRouteReconciler) ensureClientTrafficPolicy(
	ctx context.Context,
	gateway *gwapiv1.Gateway,
) error {
	gatewayName := gateway.Name
	gatewayNamespace := gateway.Namespace

	if !ctpEnabled() {
		// Cleanup mode: remove any CTP we may have created previously.
		return r.deleteClientTrafficPolicy(ctx, gatewayName, gatewayNamespace)
	}

	log := logf.FromContext(ctx)
	ctpName := gatewayName + clientTrafficPolicyNameSuffix

	desired := &egv1a1.ClientTrafficPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.envoyproxy.io/v1alpha1",
			Kind:       "ClientTrafficPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctpName,
			Namespace: gatewayNamespace,
		},
		Spec: egv1a1.ClientTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{
				TargetRefs: []gwapiv1.LocalPolicyTargetReferenceWithSectionName{
					{
						LocalPolicyTargetReference: gwapiv1.LocalPolicyTargetReference{
							Group: "gateway.networking.k8s.io",
							Kind:  "Gateway",
							Name:  gwapiv1.ObjectName(gatewayName),
						},
					},
				},
			},
			TLS: &egv1a1.ClientTLSSettings{
				TLSSettings: egv1a1.TLSSettings{
					ECDHCurves: clientTrafficPolicyPQCECDHCurves,
				},
			},
		},
	}

	// Set the Gateway as the controller owner so Kubernetes GC cleans up the CTP
	// when the Gateway is deleted.
	if err := controllerutil.SetControllerReference(gateway, desired, r.Scheme); err != nil {
		log.Error(err, "Failed to set OwnerReference on ClientTrafficPolicy", "name", ctpName)
		return err
	}

	existing := &egv1a1.ClientTrafficPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: ctpName, Namespace: gatewayNamespace}, existing)
	if err != nil {
		if apimeta.IsNoMatchError(err) {
			// CRD not installed on this cluster – skip silently.
			log.V(1).Info("ClientTrafficPolicy CRD not available, skipping", "gateway", gatewayName)
			return nil
		}
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to get ClientTrafficPolicy", "name", ctpName)
			return err
		}
		// Not found – create it.
		log.Info("Creating ClientTrafficPolicy", "name", ctpName, "namespace", gatewayNamespace)
		if createErr := r.Create(ctx, desired); createErr != nil {
			if apimeta.IsNoMatchError(createErr) {
				log.V(1).Info("ClientTrafficPolicy CRD not available, skipping create", "gateway", gatewayName)
				return nil
			}
			if errors.IsAlreadyExists(createErr) {
				// A concurrent reconcile created the CTP between our Get and Create.
				// Treat this as success; the next reconcile will converge the spec if needed.
				log.V(1).Info("ClientTrafficPolicy already exists (concurrent create), skipping", "name", ctpName)
				return nil
			}
			log.Error(createErr, "Failed to create ClientTrafficPolicy", "name", ctpName)
			return createErr
		}
		log.Info("Created ClientTrafficPolicy", "name", ctpName, "namespace", gatewayNamespace)
		return nil
	}

	// Already exists – update if ECDH curves, target references, or OwnerReference differ.
	ownerRefMissing := !hasControllerOwnerRef(existing, gateway.UID)
	specUpToDate := existing.Spec.TLS != nil &&
		stringSlicesEqual(existing.Spec.TLS.ECDHCurves, clientTrafficPolicyPQCECDHCurves) &&
		targetRefsEqual(existing.Spec.PolicyTargetReferences.TargetRefs, desired.Spec.PolicyTargetReferences.TargetRefs)
	if !ownerRefMissing && specUpToDate {
		log.V(1).Info("ClientTrafficPolicy up-to-date, skipping update", "name", ctpName)
		return nil
	}

	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch to get the latest resourceVersion before each attempt.
		latest := &egv1a1.ClientTrafficPolicy{}
		if getErr := r.Get(ctx, types.NamespacedName{Name: ctpName, Namespace: gatewayNamespace}, latest); getErr != nil {
			return getErr
		}
		latest.Spec.TLS = desired.Spec.TLS
		latest.Spec.PolicyTargetReferences = desired.Spec.PolicyTargetReferences
		if !hasControllerOwnerRef(latest, gateway.UID) {
			if err := controllerutil.SetControllerReference(gateway, latest, r.Scheme); err != nil {
				// AlreadyOwnedError is not a conflict; RetryOnConflict will not retry it.
				// Return it as-is so the caller can handle it below.
				return err
			}
		}
		return r.Update(ctx, latest)
	})
	if updateErr != nil {
		var alreadyOwned *controllerutil.AlreadyOwnedError
		if goerrors.As(updateErr, &alreadyOwned) {
			// The CTP is controlled by a stale Gateway (deleted and recreated with a new
			// UID). Delete it so the next reconcile creates a fresh one owned by the
			// current Gateway. Kubernetes GC would eventually clean this up anyway, but
			// deleting eagerly avoids repeated reconcile failures.
			log.Info("ClientTrafficPolicy owned by stale controller, deleting for recreation",
				"name", ctpName, "owner", alreadyOwned.Owner.Name)
			stale := &egv1a1.ClientTrafficPolicy{}
			if getErr := r.Get(ctx, types.NamespacedName{Name: ctpName, Namespace: gatewayNamespace}, stale); getErr != nil {
				if errors.IsNotFound(getErr) {
					return nil // already gone
				}
				log.Error(getErr, "Failed to get stale ClientTrafficPolicy for deletion", "name", ctpName)
				return getErr
			}
			if delErr := r.Delete(ctx, stale); delErr != nil && !errors.IsNotFound(delErr) {
				log.Error(delErr, "Failed to delete stale ClientTrafficPolicy", "name", ctpName)
				return delErr
			}
			log.Info("Deleted stale ClientTrafficPolicy; will recreate on next reconcile", "name", ctpName)
			return nil
		}
		log.Error(updateErr, "Failed to update ClientTrafficPolicy", "name", ctpName)
		return updateErr
	}
	log.Info("Updated ClientTrafficPolicy", "name", ctpName, "namespace", gatewayNamespace)
	return nil
}

// deleteClientTrafficPolicy removes the ClientTrafficPolicy associated with a
// Gateway. It is safe to call when the CRD is not installed.
func (r *HTTPRouteReconciler) deleteClientTrafficPolicy(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
) error {
	log := logf.FromContext(ctx)
	ctpName := gatewayName + clientTrafficPolicyNameSuffix

	ctp := &egv1a1.ClientTrafficPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: ctpName, Namespace: gatewayNamespace}, ctp)
	if err != nil {
		if apimeta.IsNoMatchError(err) || errors.IsNotFound(err) {
			return nil
		}
		log.Error(err, "Failed to get ClientTrafficPolicy for deletion", "name", ctpName)
		return err
	}

	if delErr := r.Delete(ctx, ctp); delErr != nil {
		if apimeta.IsNoMatchError(delErr) || errors.IsNotFound(delErr) {
			return nil
		}
		log.Error(delErr, "Failed to delete ClientTrafficPolicy", "name", ctpName)
		return delErr
	}

	log.Info("Deleted ClientTrafficPolicy", "name", ctpName, "namespace", gatewayNamespace)
	return nil
}

// hasControllerOwnerRef reports whether obj already carries a controller OwnerReference
// with the given UID, indicating the relationship was previously established.
func hasControllerOwnerRef(obj metav1.Object, uid types.UID) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid && ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

// stringSlicesEqual returns true if both slices contain identical elements in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// targetRefsEqual returns true if both slices of LocalPolicyTargetReferenceWithSectionName
// contain the same references (Group, Kind, Name, SectionName) in the same order.
func targetRefsEqual(a, b []gwapiv1.LocalPolicyTargetReferenceWithSectionName) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Group != b[i].Group ||
			a[i].Kind != b[i].Kind ||
			a[i].Name != b[i].Name ||
			a[i].SectionName != b[i].SectionName {
			return false
		}
	}
	return true
}
