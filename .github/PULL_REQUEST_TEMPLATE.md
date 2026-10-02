---
name: Pull request
about: Contribute a change to goform
title: ''
labels: ''
assignees: ''
---

## Summary
<!-- A clear and concise description of what this PR does. -->

## Motivation
<!-- Why is this change needed? Which issue does it fix? -->

Fixes #<issue-number>

## Changes
<!-- Bullet list of the key changes. -->

## Changelog
<!-- Add an entry under `## [Unreleased]` in CHANGELOG.md. Use Added / Changed /
     Deprecated / Removed / Fixed / Security. Write what it means for a reader,
     not what the diff does. Skip only if the change has no user-visible effect
     (typo fixes in comments, CI tweaks). -->

- [ ] Updated `CHANGELOG.md`, or not applicable

## Test plan
- [ ] `go test -race ./...` passes
- [ ] `go vet ./...` passes
- [ ] `golangci-lint run` passes (if applicable)
- [ ] New behavior is covered by tests

## Checklist
- [ ] I have run `gofmt`/`gofumpt` on my changes
- [ ] I have added tests for my changes
- [ ] Documentation (README / godoc) updated if behavior changed
- [ ] No breaking API changes without maintainer approval
