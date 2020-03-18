# SPIRE Kubernetes Registrar

A replacement for the [k8s-workload-registrar](https://github.com/spiffe/spire/blob/master/support/k8s/k8s-workload-registrar/) for automatically issuing SVIDs to Kubernetes workloads.

The main difference is that this uses a SPIFFE ID customer resource definition(CRD) along with two controllers, instead of a Validating Admission Webhook.

## Configuration

### Command Line Configuration

The registrar has the following command line flags:

| Flag         | Description                                                      | Default                       |
| ------------ | -----------------------------------------------------------------| ----------------------------- |
| `-config`    | Path on disk to the [HCL Configuration](#hcl-configuration) file | `spire-k8s-registrar.conf` |

### HCL Configuration

The configuration file is a **required** by the registrar. It contains
[HCL](https://github.com/hashicorp/hcl) encoded configurables.

| Key                        | Type      | Required? | Description                              | Default |
| -------------------------- | ----------| ---------| ----------------------------------------- | ------- |
| `cluster`                  | string    | required | Logical cluster to register nodes/workloads under. Must match the SPIRE SERVER PSAT node attestor configuration. | |
| `server_socket_path`       | string    | required | Path to the Unix domain socket of the SPIRE server if `server_address` is not set. Path to SPIRE agent socket if set. | |
| `trust_domain`             | string    | required | Trust domain of the SPIRE server | |
| `disabled_namespaces`      | []string  | optional | Comma seperated list of namespaces to disable auto SVID generation for | `"kube-system"` |
| `log_level`                | string    | optional | Log level (one of `"panic"`,`"fatal"`,`"error"`,`"warn"`, `"warning"`,`"info"`,`"debug"`) | `"info"` |
| `pod_controller`           | bool      | optional | Enable auto generation of SVIDs for new pods that are created | `true` |
| `pod_label`                | string    | optional | The pod label used for [Label Based Workload Registration](#label-based-workload-registration) | |
| `pod_annotation`           | string    | optional | The pod annotation used for [Annotation Based Workload Registration](#annotation-based-workload-registration) | |
| `server_address`           | string    | optional | The IP/Host:Port of the Spire Server | |

### Example

```
log_level = "debug"
trust_domain = "domain.test"
server_socket_path = "/run/spire/sockets/registration.sock"
cluster = "production"
```

## Node Registration

On startup, the registrar creates a node registration entry that groups all
PSAT attested nodes for the configured cluster. For example, if the configuration
defines the `example-cluster`, the following node registration entry would
be created and used as the parent for all workloads:

```
Entry ID      : 7f18a693-9f94-4e91-af7a-a8a61e9f4bce
SPIFFE ID     : spiffe://example.org/spire-k8s-registrar/example-cluster/node
Parent ID     : spiffe://example.org/spire/server
TTL           : default
Selector      : k8s_psat:cluster:example-cluster
```

