# Changelog

## [0.1.3](https://github.com/Dave-lab12/ark/compare/v0.1.2...v0.1.3) (2026-05-29)


### Bug Fixes

* add memory limit support and live resource stats for projects ([039ac45](https://github.com/Dave-lab12/ark/commit/039ac4587f4ad0d6baa88d8926a435c8ce3b526d))
* add memory limit support for projects ([946a67a](https://github.com/Dave-lab12/ark/commit/946a67adb4c7160a5cc2baaba432f38688edd1f8))
* add network group management commands and functionality ([70f0794](https://github.com/Dave-lab12/ark/commit/70f0794430b20b16a8444a1a273995c8a967ed60))
* Add network group management commands and functionality ([95bf00e](https://github.com/Dave-lab12/ark/commit/95bf00eba0e1e0a75a19d8a48b36dd25cb47744b))
* integrate Git broker for editor sessions and enhance devcontainer support ([bbe3ba0](https://github.com/Dave-lab12/ark/commit/bbe3ba07a1be4ec746acee27b336b9e7227e8b55))
* integrate Git broker for editor sessions and enhance devcontainers ([ddf418b](https://github.com/Dave-lab12/ark/commit/ddf418bfd37002f25ff2a72fa6f3358e22b89c74))

## [0.1.2](https://github.com/Dave-lab12/ark/compare/v0.1.1...v0.1.2) (2026-05-25)


### Bug Fixes

* add readonly nvim and tmux mounts ([ab782e2](https://github.com/Dave-lab12/ark/commit/ab782e2df31fdc25f6b3f74cff380d8963007c87))
* Added themes default ([eb3f9de](https://github.com/Dave-lab12/ark/commit/eb3f9de72bcce94520e2b45f51e61117e187a06e))
* generalize read-only host mounts ([e52dab9](https://github.com/Dave-lab12/ark/commit/e52dab969bc4dc919646669f00fe58512ca6c401))

## [0.1.1](https://github.com/Dave-lab12/ark/compare/v0.1.0...v0.1.1) (2026-05-18)


### Bug Fixes

* **build:** update module path to github URL and add installation guide ([bac01f4](https://github.com/Dave-lab12/ark/commit/bac01f49736dce0239ed85b93e2e778560efeebf))
* **cli:** add 'ark edit' command and support for native devcontainer-attached editor sessions ([16ddd56](https://github.com/Dave-lab12/ark/commit/16ddd56488d2596d2a5ca96a08d8bdc9948ef33a))
* **cli:** add `ark edit` command for container-attached editor sessions ([cae6cd8](https://github.com/Dave-lab12/ark/commit/cae6cd8fa01741ab9159fc24e0e6a57d033b1e6d))

## [0.1.0](https://github.com/Dave-lab12/ark/compare/v0.0.4...v0.1.0) (2026-05-17)


### ⚠ BREAKING CHANGES

* **engine:** Secures the broker's TCP fallback path via random 32-byte token-based wire authentication.

### Features

* **cli:** implement container port forwarding support via --port flag ([06019e5](https://github.com/Dave-lab12/ark/commit/06019e59431cca79a3e74017998e62b48c862014))


### Bug Fixes

* Add version and build output to CLI ([1cad957](https://github.com/Dave-lab12/ark/commit/1cad95741b330bce33503b6a6780758ac3db259e))
* **engine:** security hardening and codebase cleanup ([93637af](https://github.com/Dave-lab12/ark/commit/93637af7829855e76410dd4a8837d882d0abb224))
* **image:** init hooks, readiness signal, broker env hardening ([317a8e2](https://github.com/Dave-lab12/ark/commit/317a8e2df56ae41193863b7a4e09a2d09bb996cf))

## [0.0.4](https://github.com/Dave-lab12/ark/compare/v0.0.3...v0.0.4) (2026-05-14)


### Bug Fixes

* embed base image assets and add self-update command ([dea81eb](https://github.com/Dave-lab12/ark/commit/dea81eb37b635b5bc7c8faba7f10a5215de6ebbb))

## [0.0.3](https://github.com/Dave-lab12/ark/compare/v0.0.2...v0.0.3) (2026-05-14)


### Features

* initial ark docker MVP ([8a3ba69](https://github.com/Dave-lab12/ark/commit/8a3ba69be3992fb6d26f7fe941b2ca4fccb38854))


### Bug Fixes

* run goreleaser after release creation ([f400742](https://github.com/Dave-lab12/ark/commit/f400742e434bccacf9ce0f3c194abbe09b27dfb5))
* use plain semver release tags ([be1c3ea](https://github.com/Dave-lab12/ark/commit/be1c3ea5076d85e299dbe6ff38aeff83b57fd44c))

## [0.0.2](https://github.com/Dave-lab12/ark/compare/ark-v0.0.1...ark-v0.0.2) (2026-05-14)


### Bug Fixes

* run goreleaser after release creation ([f400742](https://github.com/Dave-lab12/ark/commit/f400742e434bccacf9ce0f3c194abbe09b27dfb5))

## 0.0.1 (2026-05-14)


### Features

* initial ark docker MVP ([8a3ba69](https://github.com/Dave-lab12/ark/commit/8a3ba69be3992fb6d26f7fe941b2ca4fccb38854))
