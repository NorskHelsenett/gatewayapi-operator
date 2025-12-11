# GatewayAPI / Publisering i VITI

### Envoy
Vi bruker Envoy-Gateway i VITI cluster. Det vil si at Envoy-Gateway behandler all trafikk inn til clusteret som er publisert med HTTPRoutes.


### GatewayAPI-operator
For å forenkle bruken av GatewayAPI har VITI cluster en GatewayAPI-Operator. Den oppretter og oppdaterer Gateway objekter automatisk slik at du kun trenger å tenker på HTTPRoute definisjonen.



### Vanlig publisering med HTTPRoute
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: <navn>
  namespace: <namespace>
  annotations:
    gatewayapi-operator.vitistack.io/enabled: "true"
spec:
  parentRefs:
    - name: <navn du ønsker på Gateway>
      sectionName: <fqdn>
      namespace: <namespace du ønsker på Gateway>
  hostnames:
    - <fqdn>
  rules:
    - backendRefs:
        - group: ""
          kind: Service
          name: <service navn>
          port: <port>
          weight: 1
      matches:
        - path:
            type: PathPrefix
            value: /
```

### Gateway organisering
Det anbefales å benytte samme gateway på tvers av HTTPRoutene i clusteret. Dette sparer allokering av IP-adresser

### Utsteder av ACME sertifikat
Denne publiseringen vil få "internpki" som cluster-issuer satt da det er default. Ønsker man å benytte en annen cluster-issuer kan man benytte følgende annotering på HTTPRoute:
```yaml
gatewayapi-operator.vitistack.io/cluster-issuer: letsencrypt-prod
gatewayapi-operator.vitistack.io/cluster-issuer: letsencrypt-staging
```
Det er kun en cluster-issuer per gateway, så ønsker man en annen utsteder må man benytte en annen gateway på "parentRefs" i HTTPRoute.

### Infrastruktur, Internett, Helsenett, Helsenett-privat
Gatewayen blir opprettet med en helsenett privat adresse. Dette kan endres på ved å benytte følgende annotering:
```yaml
ipam.vitistack.io/zone: hnet-private # Helsenett adresse på RFC1918 adresse
ipam.vitistack.io/zone: hnet         # Helsenett adresse på offentlig adresseomeråde
ipam.vitistack.io/zone: inet         # Internett adresse
```
Det er ikke mulig å endre infrastruktur på en eksisterende gateway. Da må man opprette en ny gateway ved å definere ett nytt navn under "parentRefs" på HTTPRouten.

### Opprette GatewayAPI ressurser manuelt
Dersom du ikke ønsker å benytte gatewayapi-operatoren for automatisk konfigurasjon av Gateway objekter, og heller ønsker å gjøre det selv, kan følgende annotering fjernes fra HTTPRoute eller settes til "false":
```yaml
gatewayapi-operator.vitistack.io/enabled: "true"
``` 

### Eksempel med letsencrypt-staging
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: <name>
  namespace: <namespace>
  annotations:
    gatewayapi-operator.vitistack.io/enabled: "true"
    gatewayapi-operator.vitistack.io/cluster-issuer: "letsencrypt-staging"
spec:
  parentRefs:
    - name: <navn du ønsker på Gateway>
      sectionName: <fqdn>
      namespace: <namespace du ønsker på Gateway>
  hostnames:
    - <fqdn>
  rules:
    - backendRefs:
        - group: ""
          kind: Service
          name: <service navn>
          port: <port>
          weight: 1
      matches:
        - path:
            type: PathPrefix
            value: /
```

### Kjente gotchas
1. **Flere httproutes med forskjellige cluster-issuer annoteringer som peker på samme gateway er ikke mulig. Lag en ny gateway per cluster-issuer.**
2. **Flere httproutes med forskjellige ipam.vitistack.io/zone annoteringer er ikke mulig. Lag en ny gateway per IPAM zone.**
3. **Redirect og BackendTLSPolicy må konfigureres manuelt. Det er ikke implementert i GatewayAPI-Operatoren enda.**



#### Konfigurering av redirect:
https://gateway.envoyproxy.io/docs/tasks/traffic/http-redirect/
(Gatewayapi-Operatoren oppretter ikke automatisk en redirect listener på Gatewayen, men dette er planlagt i en fremtidig release)

#### TLS mot backend - Konfigurering av BackendTLSPolicy
https://gateway.envoyproxy.io/docs/api/gateway_api/backendtlspolicy/

