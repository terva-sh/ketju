# Neoforum

A small-community message board in the spirit of phpBB, rebuilt as a single
static Go binary with a Preact client: linear threads with non-tree
"lenses", threads stored as append-only logs with offset indexes, SQLite
(pure Go) for coordination only, OIDC-first identity with an approval mode.

**Status: design only.** Nothing is implemented. Start with
[docs/00-overview.md](docs/00-overview.md).

| Doc | Contents |
|-----|----------|
| [00-overview](docs/00-overview.md) | Vision, principles, glossary |
| [01-research](docs/01-research.md) | Verified ecosystem and UX research, with decisions |
| [02-architecture](docs/02-architecture.md) | Process layout, packages, request flows |
| [03-thread-storage](docs/03-thread-storage.md) | Append-only thread logs and offset index |
| [04-data-model](docs/04-data-model.md) | What the database holds and why |
| [05-identity-and-accounts](docs/05-identity-and-accounts.md) | OIDC, sessions, approval mode, forms |
| [06-thread-views](docs/06-thread-views.md) | Linear thread and its lenses |
| [07-api-and-clients](docs/07-api-and-clients.md) | JSON API, Preact client, non-JS path |
| [08-moderation](docs/08-moderation.md) | Moderation primitives |
| [09-roadmap](docs/09-roadmap.md) | Phased plan |
| [10-open-questions](docs/10-open-questions.md) | Deferred decisions |
| [11-mvp-critical-path](docs/11-mvp-critical-path.md) | The ticket backlog in `.tickets/`, and the shortest route through it to a usable forum |
| [mockups/](docs/mockups/) | Front page and thread page mockups, desktop (1440) and mobile (390), light and dark. Regenerate with `python3 docs/mockups/generate.py`. Published canvas: https://claude.ai/code/artifact/8feff20e-cd58-4031-bc63-9277da945285 |

Design overview page (shareable summary of these docs): https://claude.ai/code/artifact/0838f811-4461-4fe3-81b7-dcded7352015
