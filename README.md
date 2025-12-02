# gatewayapi-operator

Automatically manages Gateway resources based on HTTPRoute configurations.

## Features
- Creates and updates Gateways with HTTPS listeners for each HTTPRoute
- Supports IPAM zone configuration via annotations
- Automatic TLS certificate integration with cert-manager

## How It Works
1. HTTPRoutes with `gatewayapi-operator/enabled: "true"` annotation are watched
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
- `gatewayapi-operator/enabled: "true"` - Required to enable operator management
- `gatewayapi-operator/cluster-issuer` - cert-manager cluster issuer (default: `internpki`)
- `ipam.vitistack.io/zone` - IPAM zone for gateway (default: `hnet-private`)

