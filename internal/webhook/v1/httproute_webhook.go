/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"

	"github.com/NorskHelsenett/gatewayapi-operator/internal/annotations"
	"github.com/NorskHelsenett/gatewayapi-operator/internal/webhook/v1/validations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var httproutelog = logf.Log.WithName("httproute-resource")

// SetupHTTPRouteWebhookWithManager registers the webhook for HTTPRoute in the manager.
func SetupHTTPRouteWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &gatewayv1.HTTPRoute{}).
		WithValidator(&HTTPRouteCustomValidator{Client: mgr.GetClient()}).
		WithValidatorCustomPath("/gatewayapi-operator-httproute-validator").
		Complete()
}

// +kubebuilder:webhook:path=/gatewayapi-operator-httproute-validator,mutating=false,failurePolicy=fail,sideEffects=None,groups=gateway.networking.k8s.io,resources=httproutes,verbs=create;update,versions=v1,name=vhttproute-v1.kb.io,admissionReviewVersions=v1

// HTTPRouteCustomValidator struct is responsible for validating the HTTPRoute resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type HTTPRouteCustomValidator struct {
	client.Client
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type HTTPRoute.
func (v *HTTPRouteCustomValidator) ValidateCreate(ctx context.Context, httproute *gatewayv1.HTTPRoute) (admission.Warnings, error) {

	// We don't want to act on HTTPRoutes created without the use of this operator.
	if httproute.ObjectMeta.Annotations[annotations.AnnotationUseHttprouteOperator] != "true" {
		return nil, nil
	}

	httproutelog.Info("Validation for HTTPRoute upon creation", "name", httproute.GetName())
	ctx = logf.IntoContext(ctx, httproutelog)

	// First fetch gateway if it already exists
	referredGateway, err := v.GetReferredGateway(ctx, httproute)
	if err != nil {
		return nil, err
	}

	// Validate that IPAM Zone is identical on HTTPRoute and Gateway
	err = validations.ValidateZone(httproute, referredGateway)

	if err != nil {
		return nil, err
	}

	// Validate that Issuer is identical on HTTPRoute and Gateway
	err = validations.ValidateIssuer(httproute, referredGateway)

	if err != nil {
		return nil, err
	}

	// Validate that ip-family is the same on HTTPRoute and Gateway
	err = validations.ValidateIPFamily(httproute, referredGateway)

	if err != nil {
		return nil, err
	}

	return nil, nil

}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type HTTPRoute.
func (v *HTTPRouteCustomValidator) ValidateUpdate(ctx context.Context, _, httproute *gatewayv1.HTTPRoute) (admission.Warnings, error) {

	// We don't want to act on HTTPRoutes not managed by this operator.
	if httproute.ObjectMeta.Annotations[annotations.AnnotationUseHttprouteOperator] != "true" {
		return nil, nil
	}

	httproutelog.Info("Validation for HTTPRoute upon update", "name", httproute.GetName())
	ctx = logf.IntoContext(ctx, httproutelog)

	referredGateway, err := v.GetReferredGateway(ctx, httproute)
	if err != nil {
		return nil, err
	}

	// Validate that IPAM Zone is identical on HTTPRoute and Gateway
	err = validations.ValidateZone(httproute, referredGateway)

	if err != nil {
		return nil, err
	}

	// Validate that Issuer is identical on HTTPRoute and Gateway
	err = validations.ValidateIssuer(httproute, referredGateway)

	if err != nil {
		return nil, err
	}

	// Validate that ip-family is the same on HTTPRoute and Gateway
	err = validations.ValidateIPFamily(httproute, referredGateway)

	if err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type HTTPRoute.
func (v *HTTPRouteCustomValidator) ValidateDelete(_ context.Context, httproute *gatewayv1.HTTPRoute) (admission.Warnings, error) {
	httproutelog.Info("Validation for HTTPRoute upon deletion", "name", httproute.GetName())

	return nil, nil
}
