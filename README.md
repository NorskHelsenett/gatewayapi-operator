# gatewayapi-operator

Automatically manages Gateway resources based on HTTPRoute configurations.

## Features
- Creates and updates Gateways with HTTPS listeners for each HTTPRoute
- Supports IPAM zone configuration via annotations
- Automatic TLS certificate integration with cert-manager

## How It Works
1. HTTPRoutes with `gatewayapi-operator.vitistack.io/enabled: "true"` annotation are watched
2. Gateway is created/updated with HTTPS listeners for each hostname in the HTTPRoute
3. Listeners reference TLS certificates in format: `{hostname}-tls`
4. Gateway is deleted when no HTTPRoutes reference it anymore

## Demo

```bash
❯ cat httproute.yaml | grep parentRefs -A 3
  parentRefs:
    - name: hnet-private-argo
      sectionName: argo.example.com
      namespace: argocd
    
❯ k get gateway -A
No resources found
❯ k get httproute -A
No resources found

❯ k apply -f httproute.yaml
httproute.gateway.networking.k8s.io/operator2-test-https created

❯ k get gateway -A
NAMESPACE   NAME                CLASS   ADDRESS   PROGRAMMED   AGE
argocd      hnet-private-argo   eg                False        3s

❯ k get gateway -A -o yaml | grep allowedRoutes -A 10
    - allowedRoutes:
        namespaces:
          from: All
      hostname: argo.example.com
      name: argo.example.com
      port: 443
      protocol: HTTPS
      tls:
        certificateRefs:
        - group: ""
          kind: Secret
```

## Configuration

### HTTPRoute Annotations
- `gatewayapi-operator.vitistack.io/enabled: "true"` - Required to enable operator management
- `gatewayapi-operator.vitistack.io/cluster-issuer` - cert-manager cluster issuer (default: `internpki`)
- `ipam.vitistack.io/zone` - IPAM zone for gateway (default: `hnet-private`)


## Be aware
1. Multiple httproutes with differemt cluster-issuer annotation referencing the same gateway is not possible. Create a new gateway per cluster-issuer
2. Multiple httproutes with different ipam.vitistack.io/zone annotation is not possible. Create a new gateway per IPAM zone.
3. Redirect and BackendTLSPolicy must be configured manually. It is not supported yet.


### Configuring redirect:
https://gateway.envoyproxy.io/docs/tasks/traffic/http-redirect/

### Configuring BackendTLSPolicy
https://gateway.envoyproxy.io/docs/api/gateway_api/backendtlspolicy/


# License

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
