package fleetconfig

import (
	"encoding/json"
	"sort"
	"strings"
)

func instanceFiles(instance DerivedInstance, resolved *ResolvedInstance, all []DerivedInstance) []File {
	if resolved == nil {
		return nil
	}
	base := "instances/" + instance.Spec.ID + "/"
	files := []File{
		{Path: base + "namespace.yaml", Data: []byte(namespaceYAML(instance))},
		{Path: base + "pvc.yaml", Data: []byte(pvcYAML(instance))},
		{Path: base + "service.yaml", Data: []byte(serviceYAML(instance))},
		{Path: base + "league-config.yaml", Data: []byte(leagueConfigYAML(instance, resolved.SourceJSON))},
		{Path: base + "secret.example.yaml", Data: []byte(secretYAML(instance))},
		{Path: base + "deployment.yaml", Data: []byte(deploymentYAML(instance))},
		{Path: base + "ingress.yaml", Data: []byte(ingressYAML(instance))},
		{Path: base + "http-redirect.yaml", Data: []byte(httpRedirectYAML(instance))},
		{Path: base + "security-headers.yaml", Data: []byte(securityHeadersYAML(instance))},
	}
	if instance.Spec.CommissionerHQ != nil {
		files = append(files,
			File{Path: base + "hq-provider-service.yaml", Data: []byte(providerServiceYAML(instance))},
			File{Path: base + "network-policy.yaml", Data: []byte(networkPolicyYAML(instance, all))},
		)
	}
	if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
		files = append(files,
			File{Path: base + "hq-registry.yaml", Data: []byte(registryYAML(instance))},
			File{Path: base + "hq-client-secret.example.yaml", Data: []byte(clientSecretYAML(instance, all))},
		)
	}
	return files
}

func yamlQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func namespaceYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Namespace\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app.kubernetes.io/name: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteByte('\n')
	return finishYAML(b.String())
}

func pvcYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.PVC))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\nspec:\n  accessModes:\n    - ReadWriteOnce\n  resources:\n    requests:\n      storage: ")
	b.WriteString(yamlQuote(instance.Spec.PVCStorage))
	b.WriteByte('\n')
	return finishYAML(b.String())
}

func serviceYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Service\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.Service))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\nspec:\n  type: ClusterIP\n  selector:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n  ports:\n    - name: http\n      port: 80\n      targetPort: http\n")
	return finishYAML(b.String())
}

func providerServiceYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Service\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.ProviderService))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n    app.kubernetes.io/component: commissioner-hq-provider\nspec:\n  type: ClusterIP\n  selector:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n  ports:\n    - name: hq-v1\n      port: 8091\n      targetPort: hq-v1\n")
	return finishYAML(b.String())
}

func leagueConfigYAML(instance DerivedInstance, source []byte) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.LeagueConfigMap))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\ndata:\n  league.json: |-\n")
	text := string(source)
	for len(text) > 0 {
		line := text
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			line, text = text[:newline], text[newline+1:]
		} else {
			text = ""
		}
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return finishYAML(b.String())
}

func secretYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.Secret))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\ntype: Opaque\nstringData:\n")
	placeholder := func(key, value string) {
		b.WriteString("  ")
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(yamlQuote(value))
		b.WriteByte('\n')
	}
	placeholder("SESSION_SECRET", "REPLACE_ME_WITH_A_RANDOM_SESSION_SECRET")
	placeholder("GOOGLE_CLIENT_ID", "REPLACE_ME_WITH_OAUTH_CLIENT_ID")
	placeholder("GOOGLE_CLIENT_SECRET", "REPLACE_ME_WITH_OAUTH_CLIENT_SECRET")
	placeholder("GOOGLE_REDIRECT_URL", instance.OAuthCallback)
	placeholder("LEAGUE_ALLOWED_EMAILS", "REPLACE_ME_WITH_MANAGER_EMAILS")
	placeholder("DATA_API_TOKEN", "REPLACE_ME_WITH_A_RANDOM_DATA_API_TOKEN")
	if instance.Spec.CommissionerHQ != nil {
		// Deliberately too short for the provider's 32-byte minimum so an
		// accidentally applied example fails closed instead of installing a
		// public, known HMAC credential.
		placeholder("COMMISSIONER_HQ_PROVIDER_SECRET", "REPLACE_ME")
	}
	placeholder("COMMISSIONER_EMAILS", "REPLACE_ME_WITH_COMMISSIONER_EMAILS")
	placeholder("IDENTITY_ALIASES", "REPLACE_ME_WITH_EXPLICIT_IDENTITY_ALIASES")
	return finishYAML(b.String())
}

func clientSecretYAML(host DerivedInstance, all []DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(host.ClientSecret))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(host.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(host.Spec.ResourcePrefix))
	b.WriteString("\n    app.kubernetes.io/component: commissioner-hq-v1-client\ntype: Opaque\nstringData:\n")
	participants := participantInstances(all)
	for _, participant := range participants {
		id := participant.Spec.ID
		b.WriteString("  ")
		b.WriteString(clientSecretEnv(id))
		b.WriteString(": ")
		// Deliberately too short for the consumer's 32-byte minimum. The
		// distinct key names map the pairs; operators provide unique values.
		b.WriteString(yamlQuote("REPLACE_ME"))
		b.WriteByte('\n')
	}
	b.WriteString("# Each value above is a read-only, scoped HMAC credential and must exactly match the corresponding participant's\n")
	b.WriteString("# COMMISSIONER_HQ_PROVIDER_SECRET. Rotate provider/client pairs deliberately; these are not legacy tokens, Tank01,\n")
	b.WriteString("# sessions, OAuth credentials, or browser identity values.\n")
	return finishYAML(b.String())
}

func deploymentYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.Deployment))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n    app.kubernetes.io/name: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\nspec:\n  replicas: 1\n  strategy:\n    type: Recreate\n  selector:\n    matchLabels:\n      app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n  template:\n    metadata:\n      labels:\n        app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n    spec:\n      imagePullSecrets:\n        - name: regcred\n      securityContext:\n        runAsNonRoot: true\n        runAsUser: 65532\n        runAsGroup: 65532\n        fsGroup: 65532\n      containers:\n        - name: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n          image: ")
	b.WriteString(yamlQuote(instance.Image))
	b.WriteString("\n          imagePullPolicy: IfNotPresent\n          ports:\n            - name: http\n              containerPort: 8080\n              protocol: TCP\n")
	if instance.Spec.CommissionerHQ != nil {
		b.WriteString("            - name: hq-v1\n              containerPort: 8091\n              protocol: TCP\n")
	}
	b.WriteString("          envFrom:\n            - secretRef:\n                name: ")
	b.WriteString(yamlQuote(instance.Secret))
	b.WriteByte('\n')
	if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
		b.WriteString("            - secretRef:\n                name: ")
		b.WriteString(yamlQuote(instance.ClientSecret))
		b.WriteByte('\n')
	}
	b.WriteString("          env:\n")
	quotedEnv := func(name, value string) {
		b.WriteString("            - name: ")
		b.WriteString(yamlQuote(name))
		b.WriteString("\n              value: ")
		b.WriteString(yamlQuote(value))
		b.WriteByte('\n')
	}
	quotedEnv("APP_ENV", "production")
	quotedEnv("DEMO_MODE", "false")
	quotedEnv("PORT", "8080")
	quotedEnv("LEAGUE_FILE", "/etc/gridiron/league.json")
	quotedEnv("TANK01_BASE_URL", instance.Tank01BaseURL)
	quotedEnv("APP_IMAGE_DIGEST", instance.ImageDigest)
	if instance.Spec.CommissionerHQ != nil {
		hq := instance.Spec.CommissionerHQ
		quotedEnv("COMMISSIONER_INSTANCE_ID", instance.Spec.ID)
		quotedEnv("COMMISSIONER_HQ_LEAGUE_ID", hq.LeagueID)
		quotedEnv("COMMISSIONER_HQ_PROVIDER_KEY_ID", hq.KeyID)
		quotedEnv("COMMISSIONER_HQ_PROVIDER_ADDR", ":8091")
	}
	if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
		quotedEnv("COMMISSIONER_HQ_V1_REGISTRY_FILE", instance.HQRegistryFile)
	}
	b.WriteString("          volumeMounts:\n            - name: data\n              mountPath: /app/data\n            - name: league-config\n              mountPath: /etc/gridiron\n              readOnly: true\n")
	if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
		b.WriteString("            - name: hq-registry\n              mountPath: /etc/gridiron-hq/registry.json\n              subPath: registry.json\n              readOnly: true\n")
	}
	b.WriteString("          readinessProbe:\n            httpGet:\n              path: /api/health\n              port: http\n            initialDelaySeconds: 3\n            periodSeconds: 10\n            timeoutSeconds: 5\n          livenessProbe:\n            httpGet:\n              path: /api/live\n              port: http\n            initialDelaySeconds: 15\n            periodSeconds: 30\n            timeoutSeconds: 10\n          resources:\n            requests:\n              cpu: 100m\n              memory: 128Mi\n            limits:\n              cpu: 500m\n              memory: 512Mi\n          securityContext:\n            allowPrivilegeEscalation: false\n            readOnlyRootFilesystem: true\n            capabilities:\n              drop:\n                - ALL\n      volumes:\n        - name: data\n          persistentVolumeClaim:\n            claimName: ")
	b.WriteString(yamlQuote(instance.PVC))
	b.WriteString("\n        - name: league-config\n          configMap:\n            name: ")
	b.WriteString(yamlQuote(instance.LeagueConfigMap))
	b.WriteByte('\n')
	if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
		b.WriteString("        - name: hq-registry\n          configMap:\n            name: ")
		b.WriteString(yamlQuote(instance.RegistryConfigMap))
		b.WriteString("\n            items:\n              - key: registry.json\n                path: registry.json\n")
	}
	return finishYAML(b.String())
}

func networkPolicyYAML(instance DerivedInstance, all []DerivedInstance) string {
	host := hostInstance(all)
	var b strings.Builder
	b.WriteString("apiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.NetworkPolicy))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\nspec:\n  podSelector:\n    matchLabels:\n      app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n  policyTypes:\n    - Ingress\n  ingress:\n    - ports:\n        - port: 8080\n          protocol: TCP\n    - from:\n        - namespaceSelector:\n            matchLabels:\n              kubernetes.io/metadata.name: ")
	b.WriteString(yamlQuote(host.Spec.Namespace))
	b.WriteString("\n          podSelector:\n            matchLabels:\n              app: ")
	b.WriteString(yamlQuote(host.Spec.ResourcePrefix))
	b.WriteString("\n      ports:\n        - port: 8091\n          protocol: TCP\n")
	return finishYAML(b.String())
}

func registryYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.RegistryConfigMap))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n    app.kubernetes.io/component: commissioner-hq-v1-registry\ndata:\n  registry.json: |-\n")
	for _, line := range strings.Split(strings.TrimSuffix(instance.HQRegistryJSON, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return finishYAML(b.String())
}

func participantInstances(all []DerivedInstance) []DerivedInstance {
	out := make([]DerivedInstance, 0)
	for _, instance := range all {
		if instance.Spec.CommissionerHQ != nil {
			out = append(out, instance)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].Spec.CommissionerHQ, out[j].Spec.CommissionerHQ
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return out[i].Spec.ID < out[j].Spec.ID
	})
	return out
}

func hostInstance(all []DerivedInstance) DerivedInstance {
	for _, instance := range all {
		if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
			return instance
		}
	}
	return DerivedInstance{}
}

func ingressYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.HTTPSIngress))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n  annotations:\n    cert-manager.io/cluster-issuer: ")
	b.WriteString(yamlQuote(instance.CertificateIssuer))
	b.WriteString("\n    traefik.ingress.kubernetes.io/router.tls: \"true\"\n    traefik.ingress.kubernetes.io/router.middlewares: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace + "-" + instance.SecurityHeaders + "@kubernetescrd"))
	b.WriteString("\nspec:\n  ingressClassName: ")
	b.WriteString(yamlQuote(instance.IngressClass))
	b.WriteString("\n  rules:\n    - host: ")
	b.WriteString(yamlQuote(instance.PublicHost))
	b.WriteString("\n      http:\n        paths:\n          - path: /\n            pathType: Prefix\n            backend:\n              service:\n                name: ")
	b.WriteString(yamlQuote(instance.Service))
	b.WriteString("\n                port:\n                  name: http\n  tls:\n    - hosts:\n        - ")
	b.WriteString(yamlQuote(instance.PublicHost))
	b.WriteString("\n      secretName: ")
	b.WriteString(yamlQuote(instance.TLSSecret))
	b.WriteByte('\n')
	return finishYAML(b.String())
}

func httpRedirectYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: traefik.io/v1alpha1\nkind: Middleware\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.RedirectMiddleware))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\nspec:\n  redirectScheme:\n    scheme: https\n    permanent: true\n---\napiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.HTTPIngress))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\n  annotations:\n    traefik.ingress.kubernetes.io/router.entrypoints: \"web\"\n    traefik.ingress.kubernetes.io/router.middlewares: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace + "-" + instance.RedirectMiddleware + "@kubernetescrd"))
	b.WriteString("\nspec:\n  ingressClassName: ")
	b.WriteString(yamlQuote(instance.IngressClass))
	b.WriteString("\n  rules:\n    - host: ")
	b.WriteString(yamlQuote(instance.PublicHost))
	b.WriteString("\n      http:\n        paths:\n          - path: /\n            pathType: Prefix\n            backend:\n              service:\n                name: ")
	b.WriteString(yamlQuote(instance.Service))
	b.WriteString("\n                port:\n                  name: http\n")
	return finishYAML(b.String())
}

func securityHeadersYAML(instance DerivedInstance) string {
	var b strings.Builder
	b.WriteString("apiVersion: traefik.io/v1alpha1\nkind: Middleware\nmetadata:\n")
	b.WriteString("  name: ")
	b.WriteString(yamlQuote(instance.SecurityHeaders))
	b.WriteString("\n  namespace: ")
	b.WriteString(yamlQuote(instance.Spec.Namespace))
	b.WriteString("\n  labels:\n    app: ")
	b.WriteString(yamlQuote(instance.Spec.ResourcePrefix))
	b.WriteString("\nspec:\n  headers:\n    stsSeconds: 31536000\n    stsIncludeSubdomains: false\n    stsPreload: false\n    forceSTSHeader: true\n    contentTypeNosniff: true\n    frameDeny: true\n    referrerPolicy: strict-origin-when-cross-origin\n    customResponseHeaders:\n      Permissions-Policy: \"camera=(), microphone=(), geolocation=(), payment=()\"\n")
	return finishYAML(b.String())
}

func finishYAML(value string) string { return strings.TrimRight(value, "\n") + "\n" }

func checklist(fleet Fleet, instances []DerivedInstance) string {
	var b strings.Builder
	b.WriteString("# Fleet operator checklist\n\n")
	b.WriteString("This bundle is deterministic and has no cluster, DNS, OAuth, or secret side effects. Review every file before applying it. The shared statrelay is separately owned; this bundle only wires its configured origin into each app.\n\n")
	b.WriteString("## HQ v1 topology\n\n")
	b.WriteString("A null commissioner_hq value is a nonparticipant and receives no provider port, provider environment, provider Secret field, provider Service, NetworkPolicy, registry mount, or HQ client Secret. Participant metadata is explicit and stable; the participant registry is ordered by its declared nonnegative order.\n\n")
	b.WriteString("When participants exist, exactly one declares host:true. That host alone mounts the read-only /etc/gridiron-hq/registry.json and receives COMMISSIONER_HQ_V1_REGISTRY_FILE. The registry contains topology and secret_env references only; it never contains Secret values, email addresses, member identities, or provider credentials.\n\n")
	b.WriteString("Each provider binds only :8091 behind its private per-instance ClusterIP Service. Public Services keep port 80 targeting the named application port 8080, and public Ingresses route only to that Service port. Each participant NetworkPolicy permits public 8080 ingress and permits 8091 only from the exact host namespace label kubernetes.io/metadata.name plus host pod app label.\n\n")
	b.WriteString("The host client Secret has one distinct read-only/scoped HMAC placeholder per registry connection. Fill each value with exactly the matching participant provider Secret value, then rotate provider/client pairs deliberately. These are not legacy bearer tokens, Tank01 credentials, sessions, OAuth credentials, or browser identity values.\n\n")
	b.WriteString("## Release order\n\n")
	b.WriteString("First install generation: generate/inspect the complete bundle, create each namespace, fill each Secret example, then apply the namespace, PVC, ConfigMaps, Secrets, Services, NetworkPolicies, Deployments, security middleware, HTTPS ingress, and HTTP redirect in that order. DNS, OAuth registration, Secret values, and kubectl apply are operator actions.\n\n")
	b.WriteString("Existing SK-first canary release: apply and verify the stablekernel/SK canary first, then promote the remaining instances in ascending stable ID order; do not treat first-install generation as a substitute for the canary gate.\n\n")
	b.WriteString("## Per-instance checks\n\n")
	for _, instance := range instances {
		b.WriteString("- ")
		b.WriteString(instance.Spec.ID)
		b.WriteString(": set DNS for ")
		b.WriteString(instance.Spec.PublicOrigin)
		b.WriteString(" and register this exact OAuth callback ")
		b.WriteString(instance.OAuthCallback)
		b.WriteString(". Fill and review instances/")
		b.WriteString(instance.Spec.ID)
		b.WriteString("/secret.example.yaml before applying it.")
		if instance.Spec.CommissionerHQ != nil && instance.Spec.CommissionerHQ.Host {
			b.WriteString(" This is the sole HQ browser host; review its hq-registry.yaml and hq-client-secret.example.yaml.")
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n## Storage and hardening\n\n")
	b.WriteString("The PVC intentionally relies on the cluster's local-path default: it is node-local, ReadWriteOnce, and may bind with WaitForFirstConsumer. A pod rescheduled to another node may not see the original data; node loss can make the local volume unavailable. Inspect the StorageClass reclaim policy before deleting a claim because deletion may retain or delete the local volume. Back up state before upgrades or node maintenance.\n\n")
	b.WriteString("Deployments use one replica and Recreate, a read-only league ConfigMap mount, isolated PVCs, nonroot UID/GID/fsGroup 65532, no privilege escalation, read-only root filesystems, and dropped capabilities. The middleware supplies host-only HSTS, nosniff, frame denial, strict-origin referrer policy, and Permissions-Policy. CSP remains application-owned so the app can emit its nonce-bearing policy.\n")
	return finishYAML(b.String())
}
