# Changelog

## [0.3.1](https://github.com/unmango/cloudflare-operator/compare/v0.3.0...v0.3.1) (2026-09-19)


### Bug Fixes

* reject an ingress rule that is not the catch-all from anywhere but the end ([#232](https://github.com/unmango/cloudflare-operator/issues/232)) ([ba8de3b](https://github.com/unmango/cloudflare-operator/commit/ba8de3bc4ec03c94299e58e47e99d861c3c29ac1)), closes [#227](https://github.com/unmango/cloudflare-operator/issues/227)

## [0.3.0](https://github.com/unmango/cloudflare-operator/compare/v0.2.0...v0.3.0) (2026-09-19)


### Features

* **gateway:** add CloudflareGatewayConfig and a GatewayClass controller ([#229](https://github.com/unmango/cloudflare-operator/issues/229)) ([14d58d0](https://github.com/unmango/cloudflare-operator/commit/14d58d0560c890160d0f8b29f90502aba8c61661))

## [0.2.0](https://github.com/unmango/cloudflare-operator/compare/v0.1.1...v0.2.0) (2026-09-19)


### ⚠ BREAKING CHANGES

* the originRequest field diableChunkedEncoding is renamed to disableChunkedEncoding. Stored objects that set the misspelled key lose the value on the next apply, and manifests using it must be updated.

### Bug Fixes

* allow an ingress rule without a hostname ([#224](https://github.com/unmango/cloudflare-operator/issues/224)) ([893ca22](https://github.com/unmango/cloudflare-operator/commit/893ca22b8e1e637bafe627f4c99a4d4d81eb34a1)), closes [#218](https://github.com/unmango/cloudflare-operator/issues/218)
* correct DisableChunkedEncoding json tag spelling ([#226](https://github.com/unmango/cloudflare-operator/issues/226)) ([66fced3](https://github.com/unmango/cloudflare-operator/commit/66fced3018beff99ab3bfc0f25b8bc5c72e7b524)), closes [#207](https://github.com/unmango/cloudflare-operator/issues/207)
* **deps:** update module github.com/onsi/ginkgo/v2 to v2.33.0 ([#220](https://github.com/unmango/cloudflare-operator/issues/220)) ([e62a5fd](https://github.com/unmango/cloudflare-operator/commit/e62a5fd6fe0c9d4f1270780f1742daa632bb885f))
* **deps:** update module github.com/onsi/gomega to v1.43.1 ([#219](https://github.com/unmango/cloudflare-operator/issues/219)) ([d34d42e](https://github.com/unmango/cloudflare-operator/commit/d34d42ef35e378b7224dcbaffd8fd8b223f53bc2))
* reconcile Cloudflared when its referenced tunnel changes ([#225](https://github.com/unmango/cloudflare-operator/issues/225)) ([f5537ec](https://github.com/unmango/cloudflare-operator/commit/f5537ecb85929fd1aa3ede74ba32271e793920f8))

## [0.1.1](https://github.com/unmango/cloudflare-operator/compare/v0.1.0...v0.1.1) (2026-09-14)


### Bug Fixes

* adopt an existing DNS record instead of creating a duplicate ([#212](https://github.com/unmango/cloudflare-operator/issues/212)) ([22a5a98](https://github.com/unmango/cloudflare-operator/commit/22a5a98deff8158d257bd11a1a114b6104885428)), closes [#195](https://github.com/unmango/cloudflare-operator/issues/195)
* adopt an owned cloudflared app when its Kind was never recorded ([#210](https://github.com/unmango/cloudflare-operator/issues/210)) ([c79eaf5](https://github.com/unmango/cloudflare-operator/commit/c79eaf56dd35eb73f90a2fe9b9ae6d250f507a3d)), closes [#194](https://github.com/unmango/cloudflare-operator/issues/194)
* make originRequest.caPool optional ([#208](https://github.com/unmango/cloudflare-operator/issues/208)) ([8063897](https://github.com/unmango/cloudflare-operator/commit/80638976cde7d1e550fc2dacaa27d918ce39571d)), closes [#198](https://github.com/unmango/cloudflare-operator/issues/198)
* mount user volumes into the cloudflared container ([#209](https://github.com/unmango/cloudflare-operator/issues/209)) ([37b6ba3](https://github.com/unmango/cloudflare-operator/commit/37b6ba3e10c60e4ee967cfb7f9dfd48269f32637))
* recreate a DnsRecord whose record was deleted upstream ([#211](https://github.com/unmango/cloudflare-operator/issues/211)) ([4b768ec](https://github.com/unmango/cloudflare-operator/commit/4b768ecb11ddeaaa54479dd989170207a8be5fbe)), closes [#196](https://github.com/unmango/cloudflare-operator/issues/196)

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
