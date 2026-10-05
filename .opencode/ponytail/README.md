# Ponytail OpenCode V2 adapter

This directory contains a small local adapter for
[`@dietrichgebert/ponytail`](https://github.com/DietrichGebert/ponytail) 4.10.0.

Upstream 4.10.0 still default-exports an OpenCode V1 plugin function, so
OpenCode 2.x rejects it with:

```text
Plugin must export a default definition with an id and an effect or setup function.
```

The adapter keeps the same behavior using the V2 plugin API:

- injects the selected Ponytail ruleset on every agent model request;
- persists `/ponytail lite|full|ultra|off` with plugin storage;
- uses the vendored Ponytail skills and command templates in `.opencode/`.

Remove this adapter and restore the npm package entry in `opencode.json` when
an upstream release officially supports the OpenCode V2 plugin API.

The vendored skills, command templates, and `LICENSE` come from Ponytail
4.10.0 and remain under its MIT license.
