# Axiom Rule Studio

Rule Studio is a local-first engineering UI for Axiom rules. Source DSL remains
the source of truth.

## Goal

Help the user:

- read behavior as scenarios;
- inspect rules and functions;
- simulate events;
- see blocked reasons;
- validate safety;
- generate Go stubs;
- export a concise report.

## MVP

- open `.axm` / TRIZ DSL file;
- show state, events, conditions, rules, functions and always laws;
- show rule cards in `when / do / then` form;
- simulate one event with mock outputs;
- show diagnostics with source locations;
- show normalized Axiom v0 view for advanced users;
- export Markdown.

## Not a no-code tool

Studio should not hide the source. It is a visual debugger and editor for an
engineering DSL, not a separate model.

## Production direction

- live runtime connection;
- history/replay viewer;
- why/why-not panel;
- generated test scenarios;
- migration assistant from Axiom v0 to TRIZ DSL;
- mobile dark UI for hardware-adjacent use.
