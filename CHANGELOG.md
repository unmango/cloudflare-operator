# Changelog

## [0.1.0](https://github.com/unmango/cloudflare-operator/compare/v0.0.4...v0.1.0) (2026-09-13)


### Features

* housekeeping ([#166](https://github.com/unmango/cloudflare-operator/issues/166)) ([35873a8](https://github.com/unmango/cloudflare-operator/commit/35873a8701e9c3f0e64b54e09a3fca048c0fc351))
* publish the image again on tag ([#179](https://github.com/unmango/cloudflare-operator/issues/179)) ([8542bc9](https://github.com/unmango/cloudflare-operator/commit/8542bc99b5c62f9cac6554c57a573a1d6e99a538))
* ship a Helm chart again ([#168](https://github.com/unmango/cloudflare-operator/issues/168)) ([bfbceb7](https://github.com/unmango/cloudflare-operator/commit/bfbceb7ee37dc5fc483c6921739f66670130d992))


### Bug Fixes

* adopt existing tunnel when create returns 409 ([#204](https://github.com/unmango/cloudflare-operator/issues/204)) ([1abc7f0](https://github.com/unmango/cloudflare-operator/commit/1abc7f0f89728c6b276f8ff47d0fb7cf46ed6152)), closes [#203](https://github.com/unmango/cloudflare-operator/issues/203)
* **controller:** add nil check for reader before resolving tunnel secret ([a554820](https://github.com/unmango/cloudflare-operator/commit/a554820de22ebb2d9860bcbc563d52243aa9743e))
* only push tunnel config to remotely managed tunnels ([#192](https://github.com/unmango/cloudflare-operator/issues/192)) ([1748fde](https://github.com/unmango/cloudflare-operator/commit/1748fde882511acc191a8786ede376c77dedcf89))
* rename binary from cmd to manager to match config/manager expectation ([35873a8](https://github.com/unmango/cloudflare-operator/commit/35873a8701e9c3f0e64b54e09a3fca048c0fc351))
* send spec.tunnelSecret to the Cloudflare API ([#191](https://github.com/unmango/cloudflare-operator/issues/191)) ([a554820](https://github.com/unmango/cloudflare-operator/commit/a554820de22ebb2d9860bcbc563d52243aa9743e))
* surface reconcile failures instead of reporting success ([#193](https://github.com/unmango/cloudflare-operator/issues/193)) ([7b2c34e](https://github.com/unmango/cloudflare-operator/commit/7b2c34e582533ebe47d4998f95e013ab0d17cfbc))
* use the resource name consistently for unnamed tunnels ([#190](https://github.com/unmango/cloudflare-operator/issues/190)) ([f60ef57](https://github.com/unmango/cloudflare-operator/commit/f60ef571b672c91ece3cd7ae02654571e8faff43))
