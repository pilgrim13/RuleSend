# Contributing

Use English for product copy, documentation, code comments, issues, pull requests, and commit messages. Keep the README focused on the product overview, architecture, installation, and everyday use; link to detailed operational documentation.

Start with a small issue describing the user problem and expected behavior. Keep examples synthetic: do not include real rules, device tokens, Hub databases, local backups, or private paths.

Read the [implementation contract](docs/IMPLEMENTATION_CONTRACT.md) and [threat model](docs/THREAT_MODEL.md). Use Go 1.25+; the UI has no build dependencies. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. For UI changes run `node tests/ui-regression.cjs` (Node 22+) for the deterministic DOM-model checks, then check keyboard navigation, narrow screens, empty/error states, dirty drafts, and Markdown injection in a real browser. The model checks do not replace browser testing.

Changes must preserve these boundaries: drafts do not deploy, remote enrollment does not grant paths, outside-block bytes survive, local edits cause conflicts, and file receipts never imply AI compliance. Add regression tests for bugs affecting content, access, or observed status. Describe tested platforms and remaining limitations in your pull request.

Contributions are provided under the repository's MIT license. Do not copy code with incompatible licensing. No CLA or commercial support commitment is implied.
