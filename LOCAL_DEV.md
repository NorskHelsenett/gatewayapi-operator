# Local Development

## Running the operator locally against a cluster

You can run the operator locally with `make run` while pointing it at a remote or local cluster. The instructions below are for a **Talos-in-Docker** cluster managed by OrbStack, but the same approach works for any cluster whose nodes run as Docker containers.

### Prerequisites

- OrbStack (provides `host.internal` DNS that resolves to your Mac from inside Docker containers)
- `kubectl` configured to point at your target cluster
- `openssl`

### One-time setup: TLS certificates and webhook configuration

The webhook server requires a TLS certificate. The Kubernetes API server must trust the CA that signed it, and the certificate must have a SAN matching the address the API server uses to reach your machine.

From inside Docker containers on the cluster's network, your Mac is reachable at `host.internal`.

**1. Find the Docker network your cluster uses:**

```sh
docker ps --filter name=<cluster-name> --format '{{.Names}} {{.Networks}}'
# e.g. devtest2-controlplane-1 devtest2
```

**2. Generate a CA and server certificate:**

```sh
mkdir -p /tmp/webhook-certs

# CA
openssl genrsa -out /tmp/webhook-certs/ca.key 4096
openssl req -x509 -new -nodes -key /tmp/webhook-certs/ca.key -days 365 \
  -out /tmp/webhook-certs/ca.crt -subj "/CN=webhook-ca"

# Server key + CSR
openssl genrsa -out /tmp/webhook-certs/tls.key 4096
openssl req -new -key /tmp/webhook-certs/tls.key \
  -out /tmp/webhook-certs/tls.csr -subj "/CN=webhook-server"

# Sign with SAN for host.internal
openssl x509 -req -in /tmp/webhook-certs/tls.csr \
  -CA /tmp/webhook-certs/ca.crt -CAkey /tmp/webhook-certs/ca.key \
  -CAcreateserial -out /tmp/webhook-certs/tls.crt -days 365 \
  -extfile <(echo "subjectAltName=DNS:host.internal")
```

**3. Copy certs to controller-runtime's default location:**

controller-runtime on macOS looks in `$(go env TMPDIR)/k8s-webhook-server/serving-certs/`. Find the path from a previous error message or:

```sh
CERT_DIR="$(go env TMPDIR)k8s-webhook-server/serving-certs"
mkdir -p "$CERT_DIR"
cp /tmp/webhook-certs/tls.crt "$CERT_DIR/"
cp /tmp/webhook-certs/tls.key "$CERT_DIR/"
```

**4. Apply the `ValidatingWebhookConfiguration` pointing at your Mac:**

```sh
CA_BUNDLE=$(base64 -i /tmp/webhook-certs/ca.crt | tr -d '\n')

cat > /tmp/webhook-config.yaml << YAML
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: httproute-validator
webhooks:
  - name: vhttproute-v1.kb.io
    admissionReviewVersions: ["v1"]
    clientConfig:
      url: "https://host.internal:9443/gatewayapi-operator-httproute-validator"
      caBundle: "${CA_BUNDLE}"
    rules:
      - apiGroups: ["gateway.networking.k8s.io"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["httproutes"]
    sideEffects: None
    failurePolicy: Fail
YAML

kubectl apply -f /tmp/webhook-config.yaml
```

### Running

**Terminal 1 — keep running:**
```sh
make run
```

Wait for `Serving webhook server {"host": "", "port": 9443}` before proceeding.

**Terminal 2 — test:**

Valid HTTPRoutes (should be accepted):
```sh
kubectl apply -f test-httproutes/http-httproute.yaml
kubectl apply -f test-httproutes/https-httproute.yaml
kubectl apply -f test-httproutes/http-httproute-new-gw.yaml
```

Invalid HTTPRoutes (should be **rejected** by the webhook). These test-httproutes reference a
shared Gateway (`test-gw`) that already exists with specific annotations. Each file deliberately
sets a conflicting annotation so the webhook denies the request.

**Zone mismatch** — HTTPRoute zone differs from the Gateway's `ipam.vitistack.io/zone`:
```sh
kubectl apply -f test-httproutes/invalids/zone/public.yaml    # zone "hnet" vs Gateway's zone
kubectl apply -f test-httproutes/invalids/zone/private.yaml   # zone "hnet-private" vs Gateway's zone
```

**Issuer mismatch** — HTTPRoute cluster-issuer differs from the Gateway's `cert-manager.io/cluster-issuer`:
```sh
kubectl apply -f test-httproutes/invalids/issuer/le.yaml          # issuer "letsencrypt-staging" vs Gateway's issuer
kubectl apply -f test-httproutes/invalids/issuer/internpki.yaml   # issuer "internpki" vs Gateway's issuer
```

**IP family mismatch** — HTTPRoute ip-family differs from the Gateway's `ipam.vitistack.io/ip-family`:
```sh
kubectl apply -f test-httproutes/invalids/ipfamily/dual.yaml   # ip-family "dual" vs Gateway's ip-family
kubectl apply -f test-httproutes/invalids/ipfamily/ipv4.yaml   # ip-family "ipv4" vs Gateway's ip-family
kubectl apply -f test-httproutes/invalids/ipfamily/ipv6.yaml   # ip-family "ipv6" vs Gateway's ip-family
```

Each invalid apply should return an error like:
```
Error from server (HTTPRoute <annotation> conflicts with existing Gateway ...)
```

**Cleanup** — remove test resources and webhook configuration:
```sh
kubectl delete -f test-httproutes/invalids/ipfamily/dual.yaml --ignore-not-found
kubectl delete -f test-httproutes/http-httproute.yaml --ignore-not-found
kubectl delete -f test-httproutes/https-httproute.yaml --ignore-not-found
kubectl delete -f test-httproutes/http-httproute-new-gw.yaml --ignore-not-found
kubectl delete validatingwebhookconfiguration httproute-validator --ignore-not-found
```

### Notes

- The certs in `/tmp/` are lost on reboot. Re-run steps 2–3 after a restart (step 4 only needs re-running if the cluster was recreated).
- The `ValidatingWebhookConfiguration` is deleted when the cluster is destroyed, so step 4 must be repeated for a fresh cluster.
- To disable the webhook entirely when running locally: `ENABLE_WEBHOOKS=false make run`
