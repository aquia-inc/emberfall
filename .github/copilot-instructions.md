# GitHub Copilot repository instructions

## Commits and pull requests

When generating or suggesting a commit message or pull request title:

- Use [Conventional Commits](https://www.conventionalcommits.org/) syntax:
  `<type>(<optional-scope>): <description>`.
- Prefer the types `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, and `revert`.
- Use a lowercase type and scope, an imperative description, and no trailing period.
- Include a scope only when it makes the affected area clearer.
- For commit messages, mark breaking changes with `!` before the colon or a `BREAKING CHANGE: <description>` footer; both are allowed. If using `!`, describe the break in the subject. For pull request titles, use `!`, describe the break in the title, and explain it in the pull request body.
- Base the pull request title on the complete diff. Do not prefix it with a branch name, issue number, emoji, or labels such as `PR`.

Examples:

- `feat(cli): support multipart uploads`
- `docs: clarify the release process`
- `refactor(engine)!: replace the request configuration format`

Keep pull request descriptions concise and include the motivation, material changes, validation performed, and related issues. Follow `CONTRIBUTING.md` for the repository's complete contribution requirements.
