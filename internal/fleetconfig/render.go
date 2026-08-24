package fleetconfig

import (
	"encoding/json"
	"strings"
)

func instanceFiles(instance DerivedInstance, resolved *ResolvedInstance) []File {
	if resolved == nil {
		return nil
	}
	base := "instances/" + instance.Spec.ID + "/"
	return []File{
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
	b.WriteByte('\n')
	b.WriteString("  labels:\n    app.kubernetes.io/name: ")
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
	for _, line := range strings.Split(strings.TrimRight(string(source), "\n"), "\n") {
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
	if instance.Spec.HQParticipant {
		placeholder("COMMISSIONER_HQ_TOKEN", "REPLACE_ME_WITH_A_RANDOM_HQ_TOKEN")
	}
	placeholder("COMMISSIONER_EMAILS", "REPLACE_ME_WITH_COMMISSIONER_EMAILS")
	placeholder("IDENTITY_ALIASES", "REPLACE_ME_WITH_EXPLICIT_IDENTITY_ALIASES")
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
	b.WriteString(yamlQuote(instanceImage(instance)))
	b.WriteString("\n          imagePullPolicy: IfNotPresent\n          ports:\n            - name: http\n              containerPort: 8080\n              protocol: TCP\n          envFrom:\n            - secretRef:\n                name: ")
	b.WriteString(yamlQuote(instance.Secret))
	b.WriteString("\n          env:\n")
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
	if instance.Spec.HQParticipant {
		quotedEnv("COMMISSIONER_INSTANCE_ID", instance.Spec.ID)
		quotedEnv("COMMISSIONER_HQ_PEERS", instance.HQPeersValue)
	}
	b.WriteString("          volumeMounts:\n            - name: data\n              mountPath: /app/data\n            - name: league-config\n              mountPath: /etc/gridiron\n              readOnly: true\n          readinessProbe:\n            httpGet:\n              path: /api/health\n              port: http\n          livenessProbe:\n            httpGet:\n              path: /api/live\n              port: http\n          resources:\n            requests:\n              cpu: 100m\n              memory: 128Mi\n            limits:\n              cpu: 500m\n              memory: 512Mi\n          securityContext:\n            allowPrivilegeEscalation: false\n            readOnlyRootFilesystem: true\n            capabilities:\n              drop:\n                - ALL\n      volumes:\n        - name: data\n          persistentVolumeClaim:\n            claimName: ")
	b.WriteString(yamlQuote(instance.PVC))
	b.WriteString("\n        - name: league-config\n          configMap:\n            name: ")
	b.WriteString(yamlQuote(instance.LeagueConfigMap))
	b.WriteByte('\n')
	return finishYAML(b.String())
}

func instanceImage(instance DerivedInstance) string { return instance.Image }

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
	b.WriteString("## Release order\n\n")
	b.WriteString("First install generation: generate/inspect the complete bundle, create each namespace, fill each Secret example, then apply the namespace, PVC, ConfigMap, Secret, Service, Deployment, security middleware, HTTPS ingress, and HTTP redirect in that order. DNS, OAuth registration, Secret values, and kubectl apply are operator actions.\n\n")
	b.WriteString("Existing SK-first canary release: apply and verify the stablekernel/SK canary first, then promote the remaining instances in ascending stable ID order; do not treat first-install generation as a substitute for the canary gate.\n\n")
	b.WriteString("## Per-instance checks\n\n")
	for _, instance := range instances {
		b.WriteString("- ")
		b.WriteString(instance.Spec.ID)
		b.WriteString(": set DNS for ")
		b.WriteString(instance.Spec.PublicOrigin)
		b.WriteString(" and register this exact OAuth callback ")
		b.WriteString(instance.OAuthCallback)
		b.WriteString(". Fill and review ")
		b.WriteString("instances/")
		b.WriteString(instance.Spec.ID)
		b.WriteString("/secret.example.yaml before applying it.")
		b.WriteByte('\n')
	}
	b.WriteString("\n## Storage and hardening\n\n")
	b.WriteString("The PVC intentionally relies on the cluster's local-path default: it is node-local, ReadWriteOnce, and may bind with WaitForFirstConsumer. A pod rescheduled to another node may not see the original data; node loss can make the local volume unavailable. Inspect the StorageClass reclaim policy before deleting a claim because deletion may retain or delete the local volume. Back up state before upgrades or node maintenance.\n\n")
	b.WriteString("Deployments use one replica and Recreate, a read-only league ConfigMap mount, isolated PVCs, nonroot UID/GID/fsGroup 65532, no privilege escalation, read-only root filesystems, and dropped capabilities. The middleware supplies host-only HSTS, nosniff, frame denial, strict-origin referrer policy, and Permissions-Policy. CSP remains application-owned so the app can emit its nonce-bearing policy.\n\n")
	b.WriteString("HQ participants receive only the other participants, sorted by stable ID, in COMMISSIONER_HQ_PEERS; nonparticipants receive no HQ identity or peer topology. The generated service origin is http://<service>.<namespace>.svc.cluster.local and the shared relay value is copied exactly into TANK01_BASE_URL.\n")
	return finishYAML(b.String())
}
