# SPIRE Kubernetes Registrar

A replacement for the [k8s-workload-registrar](https://github.com/spiffe/spire/blob/master/support/k8s/k8s-workload-registrar/) for automatically issuing SVIDs to Kubernetes workloads.

The main difference is that this uses a SPIFFE ID customer resource definition(CRD) along with controllers, instead of a Validating Admission Webhook. See [Differences](#differences) for more details on what's different.

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
| `add_svc_dns_name`         | bool      | optional | Enable adding service names as SAN DNS names to endpoint pods | `true` |
| `disabled_namespaces`      | []string  | optional | Comma seperated list of namespaces to disable auto SVID generation for | `"kube-system"` |
| `log_level`                | string    | optional | Log level (one of `"panic"`,`"fatal"`,`"error"`,`"warn"`, `"warning"`,`"info"`,`"debug"`) | `"info"` |
| `pod_controller`           | bool      | optional | Enable auto generation of SVIDs for new pods that are created | `true` |
| `pod_label`                | string    | optional | The pod label used for [Label Based Workload Registration](#label-based-workload-registration) | |
| `pod_annotation`           | string    | optional | The pod annotation used for [Annotation Based Workload Registration](#annotation-based-workload-registration) | |
| `server_address`           | string    | optional | The IP/Host:Port of the Spire Server | |

### Examples 

### Configuration
```
log_level = "debug"
trust_domain = "example.org"
server_socket_path = "/run/spire/sockets/registration.sock"
cluster = "production"
```

### SPIFFE ID CRD
```
apiVersion: spiffeid.spiffe.io/v1beta1
kind: SpiffeID
metadata:
  name: my-spiffe-id
  namespace: my-namespace
spec:
  dnsNames:
  - my-dns-name
  selector:
    namespace: default
    podName: my-pod-name
  spiffeId: spiffe://example.org/my-spiffe-id
```

The support selectors are:
- podLabel --  Pod label name/value to match for this SPIFFE ID
- podName -- Pod name to match for this SPIFFE ID
- podUID --  Pod UID to match for this SPIFFE ID
- namespace -- Namespace to match for this SPIFFE ID
- serviceAccount -- ServiceAccount to match for this SPIFFE ID
- arbitrary -- Arbitrary selectors

Specifying DNS Names is optional.

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

## Workload Registration

The registrar has a POD controller that handles pod CREATE and DELETE events to create
and delete registration entries for workloads running on those pods. The
workload registration entries are configured to run on any node in the
cluster.

There are three workload registration modes:
1. [Service Account Based](#service-account-based-workload-registration) -- Don't specify either `pod_label` or `pod_annotation`
2. [Label Based](#label-based-workload-registration) -- Specify only `pod_label`
3. [Annotation Based](#annotation-based-workload-registration) -- Specify only `pod_annotation`.

### Service Account Based Workload Registration

Service account derived workload registration maps the service account into a
SPIFFE ID of the form
`spiffe://<TRUSTDOMAIN>/ns/<NAMESPACE>/sa/<SERVICEACCOUNT>`. For example, if a
pod came in with the service account `blog` in the `production` namespace, the
following registration entry would be created:

```
Entry ID      : 200d8b19-8334-443d-9494-f65d0ad64eb5
SPIFFE ID     : spiffe://example.org/ns/production/sa/blog
Parent ID     : spiffe://example.org/k8s-workload-registrar/example-cluster/node
TTL           : default
Selector      : k8s:ns:production
Selector      : k8s:pod-name:example-workload-98b6b79fd-jnv5m
```

### Label Based Workload Registration

Label based workload registration maps a pod label value into a SPIFFE ID of
the form `spiffe://<TRUSTDOMAIN>/<LABELVALUE>`. For example if the registrar
was configured with the `spire-workload` label and a pod came in with
`spire-workload=example-workload`, the following registration entry would be
created:

```
Entry ID      : 200d8b19-8334-443d-9494-f65d0ad64eb5
SPIFFE ID     : spiffe://example.org/example-workload
Parent ID     : spiffe://example.org/k8s-workload-registrar/example-cluster/node
TTL           : default
Selector      : k8s:ns:production
Selector      : k8s:pod-name:example-workload-98b6b79fd-jnv5m
```

Pods that don't contain the pod label are ignored.

### Annotation Based Workload Registration

Annotation based workload registration maps a pod annotation value into a SPIFFE ID of
the form `spiffe://<TRUSTDOMAIN>/<ANNOTATIONVALUE>`. By using this mode,
it is possible to freely set the SPIFFE ID path. For example if the registrar
was configured with the `spiffe.io/spiffe-id` annotation and a pod came in with
`spiffe.io/spiffe-id: production/example-workload`, the following registration entry would be
created:

```
Entry ID      : 200d8b19-8334-443d-9494-f65d0ad64eb5
SPIFFE ID     : spiffe://example.org/production/example-workload
Parent ID     : spiffe://example.org/k8s-workload-registrar/example-cluster/node
TTL           : default
Selector      : k8s:ns:production
Selector      : k8s:pod-name:example-workload-98b6b79fd-jnv5m
```

Pods that don't contain the pod annotation are ignored.

## Deployment

The registrar can be deployed two ways:

1. As a container in the SPIRE server pod, since it talks to SPIRE server via a Unix domain socket.
It will need access to a shared volume containing the Spire Server registration socket file. 
2. As a container outside the SPIRE server pod. In this scenario you will need to provide the IP or
hostname of the SPIRE Server along with the port (`server_address`). Additionally you will need to create
an admin entry on the SPIRE Server for the registrar to use to request new SVIDs.

## Differences

Differences with the [k8s-workload-registrar](https://github.com/spiffe/spire/blob/master/support/k8s/k8s-workload-registrar/): 

- A namespace scoped SpiffeID CRD is defined. A controller watches for create, update, delete, etc. events and creates entries on the SPIRE Server accordingly.
- An option pod controller (`pod_controller`) watches for POD events and creates/deletes SpiffeID CRDs accordingly. The pod controller sets the pod as the controller owner of the SPIFFE ID CRD so it is automatically garbage collected if the POD is deleted. The pod controller add the pod name as the first DNS name, which makes it also populate the CN field of the SVID.
- An optional endpoint controller (`add_svc_dns_name`) watches for endpoint events and adds the Service Name as a SAN DNS name to the SVID for all pods that are endpoints of the service. A pod can be an endpoint of multiple services and as a result can have multiple Service Names added as SAN DNS names. If a service is removed, the Service Name is removed from the SVID of all endpoint Pods. The format of the DNS name is `<service_name>.<namespace>.svc`
- A new option to disable namespaces from auto-injection (`disabled_namespaces`). By default `kube-system` is disabled for auto-injection.
