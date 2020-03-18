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

| Key                        | Type    | Required? | Description                              | Default |
| -------------------------- | --------| ---------| ----------------------------------------- | ------- |
| `cluster`                  | string  | required | Logical cluster to register nodes/workloads under. Must match the SPIRE SERVER PSAT node attestor configuration. | |
| `pod_label`                | string  | optional | The pod label used for [Label Based Workload Registration](#label-based-workload-registration) | |
| `pod_annotation`           | string  | optional | The pod annotation used for [Annotation Based Workload Registration](#annotation-based-workload-registration) | |
| `trust_domain`             | string  | required | Trust domain of the SPIRE server | |
