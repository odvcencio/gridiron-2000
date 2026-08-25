# Operator help projection

The public help corpus is safe to publish because it carries product
contracts, not league secrets. The operator view verifies the projection and
keeps runtime-owned values out of static docs.

## Receipt

- Corpus version: `0.1`.
- Source receipt: `VerifiedSourceSHA` is rendered by `/help` and each topic.
- Route inventory and schema: [`docs/px1_help_corpus.md`](px1_help_corpus.md).

The source receipt identifies the reviewed repository snapshot; it is not a
deployment release claim. `/api/health`, the current config, and the running
service own deployment/runtime truth.

## Projection checks

1. Run the corpus and route tests for `app/help` and
   `app/help/_topic_id`.
2. Search with the deterministic fixture terms `big board`, `draft queue`,
   `FAAB`, `waiver budget`, `trade veto`, `where did my pick go`, `Pick'em`,
   `owner`, and `live data`; verify stable ordering and stable topic IDs.
3. Render `/help?q=...` and a representative topic at a narrow viewport.
   Confirm search, index links, role checklists, migration table, glossary,
   and recovery cards remain readable without horizontal page overflow.
4. Scan tracked public files for email addresses, credentials, invitation
   values, access tokens, and raw provider payloads. Test fixtures may use
   synthetic data only and must not be copied into the corpus.

## Authority boundary

Help may explain a state and link to an action; it must not decide the current
date, deadline, score, phase, lock, capability, source freshness, or
permission. Those values are projected from the running service on request.
When a topic and a runtime page disagree, the runtime page wins and the
operator records the discrepancy for the next corpus review.
