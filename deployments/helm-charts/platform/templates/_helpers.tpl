{{/*
Port from a "scheme://host[:port]/path" URL string. Sprig's urlParse has no
`.port` key (only `.host`, which is "host" or "host:port") — this splits it
out, defaulting to the scheme's standard port (443/80) when the URL doesn't
state one explicitly, since every values.yaml default here is written with an
explicit port but an override need not be. Call as
`{{ include "aep.urlPort" .Values.thunder.adminURL }}`.
*/}}
{{- define "aep.urlPort" -}}
{{- $u := urlParse . -}}
{{- $parts := splitList ":" $u.host -}}
{{- if eq (len $parts) 2 -}}
{{- index $parts 1 -}}
{{- else if eq $u.scheme "https" -}}
443
{{- else if eq $u.scheme "http" -}}
80
{{- else -}}
{{- fail (printf "aep.urlPort: %q has no explicit port and scheme %q has no default (expected http or https)" . $u.scheme) -}}
{{- end -}}
{{- end -}}

{{/*
Egress NetworkPolicy rule allowing HTTPS/HTTP to the public internet only.
Excludes RFC1918 ranges so it can't double as a back door to other in-cluster
or node-network destinations — k3d's pod (10.42.0.0/16) and service
(10.43.0.0/16) CIDRs, and most cloud VPC ranges, fall inside 10.0.0.0/8, so
this rule alone never reaches another pod/service; those need their own
podSelector/namespaceSelector rule. Also excludes 169.254.0.0/16 (link-local —
this is where the AWS/GCP/Azure instance metadata endpoint,
169.254.169.254, lives; without this exclusion any pod granted this rule could
fetch node IAM/service-account credentials over "internet" egress) and
100.64.0.0/10 (carrier-grade NAT / shared address space, used by some cloud
providers and CNIs for internal ranges outside 10/8). Used by aep-api
(GitHub), aep-agents (model provider APIs), and smee-client (smee.io) — all
call out to arbitrary third-party HTTPS endpoints we can't pin to a fixed
ipBlock.
*/}}
{{- define "aep.networkPolicy.internetEgress" -}}
- to:
    - ipBlock:
        cidr: 0.0.0.0/0
        except:
          - 10.0.0.0/8
          - 172.16.0.0/12
          - 192.168.0.0/16
          - 169.254.0.0/16
          - 100.64.0.0/10
  ports:
    - protocol: TCP
      port: 443
    - protocol: TCP
      port: 80
{{- end -}}
