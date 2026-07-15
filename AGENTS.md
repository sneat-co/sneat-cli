<!-- specstudio-snippet-version: 0.1.0 -->
# SpecStudio Producer-Shape Integration Instructions

> Pasted into the project's agent-instructions file (`CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, or `.cursor/rules/specstudio.md`) by `specstudio:init`. Do not edit the body — re-paste from `spec/features/third-party-integration/snippet.md` to update. The version comment on line 1 is what `specstudio:init --update` uses to detect drift.

This repository is **SpecStudio-managed** — its specifications live in `spec/` and follow the [SpecScore](https://specscore.md) format. The canonical producers of SpecScore artifacts are the SpecStudio skills `specstudio:ideate` (for Ideas) and `specstudio:specify` (for Features). When a third-party skill operating in this repository produces draft material that resembles a SpecScore Idea or Feature, follow the rules below.

## When these instructions apply

These rules apply to any third-party agent skill that produces structured draft material in this repository — for example: Superpowers `brainstorming`, addyosmani `idea-refine`, or any future skill that helps the user explore a problem, propose a design, or sketch requirements before formal specification.

These rules do **not** apply to:
- General-purpose chat that does not produce a structured draft
- Reviewer-shape skills (which return verdicts, not drafts)
- Capability-shape skills (which emit assets like diagrams or screenshots)
- The canonical SpecStudio skills themselves (`specstudio:ideate`, `specstudio:specify`, `specstudio:init`, etc.) — they are governed by their own SKILL.md, not by this snippet

## What your output should look like

Your output is **a draft**, intended to be canonicalized by `specstudio:ideate` or `specstudio:specify` in a follow-up step. The draft sits in the user's working dialogue or in a temporary scratch location — **not** in `spec/`. Shape your draft so the canonicalization step is mechanical, not interpretive.

### For Idea-shape drafts (problem exploration, direction-setting)

Use these section headings, in this order, with a single top-level heading naming the Idea:

```markdown
# Idea: <Title>

## Problem Statement
<One "How Might We…" sentence stating the problem.>

## Context
<Triggering observation, related work, prior art.>

## Recommended Direction
<2–3 paragraphs: what and why, over the alternatives.>

## Alternatives Considered
<2–3 directions that lost, and why each lost.>

## MVP Scope
<The single job the MVP nails. Timeboxed, not feature-listed.>

## Not Doing (and Why)
- <Thing 1> — <reason>
- <Thing 2> — <reason>

## Key Assumptions to Validate
| Tier | Assumption | How to validate |
|------|------------|-----------------|
| Must-be-true | … | … |
| Should-be-true | … | … |
| Might-be-true | … | … |

## Open Questions
- <Question that needs answering before promotion to a Feature.>
```

`Not Doing (and Why)` MUST be non-empty — at least three explicit non-goals. The full canonical Idea schema lives at <https://specscore.md/idea-specification>.

### For Feature-shape drafts (concrete buildable specifications)

Use these section headings, in this order:

```markdown
# Feature: <Title>

**Status:** Draft

## Summary
<1–3 sentences naming what the Feature does and who consumes it.>

## Problem
<Why this Feature exists. The cost of not having it.>

## Behavior

### <Topic>
#### REQ: <slug>
<Normative MUST/SHOULD/MAY paragraph stating one rule.>

#### REQ: <another-slug>
<Another rule under the same Topic, or move to a new Topic.>

## Acceptance Criteria

### AC: <name>
**Requirements:** <feature-slug>#req:<req-slug>, …

**Given** <precondition>
**When** <action>
**Then** <observable outcome>

## Open Questions
- <Question to resolve at spec time, or after.>
```

Every acceptance criterion MUST use the `Given / When / Then` form. If you cannot phrase an outcome as `Then <observable>`, the AC is too abstract — sharpen it. The full canonical Feature schema lives at <https://specscore.md/feature-specification>.

## What your output MUST NOT do

- **Do not write directly to `spec/ideas/`, `spec/features/`, `spec/plans/`, or any path under `spec/`.** Those paths are reserved for canonical SpecScore artifacts produced by SpecStudio skills. Your output sits in the user's dialogue or in a temporary scratch file outside `spec/`.
- **Do not modify existing canonical artifacts** in `spec/`. If you notice an existing Idea or Feature that should be revised, surface the suggestion to the user; let `specstudio:specify` (or `specstudio:ideate`) own the revision.
- **Do not run `git commit` on the user's behalf.** Drafting is a working-dialogue activity, not a versioned change. Commits happen after canonicalization, owned by the user (or by SpecStudio's auto-stage convention).
- **Do not invent SpecScore body-metadata fields.** Use only what the canonical schemas document. Unknown fields silently drop or fail lint.

## Handoff prompt — required ending

End your output with an explicit handoff prompt naming the canonical follow-up skill. Use one of these two forms verbatim:

For an Idea-shape draft:

> **Next step.** Run `specstudio:ideate` to canonicalize this draft into a SpecScore Idea at `spec/ideas/<slug>.md`. The skill will read this draft, ask sharpening questions if needed, and produce a lint-clean Idea artifact through its three-phase divergent/convergent flow.

For a Feature-shape draft:

> **Next step.** Run `specstudio:specify` to canonicalize this draft into a SpecScore Feature at `spec/features/<slug>/`. The skill will read this draft, dispatch its reviewer subagent gate (including any reviewers registered in `specscore.yaml`), and produce a lint-clean Feature artifact ready for plans.

The handoff prompt MUST be present. Its absence indicates either snippet drift (the pasted snippet is older than the canonical) or that the third-party skill ignored these instructions — both are recoverable by re-pasting the snippet from `spec/features/third-party-integration/snippet.md` and re-running.

## Drafting practices

- **Use canonical SpecScore path forms.** Idea slugs are lowercase-hyphen (`my-new-idea`), not snake_case or CamelCase. Feature slugs are the same. Path references in prose look like `spec/ideas/<slug>.md`, not `docs/ideas/...` or `notes/...`.
- **Use bold-prefix body metadata, not YAML front-matter.** Canonical SpecScore Features use `**Status:** Draft` on its own line, not `---\nstatus: Draft\n---`. Producer drafts that switch to YAML front-matter cost the canonicalization step extra work.
- **Bias toward fewer, sharper requirements.** A Feature with 8 well-specified REQs is healthier than one with 24 vague REQs. Combine redundant REQs; split bundled ones.
- **Bias toward observable acceptance criteria.** "Then the system honors X" is vibes; "Then the API returns 200 with `{status: ok}`" is observable. Sharpen until every Then is checkable.
- **Lead with the user's actual intent.** Do not pad. The first line of every draft section answers "what does this section say?" — no preamble, no apologetics.
