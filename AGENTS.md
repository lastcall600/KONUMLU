# AGENTS.md

> Operating norms, scope, and constraints for AI agents contributing to the KONUMLU repository.
> Every agent — regardless of model, tool, or orchestration framework — must read and follow this file before taking action.

---

## Purpose

KONUMLU uses AI agents to accelerate architecture design, documentation, spec writing, code generation, and review. This file defines the rules that keep agents aligned with human intent and prevent silent divergence from frozen decisions.

---

## Mandatory Pre-Action Checks

Before taking any action, an agent MUST:

1. Read `CURRENT-STATE.md` to understand the active phase and what is in/out of scope.
2. Read `DECISIONS.md` to identify frozen decisions that constrain the work.
3. Read `ARCHITECTURE.md` to understand domain boundaries and allowed actions.
4. Read the task description in the relevant file under `docs/TASKS/` if one exists.

If any of the above files do not yet exist, the agent MUST note this fact and proceed only with the action that creates them (if that is the stated task).

---

## Permitted Actions (by phase)

### Phase 0A (Architecture Foundation)

| Action | Permitted |
|---|---|
| Create / edit documentation files (`.md`) | ✅ Yes |
| Create directory structures | ✅ Yes |
| Write ADRs | ✅ Yes |
| Write domain specification files | ✅ Yes |
| Write task lists | ✅ Yes |
| Write architecture diagrams (Mermaid) | ✅ Yes |
| Initialize Go module or any application framework | ❌ No |
| Create `go.mod`, `package.json`, or any dependency manifest | ❌ No |
| Write application source code | ❌ No |
| Create Docker Compose files or Dockerfile | ❌ No |
| Write database migration files | ❌ No |
| Install packages or dependencies | ❌ No |

### V1 and Later

Permitted actions expand as phases advance. Always check `CURRENT-STATE.md` for the active phase scope.

---

## Prohibited Actions (all phases, always)

These actions are forbidden regardless of phase or instruction, unless explicitly overridden by the project lead with a written instruction referencing this file and the specific prohibition:

1. **Re-opening frozen decisions.** Do not propose reversing any decision marked 🔒 FROZEN in `DECISIONS.md`. If you believe a frozen decision is wrong, surface the concern and stop — do not proceed with an alternative approach.

2. **Silent conflict resolution.** If you detect a conflict, ambiguity, or inconsistency in the architecture, requirements, or task description, you MUST surface it explicitly. Do not silently pick a resolution.

3. **Modifying architecture baseline files without instruction.** The following files must not be modified unless explicitly instructed: `ARCHITECTURE.md`, `DECISIONS.md`, `AGENTS.md`. Minor editorial corrections are permitted; substantive changes require explicit instruction.

4. **Bypassing EİDS verification.** Never write code, configuration, or logic that routes around EİDS mandatory verification for operations that require it.

5. **Making AI an authorization or identity authority.** Never design or implement a flow where AI is the sole decision-maker for authorization, identity verification, EİDS verification, or irreversible fraud determination.

6. **Cross-domain data access without contracts.** Never write code that queries or writes another domain's tables directly. All cross-domain interaction must go through the owning domain's exposed contract.

7. **Leaking provider details into business logic.** Never write infrastructure-specific code (AWS SDK calls, MinIO client calls, FCM-specific code) inside domain business logic. Use abstractions.

8. **Writing durable auth tokens to localStorage.** Browser auth must use HttpOnly cookies or equivalent. Never write auth tokens (JWTs, session tokens) to `localStorage` or `sessionStorage`.

9. **Introducing unapproved dependencies.** Do not add new packages, libraries, or services that are not already in the stack (see `DECISIONS.md`) without surfacing the proposal and waiting for approval.

10. **Proceeding past architecture gates.** If an open decision in `DECISIONS.md` gates the work you are about to do, stop and surface the gate. Do not invent a resolution.

---

## Conflict and Ambiguity Protocol

When an agent detects any of the following, it MUST stop and report before proceeding:

- A requirement that contradicts a frozen decision
- Two requirements that contradict each other
- A task that requires crossing a domain boundary without a defined contract
- A task that requires resolving an open decision (O-xxx) that has not been resolved
- Any action that would modify a prohibited file

**Reporting format:**

```
⚠️ CONFLICT / AMBIGUITY DETECTED

Type: [contradiction | open gate | prohibited action | ambiguity]
Location: [file and section where the issue appears]
Description: [what the conflict or ambiguity is]
Blocked action: [what the agent cannot proceed with]
Proposed resolution: [optional — what the agent would do if authorized]
```

---

## Scope Creep Prevention

Agents must implement what is asked. Adding unrequested features, abstractions, or defensive code beyond the stated task is prohibited. Specifically:

- A documentation task does not require creating source code stubs.
- An ADR does not require implementing the decision.
- A domain spec does not require writing migration files.
- A bug fix does not require refactoring surrounding code.

---

## Task Sequencing

Agents must follow the active phase task sequence defined in `docs/TASKS/`. Do not start a new task until the current task is complete and its deliverable is verified. Do not skip tasks without explicit instruction.

---

## File Placement Rules

| File type | Location |
|---|---|
| Root orientation files | `/` |
| Architecture docs and diagrams | `/docs/architecture/` |
| ADRs | `/docs/ADR/` |
| Domain specs | `/docs/domains/` |
| Task lists | `/docs/TASKS/` |
| Spec requirements / design / tasks | `/.kiro/specs/{feature-name}/` |

---

## Versioning This File

This file (`AGENTS.md`) is itself a frozen baseline file. Changes to it require explicit instruction. When modified, add an entry to the change log below.

### Change Log

| Date | Change | Authorized by |
|---|---|---|
| Phase 0A | Initial creation | Architecture Foundation task |

CURSOR USAGE POLICY

- Small task: read only relevant files.
- No whole-repository scan unless explicitly approved.
- No full test suite for micro or documentation tasks.
- Do not repeat completed architecture audits.
- Do not rewrite accepted ADRs without a new ADR.
- Documentation tasks do not require tests.
- Targeted feature changes use targeted tests only.
- Full CI runs only at module or milestone gates.
- If scope expands, stop and report before continuing.