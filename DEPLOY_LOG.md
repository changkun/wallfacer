# Deploy Log

`make deploy` appends one row per wallfacerd prod rollout. Commit after each
ship so the audit trail lives in git. Binary + desktop releases live on the
GitHub releases page (driven by `release-binary.yml` and `release-desktop.yml`
on tag push).

| timestamp | version | rollout-output sha |
| --- | --- | --- |
