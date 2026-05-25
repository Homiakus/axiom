# Production Roadmap

## Phase 1 — Stable DSL

- finalize syntax;
- implement parser;
- implement formatter;
- implement source ranges;
- add semantic validator;
- document language.

## Phase 2 — Normalizer

- TRIZ DSL to CRFG IR;
- IR to Axiom v0 export;
- compatibility mode;
- migration from old `.axm`.

## Phase 3 — Go codegen

- state/events/functions;
- Actions interface;
- smart merge stubs;
- testkit;
- compile-time safety.

## Phase 4 — Runtime

- execution store;
- history;
- replay;
- worker queue;
- profile enforcement;
- always checker;
- dependency indexes.

## Phase 5 — Studio

- local web app;
- mobile dark UI;
- rule/action cards;
- simulation;
- diagnostics;
- source editor.

## Phase 6 — Production hardening

- storage migrations;
- observability;
- security headers;
- authentication for remote mode;
- model versioning;
- performance tests;
- backup/restore.

## Phase 7 — Domain templates

- HydroPilot;
- payments;
- onboarding;
- approvals;
- IoT control;
- notification flows.
