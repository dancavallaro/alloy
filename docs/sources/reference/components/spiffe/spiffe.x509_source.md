---
canonical: https://grafana.com/docs/alloy/latest/reference/components/spiffe/spiffe.x509_source/
description: Learn about spiffe.x509_source
labels:
  stage: experimental
  products:
    - oss
title: spiffe.x509_source
---

# `spiffe.x509_source`

{{< docs/shared lookup="stability/experimental.md" source="alloy" version="<ALLOY_VERSION>" >}}

`spiffe.x509_source` fetches an X.509-SVID and trust bundles from the [SPIFFE Workload API][workload-api] and keeps them current as they rotate.
Components that send data over HTTP use the exported `source` in their `spiffe` block to authenticate with the SVID and to authorize the server by SPIFFE ID.
The private key stays in {{< param "PRODUCT_NAME" >}}'s memory and is never written to disk.

[workload-api]: https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/

## Usage

```alloy
spiffe.x509_source "<LABEL>" {
  address = "<WORKLOAD_API_ADDRESS>"
}
```

## Arguments

You can use the following argument with `spiffe.x509_source`:

| Name      | Type     | Description                                                                         | Default                           | Required |
|-----------|----------|-------------------------------------------------------------------------------------|-----------------------------------|----------|
| `address` | `string` | Workload API address, for example `unix:///run/spire/agent.sock`.                   | `SPIFFE_ENDPOINT_SOCKET` env var  | no       |

## Exports

The following field is exported and can be referenced by other components:

| Name     | Type       | Description                                          |
|----------|------------|------------------------------------------------------|
| `source` | `capsule`  | The X.509-SVID and bundle source, for `spiffe` blocks. |

## Component health

`spiffe.x509_source` is unhealthy until it receives its first X.509-SVID, and while its connection to the Workload API is failing.
While the connection is failing it keeps serving the last SVID it received.

## Debug information

`spiffe.x509_source` exposes the current SPIFFE ID, the SVID's expiry, and the trust domains in the bundle set.

## Example

```alloy
spiffe.x509_source "agent" {
  address = "unix:///run/spire/agent.sock"
}

prometheus.remote_write "mimir" {
  endpoint {
    url = "https://mimir.example.com/api/v1/push"
    spiffe {
      source     = spiffe.x509_source.agent.source
      server_ids = ["spiffe://example.org/ns/monitoring/sa/mimir"]
    }
  }
}
```
