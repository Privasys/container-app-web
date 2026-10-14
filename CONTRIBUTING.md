# Contributing

Thanks for your interest in improving this template. It is a reference app
for the [Privasys](https://privasys.org) confidential-computing platform,
kept deliberately small so developers can read it end to end and copy it as
the starting point for a web app with a public surface.

## Ground rules

- Keep it minimal. This repo exists to teach one thing well, the three
  surfaces and their consequences; please avoid changes that obscure that.
- Standard library only. A developer should be able to read every line that
  runs.
- Match the existing style and keep pull requests focused.

## Getting started

1. Fork and clone the repo.
2. Follow the [README](README.md) to build and run the app locally.
3. Make your change and confirm it still builds, passes the tests and is
   formatted:

```bash
go vet ./...
go test ./...
gofmt -l .
```

CI runs exactly these, then builds the image and drives it the way the
platform and a webhook sender would.

## Submitting a change

- Open a pull request with a clear description of what changed and why.
- Keep CI green: pushes and pull requests build and test the app. Pushing a
  `v*` tag also publishes the image and cuts a release.

## Style

British English in prose and in user-facing strings. Comments say why, not
what. Error messages are written for the person who has to act on them.

## Security

Please do not report security issues in public pull requests or issues. See
[SECURITY.md](SECURITY.md) for how to disclose vulnerabilities privately.

## Licence and Contributor Licence Agreement

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE).

Before we can merge your first pull request, you need to accept the
[Privasys Contributor Licence Agreement](https://github.com/Privasys/cla). You
keep the copyright in your work; the agreement lets Privasys also license it
under other terms, and Privasys commits to keeping it available under an
open-source licence. A check on your pull request explains how to accept: one
comment, once, for every Privasys repository. If you contribute as part of your
work for an employer, your employer may need to sign the entity agreement
instead.
