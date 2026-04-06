# jj-domino Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

[Unreleased]: https://github.com/zombiezen/jj-domino/compare/v0.2.0...main

## [0.2.0][] - 2026-04-05

[0.2.0]: https://github.com/zombiezen/jj-domino/releases/tag/v0.2.0

### Added

- Pull request descriptions can be edited with the Jujutsu-configured text editor.
  ([#40](https://github.com/zombiezen/jj-domino/issues/40))
  The editor will be opened automatically when new pull requests are created,
  and can be requested explicitly with `jj-domino submit --editor`.
- Non-linear change graphs are now supported.
  As long as the pull request does not have multiple roots,
  then jj-domino will infer the pull request dependency graph correctly.
  ([#1](https://github.com/zombiezen/jj-domino/issues/1),
  [#4](https://github.com/zombiezen/jj-domino/issues/4),
  and [#64](https://github.com/zombiezen/jj-domino/issues/64))
  As a result, the `jj-domino submit --bookmark` flag can be passed multiple times.
- A GitHub token will be read
  from `$XDG_CONFIG_DIRS/jj-domino/github-token` on Unix-like systems
  and `%AppData%\jj-domino\github-token` on Windows.
  ([#35](https://github.com/zombiezen/jj-domino/issues/35))
  This file can be written with the new `jj-domino auth github-login` command.
  ([#43](https://github.com/zombiezen/jj-domino/issues/43))

### Changed

- jj-domino no longer sets the pull request's base ref
  to anything other than what the user sets
  (either via `jj-domino submit --base` or `trunk()`).
  This prevents issues when reordering pull requests in a chain.
  ([#72](https://github.com/zombiezen/jj-domino/issues/72))
  It does mean that reviewers have to click through a link in the pull request
  instead of directly using the "Files Changed" tab in the GitHub web UI.
- The `GITHUB_TOKEN` environment variable is now prioritized over using the `gh` CLI
  to obtain an auth token.
- `jj-domino doctor` is now called `jj-domino auth status`.
- Logging output no longer includes timestamps
  and includes more helpful information.
  ([#33](https://github.com/zombiezen/jj-domino/issues/33))

### Fixed

- Fixed crash when operating on a repository with one or more deleted bookmarks.
  ([#38](https://github.com/zombiezen/jj-domino/issues/38))
- `trunk()` is now correctly inferred on repositories
  that don't have `revset-aliases."trunk()"` set in their configuration.
  ([#47](https://github.com/zombiezen/jj-domino/issues/47))
- Hyperlinks are now disabled when `TERM=dumb`.
  ([#42](https://github.com/zombiezen/jj-domino/issues/42))
- The pull request footer now includes a brief message for reviewers
  to explain why the pull request is a draft.
  ([#70](https://github.com/zombiezen/jj-domino/issues/70))
- GitHub API requests now include a jj-domino `User-Agent` string.
  ([#73](https://github.com/zombiezen/jj-domino/issues/73))

## [0.1.0][] - 2026-03-09

Version 0.1 was the first release announced to early testers.
This version was tagged retroactively during the [version 0.2 release][0.2.0]
so that the release notes could cover the changes made since then.

[0.1.0]: https://github.com/zombiezen/jj-domino/releases/tag/v0.1.0
