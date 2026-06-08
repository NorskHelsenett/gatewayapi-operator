package controller

import (
	"context"
	"os"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
// ENABLE_CLIENTTRAFFICPOLICY_PQC environment variable.
func ctpEnabled() bool {
	return os.Getenv(enableClientTrafficPolicyPQCEnvVar) != "false"
}

// ensureClientTrafficPolicy reconciles the ClientTrafficPolicy for the given Gateway.
//
// When ENABLE_CLIENTTRAFFICPOLICY_PQC=true the CTP is created/updated with the
// configured PQC ECDH curves.
//
// When the flag is false (or unset) any previously-created CTP is deleted so
// that disabling the flag is sufficient to clean up all operator-managed CTPs.
// This makes the removal path easy: flip the env var, and every gateway
// reconcile will delete its CTP. The function is safe to call on clusters that
// do not have the ClientTrafficPolicy CRD installed.
func (r *HTTPRouteReconciler) ensureClientTrafficPolicy(
	ctx context.Context,
	gatewayName, gatewayNamespace string,
) error {
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
			log.Error(createErr, "Failed to create ClientTrafficPolicy", "name", ctpName)
			return createErr
		}
		log.Info("Created ClientTrafficPolicy", "name", ctpName, "namespace", gatewayNamespace)
		return nil
	}

	// Already exists – update only if ECDH curves differ to avoid unnecessary writes.
	if existing.Spec.TLS != nil && stringSlicesEqual(existing.Spec.TLS.ECDHCurves, clientTrafficPolicyPQCECDHCurves) {
		log.V(1).Info("ClientTrafficPolicy up-to-date, skipping update", "name", ctpName)
		return nil
	}

	existing.Spec.TLS = desired.Spec.TLS
	existing.Spec.PolicyTargetReferences = desired.Spec.PolicyTargetReferences
	if updateErr := r.Update(ctx, existing); updateErr != nil {
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
	if !ctpEnabled() {
		return nil
	}

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
