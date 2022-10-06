# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Add VerticalPodAutoscaler CR.
- `PodSecurityPolicy` are removed on newer k8s versions, so only apply it if object is registered in the k8s API.

## [0.4.0] - 2021-10-11

### Removed

- Remove unused environment variables for workload cluster's Azure subscription from helm chart.

## [0.3.0] - 2021-09-16

## [0.2.0] - 2021-09-16

## [0.1.0] - 2021-09-15

### Added

- First release

[Unreleased]: https://github.com/giantswarm/dns-operator-azure/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/giantswarm/dns-operator-azure/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/giantswarm/dns-operator-azure/releases/tag/v0.1.0
