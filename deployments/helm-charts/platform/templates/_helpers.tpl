{{/*
Port from a "scheme://host:port/path" URL string. Sprig's urlParse has no
`.port` key (only `.host`, which is "host:port") — this splits it out. Call
as `{{ include "aep.urlPort" .Values.thunder.adminURL }}`.
*/}}
{{- define "aep.urlPort" -}}
{{- index (splitList ":" (urlParse .).host) 1 -}}
{{- end -}}

{{/*
Egress NetworkPolicy rule allowing HTTPS/HTTP to the public internet only.
Excludes RFC1918 ranges so it can't double as a back door to other in-cluster
or node-network destinations — k3d's pod (10.42.0.0/16) and service
(10.43.0.0/16) CIDRs, and most cloud VPC ranges, fall inside 10.0.0.0/8, so
this rule alone never reaches another pod/service; those need their own
podSelector/namespaceSelector rule. Used by aep-api (GitHub), aep-agents
(model provider APIs), and smee-client (smee.io) — all call out to arbitrary
third-party HTTPS endpoints we can't pin to a fixed ipBlock.
*/}}
{{- define "aep.networkPolicy.internetEgress" -}}
- to:
    - ipBlock:
        cidr: 0.0.0.0/0
        except:
          - 10.0.0.0/8
          - 172.16.0.0/12
          - 192.168.0.0/16
  ports:
    - protocol: TCP
      port: 443
    - protocol: TCP
      port: 80
{{- end -}}
