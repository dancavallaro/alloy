---
canonical: https://grafana.com/docs/alloy/latest/shared/reference/components/spiffe-block/
description: Shared content, spiffe block
headless: true
---

{{< docs/shared lookup="stability/experimental_feature.md" source="alloy" version="<ALLOY_VERSION>" >}}

| Name         | Type           | Description                                                        | Default | Required |
| ------------ | -------------- | ------------------------------------------------------------------ | ------- | -------- |
| `source`     | `capsule`      | The `source` export of a `spiffe.x509_source` component.            |         | yes      |
| `server_ids` | `list(string)` | SPIFFE IDs the server may present. The connection fails otherwise. |         | yes      |

The endpoint presents the source's X.509-SVID as its client certificate, and verifies the server's certificate against the source's trust bundles.
It authorizes the server by SPIFFE ID, so the server certificate doesn't need a DNS SAN matching the URL.

The endpoint `url` must use `https`.
You can't combine the `spiffe` block with the `ca_*`, `cert_*`, `key_*`, or `insecure_skip_verify` arguments of `tls_config`.
The `server_name` and `min_version` arguments of `tls_config` still apply.
