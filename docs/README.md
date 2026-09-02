# Documentation index

Every file under `docs/`, with its audience and what it covers. The root
[`README.md`](../README.md) links this same list from its own Documentation
section; the two lists stay in sync.

Audience key: **manager** (plays in the league), **commissioner** (runs the
league), **operator** (deploys and maintains the service), **contributor**
(reads or changes the source).

| Document | Audience | Covers |
| --- | --- | --- |
| [`quickstart.md`](quickstart.md) | operator | Ten-minute Docker Compose deployment walkthrough |
| [`configuration.md`](configuration.md) | operator | Every `league.json` field, boot states, and environment override |
| [`season-operations.md`](season-operations.md) | commissioner | Draft night through week close, live scoring, and degraded-data operations |
| [`launch-checklist.md`](launch-checklist.md) | operator | Kubernetes release, canary, and rollback runbook |
| [`backup-restore.md`](backup-restore.md) | commissioner | Backup archive contents and the offline restore procedure |
| [`data-pipeline.md`](data-pipeline.md) | operator | Signal Wire and open-stats mirror architecture |
| [`sources.md`](sources.md) | commissioner | The accepted source mesh and PrizePicks/market-data policy |
| [`design-spec.md`](design-spec.md) | contributor | Visual-system tokens and the accessibility baseline |
| [`avatar-default-badges.md`](avatar-default-badges.md) | operator | Default team-badge naming convention and fallback chain |
| [`qa-1-acceptance-matrix.md`](qa-1-acceptance-matrix.md) | contributor | The bounded QA-1 server-render acceptance matrix |
| [`px1_manager-handbook.md`](px1_manager-handbook.md) | manager | Five-minute manager orientation and data-state guidance |
| [`px1_commissioner-handbook.md`](px1_commissioner-handbook.md) | commissioner | Commissioner safe-operating loop and recovery links |
| [`px1_operator-help-projection.md`](px1_operator-help-projection.md) | operator | How to verify the public `/help` corpus is safe to publish |
| [`px1_help_corpus.md`](px1_help_corpus.md) | contributor | The `/help` corpus contract: topics, search, and recovery guidance |
| [`px1_glossary.md`](px1_glossary.md) | manager | A projection of the in-app glossary |
| [`px1_concept-transition.md`](px1_concept-transition.md) | manager | A vocabulary map for managers migrating from another platform |
| [`decisions/0001-seat-scoped-big-board.md`](decisions/0001-seat-scoped-big-board.md) | contributor | Seat-scoped Big Board ownership decision record |

## Decisions

`decisions/` holds one dated record per binding product or architecture
decision. A decision stays in place after a later change supersedes part of
it; the record itself explains the follow-up, it does not disappear.

## In-app help

The interactive [`/help`](/help) center and the public [`/guide`](/guide) are
the runtime-owned, always-current sources for manager- and commissioner-facing
guidance. The `px1_*` documents above are read-only projections of that same
corpus for operators, support, and offline reading; the running page always
wins when the two disagree.
