# Changelog

All notable changes to this provider are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.7.0] - 2026-07-13

### Added

- `nerdctl_registry_image` resource: pushes a local image to its registry.
  Refresh checks the remote manifest with `nerdctl manifest inspect`
  (requires nerdctl >= 2.3) without pulling, so an image deleted from the
  registry is re-pushed on the next apply. Destroy removes only the
  Terraform state — nerdctl cannot delete from a registry.
- `nerdctl_registry_image` data source: reads a remote tag's manifest
  digest without pulling. Key a `nerdctl_image`'s `triggers` on
  `sha256_digest` to re-pull when the remote tag moves.
- `nerdctl_container`: `memory`, `cpus`, and `restart` now update in place
  via `nerdctl update`; removing a limit still replaces, since update
  cannot unset one.
- `nerdctl_container`: `wait` / `wait_timeout` block create until the
  healthcheck reports healthy. The provider drives the check itself with
  `nerdctl container healthcheck`, so it works in environments without the
  systemd timers nerdctl's own scheduler needs.
- `nerdctl_container` runtime and security attributes: `devices`,
  `read_only`, `stop_timeout`, `ulimits`, `shm_size`, `platform`,
  `security_opt`, `group_add`, `pid`, `ipc`, `init`, and `stop_signal`.
- `nerdctl_container` network identity attributes: `ip`, `ip6`,
  `mac_address`, and `extra_hosts` (accepting `host-gateway`), all with
  full drift detection and import fill.
- `nerdctl_container_start`, `nerdctl_container_stop`, and
  `nerdctl_container_restart` actions; stop and restart take `timeout`
  and `signal`.
- `nerdctl_volume`: `labels`, with drift detection and import fill; the
  data source exports them too.
- `nerdctl_network`: `ip_range`, `ipv6_subnet`, and driver `options`; the
  IPAM refresh is now address-family-aware, so IPv4 and IPv6 entries
  round-trip independently.

## [0.5.0] - 2026-07-13

### Added

- `nerdctl_image` build support: a `build` block (`context`, `dockerfile`,
  `target`, `build_args`, `labels`, `no_cache`) runs `nerdctl build`
  instead of pull. Requires a running buildkitd on the host.
- `nerdctl_image`: `platform` for pull and build, `triggers` to force a
  re-pull/rebuild, the delete-time flags `keep_locally` and
  `force_remove`, and the computed `repo_digest` (also exported by the
  data source).

## [0.4.2] - 2026-07-12

### Fixed

- Release workflow: pass the GPG key fingerprint to goreleaser so the
  `SHA256SUMS` signature is produced with the intended signing key.

## [0.4.1] - 2026-07-10

### Added

- `nerdctl_compose` resource: manages a compose project as a unit via
  `nerdctl compose up -d` / `compose down`, with `config_paths`,
  `project_name`, `project_directory`, `env_files`, `profiles`, `build`,
  `remove_orphans`, and `remove_volumes`.

### Fixed

- Container teardown race: containers are stopped before removal, so
  containerd's restart monitor cannot relaunch one mid-delete and orphan
  a task that blocks removing the image.

## [0.3.0] - 2026-07-10

### Added

- `nerdctl_container` data source: reads an existing container by name,
  exporting `id`, `image`, `status`, `running`, `pid`, `restart`,
  `memory`, `cpus`, `privileged`, `networks`, `labels`, `env`, and
  `ports`.

## [0.2.0] - 2026-07-09

### Added

- `nerdctl_container` security, logging, and health-check attributes:
  `privileged`, `cap_add`, `cap_drop`, `sysctls`, `tmpfs`, `log_driver`,
  `log_opts`, `healthcheck`, and `no_healthcheck` (healthcheck requires
  nerdctl >= 2.1.5).
- MPL-2.0 LICENSE, golangci-lint configuration with a CI lint job, and
  dependabot.

### Fixed

- Container delete now waits for the container to actually disappear
  before returning, fixing a race on rootless hosts where removing an
  image, volume, or network in the same plan could fail.

## [0.1.0] - 2026-07-08

Initial release.

### Added

- Resources: `nerdctl_container`, `nerdctl_image`, `nerdctl_volume`, and
  `nerdctl_network`, with drift detection, import by name, and plan-time
  validation.
- Data sources: `nerdctl_image`, `nerdctl_volume`, and `nerdctl_network`.
- Actions (Terraform 1.14+): `nerdctl_exec`, `nerdctl_container_export`,
  `nerdctl_image_import`, `nerdctl_image_load`, `nerdctl_image_save`, and
  `nerdctl_system_prune`.
- Local and ssh hosts, rootless or rootful (`sudo`), containerd
  `namespace` selection, and `nerdctl_path` override.
- Registry-style docs generated with tfplugindocs, a rootless-host setup
  guide, an acceptance suite running against real containerd (rootless
  and rootful CI jobs), and goreleaser release plumbing.

[0.7.0]: https://github.com/kmaris/terraform-provider-nerdctl/compare/v0.5.0...v0.7.0
[0.5.0]: https://github.com/kmaris/terraform-provider-nerdctl/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/kmaris/terraform-provider-nerdctl/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/kmaris/terraform-provider-nerdctl/compare/v0.3.0...v0.4.1
[0.3.0]: https://github.com/kmaris/terraform-provider-nerdctl/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/kmaris/terraform-provider-nerdctl/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/kmaris/terraform-provider-nerdctl/releases/tag/v0.1.0
