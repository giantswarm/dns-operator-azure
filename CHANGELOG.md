# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Add a helm switch for `PodMonitor`.

## [2.0.0] - 2024-04-24

### Added

- Add wildcard CNAME record to ingress.basedomain
- Add toleration for `node.cluster.x-k8s.io/uninitialized` taint.
- Add node affinity to prefer schedule to `control-plane` nodes.
- Enable creating DNS records in Azure for clusters of non-Azure providers

## [1.3.4] - 2024-01-22

## [1.3.3] - 2024-01-22

### Changed

- Add seccomp annotation to PSP. 
- Add seccompProfile for pod too to fix failing deployments because of PSPs.

## [1.3.2] - 2024-01-22

### Changed

- Fix missing seccompProfile.

## [1.3.1] - 2023-10-12

### Added

- Add `PolicyException` for `kyverno` policies.

## [1.3.0] - 2023-05-16

### Added

- For privateLink based workload clusters, a `privateDNS` zone in the management cluster will get created
- new `metrics` for all the privateDNS related operations

### Changed

- Updated the documentation to visualize the new `privateDNS` behavior.

## [1.2.0] - 2023-04-20

### Added

- Introduced `metrics` about generated DNS zones, records and stats about API calls towards Azure
- Add `application.giantswarm.io/team` label

## [1.1.0] - 2023-03-28

### Added

- Push new releases to `capz-app-collection`
- Create `bastion` and `bastion1` record for the bastion host based `machineDeployment`

### Changed

- Depending on the referenced `AzureClusterIdentity` the internal `dnsClient` uses either `ManagedIdentityCredential` for MSI or `DefaultAzureCredential` for Service Principal
- Decrease TTL for `A-Records` from 60 minutes down to 5 minutes

## [1.0.3] - 2023-03-09

### Changed

- To make private CAPZ clusters work for now, an additional `A-Record` (called `apiserver`) for the API-Server got created.

## [1.0.2] - 2023-02-22

### Fixed

- `dns-operator-azure` run in `hostNetwork`

## [1.0.1] - 2023-02-16

### Fixed

- Push stable releases to `control-plane-catalog`.

## [1.0.0] - 2023-02-16


### Added

- Add support for private IP endpoints from `azurecluster.spec.network.loadbalancer`.
- Add documentation.

### Changed

- Changed to a flat DNS schema which we have for CAPI clusters.
- Add VerticalPodAutoscaler CR.
- `PodSecurityPolicy` are removed on newer k8s versions, so only apply it if object is registered in the k8s API.

### Removed

- Removed `CNAME` creation.

## [0.4.0] - 2021-10-11

### Removed

- Remove unused environment variables for workload cluster's Azure subscription from helm chart.

## [0.3.0] - 2021-09-16

## [0.2.0] - 2021-09-16

## [0.1.0] - 2021-09-15

### Added

- First release

[Unreleased]: https://github.com/giantswarm/dns-operator-azure/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/giantswarm/dns-operator-azure/compare/v1.3.4...v2.0.0
[1.3.4]: https://github.com/giantswarm/dns-operator-azure/compare/v1.3.3...v1.3.4
[1.3.3]: https://github.com/giantswarm/dns-operator-azure/compare/v1.3.2...v1.3.3
[1.3.2]: https://github.com/giantswarm/dns-operator-azure/compare/v1.3.1...v1.3.2
[1.3.1]: https://github.com/giantswarm/dns-operator-azure/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/giantswarm/dns-operator-azure/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/giantswarm/dns-operator-azure/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/giantswarm/dns-operator-azure/compare/v1.0.3...v1.1.0
[1.0.3]: https://github.com/giantswarm/dns-operator-azure/compare/v1.0.2...v1.0.3
[1.0.2]: https://github.com/giantswarm/dns-operator-azure/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/giantswarm/dns-operator-azure/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.4.0...v1.0.0
[0.4.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/giantswarm/dns-operator-azure/releases/tag/v0.1.0
