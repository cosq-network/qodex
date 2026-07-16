# Changelog

## [0.2.0](https://github.com/cosq-network/qodex/compare/v0.1.0...v0.2.0) (2026-07-16)


### Features

* add 80+ built-in tools, embedded skills system, and platform coverage ([861181f](https://github.com/cosq-network/qodex/commit/861181fbdfeef798e416b4733a1b2238ea6f72fd))
* add interactive setup wizard and first-run detection ([c82767e](https://github.com/cosq-network/qodex/commit/c82767e2fe720d79a8e58eedf83f0c75b8c10b4f))
* add model tests, index staleness rebuild, approval timeout, and error hardening ([8e5b769](https://github.com/cosq-network/qodex/commit/8e5b76977efa98061747b7751b7c1df6f910f438))
* add reset command, fix setup flow bugs, and harden TUI initialization ([0811243](https://github.com/cosq-network/qodex/commit/08112432d17ad5723375e4f913c35e33389ad274))
* complete Phase 4 skills system — metadata, model routing, context slicing, script policy ([a13cbde](https://github.com/cosq-network/qodex/commit/a13cbde51c063e7c2767b7231e00b4a7cdabae05))
* complete Phase 5 coding tools — project index, test runner, formatter, output artifacts, review mode ([2dfb2f2](https://github.com/cosq-network/qodex/commit/2dfb2f29103f97d5f4262d3e464d59d14f64988d))
* harden runtime setup and release automation ([2ec9d17](https://github.com/cosq-network/qodex/commit/2ec9d17d52514b5c4a91e221d6d11af406d034a2))
* implement LSP diagnostics, definition, and references tools ([26969e1](https://github.com/cosq-network/qodex/commit/26969e1c0db34589903b93138dc7fe02f785a504))
* rework setup flow for self-contained backend management and add full cross-platform Windows support ([2c38dc4](https://github.com/cosq-network/qodex/commit/2c38dc48dc777c01754a0c238999e16e6e06015a))


### Bug Fixes

* harden skills and tooling — UTF-8 safety, dangerous-shell filtering, timeout kills, and validation ([bdca08b](https://github.com/cosq-network/qodex/commit/bdca08bfe0dabd85c294fac9f5ca1384abb8a7e0))
* **model:** improve SSE streaming error handling and goroutine cleanup ([bd8ef02](https://github.com/cosq-network/qodex/commit/bd8ef02d12a908a486aa3c5c2e4ccc139bf4bcf4))
* resolve 6 critical/major issues from code review ([9e2abec](https://github.com/cosq-network/qodex/commit/9e2abec0c8b1e0c6ad3dcfc6bd888612875cf233))
* restore ci builds across platforms ([026de1f](https://github.com/cosq-network/qodex/commit/026de1f0585f7e506e72c2a84efe593ef434952e))
* skip setup prompt when --config is explicitly provided, and fix test to use .qodex/config.toml ([319c775](https://github.com/cosq-network/qodex/commit/319c775c04def3662f10782cafbf3c9b0eca220c))
* tighten release workflow permissions and gating ([51d4d9c](https://github.com/cosq-network/qodex/commit/51d4d9cdf99a395e02ce18a1800aca9955a20e32))
* **tool-calling:** align prompt-mode results, wire approval config, fix schemas, and harden LSP paths ([fd5eb2b](https://github.com/cosq-network/qodex/commit/fd5eb2b662a03b6349a1d5b34cf513f12a4cf510))
* **tui:** cancel run goroutine on quit, fix approval backpressure, and prevent UTF-8 truncation corrupt ([aadaccc](https://github.com/cosq-network/qodex/commit/aadaccc94591293321a7597de9c2b0c57e256907))

## Changelog

All notable changes to this project will be tracked in this file.

The release workflow uses Release Please to maintain this changelog and create semantic version tags from conventional commits merged to `main`.
